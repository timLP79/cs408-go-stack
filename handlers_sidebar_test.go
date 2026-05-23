// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fetchPageWithSidebar hits /catalog with the given session and returns
// the body. /catalog is used (rather than /) because HandleIndex for
// patron users requires a linked patrons row which loginAs doesn't
// create. The sidebar lives in layout.html and renders identically
// across all authenticated pages.
func fetchPageWithSidebar(t *testing.T, router http.Handler, sess *http.Cookie) string {
	t.Helper()
	req, _ := http.NewRequest("GET", "/catalog", nil)
	if sess != nil {
		req.AddCookie(sess)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("page fetch: status %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	return rr.Body.String()
}

// assertSidebarHas / assertSidebarOmits make the expectations crisp
// per role. The strings checked are stable hrefs and labels that the
// sidebar emits literally.
func assertSidebarHas(t *testing.T, body string, items ...string) {
	t.Helper()
	for _, item := range items {
		if !strings.Contains(body, item) {
			t.Errorf("sidebar missing expected item: %q", item)
		}
	}
}

func assertSidebarOmits(t *testing.T, body string, items ...string) {
	t.Helper()
	for _, item := range items {
		if strings.Contains(body, item) {
			t.Errorf("sidebar should not contain: %q", item)
		}
	}
}

func TestSidebarForAdmin(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "sidebar_admin", "admin")

	body := fetchPageWithSidebar(t, router, sess)

	assertSidebarHas(t, body,
		">Navigation<",
		`href="/"`,
		`href="/catalog"`,
		`href="/kiosk"`,
		">Circulation<",
		`href="/loans"`,
		`href="/reports/overdue"`,
		">Tools<",
		`href="/admin"`,
		">Admin Tools<",
	)
	assertSidebarOmits(t, body,
		`href="/my/loans"`,
		`href="/staff-tools"`,
		">Staff Tools<",
		// Patrons + Staff are now under the User Management card on
		// /admin, not direct sidebar items.
		`class="nav-link rounded" href="/patrons"`,
		`class="nav-link rounded" href="/staff"`,
	)
}

func TestSidebarForStaff(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "sidebar_staff", "staff")

	body := fetchPageWithSidebar(t, router, sess)

	assertSidebarHas(t, body,
		">Navigation<",
		`href="/"`,
		`href="/catalog"`,
		`href="/kiosk"`,
		">Circulation<",
		`href="/loans"`,
		`href="/reports/overdue"`,
		">Tools<",
		`href="/staff-tools"`,
		">Staff Tools<",
	)
	assertSidebarOmits(t, body,
		`href="/my/loans"`,
		`href="/admin"`,
		">Admin Tools<",
		`class="nav-link rounded" href="/patrons"`,
		`class="nav-link rounded" href="/staff"`,
	)
}

func TestSidebarForPatron(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "sidebar_patron", "patron")

	body := fetchPageWithSidebar(t, router, sess)

	assertSidebarHas(t, body,
		">Navigation<",
		`href="/"`,
		`href="/catalog"`,
		`href="/my/loans"`,
	)
	assertSidebarOmits(t, body,
		`href="/kiosk"`,
		">Circulation<",
		`href="/loans"`,
		`href="/reports/overdue"`,
		">Tools<",
		`href="/admin"`,
		`href="/staff-tools"`,
		">Admin Tools<",
		">Staff Tools<",
	)
}

func TestStaffToolsAccessibleToStaff(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "tools_staff", "staff")

	req, _ := http.NewRequest("GET", "/staff-tools", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Patrons") {
		t.Errorf("staff-tools page missing Patrons card")
	}
}

func TestStaffToolsAccessibleToAdmin(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "tools_admin", "admin")

	req, _ := http.NewRequest("GET", "/staff-tools", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestStaffToolsForbiddenForPatron(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "tools_patron", "patron")

	req, _ := http.NewRequest("GET", "/staff-tools", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// RequireStaff redirects non-staff to /login (302) rather than
	// returning 403. The pin here is "patron does not get the staff
	// hub page" -- either redirect or forbidden is acceptable; we
	// assert the body is NOT the staff-tools page.
	if rr.Code == http.StatusOK && strings.Contains(rr.Body.String(), "Staff Tools") {
		t.Errorf("patron should not reach /staff-tools content; got 200 with Staff Tools body")
	}
}

func TestAdminPageRendersUserManagementCard(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin_um", "admin")

	req, _ := http.NewRequest("GET", "/admin", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "User Management") {
		t.Errorf("/admin missing User Management card")
	}
	// The card links out to both legacy pages so users have a path
	// regardless of which UI a future unified-users page lands.
	if !strings.Contains(body, `href="/patrons"`) {
		t.Errorf("User Management card missing Patrons link")
	}
	if !strings.Contains(body, `href="/staff"`) {
		t.Errorf("User Management card missing Staff link")
	}
}
