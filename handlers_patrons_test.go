// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// validPatronFields returns a fresh set of valid form fields for
// HandlePatronCreate. Tests copy and mutate a single field to exercise
// one validation branch at a time without respelling the whole form.
func validPatronFields() map[string]string {
	return map[string]string{
		"name":             "Jane Smith",
		"email":            "jane@example.com",
		"phone":            "555-1234",
		"password":         "Patron123!",
		"password_confirm": "Patron123!",
	}
}

// ---------- List handler ----------

// TestPatronListRendersAsAdmin pins GET /patrons happy path: the list
// heading is present and the page does not 500 on an empty patrons
// table. Protects against a future edit routing /patrons to the wrong
// template or dropping the handler.
func TestPatronListRendersAsAdmin(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	req, _ := http.NewRequest("GET", "/patrons", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Patrons") {
		t.Errorf("expected body to contain 'Patrons' heading")
	}
}

// ---------- Create handler ----------

// TestPatronCreateHappyPath verifies the full create flow: patrons row
// inserted, linked users row inserted with role='patron' and a
// generateBaseUsername-derived username, PRG redirect to /patrons with
// success flash set. The end-to-end happy path for the #21 handler
// surface.
func TestPatronCreateHappyPath(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	rr := postStaffForm(t, router, "/patrons", sess, csrf, validPatronFields())

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/patrons" {
		t.Errorf("expected redirect to /patrons, got %q", loc)
	}
	assertFlashSuccessSet(t, rr)
	assertNoFlashError(t, rr)

	// Linked users row exists with role='patron' and the generated
	// username matches the first-initial + last-word rule.
	user, err := dm.GetUserByUsername("jsmith")
	if err != nil {
		t.Fatalf("expected user 'jsmith' to exist after patron create, got %v", err)
	}
	if user.Role != "patron" {
		t.Errorf("expected role 'patron' on created user, got %q", user.Role)
	}
	if user.PatronID == nil {
		t.Fatalf("expected users.patron_id to be set on a patron-role user")
	}

	// Patron row exists with the submitted name/email/phone.
	patron, err := dm.GetPatronByID(*user.PatronID)
	if err != nil {
		t.Fatalf("GetPatronByID(%d): %v", *user.PatronID, err)
	}
	if patron.Name != "Jane Smith" {
		t.Errorf("name: got %q, want %q", patron.Name, "Jane Smith")
	}
	if patron.Email == nil || *patron.Email != "jane@example.com" {
		t.Errorf("email: got %+v, want jane@example.com", patron.Email)
	}
	if patron.Phone == nil || *patron.Phone != "555-1234" {
		t.Errorf("phone: got %+v, want 555-1234", patron.Phone)
	}
}

