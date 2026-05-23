// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// setupTestDB builds a fresh DatabaseManager against a temporary SQLite
// file. The temp directory is cleaned up automatically when the test
// finishes.
func setupTestDB(t *testing.T) *DatabaseManager {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "libreshelf-db-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	return NewDatabaseManager(tmpDir + "/test.sqlite")
}

// mustCreateUser is a test helper that inserts a user with a known
// bcrypt-hashed password and returns the row's id. Tests that only care
// about a user existing use this to skip the hash-and-fetch boilerplate.
func mustCreateUser(t *testing.T, dm *DatabaseManager, username, role string) int {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("Pw123456!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	if err := dm.CreateUser(username, string(hash), role, nil); err != nil {
		t.Fatalf("CreateUser(%q, %q): %v", username, role, err)
	}
	user, err := dm.GetUserByUsername(username)
	if err != nil {
		t.Fatalf("GetUserByUsername(%q): %v", username, err)
	}
	return user.ID
}

// TestGetAllStaffReturnsOnlyAdminAndStaff verifies patrons are excluded
// from the staff list. A regression where patrons leak in would expose
// patron account existence on the /staff page and blur the role boundary
// established in CP4 (DEC-014).
func TestGetAllStaffReturnsOnlyAdminAndStaff(t *testing.T) {
	dm := setupTestDB(t)
	mustCreateUser(t, dm, "admin1", "admin")
	mustCreateUser(t, dm, "staff_alice", "staff")
	mustCreateUser(t, dm, "patron_bob", "patron")

	staff, err := dm.GetAllStaff()
	if err != nil {
		t.Fatalf("GetAllStaff: %v", err)
	}
	if len(staff) != 2 {
		t.Fatalf("expected 2 staff, got %d", len(staff))
	}
	for _, u := range staff {
		if u.Role == "patron" {
			t.Errorf("patron %q leaked into staff list", u.Username)
		}
	}
}

// TestGetAllStaffOrdering verifies admin rows come before staff rows,
// and within a role usernames are alphabetical. The staff.html template
// relies on this ordering to render the table without its own sort.
func TestGetAllStaffOrdering(t *testing.T) {
	dm := setupTestDB(t)
	mustCreateUser(t, dm, "staff_zach", "staff")
	mustCreateUser(t, dm, "admin_bob", "admin")
	mustCreateUser(t, dm, "staff_alice", "staff")
	mustCreateUser(t, dm, "admin_alice", "admin")

	staff, err := dm.GetAllStaff()
	if err != nil {
		t.Fatalf("GetAllStaff: %v", err)
	}

	expected := []string{"admin_alice", "admin_bob", "staff_alice", "staff_zach"}
	if len(staff) != len(expected) {
		t.Fatalf("expected %d staff, got %d", len(expected), len(staff))
	}
	for i, u := range staff {
		if u.Username != expected[i] {
			t.Errorf("position %d: expected %q, got %q", i, expected[i], u.Username)
		}
	}
}

// TestGetUserByIDHit verifies a round-trip lookup returns the same row
// that CreateUser inserted.
func TestGetUserByIDHit(t *testing.T) {
	dm := setupTestDB(t)
	id := mustCreateUser(t, dm, "admin1", "admin")

	user, err := dm.GetUserByID(id)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user.Username != "admin1" {
		t.Errorf("expected username admin1, got %q", user.Username)
	}
	if user.Role != "admin" {
		t.Errorf("expected role admin, got %q", user.Role)
	}
}

