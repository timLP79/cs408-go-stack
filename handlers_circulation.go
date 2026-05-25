// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Rapid-scan checkout/checkin portal (cs408-go-stack-1v5). The two GET
// handlers render stateless portal pages; the JS on those pages tracks
// the session locally. Each scan POSTs to a JSON endpoint here and
// the response shape is the same for both portals: a small struct with
// either Success: true plus the scan row data, or Success: false plus
// an ErrorCode (mapped to a flash slug string) and human-readable
// ErrorMessage. Refreshing the page resets the session; no server-side
// session state is held between scans.

// HandleCheckoutPortal renders the rapid-scan checkout page.
func HandleCheckoutPortal(c *gin.Context) {
	dm := getDB(c)
	patrons, err := dm.GetAllPatrons()
	if err != nil {
		log.Printf("HandleCheckoutPortal: GetAllPatrons: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	renderTemplate(c, "checkout_portal", gin.H{
		"Title":   "Check Out",
		"Patrons": patrons,
	})
}

// HandleCheckinPortal renders the rapid-scan checkin page.
func HandleCheckinPortal(c *gin.Context) {
	renderTemplate(c, "checkin_portal", gin.H{
		"Title": "Check In",
	})
}

// scanResponse is the JSON shape returned by both scan endpoints.
// Success path fills the data fields; failure path fills ErrorCode +
// ErrorMessage. The client uses ErrorCode for stable behavioral
// branching and ErrorMessage for the user-visible banner.
type scanResponse struct {
	Success      bool   `json:"success"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	// Per-scan data (success only):
	LoanID     int    `json:"loan_id,omitempty"`
	Barcode    string `json:"barcode,omitempty"`
	BookTitle  string `json:"book_title,omitempty"`
	PatronName string `json:"patron_name,omitempty"`
	DueDate    string `json:"due_date,omitempty"`
}

// scanError builds a failure response with the given slug + the flash
// message text for that slug from flashMessages (so client and book-
// detail flow share the same wording).
func scanError(code string) scanResponse {
	msg := flashMessages[code]
	if msg == "" {
		msg = "An unexpected error occurred. Please try again."
	}
	return scanResponse{Success: false, ErrorCode: code, ErrorMessage: msg}
}

// HandleCheckoutScan is the AJAX endpoint that backs the checkout
// portal. Reads patron_id + barcode from the form, looks up the copy,
// verifies it belongs to no other open loan, creates the loan.
func HandleCheckoutScan(c *gin.Context) {
	dm := getDB(c)

	patronID, err := strconv.Atoi(c.PostForm("patron_id"))
	if err != nil || patronID <= 0 {
		c.JSON(http.StatusOK, scanError("loan_patron_required"))
		return
	}

	barcode := strings.TrimSpace(c.PostForm("barcode"))
	if barcode == "" {
		c.JSON(http.StatusOK, scanError("loan_barcode_required"))
		return
	}

	cp, err := dm.GetCopyByBarcode(barcode)
	if errors.Is(err, ErrCopyNotFound) {
		c.JSON(http.StatusOK, scanError("loan_barcode_unknown"))
		return
	}
	if err != nil {
		log.Printf("HandleCheckoutScan: GetCopyByBarcode(%q): %v", barcode, err)
		c.JSON(http.StatusInternalServerError, scanError("internal_error"))
		return
	}

	dueDate := time.Now().AddDate(0, 0, DefaultLoanTermDays)
	switch err := dm.CheckoutBook(cp.ID, patronID, dueDate); {
	case err == nil:
		// Fetch loan id + supporting data for the scan row. We re-derive
		// rather than threading return values out of CheckoutBook to
		// keep that method's signature stable across callers.
		var loanID int
		var bookTitle, patronName string
		if err := dm.db.QueryRow(`
			SELECT l.id, b.title, p.name
			FROM loans l
			JOIN copies c ON l.copy_id = c.id
			JOIN books b ON c.book_id = b.id
			JOIN patrons p ON l.patron_id = p.id
			WHERE l.copy_id = ? AND l.patron_id = ? AND l.returned_at IS NULL
			ORDER BY l.id DESC LIMIT 1`,
			cp.ID, patronID).Scan(&loanID, &bookTitle, &patronName); err != nil {
			log.Printf("HandleCheckoutScan: post-insert lookup: %v", err)
			c.JSON(http.StatusInternalServerError, scanError("internal_error"))
			return
		}
		c.JSON(http.StatusOK, scanResponse{
			Success:    true,
			LoanID:     loanID,
			Barcode:    cp.Barcode,
			BookTitle:  bookTitle,
			PatronName: patronName,
			DueDate:    dueDate.UTC().Format("2006-01-02"),
		})
	case errors.Is(err, ErrPatronHasOverdue):
		c.JSON(http.StatusOK, scanError("loan_blocked_overdue"))
	case errors.Is(err, ErrPatronAtLoanLimit):
		c.JSON(http.StatusOK, scanError("loan_blocked_limit"))
	case errors.Is(err, ErrNoCopiesAvailable):
		c.JSON(http.StatusOK, scanError("loan_no_copies"))
	default:
		log.Printf("HandleCheckoutScan: CheckoutBook(copy=%d, patron=%d): %v", cp.ID, patronID, err)
		c.JSON(http.StatusInternalServerError, scanError("internal_error"))
	}
}

// HandleCheckoutUndo deletes a just-created loan row. The client posts
// the loan_id from its session-table memory. Refuses to delete a loan
// that has already been returned (preserves history).
func HandleCheckoutUndo(c *gin.Context) {
	dm := getDB(c)
	loanID, err := strconv.Atoi(c.PostForm("loan_id"))
	if err != nil || loanID <= 0 {
		c.JSON(http.StatusOK, scanError("loan_undo_invalid"))
		return
	}
	switch err := dm.DeleteLoanIfActive(loanID); {
	case err == nil:
		c.JSON(http.StatusOK, scanResponse{Success: true, LoanID: loanID})
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusOK, scanError("loan_undo_not_found"))
	case errors.Is(err, ErrLoanAlreadyReturned):
		c.JSON(http.StatusOK, scanError("loan_undo_already_returned"))
	default:
		log.Printf("HandleCheckoutUndo: DeleteLoanIfActive(%d): %v", loanID, err)
		c.JSON(http.StatusInternalServerError, scanError("internal_error"))
	}
}

// HandleCheckinScan is the AJAX endpoint that backs the checkin
// portal. Reads barcode from the form, looks up the active loan,
// returns the book.
func HandleCheckinScan(c *gin.Context) {
	dm := getDB(c)

	barcode := strings.TrimSpace(c.PostForm("barcode"))
	if barcode == "" {
		c.JSON(http.StatusOK, scanError("loan_barcode_required"))
		return
	}

	info, err := dm.GetActiveLoanByBarcode(barcode)
	switch {
	case err == nil:
		// fall through to ReturnBook
	case errors.Is(err, ErrCopyNotFound):
		c.JSON(http.StatusOK, scanError("loan_barcode_unknown"))
		return
	case errors.Is(err, ErrNoActiveLoanForBarcode):
		c.JSON(http.StatusOK, scanError("loan_copy_not_on_loan"))
		return
	default:
		log.Printf("HandleCheckinScan: GetActiveLoanByBarcode(%q): %v", barcode, err)
		c.JSON(http.StatusInternalServerError, scanError("internal_error"))
		return
	}

	switch err := dm.ReturnBook(info.LoanID); {
	case err == nil:
		c.JSON(http.StatusOK, scanResponse{
			Success:    true,
			LoanID:     info.LoanID,
			Barcode:    info.Barcode,
			BookTitle:  info.BookTitle,
			PatronName: info.PatronName,
		})
	case errors.Is(err, ErrLoanAlreadyReturned):
		// Race: another staffer returned this between our lookup and
		// our ReturnBook call. Surface as the not-on-loan slug since
		// from the user's perspective the copy is already on the shelf.
		c.JSON(http.StatusOK, scanError("loan_copy_not_on_loan"))
	default:
		log.Printf("HandleCheckinScan: ReturnBook(%d): %v", info.LoanID, err)
		c.JSON(http.StatusInternalServerError, scanError("internal_error"))
	}
}

// HandleCheckinUndo re-opens a just-returned loan by clearing its
// returned_at timestamp.
func HandleCheckinUndo(c *gin.Context) {
	dm := getDB(c)
	loanID, err := strconv.Atoi(c.PostForm("loan_id"))
	if err != nil || loanID <= 0 {
		c.JSON(http.StatusOK, scanError("loan_undo_invalid"))
		return
	}
	switch err := dm.ReopenReturnedLoan(loanID); {
	case err == nil:
		c.JSON(http.StatusOK, scanResponse{Success: true, LoanID: loanID})
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusOK, scanError("loan_undo_not_found"))
	default:
		log.Printf("HandleCheckinUndo: ReopenReturnedLoan(%d): %v", loanID, err)
		c.JSON(http.StatusInternalServerError, scanError("internal_error"))
	}
}
