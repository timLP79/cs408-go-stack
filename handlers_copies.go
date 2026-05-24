// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// HandleBookCopies renders the per-book Manage Copies page: a table of
// every copy of one title with status, last-loan date, and an action
// dropdown per row (Mark Lost / Damaged / Withdrawn / Available, plus
// Delete when there is no loan history).
func HandleBookCopies(c *gin.Context) {
	dm := getDB(c)

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Status(http.StatusNotFound)
		renderTemplate(c, "error", gin.H{
			"Title":   "Not Found",
			"Status":  404,
			"Message": "Page not found",
		})
		return
	}

	book, err := dm.GetBookByID(id)
	if err == sql.ErrNoRows {
		c.Status(http.StatusNotFound)
		renderTemplate(c, "error", gin.H{
			"Title":   "Not Found",
			"Status":  404,
			"Message": "Book not found",
		})
		return
	}
	if err != nil {
		log.Printf("HandleBookCopies: GetBookByID(%d): %v", id, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	copies, err := dm.GetCopiesByBookIDWithLoanInfo(id)
	if err != nil {
		log.Printf("HandleBookCopies: GetCopiesByBookIDWithLoanInfo(%d): %v", id, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	renderTemplate(c, "book_copies", gin.H{
		"Title":         fmt.Sprintf("Copies of %s", book.Title),
		"Book":          book,
		"Copies":        copies,
		"Success":       readAndClearFlash(c, flashKindSuccess),
		"SuccessDetail": readAndClearFlashDetail(c),
		"Error":         readAndClearFlash(c, flashKindError),
	})
}

// HandleInventory renders the top-level inventory listing across all
// books, with optional status and needs_relabel filters from the
// query string. Both staff and admin can view + act on this page.
func HandleInventory(c *gin.Context) {
	dm := getDB(c)

	statusFilter := strings.TrimSpace(c.Query("status"))
	needsRelabelOnly := c.Query("needs_relabel") == "1"

	copies, err := dm.GetAllCopiesWithFilters(statusFilter, needsRelabelOnly)
	if err != nil {
		log.Printf("HandleInventory: GetAllCopiesWithFilters: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	renderTemplate(c, "inventory", gin.H{
		"Title":            "Inventory",
		"Copies":           copies,
		"StatusFilter":     statusFilter,
		"NeedsRelabelOnly": needsRelabelOnly,
		"Success":          readAndClearFlash(c, flashKindSuccess),
		"SuccessDetail":    readAndClearFlashDetail(c),
		"Error":            readAndClearFlash(c, flashKindError),
	})
}

// HandleCopyStatus updates a single copy's status from a POST form
// field ("status"). Redirects to the referrer-aware destination (the
// per-book page when the copy's book is known, else /inventory) with
// a flash describing the outcome.
func HandleCopyStatus(c *gin.Context) {
	dm := getDB(c)

	copyID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Status(http.StatusNotFound)
		renderTemplate(c, "error", gin.H{
			"Title":   "Not Found",
			"Status":  404,
			"Message": "Page not found",
		})
		return
	}

	cp, err := dm.GetCopyByID(copyID)
	if errors.Is(err, ErrCopyNotFound) {
		c.Status(http.StatusNotFound)
		renderTemplate(c, "error", gin.H{
			"Title":   "Not Found",
			"Status":  404,
			"Message": "Copy not found",
		})
		return
	}
	if err != nil {
		log.Printf("HandleCopyStatus: GetCopyByID(%d): %v", copyID, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	dest := copyMutationRedirect(c, cp.BookID)
	status := strings.TrimSpace(c.PostForm("status"))

	switch err := dm.UpdateCopyStatus(copyID, status); {
	case err == nil:
		setFlash(c, flashKindSuccess, "copy_status_updated")
		setFlashDetail(c, cp.Barcode+" -> "+status)
	case errors.Is(err, ErrCopyStatusInvalid):
		setFlash(c, flashKindError, "copy_status_invalid")
	case errors.Is(err, ErrCopyNotFound):
		setFlash(c, flashKindError, "copy_not_found")
	default:
		log.Printf("HandleCopyStatus: UpdateCopyStatus(%d, %q): %v", copyID, status, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	c.Redirect(http.StatusFound, dest)
}

// HandleCopyDelete removes a single copy after the loan-history guard.
// Same redirect strategy as HandleCopyStatus.
func HandleCopyDelete(c *gin.Context) {
	dm := getDB(c)

	copyID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Status(http.StatusNotFound)
		renderTemplate(c, "error", gin.H{
			"Title":   "Not Found",
			"Status":  404,
			"Message": "Page not found",
		})
		return
	}

	cp, err := dm.GetCopyByID(copyID)
	if errors.Is(err, ErrCopyNotFound) {
		c.Status(http.StatusNotFound)
		renderTemplate(c, "error", gin.H{
			"Title":   "Not Found",
			"Status":  404,
			"Message": "Copy not found",
		})
		return
	}
	if err != nil {
		log.Printf("HandleCopyDelete: GetCopyByID(%d): %v", copyID, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	dest := copyMutationRedirect(c, cp.BookID)

	switch err := dm.DeleteCopy(copyID); {
	case err == nil:
		setFlash(c, flashKindSuccess, "copy_deleted")
		setFlashDetail(c, cp.Barcode)
	case errors.Is(err, ErrCopyHasLoans):
		setFlash(c, flashKindError, "copy_has_loans")
	case errors.Is(err, ErrCopyNotFound):
		setFlash(c, flashKindError, "copy_not_found")
	default:
		log.Printf("HandleCopyDelete: DeleteCopy(%d): %v", copyID, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	c.Redirect(http.StatusFound, dest)
}

// copyMutationRedirect chooses the post-action destination. If the
// caller submitted from the per-book page (Referer ends with that
// book's /copies path), redirect back there; otherwise redirect to
// the top-level /inventory page. Honoring the referer keeps the
// admin's filter state on /inventory across status changes without
// passing redirect tokens around.
func copyMutationRedirect(c *gin.Context, bookID int) string {
	bookPath := fmt.Sprintf("/books/%d/copies", bookID)
	referer := c.Request.Referer()
	if strings.HasSuffix(referer, bookPath) ||
		strings.HasSuffix(referer, fmt.Sprintf("/books/%d", bookID)) {
		return bookPath
	}
	return "/inventory"
}