// TestGetUserByIDMiss verifies that a missing id returns sql.ErrNoRows
// so the caller can distinguish "not found" from "db error" and respond
// with 404 instead of 500.
func TestGetUserByIDMiss(t *testing.T) {
	dm := setupTestDB(t)

	if _, err := dm.GetUserByID(99999); err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestUpdateStaffUserUpdates verifies a successful rename + role change
// applies both fields in a single call (DEC-020: combined edit endpoint).
func TestUpdateStaffUserUpdates(t *testing.T) {
	dm := setupTestDB(t)
	id := mustCreateUser(t, dm, "staff1", "staff")

	if err := dm.UpdateStaffUser(id, "renamed", "admin"); err != nil {
		t.Fatalf("UpdateStaffUser: %v", err)
	}

	user, err := dm.GetUserByID(id)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user.Username != "renamed" {
		t.Errorf("expected username renamed, got %q", user.Username)
	}
	if user.Role != "admin" {
		t.Errorf("expected role admin, got %q", user.Role)
	}
}

// TestUpdateStaffUserRejectsDuplicateUsername verifies the UNIQUE
// constraint on users.username surfaces as an error to the caller. A
// regression here (e.g. swallowed error) would let the handler report
// "success" while the DB silently rejected the write.
func TestUpdateStaffUserRejectsDuplicateUsername(t *testing.T) {
	dm := setupTestDB(t)
	mustCreateUser(t, dm, "taken", "staff")
	id := mustCreateUser(t, dm, "rename_me", "staff")

	if err := dm.UpdateStaffUser(id, "taken", "staff"); err == nil {
		t.Fatal("expected error on duplicate username, got nil")
	}
}

// TestDeleteUserRemovesSessions verifies the transactional DeleteUser
// wipes both the user row and any sessions pointing at it. Without the
// session delete, the foreign-key constraint would reject the user
// delete, so "user still present" is also a regression signal.
func TestDeleteUserRemovesSessions(t *testing.T) {
	dm := setupTestDB(t)
	id := mustCreateUser(t, dm, "soon_gone", "staff")

	if err := dm.CreateSession("tok-test", id, "csrf-test", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := dm.DeleteUser(id); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := dm.GetUserByID(id); err != sql.ErrNoRows {
		t.Errorf("expected user deleted (ErrNoRows), got %v", err)
	}
	if _, err := dm.GetSession("tok-test"); err != sql.ErrNoRows {
		t.Errorf("expected session deleted (ErrNoRows), got %v", err)
	}
}

// TestCountAdminsReflectsCurrentState exercises the three operations the
// last-admin guard depends on: creation, demotion, and pure lookup. A
// silent regression here would let the last admin be deleted or demoted
// even though the guard "runs."
func TestCountAdminsReflectsCurrentState(t *testing.T) {
	dm := setupTestDB(t)

	count, err := dm.CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins (empty): %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 admins initially, got %d", count)
	}

	id1 := mustCreateUser(t, dm, "admin1", "admin")
	mustCreateUser(t, dm, "admin2", "admin")
	mustCreateUser(t, dm, "staff1", "staff")

	count, err = dm.CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins (after seeds): %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 admins, got %d", count)
	}

	if err := dm.UpdateStaffUser(id1, "admin1", "staff"); err != nil {
		t.Fatalf("UpdateStaffUser (demote): %v", err)
	}

	count, err = dm.CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins (after demote): %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 admin after demotion, got %d", count)
	}
}