func TestPatronCreatePersistsAddressAndEditUpdates(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validPatronFields()
	fields["address"] = "123 Main St\nBoise, ID 83702"

	rr := postStaffForm(t, router, "/patrons", sess, csrf, fields)
	if rr.Code != http.StatusFound {
		t.Fatalf("create: expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}

	user, err := dm.GetUserByUsername("jsmith")
	if err != nil {
		t.Fatalf("GetUserByUsername jsmith: %v", err)
	}
	patron, err := dm.GetPatronByID(*user.PatronID)
	if err != nil {
		t.Fatalf("GetPatronByID: %v", err)
	}
	if patron.Address == nil || *patron.Address != "123 Main St\nBoise, ID 83702" {
		t.Errorf("address not persisted on create: %+v", patron.Address)
	}

	editFields := map[string]string{
		"name":    "Jane Smith",
		"email":   "jane@example.com",
		"phone":   "555-1234",
		"address": "456 Oak Ave\nBoise, ID 83703",
	}
	editURL := fmt.Sprintf("/patrons/%d/edit", *user.PatronID)
	editRR := postStaffForm(t, router, editURL, sess, csrf, editFields)
	if editRR.Code != http.StatusFound {
		t.Fatalf("edit: expected 302, got %d. body: %s", editRR.Code, editRR.Body.String())
	}

	patron2, err := dm.GetPatronByID(*user.PatronID)
	if err != nil {
		t.Fatalf("GetPatronByID after edit: %v", err)
	}
	if patron2.Address == nil || *patron2.Address != "456 Oak Ave\nBoise, ID 83703" {
		t.Errorf("address not updated on edit: %+v", patron2.Address)
	}

	// Empty address on edit clears it (NULL in DB).
	editFields["address"] = ""
	clearRR := postStaffForm(t, router, editURL, sess, csrf, editFields)
	if clearRR.Code != http.StatusFound {
		t.Fatalf("clear: expected 302, got %d", clearRR.Code)
	}
	patron3, err := dm.GetPatronByID(*user.PatronID)
	if err != nil {
		t.Fatalf("GetPatronByID after clear: %v", err)
	}
	if patron3.Address != nil {
		t.Errorf("address should be nil after clear, got %q", *patron3.Address)
	}
}

// TestPatronCreateAutoSuffixesDuplicateUsername pins the collision
// retry loop inside CreatePatron's transaction: the second patron with
// the same auto-generated base gets suffix "2", the third "3", etc.
// Without the loop, a UNIQUE constraint violation on users.username
// would surface as a 500 to the admin.
func TestPatronCreateAutoSuffixesDuplicateUsername(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	// First patron -> "jsmith".
	if rr := postStaffForm(t, router, "/patrons", sess, csrf, validPatronFields()); rr.Code != http.StatusFound {
		t.Fatalf("first create: expected 302, got %d", rr.Code)
	}
	// Second patron with the same name -> "jsmith2".
	if rr := postStaffForm(t, router, "/patrons", sess, csrf, validPatronFields()); rr.Code != http.StatusFound {
		t.Fatalf("second create: expected 302, got %d", rr.Code)
	}

	if _, err := dm.GetUserByUsername("jsmith"); err != nil {
		t.Errorf("expected 'jsmith' to exist, got %v", err)
	}
	if _, err := dm.GetUserByUsername("jsmith2"); err != nil {
		t.Errorf("expected 'jsmith2' to exist after auto-suffix, got %v", err)
	}
}

// TestPatronCreateRejectsMissingName verifies the name-required
// validator. An empty name must redirect with flash_error and NOT
// create any rows.
func TestPatronCreateRejectsMissingName(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validPatronFields()
	fields["name"] = ""

	rr := postStaffForm(t, router, "/patrons", sess, csrf, fields)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 PRG redirect, got %d", rr.Code)
	}
	assertFlashErrorSet(t, rr)

	patrons, err := dm.GetAllPatrons()
	if err != nil {
		t.Fatalf("GetAllPatrons: %v", err)
	}
	if len(patrons) != 0 {
		t.Errorf("expected 0 patrons after rejected create, got %d", len(patrons))
	}
}

// TestPatronCreateRejectsUnusableName verifies the generateBaseUsername
// guard: names with zero alphanumerics (all punctuation) cannot produce
// a valid username and must be rejected at the handler level before
// the DB call would fail.
func TestPatronCreateRejectsUnusableName(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validPatronFields()
	fields["name"] = "!!!"

	rr := postStaffForm(t, router, "/patrons", sess, csrf, fields)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 PRG redirect, got %d", rr.Code)
	}
	assertFlashErrorSet(t, rr)

	patrons, err := dm.GetAllPatrons()
	if err != nil {
		t.Fatalf("GetAllPatrons: %v", err)
	}
	if len(patrons) != 0 {
		t.Errorf("expected 0 patrons after unusable-name rejection, got %d", len(patrons))
	}
}

// TestPatronCreateRejectsPasswordMismatch verifies the password/
// password_confirm match check fires before any hash write. Reuses
// the password_mismatch flash code from #39.
func TestPatronCreateRejectsPasswordMismatch(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validPatronFields()
	fields["password_confirm"] = "Different123!"

	rr := postStaffForm(t, router, "/patrons", sess, csrf, fields)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 PRG redirect, got %d", rr.Code)
	}
	assertFlashErrorSet(t, rr)

	if _, err := dm.GetUserByUsername("jsmith"); err != sql.ErrNoRows {
		t.Errorf("no user should have been created on password mismatch, got %v", err)
	}
}

// TestPatronCreateRejectsWeakPassword verifies ValidatePassword is
// enforced on patron create, same as on staff create.
func TestPatronCreateRejectsWeakPassword(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validPatronFields()
	fields["password"] = "weakpass"
	fields["password_confirm"] = "weakpass"

	rr := postStaffForm(t, router, "/patrons", sess, csrf, fields)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 PRG redirect, got %d", rr.Code)
	}
	assertFlashErrorSet(t, rr)

	if _, err := dm.GetUserByUsername("jsmith"); err != sql.ErrNoRows {
		t.Errorf("no user should have been created on weak password, got %v", err)
	}
}

// ---------- Edit handler ----------

