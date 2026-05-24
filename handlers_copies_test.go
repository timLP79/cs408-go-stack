// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// postStaffFormWithReferer mirrors postStaffForm but sets a Referer
// header so HandleCopyStatus / HandleCopyDelete can choose the
// per-book redirect destination over the /inventory fallback.
func postStaffFormWithReferer(t *testing.T, router *http.Handler, path, referer string, sess *http.Cookie, csrf string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("csrf_token", csrf)
	for k, v := range fields {
		form.Set(k, v)
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	(*router).ServeHTTP(rr, req)
	return rr
}

// ---------- HandleBookCopies ----------

// TestBookCopiesRendersForStaff pins GET /books/:id/copies: 200, the
// rendered HTML contains the seeded book title and at least one
// barcode (formatted in <code>).
func TestBookCopiesRendersForStaff(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Inventory Subject", 2)

	req, _ := http.NewRequest("GET", fmt.Sprintf("/books/%d/copies", bookID), nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Inventory Subject") {
		t.Errorf("body should contain book title")
	}
	if !strings.Contains(body, "<code>LSF") {
		t.Errorf("body should contain at least one LSF barcode in <code>")
	}
}

// TestBookCopiesNotFound pins the 404 path for a missing book id.
func TestBookCopiesNotFound(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_user", "staff")

	req, _ := http.NewRequest("GET", "/books/99999/copies", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// ---------- HandleCopyStatus ----------

// TestCopyStatusHappyPath pins POST /copies/:id/status: 302 to
// /inventory (no referer set), copy_status_updated flash, copy is
// now in the requested status, and is not in the available pool.
func TestCopyStatusHappyPath(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Status Subject", 1)
	copyID := firstCopyOf(t, dm, bookID)

	rr := postStaffForm(t, router, fmt.Sprintf("/copies/%d/status", copyID), sess, csrf, map[string]string{
		"status": "lost",
	})

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/inventory" {
		t.Errorf("expected redirect to /inventory (no referer), got %q", loc)
	}
	if got := flashCode(rr, "flash_success"); got != "copy_status_updated" {
		t.Errorf("expected flash_success=copy_status_updated, got %q", got)
	}

	cp, err := dm.GetCopyByID(copyID)
	if err != nil {
		t.Fatalf("GetCopyByID: %v", err)
	}
	if cp.Status != "lost" {
		t.Errorf("Status = %q, want lost", cp.Status)
	}
	if got := availableCopiesOf(t, dm, bookID); got != 0 {
		t.Errorf("lost copy should not count toward available; got %d available", got)
	}
}

// TestCopyStatusRedirectsToBookWithReferer pins the per-book redirect
// when the Referer header points at the book detail or its copies
// page. Keeps the admin's per-book context across the status change.
func TestCopyStatusRedirectsToBookWithReferer(t *testing.T) {
	var handler http.Handler
	router, dm := setupTestRouter(t)
	handler = router
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Per-book redirect", 1)
	copyID := firstCopyOf(t, dm, bookID)

	rr := postStaffFormWithReferer(t, &handler,
		fmt.Sprintf("/copies/%d/status", copyID),
		fmt.Sprintf("http://x/books/%d/copies", bookID),
		sess, csrf, map[string]string{"status": "damaged"})

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	want := fmt.Sprintf("/books/%d/copies", bookID)
	if loc := rr.Header().Get("Location"); loc != want {
		t.Errorf("expected redirect to %q, got %q", want, loc)
	}
}

// TestCopyStatusInvalid pins that an unknown status value is rejected
// with the copy_status_invalid flash and the copy stays in its prior
// status. Guards against typos becoming silent UPDATEs of garbage.
func TestCopyStatusInvalid(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Garbage Subject", 1)
	copyID := firstCopyOf(t, dm, bookID)

	rr := postStaffForm(t, router, fmt.Sprintf("/copies/%d/status", copyID), sess, csrf, map[string]string{
		"status": "demolished",
	})

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if got := flashCode(rr, "flash_error"); got != "copy_status_invalid" {
		t.Errorf("expected flash_error=copy_status_invalid, got %q", got)
	}

	cp, _ := dm.GetCopyByID(copyID)
	if cp.Status != "available" {
		t.Errorf("status should remain 'available' on invalid input; got %q", cp.Status)
	}
}

// TestCopyStatusUnknownCopy pins the 404 path for a copy id that
// does not exist. Handler must short-circuit before UpdateCopyStatus
// since the redirect chooses /inventory by referer, not by copy id.
func TestCopyStatusUnknownCopy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")

	rr := postStaffForm(t, router, "/copies/99999/status", sess, csrf, map[string]string{
		"status": "lost",
	})
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// TestCopyStatusPatronForbidden pins the auth gate: a patron-role
// user cannot mutate copy status. Either 403 or 302-to-login is
// acceptable depending on how RequireStaff handles non-staff users.
func TestCopyStatusPatronForbidden(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf, _ := loginAsPatron(t, dm, "Borrower")
	bookID := mustCreateBook(t, dm, "Off-limits", 1)
	copyID := firstCopyOf(t, dm, bookID)

	rr := postStaffForm(t, router, fmt.Sprintf("/copies/%d/status", copyID), sess, csrf, map[string]string{
		"status": "lost",
	})

	if rr.Code == http.StatusFound {
		// If 302, must not be a success redirect.
		if got := flashCode(rr, "flash_success"); got == "copy_status_updated" {
			t.Errorf("patron should not have been able to set status; flash success leaked")
		}
	}
	cp, _ := dm.GetCopyByID(copyID)
	if cp.Status != "available" {
		t.Errorf("Status mutated by patron to %q", cp.Status)
	}
}

// ---------- HandleCopyDelete ----------

// TestCopyDeleteHappyPath pins POST /copies/:id/delete: 302 to
// /inventory, copy_deleted flash, GetCopyByID returns ErrCopyNotFound.
func TestCopyDeleteHappyPath(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Doomed Subject", 1)
	copyID := firstCopyOf(t, dm, bookID)

	rr := postStaffForm(t, router, fmt.Sprintf("/copies/%d/delete", copyID), sess, csrf, nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if got := flashCode(rr, "flash_success"); got != "copy_deleted" {
		t.Errorf("expected flash_success=copy_deleted, got %q", got)
	}
	if _, err := dm.GetCopyByID(copyID); err != ErrCopyNotFound {
		t.Errorf("expected ErrCopyNotFound after delete, got %v", err)
	}
}

// TestCopyDeleteRejectsHasLoans pins the loan-history guard:
// attempting to delete a copy with any loan row (active or returned)
// flashes copy_has_loans and leaves the copy in place. Without this
// guard a librarian could erase a historical loan link by accident.
func TestCopyDeleteRejectsHasLoans(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBook(t, dm, "Has History", 1)
	patronID := mustCreatePatron(t, dm, "Borrower")
	mustCheckout(t, dm, bookID, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays))
	copyID := firstCopyOf(t, dm, bookID)

	rr := postStaffForm(t, router, fmt.Sprintf("/copies/%d/delete", copyID), sess, csrf, nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if got := flashCode(rr, "flash_error"); got != "copy_has_loans" {
		t.Errorf("expected flash_error=copy_has_loans, got %q", got)
	}
	if _, err := dm.GetCopyByID(copyID); err != nil {
		t.Errorf("copy should still exist after rejected delete: %v", err)
	}
}

// TestCopyDeleteUnknownCopy pins the 404 path for a copy id that
// does not exist.
func TestCopyDeleteUnknownCopy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_user", "staff")

	rr := postStaffForm(t, router, "/copies/99999/delete", sess, csrf, nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// ---------- HandleInventory ----------

// TestInventoryRendersForStaff pins GET /inventory: 200, lists every
// seeded copy across multiple books.
func TestInventoryRendersForStaff(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_user", "staff")
	mustCreateBook(t, dm, "Alpha", 1)
	mustCreateBook(t, dm, "Beta", 1)

	req, _ := http.NewRequest("GET", "/inventory", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Alpha") || !strings.Contains(body, "Beta") {
		t.Errorf("inventory page should list both seeded books")
	}
}

// TestInventoryStatusFilter pins ?status=lost: the response includes
// the lost copy and excludes a sibling available copy.
func TestInventoryStatusFilter(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_user", "staff")
	bookID := mustCreateBookWithAvailable(t, dm, "Filter Subject", 2, 1) // 1 lost, 1 available

	copies, err := dm.GetCopiesByBookID(bookID)
	if err != nil || len(copies) != 2 {
		t.Fatalf("expected 2 copies, got %d (err %v)", len(copies), err)
	}
	var lostBarcode, availBarcode string
	for _, c := range copies {
		if c.Status == "lost" {
			lostBarcode = c.Barcode
		} else {
			availBarcode = c.Barcode
		}
	}

	req, _ := http.NewRequest("GET", "/inventory?status=lost", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, lostBarcode) {
		t.Errorf("lost barcode %s should appear under status=lost filter", lostBarcode)
	}
	if strings.Contains(body, availBarcode) {
		t.Errorf("available barcode %s should NOT appear under status=lost filter", availBarcode)
	}
}
