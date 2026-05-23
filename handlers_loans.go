// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func HandleCheckout(c *gin.Context) {
	bookID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Status(http.StatusNotFound)
		renderTemplate(c, "error", gin.H{
			"Title":   "Not Found",
			"Status":  404,
			"Message": "Page not found",
		})
		return
	}

	dm := getDB(c)
	book, err := dm.GetBookByID(bookID)
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
		log.Printf("HandleCheckout: GetBookByID(%d): %v", bookID, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	bookPath := fmt.Sprintf("/books/%d", bookID)

	patronID, err := strconv.Atoi(c.PostForm("patron_id"))
	if err != nil || patronID <= 0 {
		setFlash(c, flashKindError, "loan_patron_required")
		c.Redirect(http.StatusFound, bookPath)
		return
	}

	barcode := strings.TrimSpace(c.PostForm("barcode"))
	if barcode == "" {
		setFlash(c, flashKindError, "loan_barcode_required")
		c.Redirect(http.StatusFound, bookPath)
		return
	}
	copy, err := dm.GetCopyByBarcode(barcode)
	if err == ErrCopyNotFound {
		setFlash(c, flashKindError, "loan_barcode_unknown")
		c.Redirect(http.StatusFound, bookPath)
		return
	}
	if err != nil {
		log.Printf("HandleCheckout: GetCopyByBarcode(%q): %v", barcode, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if copy.BookID != bookID {
		setFlash(c, flashKindError, "loan_barcode_book_mismatch")
		c.Redirect(http.StatusFound, bookPath)
		return
	}

	dueDate := time.Now().AddDate(0, 0, DefaultLoanTermDays)
	err = dm.CheckoutBook(copy.ID, patronID, dueDate)
	switch err {
	case nil:
		setFlash(c, flashKindSuccess, "loan_checkout_success")
		setFlashDetail(c, book.Title)
	case ErrPatronHasOverdue:
		setFlash(c, flashKindError, "loan_blocked_overdue")
	case ErrPatronAtLoanLimit:
		setFlash(c, flashKindError, "loan_blocked_limit")
	case ErrNoCopiesAvailable:
		setFlash(c, flashKindError, "loan_no_copies")
	default:
		log.Printf("HandleCheckout: CheckoutBook(copy=%d, patron=%d): %v", copy.ID, patronID, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	c.Redirect(http.StatusFound, bookPath)
}

func HandleReturn(c *gin.Context) {
	loanID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Status(http.StatusNotFound)
		renderTemplate(c, "error", gin.H{
			"Title":   "Not Found",
			"Status":  404,
			"Message": "Page not found",
		})
		return
	}

	dm := getDB(c)
	err = dm.ReturnBook(loanID)
	switch err {
	case nil:
		setFlash(c, flashKindSuccess, "loan_return_success")
	case sql.ErrNoRows:
		c.Status(http.StatusNotFound)
		renderTemplate(c, "error", gin.H{
			"Title":   "Not Found",
			"Status":  404,
			"Message": "Loan not found",
		})
		return
	case ErrLoanAlreadyReturned:
		setFlash(c, flashKindError, "loan_already_returned")
	default:
		log.Printf("HandleReturn: ReturnBook(%d): %v", loanID, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	c.Redirect(http.StatusFound, "/loans")
}

func HandleLoansList(c *gin.Context) {
	filter := c.Query("filter")
	if filter != "active" && filter != "overdue" {
		filter = "active"
	}

	dm := getDB(c)
	var loans []LoanListRow
	var err error
	if filter == "overdue" {
		loans, err = dm.GetOverdueLoans()
	} else {
		loans, err = dm.GetActiveLoans()
	}
	if err != nil {
		log.Printf("HandleLoansList: fetch loans (filter=%s): %v", filter, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	renderTemplate(c, "loans", gin.H{
		"Title":         "Loans",
		"Loans":         loans,
		"Filter":        filter,
		"Success":       readAndClearFlash(c, flashKindSuccess),
		"SuccessDetail": readAndClearFlashDetail(c),
		"Error":         readAndClearFlash(c, flashKindError),
	})
}

func HandleMyLoans(c *gin.Context) {
	user := c.MustGet("user").(*User)
	if user.PatronID == nil {
		log.Printf("HandleMyLoans: patron user %d has nil patron_id", user.ID)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	dm := getDB(c)
	loans, err := dm.GetPatronActiveLoans(*user.PatronID)
	if err != nil {
		log.Printf("HandleMyLoans: GetPatronActiveLoans(%d): %v", *user.PatronID, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	renderTemplate(c, "my_loans", gin.H{
		"Title": "My Loans",
		"Loans": loans,
	})
}