// TestPatronEditHappyPath verifies POST /patrons/:id/edit updates the
// three editable fields (name, email, phone) and leaves the username
// untouched. Username-not-editable is part of the #21 design
// (rename via delete-recreate, not via edit) so this pin would fire
// if a future edit accidentally updates users.username.
func TestPatronEditHappyPath(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	patronID, username, err := dm.CreatePatron("Jane Smith", "jane@example.com", "555-1234", "", "fake-hash")
	if err != nil {
		t.Fatalf("seed CreatePatron: %v", err)
	}

	path := fmt.Sprintf("/patrons/%d/edit", patronID)
	rr := postStaffForm(t, router, path, sess, csrf, map[string]string{
		"name":  "Jane Doe",
		"email": "jane.doe@example.com",
		"phone": "555-9999",
	})

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	assertFlashSuccessSet(t, rr)

	after, err := dm.GetPatronByID(patronID)
	if err != nil {
		t.Fatalf("GetPatronByID: %v", err)
	}
	if after.Name != "Jane Doe" {
		t.Errorf("name: got %q, want %q", after.Name, "Jane Doe")
	}
	if after.Email == nil || *after.Email != "jane.doe@example.com" {
		t.Errorf("email not updated: %+v", after.Email)
	}
	if after.Phone == nil || *after.Phone != "555-9999" {
		t.Errorf("phone not updated: %+v", after.Phone)
	}
	if after.Username != username {
		t.Errorf("username must not change on edit: got %q, want %q", after.Username, username)
	}
}

