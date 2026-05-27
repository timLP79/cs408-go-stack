// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// decodeScan unmarshals a /checkout/scan or /checkin/scan response.
// Mirrors the handlers' scanResponse shape via the public JSON tags.
type scanResp struct {
	Success      bool   `json:"success"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	LoanID       int    `json:"loan_id,omitempty"`
	Barcode      string `json:"barcode,omitempty"`
	BookTitle    string `json:"book_title,omitempty"`
	PatronName   string `json:"patron_name,omitempty"`
	DueDate      string `json:"due_date,omitempty"`
}

func decodeScan(t *testing.T, rr *httptest.ResponseRecorder) scanResp {
	t.Helper()
	var r scanResp
	if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode scan response: %v; body=%s", err, rr.Body.String())
	}
	return r
}

// ---------- HandleCheckoutPortal + HandleCheckinPortal (GET pages) ----------

func TestCheckoutPortalRenders(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_user", "staff")

	req, _ := http.NewRequest("GET", "/checkout", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"Check Out", "checkout-portal-barcode", "checkout-portal-patron"} {
		if !strings.Contains(body, want) {
			t.Errorf("checkout portal body missing %q", want)
		}
	}
}

func TestCheckinPortalRenders(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_user", "staff")

	req, _ := http.NewRequest("GET", "/checkin", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"Check In", "checkin-portal-barcode"} {
		if !strings.Contains(body, want) {
			t.Errorf("checkin portal body missing %q", want)
		}
	}
}

// ---------- HandleCheckoutScan ----------

func TestCheckoutScanHappy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Scan Subject", 1)
	patronID := mustCreatePatron(t, dm, "Borrower")
	barcode := firstAvailableBarcodeOf(t, dm, bookID)

	rr := postStaffForm(t, router, "/checkout/scan", sess, csrf, map[string]string{
		"patron_id": fmt.Sprintf("%d", patronID),
		"barcode":   barcode,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}
	resp := decodeScan(t, rr)
	if !resp.Success {
		t.Fatalf("expected success, got error_code=%q msg=%q", resp.ErrorCode, resp.ErrorMessage)
	}
	if resp.Barcode != barcode {
		t.Errorf("Barcode echo = %q, want %q", resp.Barcode, barcode)
	}
	if resp.BookTitle != "Scan Subject" {
		t.Errorf("BookTitle = %q, want Scan Subject", resp.BookTitle)
	}
	if resp.PatronName != "Borrower" {
		t.Errorf("PatronName = %q, want Borrower", resp.PatronName)
	}
	if resp.LoanID <= 0 {
		t.Errorf("LoanID should be positive, got %d", resp.LoanID)
	}
	if got := availableCopiesOf(t, dm, bookID); got != 0 {
		t.Errorf("expected 0 available after checkout scan, got %d", got)
	}
}

func TestCheckoutScanMissingPatron(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "X", 1)
	barcode := firstAvailableBarcodeOf(t, dm, bookID)

	rr := postStaffForm(t, router, "/checkout/scan", sess, csrf, map[string]string{
		"patron_id": "",
		"barcode":   barcode,
	})
	resp := decodeScan(t, rr)
	if resp.Success {
		t.Fatalf("expected failure")
	}
	if resp.ErrorCode != "loan_patron_required" {
		t.Errorf("ErrorCode = %q, want loan_patron_required", resp.ErrorCode)
	}
}

func TestCheckoutScanUnknownBarcode(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	patronID := mustCreatePatron(t, dm, "P")

	rr := postStaffForm(t, router, "/checkout/scan", sess, csrf, map[string]string{
		"patron_id": fmt.Sprintf("%d", patronID),
		"barcode":   "LSF99999999",
	})
	resp := decodeScan(t, rr)
	if resp.Success || resp.ErrorCode != "loan_barcode_unknown" {
		t.Errorf("expected loan_barcode_unknown, got success=%v code=%q", resp.Success, resp.ErrorCode)
	}
}

func TestCheckoutScanAlreadyCheckedOut(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Single Copy", 1)
	firstPatron := mustCreatePatron(t, dm, "First")
	secondPatron := mustCreatePatron(t, dm, "Second")
	barcode := firstAvailableBarcodeOf(t, dm, bookID)
	mustCheckout(t, dm, bookID, firstPatron, time.Now().AddDate(0, 0, DefaultLoanTermDays))

	rr := postStaffForm(t, router, "/checkout/scan", sess, csrf, map[string]string{
		"patron_id": fmt.Sprintf("%d", secondPatron),
		"barcode":   barcode,
	})
	resp := decodeScan(t, rr)
	if resp.Success || resp.ErrorCode != "loan_no_copies" {
		t.Errorf("expected loan_no_copies on already-out copy, got success=%v code=%q", resp.Success, resp.ErrorCode)
	}
}

// ---------- HandleCheckoutUndo ----------

func TestCheckoutUndoHappy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Undo Subject", 1)
	patronID := mustCreatePatron(t, dm, "P")
	barcode := firstAvailableBarcodeOf(t, dm, bookID)

	rr1 := postStaffForm(t, router, "/checkout/scan", sess, csrf, map[string]string{
		"patron_id": fmt.Sprintf("%d", patronID),
		"barcode":   barcode,
	})
	scan := decodeScan(t, rr1)
	if !scan.Success {
		t.Fatalf("setup scan failed: %+v", scan)
	}

	rr2 := postStaffForm(t, router, "/checkout/undo", sess, csrf, map[string]string{
		"loan_id": fmt.Sprintf("%d", scan.LoanID),
	})
	undo := decodeScan(t, rr2)
	if !undo.Success {
		t.Errorf("undo failed: %+v", undo)
	}
	if got := availableCopiesOf(t, dm, bookID); got != 1 {
		t.Errorf("expected 1 available after undo, got %d", got)
	}
}

func TestCheckoutUndoRefusesReturnedLoan(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Returned Already", 1)
	patronID := mustCreatePatron(t, dm, "P")
	mustCheckout(t, dm, bookID, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays))
	// Find the loan and return it.
	var loanID int
	if err := dm.db.QueryRow(`
		SELECT l.id FROM loans l
		JOIN copies c ON l.copy_id = c.id
		WHERE c.book_id = ?`, bookID).Scan(&loanID); err != nil {
		t.Fatalf("query loan id: %v", err)
	}
	if err := dm.ReturnBook(loanID); err != nil {
		t.Fatalf("ReturnBook: %v", err)
	}

	rr := postStaffForm(t, router, "/checkout/undo", sess, csrf, map[string]string{
		"loan_id": fmt.Sprintf("%d", loanID),
	})
	resp := decodeScan(t, rr)
	if resp.Success || resp.ErrorCode != "loan_undo_already_returned" {
		t.Errorf("expected loan_undo_already_returned, got success=%v code=%q",
			resp.Success, resp.ErrorCode)
	}
}

// ---------- HandleCheckinScan ----------

func TestCheckinScanHappy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Return Subject", 1)
	patronID := mustCreatePatron(t, dm, "Borrower")
	barcode := firstAvailableBarcodeOf(t, dm, bookID)
	mustCheckout(t, dm, bookID, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays))

	rr := postStaffForm(t, router, "/checkin/scan", sess, csrf, map[string]string{
		"barcode": barcode,
	})
	resp := decodeScan(t, rr)
	if !resp.Success {
		t.Fatalf("expected success, got error_code=%q msg=%q", resp.ErrorCode, resp.ErrorMessage)
	}
	if resp.BookTitle != "Return Subject" || resp.PatronName != "Borrower" {
		t.Errorf("response = %+v, want Book=Return Subject Patron=Borrower", resp)
	}
	if got := availableCopiesOf(t, dm, bookID); got != 1 {
		t.Errorf("expected 1 available after checkin, got %d", got)
	}
}

func TestCheckinScanNotOnLoan(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "On Shelf", 1)
	barcode := firstAvailableBarcodeOf(t, dm, bookID)

	rr := postStaffForm(t, router, "/checkin/scan", sess, csrf, map[string]string{
		"barcode": barcode,
	})
	resp := decodeScan(t, rr)
	if resp.Success || resp.ErrorCode != "loan_copy_not_on_loan" {
		t.Errorf("expected loan_copy_not_on_loan, got success=%v code=%q", resp.Success, resp.ErrorCode)
	}
}

func TestCheckinScanUnknownBarcode(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")

	rr := postStaffForm(t, router, "/checkin/scan", sess, csrf, map[string]string{
		"barcode": "LSF99999999",
	})
	resp := decodeScan(t, rr)
	if resp.Success || resp.ErrorCode != "loan_barcode_unknown" {
		t.Errorf("expected loan_barcode_unknown, got success=%v code=%q", resp.Success, resp.ErrorCode)
	}
}

// ---------- HandleCheckinUndo ----------

func TestCheckinUndoHappy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Undo Return Subject", 1)
	patronID := mustCreatePatron(t, dm, "P")
	barcode := firstAvailableBarcodeOf(t, dm, bookID)
	mustCheckout(t, dm, bookID, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays))

	// Return it via the portal.
	rr1 := postStaffForm(t, router, "/checkin/scan", sess, csrf, map[string]string{"barcode": barcode})
	scan := decodeScan(t, rr1)
	if !scan.Success {
		t.Fatalf("setup return failed: %+v", scan)
	}
	if got := availableCopiesOf(t, dm, bookID); got != 1 {
		t.Errorf("setup precondition: expected 1 available after return, got %d", got)
	}

	// Undo the return.
	rr2 := postStaffForm(t, router, "/checkin/undo", sess, csrf, map[string]string{
		"loan_id": fmt.Sprintf("%d", scan.LoanID),
	})
	undo := decodeScan(t, rr2)
	if !undo.Success {
		t.Errorf("undo failed: %+v", undo)
	}
	if got := availableCopiesOf(t, dm, bookID); got != 0 {
		t.Errorf("expected 0 available after undo (loan re-opened), got %d", got)
	}
}

// ---------- Auth: patron role cannot use portals ----------

func TestPortalsRejectPatron(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _, _ := loginAsPatron(t, dm, "Patron")

	for _, path := range []string{"/checkout", "/checkin"} {
		req, _ := http.NewRequest("GET", path, nil)
		req.AddCookie(sess)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code == http.StatusOK {
			t.Errorf("patron should not see %s; got 200", path)
		}
	}
}

// ---------- DB methods ----------

func TestGetActiveLoanByBarcodeHappy(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "DB Subject", 1)
	patronID := mustCreatePatron(t, dm, "DB Patron")
	barcode := firstAvailableBarcodeOf(t, dm, bookID)
	mustCheckout(t, dm, bookID, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays))

	info, err := dm.GetActiveLoanByBarcode(barcode)
	if err != nil {
		t.Fatalf("GetActiveLoanByBarcode: %v", err)
	}
	if info.PatronName != "DB Patron" || info.BookTitle != "DB Subject" || info.Barcode != barcode {
		t.Errorf("info = %+v, want patron=DB Patron book=DB Subject barcode=%s", info, barcode)
	}
}

func TestGetActiveLoanByBarcodeNotOnLoan(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "On Shelf DB", 1)
	barcode := firstAvailableBarcodeOf(t, dm, bookID)

	_, err := dm.GetActiveLoanByBarcode(barcode)
	if err != ErrNoActiveLoanForBarcode {
		t.Errorf("expected ErrNoActiveLoanForBarcode, got %v", err)
	}
}

func TestGetActiveLoanByBarcodeUnknown(t *testing.T) {
	dm := setupTestDB(t)
	_, err := dm.GetActiveLoanByBarcode("LSF99999999")
	if err != ErrCopyNotFound {
		t.Errorf("expected ErrCopyNotFound for unknown barcode, got %v", err)
	}
}

func TestDeleteLoanIfActiveHappy(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Delete-If-Active", 1)
	patronID := mustCreatePatron(t, dm, "P")
	mustCheckout(t, dm, bookID, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays))
	var loanID int
	if err := dm.db.QueryRow(`
		SELECT l.id FROM loans l JOIN copies c ON l.copy_id = c.id WHERE c.book_id = ?`,
		bookID).Scan(&loanID); err != nil {
		t.Fatalf("query loan id: %v", err)
	}

	if err := dm.DeleteLoanIfActive(loanID); err != nil {
		t.Fatalf("DeleteLoanIfActive: %v", err)
	}
	// Loan should be gone and copy back to available.
	if got := availableCopiesOf(t, dm, bookID); got != 1 {
		t.Errorf("expected 1 available after delete, got %d", got)
	}
}

func TestDeleteLoanIfActiveRefusesReturned(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Already Returned", 1)
	patronID := mustCreatePatron(t, dm, "P")
	mustCheckout(t, dm, bookID, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays))
	var loanID int
	if err := dm.db.QueryRow(`
		SELECT l.id FROM loans l JOIN copies c ON l.copy_id = c.id WHERE c.book_id = ?`,
		bookID).Scan(&loanID); err != nil {
		t.Fatalf("query loan id: %v", err)
	}
	if err := dm.ReturnBook(loanID); err != nil {
		t.Fatalf("ReturnBook: %v", err)
	}

	if err := dm.DeleteLoanIfActive(loanID); err != ErrLoanAlreadyReturned {
		t.Errorf("expected ErrLoanAlreadyReturned, got %v", err)
	}
}

func TestReopenReturnedLoanHappy(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Reopen Subject", 1)
	patronID := mustCreatePatron(t, dm, "P")
	mustCheckout(t, dm, bookID, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays))
	var loanID int
	if err := dm.db.QueryRow(`
		SELECT l.id FROM loans l JOIN copies c ON l.copy_id = c.id WHERE c.book_id = ?`,
		bookID).Scan(&loanID); err != nil {
		t.Fatalf("query loan id: %v", err)
	}
	if err := dm.ReturnBook(loanID); err != nil {
		t.Fatalf("ReturnBook: %v", err)
	}

	if err := dm.ReopenReturnedLoan(loanID); err != nil {
		t.Fatalf("ReopenReturnedLoan: %v", err)
	}
	if got := availableCopiesOf(t, dm, bookID); got != 0 {
		t.Errorf("expected 0 available after reopen, got %d", got)
	}
}