func TestUpdateBookCover(t *testing.T) {
	dm := setupTestDB(t)

	isbn := "9780000000001"
	id, err := dm.CreateBook(&Book{
		Title: "Test Book",
		ISBN:  &isbn,
	}, []string{"Author"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	if err := dm.UpdateBookCover(id, "abc123.jpg"); err != nil {
		t.Fatalf("UpdateBookCover: %v", err)
	}
	got, err := dm.GetBookByID(id)
	if err != nil {
		t.Fatalf("GetBookByID: %v", err)
	}
	if got.CoverFilename == nil || *got.CoverFilename != "abc123.jpg" {
		t.Errorf("CoverFilename = %v, want abc123.jpg", got.CoverFilename)
	}

	if err := dm.UpdateBookCover(id, "xyz999.png"); err != nil {
		t.Fatalf("UpdateBookCover (second): %v", err)
	}
	got, _ = dm.GetBookByID(id)
	if got.CoverFilename == nil || *got.CoverFilename != "xyz999.png" {
		t.Errorf("after second update, CoverFilename = %v, want xyz999.png", got.CoverFilename)
	}

	// Update of a non-existent id is a no-op (UPDATE with no matching
	// row does not error in SQLite).
	if err := dm.UpdateBookCover(999999, "ignored.jpg"); err != nil {
		t.Errorf("UpdateBookCover on missing id should be a no-op, got %v", err)
	}
}

func TestUpdateBookHappy(t *testing.T) {
	dm := setupTestDB(t)
	isbn := "9780000111111"
	id, err := dm.CreateBook(&Book{
		Title: "Original",
		ISBN:  &isbn,
	}, []string{"First Author"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}

	newTitle := "Updated"
	newISBN := "9780000222222"
	year := 2026
	if err := dm.UpdateBook(id, &Book{
		Title: newTitle,
		ISBN:  &newISBN,
		Year:  &year,
	}, []string{"Author A", "Author B"}); err != nil {
		t.Fatalf("UpdateBook: %v", err)
	}

	got, err := dm.GetBookByID(id)
	if err != nil {
		t.Fatalf("GetBookByID: %v", err)
	}
	if got.Title != newTitle {
		t.Errorf("Title = %q, want %q", got.Title, newTitle)
	}
	if got.ISBN == nil || *got.ISBN != newISBN {
		t.Errorf("ISBN = %v, want %s", got.ISBN, newISBN)
	}
	if !strings.Contains(got.Authors, "Author A") || !strings.Contains(got.Authors, "Author B") {
		t.Errorf("Authors = %q, want both Author A and Author B", got.Authors)
	}
}

func TestGetLoanHistoryStatuses(t *testing.T) {
	dm := setupTestDB(t)
	isbn := "9780000333333"
	bookID, err := dm.CreateBook(&Book{
		Title: "T", ISBN: &isbn,
	}, []string{"A"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, _, err := dm.AddLibraryCopy(bookID); err != nil {
			t.Fatalf("AddLibraryCopy %d: %v", i, err)
		}
	}
	copies, err := dm.GetCopiesByBookID(bookID)
	if err != nil || len(copies) < 3 {
		t.Fatalf("expected at least 3 copies, got %d (err %v)", len(copies), err)
	}
	patronID := mustCreateUser(t, dm, "p_history", "patron")
	if _, err := dm.db.Exec(`INSERT INTO patrons (id, name) VALUES (?, ?)`, patronID, "Test Patron"); err != nil {
		t.Fatalf("seed patron: %v", err)
	}

	// Active loan: due in the future, not returned.
	if _, err := dm.db.Exec(
		`INSERT INTO loans (copy_id, patron_id, due_date) VALUES (?, ?, DATE('now', '+14 days'))`,
		copies[0].ID, patronID,
	); err != nil {
		t.Fatalf("active loan: %v", err)
	}
	// Overdue loan: due in the past, not returned.
	if _, err := dm.db.Exec(
		`INSERT INTO loans (copy_id, patron_id, due_date) VALUES (?, ?, DATE('now', '-3 days'))`,
		copies[1].ID, patronID,
	); err != nil {
		t.Fatalf("overdue loan: %v", err)
	}
	// Returned loan: returned_at set. Reuse copy[0]; historical row.
	if _, err := dm.db.Exec(
		`INSERT INTO loans (copy_id, patron_id, due_date, returned_at) VALUES (?, ?, DATE('now', '-30 days'), DATETIME('now', '-25 days'))`,
		copies[2].ID, patronID,
	); err != nil {
		t.Fatalf("returned loan: %v", err)
	}

	records, err := dm.GetLoanHistory(bookID)
	if err != nil {
		t.Fatalf("GetLoanHistory: %v", err)
	}
	statuses := map[string]int{}
	for _, r := range records {
		statuses[r.Status]++
	}
	if statuses["active"] != 1 {
		t.Errorf("active count = %d, want 1; statuses=%v", statuses["active"], statuses)
	}
	if statuses["overdue"] != 1 {
		t.Errorf("overdue count = %d, want 1; statuses=%v", statuses["overdue"], statuses)
	}
	if statuses["returned"] != 1 {
		t.Errorf("returned count = %d, want 1; statuses=%v", statuses["returned"], statuses)
	}
}

func TestSetMustChangePassword(t *testing.T) {
	dm := setupTestDB(t)
	id := mustCreateUser(t, dm, "alice", "patron")

	user, err := dm.GetUserByID(id)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user.MustChangePassword {
		t.Errorf("must_change_password should default to false")
	}

	if err := dm.SetMustChangePassword(id); err != nil {
		t.Fatalf("SetMustChangePassword: %v", err)
	}

	user, err = dm.GetUserByID(id)
	if err != nil {
		t.Fatalf("GetUserByID after set: %v", err)
	}
	if !user.MustChangePassword {
		t.Errorf("must_change_password should be true after Set")
	}
}

func TestUpdateUserPasswordClearsMustChangeAndTempPassword(t *testing.T) {
	dm := setupTestDB(t)
	_, userID, _, tempPassword, err := dm.CreatePatronWithLogin("Charlie Brown", "", "", "")
	if err != nil {
		t.Fatalf("CreatePatronWithLogin: %v", err)
	}
	if tempPassword == "" {
		t.Fatalf("expected non-empty temp password")
	}

	user, _ := dm.GetUserByID(userID)
	if !user.MustChangePassword {
		t.Fatalf("expected MustChangePassword=true after CreatePatronWithLogin")
	}
	if user.TempPassword == nil || *user.TempPassword != tempPassword {
		t.Fatalf("expected temp_password column populated with %q, got %v", tempPassword, user.TempPassword)
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte("NewPass1!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if err := dm.UpdateUserPassword(userID, string(newHash)); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}

	user, _ = dm.GetUserByID(userID)
	if user.MustChangePassword {
		t.Errorf("must_change_password should clear after UpdateUserPassword")
	}
	if user.TempPassword != nil {
		t.Errorf("temp_password should clear after UpdateUserPassword, got %v", *user.TempPassword)
	}
}

func TestCreatePatronWithLogin_SetsTempAndFlag(t *testing.T) {
	dm := setupTestDB(t)
	patronID, userID, username, tempPassword, err := dm.CreatePatronWithLogin("Alice Brown", "alice@example.com", "555-0101", `{"external_id":"LC-0001"}`)
	if err != nil {
		t.Fatalf("CreatePatronWithLogin: %v", err)
	}
	if patronID == 0 || userID == 0 {
		t.Fatalf("expected non-zero IDs, got patronID=%d userID=%d", patronID, userID)
	}
	if username != "abrown" {
		t.Errorf("username = %q, want %q", username, "abrown")
	}
	if err := ValidatePassword(tempPassword); err != nil {
		t.Errorf("temp password fails ValidatePassword: %v", err)
	}

	user, _ := dm.GetUserByID(userID)
	if !user.MustChangePassword {
		t.Errorf("must_change_password should be true on import")
	}
	if user.TempPassword == nil || *user.TempPassword != tempPassword {
		t.Errorf("temp_password column should equal returned plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(tempPassword)); err != nil {
		t.Errorf("returned plaintext should match stored hash: %v", err)
	}
}

func TestCreatePatronNoLogin_OnlyPatronRow(t *testing.T) {
	dm := setupTestDB(t)
	patronID, err := dm.CreatePatronNoLogin("John Smith", "", "", `{"external_id":"IDOC123"}`)
	if err != nil {
		t.Fatalf("CreatePatronNoLogin: %v", err)
	}
	if patronID == 0 {
		t.Fatalf("expected non-zero patronID")
	}

	patron, err := dm.GetPatronByID(patronID)
	if err != nil {
		t.Fatalf("GetPatronByID: %v", err)
	}
	if patron.Username != "" {
		t.Errorf("expected empty Username for no-login patron, got %q", patron.Username)
	}
	if patron.HasTempPassword {
		t.Errorf("expected HasTempPassword=false for no-login patron")
	}
}

func TestFindPatronByExternalID(t *testing.T) {
	dm := setupTestDB(t)
	wantID, err := dm.CreatePatronNoLogin("Jane Doe", "", "", `{"external_id":"IDOC234567"}`)
	if err != nil {
		t.Fatalf("CreatePatronNoLogin: %v", err)
	}

	hit, err := dm.FindPatronByExternalID("IDOC234567")
	if err != nil {
		t.Fatalf("FindPatronByExternalID hit: %v", err)
	}
	if hit == nil || hit.ID != wantID {
		t.Errorf("expected hit ID=%d, got %v", wantID, hit)
	}

	miss, err := dm.FindPatronByExternalID("IDOC999999")
	if err != nil {
		t.Fatalf("FindPatronByExternalID miss: %v", err)
	}
	if miss != nil {
		t.Errorf("expected nil for miss, got %v", miss)
	}

	empty, err := dm.FindPatronByExternalID("")
	if err != nil {
		t.Fatalf("FindPatronByExternalID empty: %v", err)
	}
	if empty != nil {
		t.Errorf("expected nil for empty input, got %v", empty)
	}
}

func TestFindPatronByEmail(t *testing.T) {
	dm := setupTestDB(t)
	wantID, err := dm.CreatePatronNoLogin("Bob Wilson", "bob@example.com", "", "")
	if err != nil {
		t.Fatalf("CreatePatronNoLogin: %v", err)
	}

	hit, err := dm.FindPatronByEmail("bob@example.com")
	if err != nil {
		t.Fatalf("FindPatronByEmail hit: %v", err)
	}
	if hit == nil || hit.ID != wantID {
		t.Errorf("expected hit ID=%d, got %v", wantID, hit)
	}

	miss, err := dm.FindPatronByEmail("nobody@example.com")
	if err != nil {
		t.Fatalf("FindPatronByEmail miss: %v", err)
	}
	if miss != nil {
		t.Errorf("expected nil for miss, got %v", miss)
	}
}

func TestGetAllPatrons_IncludesNoLoginPatron(t *testing.T) {
	dm := setupTestDB(t)
	noLoginID, err := dm.CreatePatronNoLogin("Records Only", "", "", "")
	if err != nil {
		t.Fatalf("CreatePatronNoLogin: %v", err)
	}
	loginPatronID, _, _, _, err := dm.CreatePatronWithLogin("With Login", "", "", "")
	if err != nil {
		t.Fatalf("CreatePatronWithLogin: %v", err)
	}

	patrons, err := dm.GetAllPatrons()
	if err != nil {
		t.Fatalf("GetAllPatrons: %v", err)
	}
	if len(patrons) != 2 {
		t.Fatalf("expected 2 patrons, got %d", len(patrons))
	}

	byID := map[int]Patron{}
	for _, p := range patrons {
		byID[p.ID] = p
	}
	noLogin, ok := byID[noLoginID]
	if !ok {
		t.Fatalf("no-login patron missing from list")
	}
	if noLogin.Username != "" {
		t.Errorf("no-login Username = %q, want empty", noLogin.Username)
	}
	if noLogin.HasTempPassword {
		t.Errorf("no-login HasTempPassword should be false")
	}
	withLogin := byID[loginPatronID]
	if withLogin.Username == "" {
		t.Errorf("with-login Username should be non-empty")
	}
	if !withLogin.HasTempPassword {
		t.Errorf("with-login HasTempPassword should be true (just imported)")
	}
}

func TestClearTempPassword(t *testing.T) {
	dm := setupTestDB(t)
	_, userID, _, _, err := dm.CreatePatronWithLogin("Mark Delivered", "", "", "")
	if err != nil {
		t.Fatalf("CreatePatronWithLogin: %v", err)
	}

	if err := dm.ClearTempPassword(userID); err != nil {
		t.Fatalf("ClearTempPassword: %v", err)
	}

	user, _ := dm.GetUserByID(userID)
	if user.TempPassword != nil {
		t.Errorf("temp_password should be NULL after Clear, got %v", *user.TempPassword)
	}
	if !user.MustChangePassword {
		t.Errorf("must_change_password should remain true after Clear (admin-dismissed != patron-changed)")
	}
}

func TestRegenerateTempPassword_NewTempReplacesOldAndWipesSessions(t *testing.T) {
	dm := setupTestDB(t)
	_, userID, _, originalTemp, err := dm.CreatePatronWithLogin("Bob Wilson", "", "", "")
	if err != nil {
		t.Fatalf("CreatePatronWithLogin: %v", err)
	}
	if err := dm.CreateSession("session-pre-regen", userID, "csrf-pre", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	newTemp, err := dm.RegenerateTempPassword(userID)
	if err != nil {
		t.Fatalf("RegenerateTempPassword: %v", err)
	}
	if newTemp == "" || newTemp == originalTemp {
		t.Errorf("expected new non-empty temp distinct from %q, got %q", originalTemp, newTemp)
	}

	user, _ := dm.GetUserByID(userID)
	if user.TempPassword == nil || *user.TempPassword != newTemp {
		t.Errorf("temp_password should equal newTemp, got %v", user.TempPassword)
	}
	if !user.MustChangePassword {
		t.Errorf("must_change_password should be true after regenerate")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(originalTemp)); err == nil {
		t.Errorf("original temp should NO LONGER match stored hash after regenerate")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(newTemp)); err != nil {
		t.Errorf("new temp should match stored hash: %v", err)
	}

	if _, err := dm.GetSession("session-pre-regen"); err != sql.ErrNoRows {
		t.Errorf("expected pre-regen session deleted, got %v", err)
	}
}

func TestSettings_GetReturnsEmptyForUnset(t *testing.T) {
	dm := setupTestDB(t)
	v, err := dm.GetSetting("never_set")
	if err != nil {
		t.Fatalf("GetSetting on unset key: %v", err)
	}
	if v != "" {
		t.Errorf("expected empty string for unset key, got %q", v)
	}
}

func TestSettings_SetThenGetRoundtrip(t *testing.T) {
	dm := setupTestDB(t)
	adminID := mustCreateUser(t, dm, "admin1", "admin")

	if err := dm.SetSetting("staff_can_import_patrons", "true", adminID); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	v, err := dm.GetSetting("staff_can_import_patrons")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if v != "true" {
		t.Errorf("expected 'true', got %q", v)
	}

	if err := dm.SetSetting("staff_can_import_patrons", "false", adminID); err != nil {
		t.Fatalf("SetSetting overwrite: %v", err)
	}
	v, _ = dm.GetSetting("staff_can_import_patrons")
	if v != "false" {
		t.Errorf("expected 'false' after overwrite, got %q", v)
	}
}

func TestSettings_GetBoolDefaults(t *testing.T) {
	dm := setupTestDB(t)
	adminID := mustCreateUser(t, dm, "admin1", "admin")

	if dm.GetSettingBool("nope", true) != true {
		t.Errorf("expected default=true for unset key")
	}
	if dm.GetSettingBool("nope", false) != false {
		t.Errorf("expected default=false for unset key")
	}

	_ = dm.SetSetting("on_key", "true", adminID)
	if !dm.GetSettingBool("on_key", false) {
		t.Errorf("expected true for 'true' value")
	}
	_ = dm.SetSetting("off_key", "false", adminID)
	if dm.GetSettingBool("off_key", true) {
		t.Errorf("expected false for 'false' value")
	}
	_ = dm.SetSetting("garbage_key", "yes", adminID)
	if dm.GetSettingBool("garbage_key", true) {
		t.Errorf("expected default(true) for malformed value 'yes' -- only 'true' (case-insensitive) returns true")
	}
}

func TestGenerateTempPasswordSatisfiesPolicy(t *testing.T) {
	for i := 0; i < 50; i++ {
		pw, err := generateTempPassword()
		if err != nil {
			t.Fatalf("generateTempPassword: %v", err)
		}
		if err := ValidatePassword(pw); err != nil {
			t.Errorf("generated %q failed ValidatePassword: %v", pw, err)
		}
		if strings.ContainsAny(pw, "0Oo1lI") {
			t.Errorf("generated %q contains an excluded ambiguous character", pw)
		}
	}
}

func TestFetchAndStoreSeedCovers_OfflineSkipsWithoutHTTP(t *testing.T) {
	dm := setupTestDB(t)
	adminID := mustCreateUser(t, dm, "admin_seed_off", "admin")
	if err := dm.SetSetting("offline_mode", "true", adminID); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	// Insert a book with ISBN but no cover so the function has work to
	// do if it ignored offline mode.
	_, err := dm.db.Exec(`INSERT INTO books (title, isbn) VALUES (?, ?)`,
		"Offline Test Book", "9780000000000")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Hit the function with a short-fused context (100ms) so any HTTP
	// attempt that slips through the gate eventually times out rather than
	// blocking the test suite. The offline short-circuit must fire before
	// the request loop.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	dm.FetchAndStoreSeedCovers(ctx)
	elapsed := time.Since(start)

	// The offline gate returns in microseconds. Any HTTP attempt (even one
	// that bails at DNS or connection setup) takes at least several
	// milliseconds. If this assertion fails, the gate was likely removed
	// and the function fell through to the per-book HTTP loop.
	if elapsed > 200*time.Millisecond {
		t.Errorf("offline gate should return in <200ms, took %v (HTTP attempt slipped through?)", elapsed)
	}

	// Secondary check: verify the book still has no cover (the function did not
	// reach the save step).
	var coverFilename *string
	if err := dm.db.QueryRow(
		`SELECT cover_filename FROM books WHERE isbn = ?`,
		"9780000000000",
	).Scan(&coverFilename); err != nil {
		t.Fatalf("select cover: %v", err)
	}
	if coverFilename != nil {
		t.Errorf("offline mode allowed a cover to be written: %q", *coverFilename)
	}
}