// TestPatronEditReturns404ForMissing verifies the not-found branch on
// a non-existent patron id. Standard 404, not 500 or silent success.
func TestPatronEditReturns404ForMissing(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	rr := postStaffForm(t, router, "/patrons/99999/edit", sess, csrf, map[string]string{
		"name": "Nobody",
	})

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// TestPatronEditRejectsMissingName verifies the name-required
// validator on edit mirrors the one on create. Existing row is
// untouched on rejection.
func TestPatronEditRejectsMissingName(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	patronID, _, err := dm.CreatePatron("Jane Smith", "", "", "", "fake-hash")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	path := fmt.Sprintf("/patrons/%d/edit", patronID)
	rr := postStaffForm(t, router, path, sess, csrf, map[string]string{
		"name":  "",
		"email": "different@example.com",
	})

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	assertFlashErrorSet(t, rr)

	after, err := dm.GetPatronByID(patronID)
	if err != nil {
		t.Fatalf("GetPatronByID: %v", err)
	}
	if after.Name != "Jane Smith" {
		t.Errorf("name should be unchanged on rejected edit, got %q", after.Name)
	}
}

// ---------- Delete handler ----------

// TestPatronDeleteHappyPath verifies DeletePatron's three-table
// transaction: sessions wiped, users row removed, patrons row removed,
// all in one tx. Mirrors DeleteUser's session-wipe pin from #39.
func TestPatronDeleteHappyPath(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	patronID, username, err := dm.CreatePatron("Doomed Patron", "", "", "", "fake-hash")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	user, err := dm.GetUserByUsername(username)
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if err := dm.CreateSession("patron-sess", user.ID, "patron-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	path := fmt.Sprintf("/patrons/%d/delete", patronID)
	rr := postStaffForm(t, router, path, sess, csrf, nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/patrons" {
		t.Errorf("expected redirect to /patrons, got %q", loc)
	}
	assertFlashSuccessSet(t, rr)

	if _, err := dm.GetPatronByID(patronID); err != sql.ErrNoRows {
		t.Errorf("expected patron row deleted, got %v", err)
	}
	if _, err := dm.GetUserByUsername(username); err != sql.ErrNoRows {
		t.Errorf("expected users row deleted, got %v", err)
	}
	if _, err := dm.GetSession("patron-sess"); err != sql.ErrNoRows {
		t.Errorf("expected patron session wiped, got %v", err)
	}
}

// TestPatronDeleteRejectsWhenHasLoans pins the ErrPatronHasLoans
// guard. Seeds a loan row directly via dm.db.Exec because the loan
// handlers are CP6 work. Any loan row (active, returned, overdue)
// blocks delete so history survives.
func TestPatronDeleteRejectsWhenHasLoans(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	patronID, _, err := dm.CreatePatron("Patron With Loans", "", "", "", "fake-hash")
	if err != nil {
		t.Fatalf("seed patron: %v", err)
	}
	// Seed a book to reference from loans (foreign key).
	bookID, err := dm.CreateBook(
		&Book{Title: "Loaned Book"},
		[]string{"Seed Author"},
	)
	if err != nil {
		t.Fatalf("seed book: %v", err)
	}
	if _, err := dm.db.Exec(
		"INSERT INTO loans (book_id, patron_id, due_date) VALUES (?, ?, ?)",
		bookID, patronID, "2026-05-01 00:00:00",
	); err != nil {
		t.Fatalf("seed loan: %v", err)
	}

	path := fmt.Sprintf("/patrons/%d/delete", patronID)
	rr := postStaffForm(t, router, path, sess, csrf, nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 PRG redirect, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/patrons" {
		t.Errorf("expected redirect to /patrons, got %q", loc)
	}
	assertFlashErrorSet(t, rr)

	if _, err := dm.GetPatronByID(patronID); err != nil {
		t.Errorf("patron with loans should NOT be deleted, GetPatronByID: %v", err)
	}
}

// TestPatronDeleteReturns404ForMissing: delete of a nonexistent patron
// id must 404, not silently succeed or 500.
func TestPatronDeleteReturns404ForMissing(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	rr := postStaffForm(t, router, "/patrons/99999/delete", sess, csrf, nil)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPatronLoginCredentials_RendersTempPasswordPage(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")
	patronID, _, username, temp, err := dm.CreatePatronWithLogin("Alice Brown", "alice@example.com", "", "")
	if err != nil {
		t.Fatalf("CreatePatronWithLogin: %v", err)
	}

	req := httptest.NewRequest("GET", "/patrons/"+strconv.Itoa(patronID)+"/login-credentials", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, username) {
		t.Errorf("body should contain username %q", username)
	}
	// The temp password is rendered into HTML, so '&' becomes '&amp;'.
	// Compare against the escaped form to avoid a flaky substring check.
	if !strings.Contains(body, html.EscapeString(temp)) {
		t.Errorf("body should contain temp password")
	}
	if got := rr.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestPatronLoginCredentials_RedirectsWhenNoTemp(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")
	patronID, userID, _, _, err := dm.CreatePatronWithLogin("Charlie Davis", "", "", "")
	if err != nil {
		t.Fatalf("CreatePatronWithLogin: %v", err)
	}
	if err := dm.ClearTempPassword(userID); err != nil {
		t.Fatalf("ClearTempPassword: %v", err)
	}

	req := httptest.NewRequest("GET", "/patrons/"+strconv.Itoa(patronID)+"/login-credentials", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 redirect", rr.Code)
	}
	if got := rr.Header().Get("Location"); got != "/patrons" {
		t.Errorf("Location = %q, want /patrons", got)
	}
}

func TestPatronLoginCredentials_404ForUnknown(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	req := httptest.NewRequest("GET", "/patrons/99999/login-credentials", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestPatronDismissTemp_ClearsTempAndRedirects(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	patronID, userID, _, _, err := dm.CreatePatronWithLogin("Mark Delivered", "", "", "")
	if err != nil {
		t.Fatalf("CreatePatronWithLogin: %v", err)
	}

	rr := postStaffForm(t, router, "/patrons/"+strconv.Itoa(patronID)+"/dismiss-temp", sess, csrf, nil)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if got := rr.Header().Get("Location"); got != "/patrons" {
		t.Errorf("Location = %q, want /patrons", got)
	}

	user, _ := dm.GetUserByID(userID)
	if user.TempPassword != nil {
		t.Errorf("temp_password should be NULL after dismiss")
	}
}

func TestPatronRegenerateTemp_GeneratesNewAndRedirectsToCredentials(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	patronID, userID, _, originalTemp, err := dm.CreatePatronWithLogin("Bob Wilson", "", "", "")
	if err != nil {
		t.Fatalf("CreatePatronWithLogin: %v", err)
	}

	rr := postStaffForm(t, router, "/patrons/"+strconv.Itoa(patronID)+"/regenerate-temp", sess, csrf, nil)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	wantLoc := "/patrons/" + strconv.Itoa(patronID) + "/login-credentials"
	if got := rr.Header().Get("Location"); got != wantLoc {
		t.Errorf("Location = %q, want %q", got, wantLoc)
	}

	user, _ := dm.GetUserByID(userID)
	if user.TempPassword == nil || *user.TempPassword == originalTemp {
		t.Errorf("temp_password should be new and non-empty, got %v", user.TempPassword)
	}
	if !user.MustChangePassword {
		t.Errorf("must_change_password should be true after regenerate")
	}
}
