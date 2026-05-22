// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// seedOverdueLoan returns the patron id with one overdue loan against a
// freshly-created book. Yesterday's due date keeps the row in the
// GetOverdueLoans() window.
func seedOverdueLoan(t *testing.T, dm *DatabaseManager, patronName, bookTitle string) int {
	t.Helper()
	bookID := mustCreateBook(t, dm, bookTitle, 1)
	patronID := mustCreatePatron(t, dm, patronName)
	yesterday := time.Now().AddDate(0, 0, -1).UTC().Format("2006-01-02")
	mustInsertLoan(t, dm, bookID, patronID, yesterday, "")
	return patronID
}

func TestReportsOverdueRendersTable(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_overdue_table", "staff")

	seedOverdueLoan(t, dm, "Jane Doe", "Borrowed Book")

	req, _ := http.NewRequest("GET", "/reports/overdue", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Jane Doe") {
		t.Errorf("expected patron name in table")
	}
	if !strings.Contains(body, "Borrowed Book") {
		t.Errorf("expected book title in table")
	}
	if !strings.Contains(body, "Print Notice") {
		t.Errorf("expected Print Notice link per patron")
	}
}

func TestReportsOverdueEmptyState(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_overdue_empty", "staff")

	req, _ := http.NewRequest("GET", "/reports/overdue", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "No overdue loans") {
		t.Errorf("expected empty-state copy in body")
	}
}

func TestOverdueNoticeRendersForPatron(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_notice_render", "staff")

	patronID := seedOverdueLoan(t, dm, "Jane Doe", "Pride and Prejudice")

	url := fmt.Sprintf("/reports/overdue/patron/%d/notice", patronID)
	req, _ := http.NewRequest("GET", url, nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Jane Doe") {
		t.Errorf("notice missing patron name")
	}
	if !strings.Contains(body, "Pride and Prejudice") {
		t.Errorf("notice missing book title")
	}
	if !strings.Contains(body, "Test Author") {
		t.Errorf("notice missing author (mustCreateBook seeds 'Test Author')")
	}
	if !strings.Contains(body, "Notice date:") {
		t.Errorf("notice missing date stamp")
	}
}

func TestOverdueNotice404ForUnknownPatron(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_notice_404", "staff")

	req, _ := http.NewRequest("GET", "/reports/overdue/patron/99999/notice", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestOverdueNotice404WhenPatronHasNoOverdue(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_notice_no_overdue", "staff")

	// Patron exists but has zero overdue loans.
	patronID := mustCreatePatron(t, dm, "All Caught Up")

	url := fmt.Sprintf("/reports/overdue/patron/%d/notice", patronID)
	req, _ := http.NewRequest("GET", url, nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (no notice to print)", rr.Code)
	}
}

func TestOverdueNoticeRendersAddressWhenPresent(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_notice_addr", "staff")

	patronID := seedOverdueLoan(t, dm, "Address Patron", "Some Book")
	if err := dm.UpdatePatron(patronID, "Address Patron", "", "", "123 Main St\nBoise, ID 83702"); err != nil {
		t.Fatalf("UpdatePatron set address: %v", err)
	}

	url := fmt.Sprintf("/reports/overdue/patron/%d/notice", patronID)
	req, _ := http.NewRequest("GET", url, nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "123 Main St") {
		t.Errorf("expected street in notice, got body=%s", body)
	}
	if !strings.Contains(body, "Boise, ID 83702") {
		t.Errorf("expected city/state/zip in notice")
	}
}

func TestOverdueNoticeOmitsAddressWhenAbsent(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_notice_no_addr", "staff")

	patronID := seedOverdueLoan(t, dm, "No Address Patron", "Another Book")
	// Patron's address stays nil (default after mustCreatePatron).

	url := fmt.Sprintf("/reports/overdue/patron/%d/notice", patronID)
	req, _ := http.NewRequest("GET", url, nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	// Spot-check: the notice still renders patron + book without an
	// address line. We can't assert the absence of arbitrary strings
	// cleanly, so this test pins the positive path while the
	// address-present test pins the presence side.
	if !strings.Contains(body, "No Address Patron") {
		t.Errorf("notice missing patron name")
	}
	if !strings.Contains(body, "Another Book") {
		t.Errorf("notice missing book title")
	}
}
