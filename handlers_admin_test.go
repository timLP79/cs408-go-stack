// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"archive/zip"
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// postBackupImport posts a multipart form to /admin/backup/import.
// fields are extra string fields beyond csrf_token (e.g. "confirm": "on").
// zipBytes becomes the backup_zip file part. Returns the response.
func postBackupImport(t *testing.T, router *gin.Engine, sess *http.Cookie, csrf string, zipBytes []byte, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("csrf_token", csrf); err != nil {
		t.Fatalf("write csrf: %v", err)
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %q: %v", k, err)
		}
	}
	fw, err := w.CreateFormFile("backup_zip", "backup.zip")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(zipBytes); err != nil {
		t.Fatalf("write zip body: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := httptest.NewRequest("POST", "/admin/backup/import", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// TestAdminToolsIndexRenders pins the /admin tools-index page so a
// future template edit that breaks the page surfaces here. Admin
// only.
func TestAdminToolsIndexRenders(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	req := httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Backup and Restore") {
		t.Errorf("admin index missing Backup card; body=%s", body)
	}
}

func TestBackupExport_Happy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	// Drop one real cover and one .bak in the covers dir under DATA_DIR.
	coversPath := filepath.Join(os.Getenv("DATA_DIR"), "covers")
	if err := os.MkdirAll(coversPath, 0o755); err != nil {
		t.Fatalf("mkdir covers: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coversPath, "real.jpg"), []byte("\xff\xd8\xff\xe0fake"), 0o644); err != nil {
		t.Fatalf("write real cover: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coversPath, "stale.jpg.bak"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write bak: %v", err)
	}

	req := httptest.NewRequest("GET", "/admin/backup/export", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	cd := rr.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "libreshelf-backup-") || !strings.Contains(cd, ".zip") {
		t.Errorf("Content-Disposition = %q, expected libreshelf-backup-*.zip", cd)
	}

	body := rr.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	var foundDB, foundReal bool
	for _, f := range zr.File {
		switch {
		case f.Name == "database.sqlite":
			foundDB = true
			if f.UncompressedSize64 == 0 {
				t.Errorf("database.sqlite is empty")
			}
		case f.Name == "covers/real.jpg":
			foundReal = true
		case strings.HasSuffix(f.Name, ".bak"):
			t.Errorf("backup ZIP contains a .bak file: %q", f.Name)
		}
	}
	if !foundDB {
		t.Errorf("backup ZIP missing database.sqlite")
	}
	if !foundReal {
		t.Errorf("backup ZIP missing covers/real.jpg")
	}
}

func TestBackupExport_RejectsNonAdmin(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staffuser", "staff")

	req := httptest.NewRequest("GET", "/admin/backup/export", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Errorf("staff user should not be able to export backup; got 200")
	}
}

func TestBackupImport_Happy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	initialCount, err := dm.CountBooks()
	if err != nil {
		t.Fatalf("initial count: %v", err)
	}
	if initialCount == 0 {
		t.Fatalf("expected seeded books, got 0")
	}

	// Export the current state.
	req := httptest.NewRequest("GET", "/admin/backup/export", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("export status = %d", rr.Code)
	}
	backupZip := rr.Body.Bytes()

	// Mutate: delete one book and its copies (FK ordering).
	var victimID int
	if err := dm.db.QueryRow(`SELECT MIN(id) FROM books`).Scan(&victimID); err != nil {
		t.Fatalf("pick victim book: %v", err)
	}
	if _, err := dm.db.Exec(`DELETE FROM copies WHERE book_id = ?`, victimID); err != nil {
		t.Fatalf("delete copies: %v", err)
	}
	if _, err := dm.db.Exec(`DELETE FROM books WHERE id = ?`, victimID); err != nil {
		t.Fatalf("delete book: %v", err)
	}
	if c, _ := dm.CountBooks(); c == initialCount {
		t.Fatalf("delete didn't change count: %d", c)
	}

	// Import the saved export -- should restore the deleted book.
	rr2 := postBackupImport(t, router, sess, csrf, backupZip, map[string]string{"confirm": "on"})
	if rr2.Code != http.StatusSeeOther {
		t.Fatalf("import status = %d, want 303; body=%s", rr2.Code, rr2.Body.String())
	}

	if c, _ := dm.CountBooks(); c != initialCount {
		t.Errorf("post-import count = %d, want %d", c, initialCount)
	}

	// Session preserved across the swap -- admin cookie still valid.
	req3 := httptest.NewRequest("GET", "/admin/backup", nil)
	req3.AddCookie(sess)
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Errorf("post-import GET /admin/backup = %d, want 200; session not preserved", rr3.Code)
	}

	// .bak files preserved per DEC-027.
	if _, err := os.Stat(dm.dbPath + ".bak"); err != nil {
		t.Errorf("expected database.sqlite.bak after successful import, got %v", err)
	}
}

func TestBackupImport_RejectsZipSlip(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	f, _ := zw.Create("../evil.txt")
	f.Write([]byte("pwned"))
	zw.Close()

	rr := postBackupImport(t, router, sess, csrf, zb.Bytes(), map[string]string{"confirm": "on"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	// No swap should have occurred -- no .bak file.
	if _, err := os.Stat(dm.dbPath + ".bak"); err == nil {
		t.Errorf("rejected import should not have created %s", dm.dbPath+".bak")
	}
	// Server still operational.
	req := httptest.NewRequest("GET", "/admin/backup", nil)
	req.AddCookie(sess)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Errorf("server unhealthy after rejected zipslip import: %d", rr2.Code)
	}
}

func TestBackupImport_RequiresConfirmCheckbox(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	f, _ := zw.Create("database.sqlite")
	f.Write([]byte("not a sqlite"))
	zw.Close()

	rr := postBackupImport(t, router, sess, csrf, zb.Bytes(), nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Confirmation") {
		t.Errorf("body = %q, expected mention of Confirmation", rr.Body.String())
	}
}

func TestBackupImport_RejectsMissingDatabase(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	f, _ := zw.Create("covers/test.jpg")
	f.Write([]byte("\xff\xd8\xff"))
	zw.Close()

	rr := postBackupImport(t, router, sess, csrf, zb.Bytes(), map[string]string{"confirm": "on"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "database.sqlite") {
		t.Errorf("body = %q, expected mention of database.sqlite", rr.Body.String())
	}
}

func TestBackupImport_RejectsNonAdmin(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staffuser", "staff")

	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	zw.Close()

	rr := postBackupImport(t, router, sess, csrf, zb.Bytes(), map[string]string{"confirm": "on"})
	if rr.Code == http.StatusOK || rr.Code == http.StatusSeeOther {
		t.Errorf("non-admin should not be able to import; got %d", rr.Code)
	}
}
