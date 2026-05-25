// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// postBookMultipart POSTs a multipart/form-data body to path. The book
// form always submits multipart so HandleBookCreate can accept a cover
// upload; tests must match that content type or ParseMultipartForm
// returns ErrNotMultipart and the handler rejects the request as
// "Invalid form submission."
//
// If coverFilename is empty, no file part is attached. Otherwise,
// coverBytes is written as that file under form field "cover".
func postBookMultipart(t *testing.T, router *gin.Engine, path string, sess *http.Cookie, csrf string, fields map[string]string, coverFilename string, coverBytes []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("csrf_token", csrf); err != nil {
		t.Fatalf("WriteField csrf_token: %v", err)
	}
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("WriteField %s: %v", k, err)
		}
	}

	if coverFilename != "" {
		part, err := writer.CreateFormFile("cover", coverFilename)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := part.Write(coverBytes); err != nil {
			t.Fatalf("cover part write: %v", err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}

	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// validBookFields returns a minimal-valid field set for HandleBookCreate.
// Tests copy and mutate individual fields to exercise single-field
// validation failures without respelling the full form each time.
//
// Quantity is no longer a book-level form field (DEC-037); inventory is
// added per-copy via the add-a-copy flow after the book exists.
func validBookFields() map[string]string {
	return map[string]string{
		"title":   "New Book",
		"authors": "Example Author",
	}
}

// minimalJPEG is enough bytes for http.DetectContentType to classify as
// image/jpeg (signature is \xFF\xD8\xFF) and for SaveUploadedCover to
// write a non-empty file to disk. Not a valid image; tests only care
// about the sniff + write paths, not image decoding.
var minimalJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xDB}, bytes.Repeat([]byte{0x00}, 128)...)

// minimalPNG is the PNG magic bytes plus padding. Used to drive the
// "extension says jpg but contents are png" MIME-mismatch test.
var minimalPNG = append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0x00}, 128)...)

// ---------- Happy paths ----------

// TestBookCreateHappyPath verifies minimal-valid POST /books inserts the
// book, links exactly one author, and redirects to /books/:id with a
// success flash. Pins the end-to-end path: multipart parse, validation
// chain, CreateBook transaction, PRG redirect, flash cookies.
func TestBookCreateHappyPath(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	rr := postBookMultipart(t, router, "/books", sess, csrf, validBookFields(), "", nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/books/") || loc == "/books/new" {
		t.Errorf("expected redirect to /books/:id, got %q", loc)
	}
	assertNoFlashError(t, rr)
	assertFlashSuccessSet(t, rr)

	// Minimal path sends no ISBN, so query by title via GetAllBooks.
	books, err := dm.GetAllBooks()
	if err != nil {
		t.Fatalf("GetAllBooks: %v", err)
	}
	found := false
	for _, b := range books {
		if b.Title == "New Book" && b.Authors == "Example Author" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'New Book' with author 'Example Author' in catalog, got %+v", books)
	}
}

// TestBookCreateHappyPathFullFields verifies all optional fields (ISBN,
// year, publisher, genre, description) are persisted when provided.
// Without this, a regression that silently dropped an optional field
// would only surface in manual testing.
func TestBookCreateHappyPathFullFields(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validBookFields()
	fields["title"] = "Full Book"
	fields["isbn"] = "9781234567897"
	fields["year"] = "1999"
	fields["publisher"] = "Test House"
	fields["genre"] = "Test Genre"
	fields["description"] = "A book used as a full-fields test fixture."

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}

	book, err := dm.GetBookByISBN("9781234567897")
	if err != nil {
		t.Fatalf("GetBookByISBN: %v", err)
	}
	// GetBookByISBN only selects id+title; fetch the full row via GetBookByID.
	full, err := dm.GetBookByID(book.ID)
	if err != nil {
		t.Fatalf("GetBookByID: %v", err)
	}
	if full.Title != "Full Book" {
		t.Errorf("title: got %q", full.Title)
	}
	if full.ISBN == nil || *full.ISBN != "9781234567897" {
		t.Errorf("ISBN not stored: %+v", full.ISBN)
	}
	if full.Year == nil || *full.Year != 1999 {
		t.Errorf("year not stored: %+v", full.Year)
	}
	if full.Publisher == nil || *full.Publisher != "Test House" {
		t.Errorf("publisher not stored: %+v", full.Publisher)
	}
	if full.Genre == nil || *full.Genre != "Test Genre" {
		t.Errorf("genre not stored: %+v", full.Genre)
	}
	if full.Description == nil || !strings.Contains(*full.Description, "full-fields test fixture") {
		t.Errorf("description not stored: %+v", full.Description)
	}
}

// TestBookCreateAddAnotherRedirectsToNew pins the Variant B branching:
// when submit_action=add_another is set, the handler must redirect back
// to /books/new (not /books/:id). Flash is still set so the form page
// shows a success banner.
func TestBookCreateAddAnotherRedirectsToNew(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validBookFields()
	fields["submit_action"] = "add_another"

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/books/new" {
		t.Errorf("expected redirect to /books/new, got %q", loc)
	}
	assertFlashSuccessSet(t, rr)
}

// TestBookCreateSaveDefaultRedirectsToDetail mirrors the branching test:
// the "save" submit_action (or none at all) must redirect to the
// /books/:id detail page, not back to the form.
func TestBookCreateSaveDefaultRedirectsToDetail(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validBookFields()
	fields["submit_action"] = "save"

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	loc := rr.Header().Get("Location")
	if loc == "/books/new" || !strings.HasPrefix(loc, "/books/") {
		t.Errorf("expected redirect to /books/:id, got %q", loc)
	}
}

// ---------- Validation rejections ----------

// TestBookCreateRejectsMissingTitle verifies an empty title re-renders
// the form (400) rather than 302-redirecting. Book must NOT be created.
func TestBookCreateRejectsMissingTitle(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	before, _ := dm.GetAllBooks()

	fields := validBookFields()
	fields["title"] = ""

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 re-render, got %d. body: %s", rr.Code, rr.Body.String())
	}
	after, _ := dm.GetAllBooks()
	if len(after) != len(before) {
		t.Errorf("book count changed on invalid submit: before=%d after=%d", len(before), len(after))
	}
}

// TestBookCreateRejectsNoAuthors verifies the authors-required validator.
// An empty authors field must re-render, not create a book.
func TestBookCreateRejectsNoAuthors(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	before, _ := dm.GetAllBooks()

	fields := validBookFields()
	fields["authors"] = ""

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 re-render, got %d. body: %s", rr.Code, rr.Body.String())
	}
	after, _ := dm.GetAllBooks()
	if len(after) != len(before) {
		t.Errorf("book count changed on invalid submit")
	}
}

// TestBookCreateRejectsYearOutOfRange pins the 1500-2100 bound. 1400 is
// chosen because typography pre-Gutenberg printing is clearly invalid
// for a physical book record.
func TestBookCreateRejectsYearOutOfRange(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validBookFields()
	fields["year"] = "1400"

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 re-render, got %d. body: %s", rr.Code, rr.Body.String())
	}
}

// TestBookCreateRejectsInvalidISBN pins IsValidISBN: 11-char numeric
// strings are neither ISBN-10 nor ISBN-13, and must be rejected even
// though they pass an "only digits" sanity check.
func TestBookCreateRejectsInvalidISBN(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validBookFields()
	fields["isbn"] = "12345678901"

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 re-render, got %d. body: %s", rr.Code, rr.Body.String())
	}
}

// TestBookCreateRejectsDuplicateISBN verifies the pre-insert ISBN
// duplicate check surfaces as a form re-render (not a 500 from the
// UNIQUE constraint). Also verifies the error message names the
// existing title so the operator knows what they collided with.
func TestBookCreateRejectsDuplicateISBN(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	existingISBN := "9781234567897"
	if _, err := dm.CreateBook(
		&Book{Title: "Existing", ISBN: &existingISBN},
		[]string{"Seed Author"},
	); err != nil {
		t.Fatalf("seed CreateBook: %v", err)
	}

	fields := validBookFields()
	fields["title"] = "Collider"
	fields["isbn"] = existingISBN

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 re-render on duplicate ISBN, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Existing") {
		t.Errorf("expected duplicate-ISBN error to name the existing title %q, body: %s", "Existing", rr.Body.String())
	}

	// Verify only the seeded "Existing" book carries that ISBN; no second row.
	books, err := dm.GetAllBooks()
	if err != nil {
		t.Fatalf("GetAllBooks: %v", err)
	}
	matches := 0
	for _, b := range books {
		if b.ISBN != nil && *b.ISBN == existingISBN {
			matches++
		}
	}
	if matches != 1 {
		t.Errorf("expected exactly 1 row with ISBN %s, got %d", existingISBN, matches)
	}
}

// ---------- Cover upload ----------

// TestBookCreateAcceptsValidJPEGCover verifies a valid JPEG (extension +
// magic-byte match) is saved to disk under DATA_DIR/covers and the
// resulting filename is persisted on the book row. Uses minimalJPEG
// which satisfies http.DetectContentType's image/jpeg signature.
func TestBookCreateAcceptsValidJPEGCover(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validBookFields()
	fields["title"] = "Book With Cover"
	fields["isbn"] = "9789999999990"

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "cover.jpg", minimalJPEG)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}

	book, err := dm.GetBookByISBN("9789999999990")
	if err != nil {
		t.Fatalf("GetBookByISBN: %v", err)
	}
	full, err := dm.GetBookByID(book.ID)
	if err != nil {
		t.Fatalf("GetBookByID: %v", err)
	}
	if full.CoverFilename == nil {
		t.Fatalf("expected cover_filename to be set on the book row")
	}
	if !strings.HasSuffix(*full.CoverFilename, ".jpg") {
		t.Errorf("expected .jpg extension on saved cover, got %q", *full.CoverFilename)
	}

	path := filepath.Join(os.Getenv("DATA_DIR"), "covers", *full.CoverFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected cover file on disk at %s, got %v", path, err)
	}
	if info.Size() == 0 {
		t.Errorf("cover file is empty")
	}
}

// TestBookCreateRejectsMimeMismatch verifies the magic-byte check:
// filename ends in .jpg but the bytes are PNG. SaveUploadedCover must
// reject, the book must NOT be created, and the response is a re-render.
func TestBookCreateRejectsMimeMismatch(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	before, _ := dm.GetAllBooks()

	fields := validBookFields()
	fields["title"] = "Bogus Cover"

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "cover.jpg", minimalPNG)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 re-render on MIME mismatch, got %d. body: %s", rr.Code, rr.Body.String())
	}
	after, _ := dm.GetAllBooks()
	if len(after) != len(before) {
		t.Errorf("book count changed despite bad cover: before=%d after=%d", len(before), len(after))
	}
}

// ---------- Open Library proxy ----------

// TestOpenLibraryLookupRejectsInvalidISBN verifies the proxy endpoint
// short-circuits on a malformed ISBN with 400 and never makes an
// outbound request. Using a 5-digit string so length validation fires
// regardless of checksum logic.
func TestOpenLibraryLookupRejectsInvalidISBN(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	req, _ := http.NewRequest("GET", "/api/openlibrary/isbn/12345", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid ISBN, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid_isbn") {
		t.Errorf("expected body to identify invalid_isbn, got %s", rr.Body.String())
	}
}

// ---------- Flash detail cookie ----------

// TestBookCreateFlashDetailRoundTripsSpecialChars verifies the new
// flash_detail cookie plumbing: a title containing an apostrophe and
// non-ASCII character is URL-escaped on set and correctly decoded on
// read. The decoded title must appear in the subsequent /books/:id
// page body. Without URL-escape, the raw apostrophe would either be
// silently dropped by net/http cookie validation or break the cookie
// header entirely.
func TestBookCreateFlashDetailRoundTripsSpecialChars(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	const title = "O'Brien's Café"
	fields := validBookFields()
	fields["title"] = title

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}

	// The flash_detail cookie value on the wire is URL-escaped.
	cookieVal, ok := flashSet(rr, "flash_detail")
	if !ok {
		t.Fatalf("expected flash_detail cookie set on redirect, cookies: %v", rr.Result().Cookies())
	}
	unescaped, err := url.QueryUnescape(cookieVal)
	if err != nil {
		t.Fatalf("flash_detail cookie not url-escaped: %v (raw=%q)", err, cookieVal)
	}
	if unescaped != title {
		t.Errorf("flash_detail round-trip: got %q, want %q", unescaped, title)
	}

	// Follow the redirect manually to verify HandleBookDetail reads and
	// renders the detail into the success banner. Carry both the session
	// cookie and the freshly-set flash cookies forward.
	loc := rr.Header().Get("Location")
	followReq, _ := http.NewRequest("GET", loc, nil)
	followReq.AddCookie(sess)
	for _, c := range rr.Result().Cookies() {
		followReq.AddCookie(c)
	}
	followRR := httptest.NewRecorder()
	router.ServeHTTP(followRR, followReq)

	if followRR.Code != http.StatusOK {
		t.Fatalf("expected 200 on detail page, got %d", followRR.Code)
	}
	// html/template escapes the apostrophe as &#39; and é as é (UTF-8 kept),
	// so assert via the unambiguous UTF-8 suffix.
	if !strings.Contains(followRR.Body.String(), "Café") {
		t.Errorf("expected detail page to render flash_detail title, body missing 'Café'")
	}
}

// TestFlashDetailCookieClearedOnRead verifies readAndClearFlashDetail
// sets the cookie MaxAge to -1 on the response, so a subsequent request
// would not re-display the banner. Uses HandleBookDetail as the reader;
// the second request to the same URL must NOT see the detail rendered.
func TestFlashDetailCookieClearedOnRead(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validBookFields()
	fields["title"] = "Clear Me"
	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 on create, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")

	// First GET consumes the flash cookies.
	req1, _ := http.NewRequest("GET", loc, nil)
	req1.AddCookie(sess)
	for _, c := range rr.Result().Cookies() {
		req1.AddCookie(c)
	}
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected 200 on first detail view, got %d", rr1.Code)
	}
	if !strings.Contains(rr1.Body.String(), "Clear Me") {
		t.Fatalf("first view should have rendered the detail banner")
	}
	// The response should set flash_detail with MaxAge <= 0 (cleared).
	cleared := false
	for _, c := range rr1.Result().Cookies() {
		if c.Name == "flash_detail" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("expected flash_detail cookie to be cleared (MaxAge=-1) on read, cookies: %v", rr1.Result().Cookies())
	}

	// Second GET, without forwarding the now-cleared cookies, should not
	// render the banner text because GetBookByID's Book.Title is "Clear
	// Me" -- so filter by the bolded <strong> wrapper the template uses
	// to surface the detail specifically.
	req2, _ := http.NewRequest("GET", loc, nil)
	req2.AddCookie(sess)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 on second detail view, got %d", rr2.Code)
	}
	if strings.Contains(rr2.Body.String(), "<strong>Clear Me</strong>") {
		t.Errorf("flash banner should not render on second view (cookie cleared)")
	}
}

// ---------- Negative: unauthenticated create is already covered in
// main_test.go by TestProtectedRoutesRedirectWithoutAuth /
// TestPatronCannotAccessStaffRoutes. Not duplicated here to keep this
// file focused on handler logic, not middleware wiring.

// TestBookCreateUnknownSubmitActionFallsThrough verifies that an
// unexpected submit_action value does not trigger the "add_another"
// branch -- the handler treats anything other than exactly "add_another"
// as the default /books/:id redirect.
func TestBookCreateUnknownSubmitActionFallsThrough(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validBookFields()
	fields["submit_action"] = "gibberish"

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc == "/books/new" {
		t.Errorf("unknown submit_action should not route to /books/new, got %q", loc)
	}
}

// ---------- Edit handler (GET) ----------

// mustCreateBookWithCover seeds a book plus an on-disk cover file so
// cover-preservation / cover-replacement tests can observe the file
// system side effects. Uses a random-ish fake filename (cover bytes are
// not a real image; disk presence is all tests assert).
func mustCreateBookWithCover(t *testing.T, dm *DatabaseManager, title, isbn string) (int, string) {
	t.Helper()
	filename := "test-" + title + ".jpg"
	full := filepath.Join(coversDir(), filename)
	if err := os.MkdirAll(coversDir(), 0o755); err != nil {
		t.Fatalf("MkdirAll covers: %v", err)
	}
	if err := os.WriteFile(full, []byte("not-a-real-image"), 0o644); err != nil {
		t.Fatalf("WriteFile cover: %v", err)
	}
	book := &Book{Title: title, CoverFilename: &filename}
	if isbn != "" {
		book.ISBN = &isbn
	}
	id, err := dm.CreateBook(book, []string{"Seed Author"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	return id, filename
}

// TestBookEditRendersFormPrefilled pins the GET edit page: the form must
// render with the book's current title and authors so the admin sees
// what they're editing. Without the prefill, edits would silently clear
// unmodified fields.
func TestBookEditRendersFormPrefilled(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	// Book id 1 is "Pride and Prejudice" by "Jane Austen" per SeedBooks.
	req, _ := http.NewRequest("GET", "/books/1/edit", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Pride and Prejudice") {
		t.Errorf("expected edit form to contain title 'Pride and Prejudice'")
	}
	if !strings.Contains(body, "Jane Austen") {
		t.Errorf("expected edit form to contain author 'Jane Austen'")
	}
	if !strings.Contains(body, `action="/books/1/edit"`) {
		t.Errorf("expected form action to point at /books/1/edit, body: %s", body)
	}
}

// TestBookEditReturns404ForMissing pins the not-found handling: asking
// to edit a book id that does not exist must 404, not 500 or empty
// form.
func TestBookEditReturns404ForMissing(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	req, _ := http.NewRequest("GET", "/books/99999/edit", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d. body: %s", rr.Code, rr.Body.String())
	}
}

// ---------- Update handler (POST) ----------

// TestBookUpdateHappyPath verifies a title change is persisted and the
// response redirects to the detail page with a success flash.
func TestBookUpdateHappyPath(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	id, _ := dm.CreateBook(&Book{Title: "Original"}, []string{"Seed Author"})

	fields := map[string]string{
		"title":    "Updated Title",
		"authors":  "Seed Author",
		"quantity": "1",
	}
	rr := postBookMultipart(t, router, fmt.Sprintf("/books/%d/edit", id), sess, csrf, fields, "", nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != fmt.Sprintf("/books/%d", id) {
		t.Errorf("expected redirect to /books/%d, got %q", id, loc)
	}
	assertFlashSuccessSet(t, rr)

	book, err := dm.GetBookByID(id)
	if err != nil {
		t.Fatalf("GetBookByID: %v", err)
	}
	if book.Title != "Updated Title" {
		t.Errorf("title not updated: got %q", book.Title)
	}
}

// TestBookUpdatePreservesExistingCover verifies that editing a book
// without uploading a new cover or providing a cover_url leaves both
// the DB row's cover_filename AND the on-disk file intact. Regression
// pin for the "carry existing.CoverFilename through to the updated row"
// branch in HandleBookUpdate.
func TestBookUpdatePreservesExistingCover(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	id, filename := mustCreateBookWithCover(t, dm, "CoverBook", "")
	fullPath := filepath.Join(coversDir(), filename)

	fields := map[string]string{
		"title":    "CoverBook Retitled",
		"authors":  "Seed Author",
		"quantity": "1",
	}
	rr := postBookMultipart(t, router, fmt.Sprintf("/books/%d/edit", id), sess, csrf, fields, "", nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	book, err := dm.GetBookByID(id)
	if err != nil {
		t.Fatalf("GetBookByID: %v", err)
	}
	if book.CoverFilename == nil || *book.CoverFilename != filename {
		t.Errorf("expected cover_filename preserved as %q, got %+v", filename, book.CoverFilename)
	}
	if _, err := os.Stat(fullPath); err != nil {
		t.Errorf("expected existing cover file to still be on disk at %s, got %v", fullPath, err)
	}
}

// TestBookUpdateReplacesExistingCover verifies that uploading a new
// cover both (a) updates the DB row to the new filename and (b) removes
// the prior cover file from disk after the DB write succeeds. Without
// the cleanup, every edit would accumulate orphaned files in
// DATA_DIR/covers.
func TestBookUpdateReplacesExistingCover(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	id, oldFilename := mustCreateBookWithCover(t, dm, "ReplaceCover", "")
	oldPath := filepath.Join(coversDir(), oldFilename)

	fields := map[string]string{
		"title":    "ReplaceCover",
		"authors":  "Seed Author",
		"quantity": "1",
	}
	rr := postBookMultipart(t, router, fmt.Sprintf("/books/%d/edit", id), sess, csrf, fields, "new.jpg", minimalJPEG)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	book, err := dm.GetBookByID(id)
	if err != nil {
		t.Fatalf("GetBookByID: %v", err)
	}
	if book.CoverFilename == nil {
		t.Fatalf("expected cover_filename to still be set")
	}
	if *book.CoverFilename == oldFilename {
		t.Errorf("cover_filename was not replaced, still %q", oldFilename)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("expected old cover file to be removed from disk, stat: %v", err)
	}
	newPath := filepath.Join(coversDir(), *book.CoverFilename)
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("expected new cover file on disk at %s, got %v", newPath, err)
	}
}

// TestBookUpdateRejectsDuplicateISBN verifies the conflict check: if
// another book already owns the ISBN being set, the edit is rejected
// with a 400 re-render and the error names the conflicting title.
func TestBookUpdateRejectsDuplicateISBN(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	takenISBN := "9781111111116"
	takenTitle := "Taken"
	if _, err := dm.CreateBook(
		&Book{Title: takenTitle, ISBN: &takenISBN},
		[]string{"Seed Author"},
	); err != nil {
		t.Fatalf("seed taken book: %v", err)
	}

	targetID, err := dm.CreateBook(
		&Book{Title: "Target"},
		[]string{"Seed Author"},
	)
	if err != nil {
		t.Fatalf("seed target book: %v", err)
	}

	fields := map[string]string{
		"title":    "Target",
		"authors":  "Seed Author",
		"quantity": "1",
		"isbn":     takenISBN,
	}
	rr := postBookMultipart(t, router, fmt.Sprintf("/books/%d/edit", targetID), sess, csrf, fields, "", nil)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 re-render on duplicate ISBN, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), takenTitle) {
		t.Errorf("expected duplicate-ISBN error to name %q, body: %s", takenTitle, rr.Body.String())
	}

	after, err := dm.GetBookByID(targetID)
	if err != nil {
		t.Fatalf("GetBookByID: %v", err)
	}
	if after.ISBN != nil {
		t.Errorf("target book's ISBN should still be nil (edit rejected), got %+v", after.ISBN)
	}
}

// TestBookUpdateAllowsSameISBNOnSelfEdit pins the "exclude self from
// conflict check" clause. A no-op edit that leaves a book's own ISBN
// unchanged must succeed, not fire the duplicate guard against itself.
func TestBookUpdateAllowsSameISBNOnSelfEdit(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	isbn := "9782222222229"
	id, err := dm.CreateBook(
		&Book{Title: "SelfEdit", ISBN: &isbn},
		[]string{"Seed Author"},
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	fields := map[string]string{
		"title":    "SelfEdit Retitled",
		"authors":  "Seed Author",
		"quantity": "1",
		"isbn":     isbn,
	}
	rr := postBookMultipart(t, router, fmt.Sprintf("/books/%d/edit", id), sess, csrf, fields, "", nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 success, got %d. body: %s", rr.Code, rr.Body.String())
	}
	after, err := dm.GetBookByID(id)
	if err != nil {
		t.Fatalf("GetBookByID: %v", err)
	}
	if after.Title != "SelfEdit Retitled" {
		t.Errorf("title not updated on self-ISBN edit: got %q", after.Title)
	}
}

// TestBookUpdateReturns404ForMissing: POST to an edit URL for a book id
// that does not exist must 404, not silently create or 500.
func TestBookUpdateReturns404ForMissing(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	rr := postBookMultipart(t, router, "/books/99999/edit", sess, csrf, validBookFields(), "", nil)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d. body: %s", rr.Code, rr.Body.String())
	}
}

// ---------- Delete handler (POST) ----------

// TestBookDeleteHappyPath verifies the row is removed AND the cover
// file is deleted from disk. Tests both halves of the success path in
// one pass since they always travel together.
func TestBookDeleteHappyPath(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	id, filename := mustCreateBookWithCover(t, dm, "ToDelete", "")
	fullPath := filepath.Join(coversDir(), filename)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/delete", id), sess, csrf, nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/catalog" {
		t.Errorf("expected redirect to /catalog, got %q", loc)
	}
	assertNoFlashError(t, rr)
	assertFlashSuccessSet(t, rr)

	if _, err := dm.GetBookByID(id); err == nil {
		t.Errorf("expected book row deleted, GetBookByID returned success")
	}
	if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
		t.Errorf("expected cover file removed from disk, stat: %v", err)
	}
}

// TestBookDeleteRejectsWhenHasLoans pins the ErrBookHasLoans guard: a
// book with any row in loans (even returned loans) must refuse delete
// with a 302 back to the detail page and a flash_error. Seeding the
// loan directly via dm.db.Exec because the loan handlers are CP6 work
// and cannot be invoked yet.
func TestBookDeleteRejectsWhenHasLoans(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	id := mustCreateBook(t, dm, "BookWithLoan", 1)
	copyID := firstCopyOf(t, dm, id)

	res, err := dm.db.Exec("INSERT INTO patrons (name) VALUES (?)", "Loan Patron")
	if err != nil {
		t.Fatalf("seed patron: %v", err)
	}
	patronID, _ := res.LastInsertId()
	if _, err := dm.db.Exec(
		"INSERT INTO loans (copy_id, patron_id, due_date) VALUES (?, ?, ?)",
		copyID, patronID, "2026-05-01 00:00:00",
	); err != nil {
		t.Fatalf("seed loan: %v", err)
	}

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/delete", id), sess, csrf, nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 PRG redirect back to detail, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != fmt.Sprintf("/books/%d", id) {
		t.Errorf("expected redirect to /books/%d, got %q", id, loc)
	}
	assertFlashErrorSet(t, rr)

	if _, err := dm.GetBookByID(id); err != nil {
		t.Errorf("book with loans should NOT be deleted, GetBookByID: %v", err)
	}
}

// TestBookDeleteRejectsStaffRole pins the admin-only boundary on delete.
// Staff-role users can reach Create and Edit but must be rejected here
// with 403. Regression pin: if a future edit drops the admin.POST
// registration and instead registers under staff.POST, this test fires.
func TestBookDeleteRejectsStaffRole(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff1", "staff")

	id, err := dm.CreateBook(
		&Book{Title: "StaffShouldNotDelete"},
		[]string{"Seed Author"},
	)
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/delete", id), sess, csrf, nil)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for staff role on admin-only delete, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if _, err := dm.GetBookByID(id); err != nil {
		t.Errorf("book must not be deleted by staff role, GetBookByID: %v", err)
	}
}

// TestBookDeleteReturns404ForMissing: delete of a nonexistent book id
// must 404, not silently succeed or 500.
func TestBookDeleteReturns404ForMissing(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	rr := postStaffForm(t, router, "/books/99999/delete", sess, csrf, nil)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d. body: %s", rr.Code, rr.Body.String())
	}
}

// TestBookNewFormRenders pins the GET /books/new form for staff.
// The handler is a thin renderTemplate call; this test exists so a
// future template edit that breaks the form surfaces here rather
// than only at create-time.
func TestBookNewFormRenders(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_new_book_form", "staff")

	req, _ := http.NewRequest("GET", "/books/new", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "<form") {
		t.Errorf("expected a form in the body; got %s", body[:min(200, len(body))])
	}
	if !strings.Contains(body, `id="ol-lookup-btn"`) {
		t.Errorf("Lookup button missing when external calls are allowed")
	}
}

func TestBookNewFormHidesLookupWhenOffline(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_new_off", "staff")

	actor, err := dm.GetUserByUsername("staff_new_off")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if err := dm.SetSetting("offline_mode", "true", actor.ID); err != nil {
		t.Fatalf("SetSetting offline_mode: %v", err)
	}

	req, _ := http.NewRequest("GET", "/books/new", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `id="ol-lookup-btn"`) {
		t.Errorf("Lookup button rendered when offline_mode is on")
	}
}

func TestBookEditFormHidesLookupWhenOffline(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin_edit_off", "admin")

	actor, err := dm.GetUserByUsername("admin_edit_off")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if err := dm.SetSetting("offline_mode", "true", actor.ID); err != nil {
		t.Fatalf("SetSetting offline_mode: %v", err)
	}

	req, _ := http.NewRequest("GET", "/books/1/edit", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `id="ol-lookup-btn"`) {
		t.Errorf("Lookup button rendered when offline_mode is on")
	}
}

func TestOpenLibraryLookupHappy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_ol_happy", "staff")

	prev := openLibraryBaseURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"ISBN:9780141439518": {
				"details": {
					"title": "Pride and Prejudice",
					"authors": [{"name": "Jane Austen"}],
					"publishers": ["Penguin"],
					"publish_date": "1813",
					"covers": [1234567]
				}
			}
		}`))
	}))
	openLibraryBaseURL = srv.URL
	t.Cleanup(func() { openLibraryBaseURL = prev; srv.Close() })

	req, _ := http.NewRequest("GET", "/api/openlibrary/isbn/9780141439518", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Pride and Prejudice") {
		t.Errorf("body missing title: %s", rr.Body.String())
	}
}

func TestOpenLibraryLookupNotFound(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_ol_404", "staff")

	prev := openLibraryBaseURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	openLibraryBaseURL = srv.URL
	t.Cleanup(func() { openLibraryBaseURL = prev; srv.Close() })

	req, _ := http.NewRequest("GET", "/api/openlibrary/isbn/9780141439518", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "not_found") {
		t.Errorf("body missing not_found: %s", rr.Body.String())
	}
}

func TestOpenLibraryLookupUpstreamError(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_ol_5xx", "staff")

	prev := openLibraryBaseURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	openLibraryBaseURL = srv.URL
	t.Cleanup(func() { openLibraryBaseURL = prev; srv.Close() })

	req, _ := http.NewRequest("GET", "/api/openlibrary/isbn/9780141439518", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rr.Code)
	}
}

func TestOpenLibraryLookupExternalSourcesUnavailable(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_ol_miss_gb_5xx", "staff")

	prevOL := openLibraryBaseURL
	olSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	openLibraryBaseURL = olSrv.URL
	t.Cleanup(func() { openLibraryBaseURL = prevOL; olSrv.Close() })

	gbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(gbSrv.Close)
	withGoogleBooksBaseURL(t, gbSrv.URL)
	withGoogleBooksAPIKey(t, "test-key")

	req, _ := http.NewRequest("GET", "/api/openlibrary/isbn/9780141439518", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "external_sources_unavailable") {
		t.Errorf("body missing external_sources_unavailable: %s", rr.Body.String())
	}
}

func TestCatalogRendersForStaff(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_catalog", "staff")

	req, _ := http.NewRequest("GET", "/catalog", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Catalog") {
		t.Errorf("expected 'Catalog' in body")
	}
}

// TestCatalogFilterOutShowsOnlyOutOfStock pins the ?filter=out branch
// of HandleCatalog. With one out-of-stock book and one in-stock book,
// the filtered view must contain the out-of-stock title and NOT the
// in-stock one, plus the "Showing out of stock only" banner.
func TestCatalogFilterOutShowsOnlyOutOfStock(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "filter_staff", "staff")

	mustCreateBookWithAvailable(t, dm, "OOS-Title-XYZZY", 1, 0)
	mustCreateBookWithAvailable(t, dm, "InStock-Title-PLUGH", 1, 1)

	req, _ := http.NewRequest("GET", "/catalog?filter=out", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "OOS-Title-XYZZY") {
		t.Errorf("body should contain out-of-stock title")
	}
	if strings.Contains(body, "InStock-Title-PLUGH") {
		t.Errorf("body should NOT contain in-stock title under ?filter=out")
	}
	if !strings.Contains(body, "out of stock") {
		t.Errorf("body should contain the filter banner text")
	}
}

// TestCatalogNoFilterShowsAllBooks confirms the default (no filter)
// branch still returns every book. Companion to the filter-out test.
func TestCatalogNoFilterShowsAllBooks(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "noflt_staff", "staff")

	mustCreateBookWithAvailable(t, dm, "OOS-AllView-XYZZY", 1, 0)
	mustCreateBookWithAvailable(t, dm, "InStock-AllView-PLUGH", 1, 1)

	req, _ := http.NewRequest("GET", "/catalog", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "OOS-AllView-XYZZY") {
		t.Errorf("body should contain out-of-stock title in unfiltered view")
	}
	if !strings.Contains(body, "InStock-AllView-PLUGH") {
		t.Errorf("body should contain in-stock title in unfiltered view")
	}
}

func TestDashboardRendersForStaff(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_dash", "staff")

	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("dashboard status = %d, want 200", rr.Code)
	}
}

func TestHandleOpenLibraryLookup_OfflineReturns503(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin1", "admin")
	adminID := mustCreateUser(t, dm, "admin_lookup_off", "admin")
	if err := dm.SetSetting("offline_mode", "true", adminID); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/openlibrary/isbn/9780000000000", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "offline_mode") {
		t.Errorf("body should mention offline_mode, got %q", rr.Body.String())
	}
}

func TestHandleBookCreate_CoverURLSkippedWhenOffline(t *testing.T) {
	router, dm := setupTestRouter(t)
	withOfflineEnvDefault(t, true)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	fields := validBookFields()
	fields["isbn"] = "9780000000001"
	// .invalid is RFC 6761 reserved TLD; any accidental HTTP attempt
	// will fail fast. The offline gate must fire before any attempt.
	fields["cover_url"] = "https://example.invalid/cover.jpg"

	rr := postBookMultipart(t, router, "/books", sess, csrf, fields, "", nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if got := flashCode(rr, "flash_success"); got != "book_created_cover_skipped_offline" {
		t.Errorf("expected flash_success=book_created_cover_skipped_offline, got %q", got)
	}

	// The book row should exist with no cover.
	got, err := dm.GetBookByISBN("9780000000001")
	if err != nil {
		t.Fatalf("GetBookByISBN: %v", err)
	}
	if got.CoverFilename != nil {
		t.Errorf("expected nil cover_filename after offline skip, got %q", *got.CoverFilename)
	}
}

func TestHandleBookUpdate_CoverURLSkippedWhenOffline(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	// Pre-seed a book with no cover.
	id, err := dm.CreateBook(&Book{Title: "Existing"}, []string{"Author"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}

	// Lock AFTER the seed so seed-time external calls are not relevant.
	withOfflineEnvDefault(t, true)

	fields := map[string]string{
		"title":     "Existing",
		"authors":   "Author",
		"quantity":  "1",
		"cover_url": "https://example.invalid/cover.jpg",
	}
	rr := postBookMultipart(t, router, fmt.Sprintf("/books/%d/edit", id), sess, csrf, fields, "", nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if got := flashCode(rr, "flash_success"); got != "book_updated_cover_skipped_offline" {
		t.Errorf("expected flash_success=book_updated_cover_skipped_offline, got %q", got)
	}

	got, err := dm.GetBookByID(id)
	if err != nil {
		t.Fatalf("GetBookByID: %v", err)
	}
	if got.CoverFilename != nil {
		t.Errorf("expected nil cover_filename after offline skip, got %q", *got.CoverFilename)
	}
}

// ---------- HandleAddCopy ----------

// TestAddCopyHappyPath pins POST /books/:id/copies: the handler
// allocates an LSF barcode via AddLibraryCopy, flashes copy_added with
// the barcode as detail, redirects to /books/:id. The new copy is
// queryable via GetCopyByBarcode.
func TestAddCopyHappyPath(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Empty Inventory", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, nil)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != fmt.Sprintf("/books/%d", bookID) {
		t.Errorf("expected redirect to /books/%d, got %q", bookID, loc)
	}
	if got := flashCode(rr, "flash_success"); got != "copy_added" {
		t.Errorf("expected flash_success=copy_added, got %q", got)
	}
	barcode := flashCode(rr, "flash_detail")
	if !strings.HasPrefix(barcode, "LSF") || len(barcode) != 11 {
		t.Errorf("expected LSF barcode in flash_detail, got %q", barcode)
	}

	copies, err := dm.GetCopiesByBookID(bookID)
	if err != nil {
		t.Fatalf("GetCopiesByBookID: %v", err)
	}
	if len(copies) != 1 {
		t.Fatalf("expected 1 copy after add, got %d", len(copies))
	}
	if copies[0].Barcode != barcode {
		t.Errorf("flash barcode %q != stored barcode %q", barcode, copies[0].Barcode)
	}
	if copies[0].BarcodeFormat != "code128" {
		t.Errorf("BarcodeFormat = %q, want code128", copies[0].BarcodeFormat)
	}
	if copies[0].Status != "available" {
		t.Errorf("Status = %q, want available", copies[0].Status)
	}
}

// TestAddCopyMonotonicLSF pins that consecutive AddCopy calls allocate
// distinct LSF sequences. Without monotonic allocation the second call
// would collide on the UNIQUE barcode constraint and the handler would
// surface a 500.
func TestAddCopyMonotonicLSF(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Two Copies", 0)

	for i := 0; i < 2; i++ {
		rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, nil)
		if rr.Code != http.StatusFound {
			t.Fatalf("add %d: status = %d, want 302; body=%s", i+1, rr.Code, rr.Body.String())
		}
	}

	copies, err := dm.GetCopiesByBookID(bookID)
	if err != nil {
		t.Fatalf("GetCopiesByBookID: %v", err)
	}
	if len(copies) != 2 {
		t.Fatalf("expected 2 copies, got %d", len(copies))
	}
	if copies[0].Barcode == copies[1].Barcode {
		t.Errorf("expected distinct barcodes, got %q and %q", copies[0].Barcode, copies[1].Barcode)
	}
}

// TestAddCopyBookNotFound pins the 404 path: adding a copy to a
// nonexistent book id renders the error page, not a 500 or a phantom
// copy row.
func TestAddCopyBookNotFound(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	rr := postStaffForm(t, router, "/books/99999/copies", sess, csrf, nil)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// TestAddCopyNonStaffForbidden pins the auth gate: a patron-role user
// cannot POST /books/:id/copies.
func TestAddCopyNonStaffForbidden(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf, _ := loginAsPatron(t, dm, "Borrower")
	bookID := mustCreateBook(t, dm, "Restricted", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, nil)

	if rr.Code != http.StatusForbidden && rr.Code != http.StatusFound {
		t.Errorf("expected 403 (or 302 redirect to login), got %d", rr.Code)
	}
	copies, _ := dm.GetCopiesByBookID(bookID)
	if len(copies) != 0 {
		t.Errorf("patron should not have been able to add copies; got %d", len(copies))
	}
}

// ---------- HandleAddCopy: multi-format + bulk (cs408-go-stack-stb) ----------

// TestAddCopyLibraryBulkHappy pins source=library count=5: five copies
// created with monotonic distinct LSF barcodes, flash copy_added_bulk.
func TestAddCopyLibraryBulkHappy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Bulk Subject", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, map[string]string{
		"source": "library",
		"count":  "5",
	})

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if got := flashCode(rr, "flash_success"); got != "copy_added_bulk" {
		t.Errorf("expected flash_success=copy_added_bulk, got %q", got)
	}

	copies, err := dm.GetCopiesByBookID(bookID)
	if err != nil {
		t.Fatalf("GetCopiesByBookID: %v", err)
	}
	if len(copies) != 5 {
		t.Fatalf("expected 5 copies, got %d", len(copies))
	}
	seen := make(map[string]bool, 5)
	for _, c := range copies {
		if !strings.HasPrefix(c.Barcode, "LSF") || len(c.Barcode) != 11 {
			t.Errorf("expected LSF barcode, got %q", c.Barcode)
		}
		if seen[c.Barcode] {
			t.Errorf("duplicate barcode in bulk allocation: %q", c.Barcode)
		}
		seen[c.Barcode] = true
		if c.BarcodeFormat != "code128" {
			t.Errorf("BarcodeFormat = %q, want code128", c.BarcodeFormat)
		}
	}
}

// TestAddCopyLibraryCountInvalid pins the validation guard: count="abc"
// flashes copy_count_invalid and creates no copies.
func TestAddCopyLibraryCountInvalid(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Bad Count Subject", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, map[string]string{
		"source": "library",
		"count":  "abc",
	})

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if got := flashCode(rr, "flash_error"); got != "copy_count_invalid" {
		t.Errorf("expected flash_error=copy_count_invalid, got %q", got)
	}
	copies, _ := dm.GetCopiesByBookID(bookID)
	if len(copies) != 0 {
		t.Errorf("expected 0 copies after invalid count, got %d", len(copies))
	}
}

// TestAddCopyLibraryCountTooLarge pins count > MaxBulkCopiesPerRequest
// is rejected with copy_count_too_large flash.
func TestAddCopyLibraryCountTooLarge(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Cap Subject", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, map[string]string{
		"source": "library",
		"count":  "51",
	})

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if got := flashCode(rr, "flash_error"); got != "copy_count_too_large" {
		t.Errorf("expected flash_error=copy_count_too_large, got %q", got)
	}
	copies, _ := dm.GetCopiesByBookID(bookID)
	if len(copies) != 0 {
		t.Errorf("expected 0 copies after over-cap count, got %d", len(copies))
	}
}

// TestAddCopyScanEAN13Happy pins source=scan format=ean13 with a valid
// EAN-13: copy created with the supplied barcode + format, flash
// copy_added with the barcode as detail.
func TestAddCopyScanEAN13Happy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "EAN13 Subject", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, map[string]string{
		"source":  "scan",
		"format":  "ean13",
		"barcode": "9780141439518", // real Pride and Prejudice ISBN
	})

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. body: %s", rr.Code, rr.Body.String())
	}
	if got := flashCode(rr, "flash_success"); got != "copy_added" {
		t.Errorf("expected flash_success=copy_added, got %q", got)
	}
	if got := flashCode(rr, "flash_detail"); got != "9780141439518" {
		t.Errorf("expected flash_detail=9780141439518, got %q", got)
	}

	copies, _ := dm.GetCopiesByBookID(bookID)
	if len(copies) != 1 {
		t.Fatalf("expected 1 copy, got %d", len(copies))
	}
	if copies[0].Barcode != "9780141439518" {
		t.Errorf("Barcode = %q, want 9780141439518", copies[0].Barcode)
	}
	if copies[0].BarcodeFormat != "ean13" {
		t.Errorf("BarcodeFormat = %q, want ean13", copies[0].BarcodeFormat)
	}
}

// TestAddCopyScanEAN13BadCheck pins that an EAN-13 with a wrong check
// digit fails with barcode_value_invalid and creates no copy.
func TestAddCopyScanEAN13BadCheck(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Bad Check Subject", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, map[string]string{
		"source":  "scan",
		"format":  "ean13",
		"barcode": "9780141439516", // valid format, wrong check digit
	})

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if got := flashCode(rr, "flash_error"); got != "barcode_value_invalid" {
		t.Errorf("expected flash_error=barcode_value_invalid, got %q", got)
	}
	copies, _ := dm.GetCopiesByBookID(bookID)
	if len(copies) != 0 {
		t.Errorf("expected 0 copies, got %d", len(copies))
	}
}

// TestAddCopyScanFormatInvalid pins that an unknown format value is
// rejected with barcode_format_invalid.
func TestAddCopyScanFormatInvalid(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Unknown Format Subject", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, map[string]string{
		"source":  "scan",
		"format":  "qrcode", // not one of the four
		"barcode": "anything",
	})

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if got := flashCode(rr, "flash_error"); got != "barcode_format_invalid" {
		t.Errorf("expected flash_error=barcode_format_invalid, got %q", got)
	}
}

// TestAddCopyScanDuplicateBarcode pins that adding the same barcode
// twice fails on the second attempt with barcode_already_exists.
func TestAddCopyScanDuplicateBarcode(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Dupe Subject", 0)

	// First add succeeds.
	fields := map[string]string{
		"source":  "scan",
		"format":  "ean13",
		"barcode": "9780141439518",
	}
	rr1 := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, fields)
	if got := flashCode(rr1, "flash_success"); got != "copy_added" {
		t.Fatalf("first add should succeed; flash_success=%q", got)
	}

	// Second add of the same barcode (same book or another) fails.
	rr2 := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, fields)
	if got := flashCode(rr2, "flash_error"); got != "barcode_already_exists" {
		t.Errorf("expected flash_error=barcode_already_exists on second add, got %q", got)
	}
	copies, _ := dm.GetCopiesByBookID(bookID)
	if len(copies) != 1 {
		t.Errorf("expected 1 copy after dupe attempt, got %d", len(copies))
	}
}

// TestAddCopyScanEmptyBarcode pins that an empty barcode value is
// rejected before the format dispatcher runs, with the "required"
// slug (not the "value-invalid" slug, which is for actual format /
// check-digit mismatches).
func TestAddCopyScanEmptyBarcode(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Empty Scan Subject", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, map[string]string{
		"source":  "scan",
		"format":  "ean13",
		"barcode": "   ",
	})

	if got := flashCode(rr, "flash_error"); got != "barcode_value_required" {
		t.Errorf("expected flash_error=barcode_value_required for empty barcode, got %q", got)
	}
}

// TestAddCopyScanUPCAHappy + Code128 + Code39 round-trip each of the
// remaining formats end-to-end.
func TestAddCopyScanUPCAHappy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "UPC-A Subject", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, map[string]string{
		"source":  "scan",
		"format":  "upca",
		"barcode": "036000291452",
	})
	if got := flashCode(rr, "flash_success"); got != "copy_added" {
		t.Fatalf("expected flash_success=copy_added, got %q", got)
	}
	copies, _ := dm.GetCopiesByBookID(bookID)
	if len(copies) != 1 || copies[0].BarcodeFormat != "upca" {
		t.Errorf("UPC-A round trip failed: %+v", copies)
	}
}

func TestAddCopyScanCode128Happy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Code128 Subject", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, map[string]string{
		"source":  "scan",
		"format":  "code128",
		"barcode": "LIBRARY-A-001",
	})
	if got := flashCode(rr, "flash_success"); got != "copy_added" {
		t.Fatalf("expected flash_success=copy_added, got %q", got)
	}
}

func TestAddCopyScanCode39Happy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Code39 Subject", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, map[string]string{
		"source":  "scan",
		"format":  "code39",
		"barcode": "BOOK-001",
	})
	if got := flashCode(rr, "flash_success"); got != "copy_added" {
		t.Fatalf("expected flash_success=copy_added, got %q", got)
	}
}

// TestAddCopySourceMissingDefaultsLibrary pins back-compat: a form
// with no "source" field at all still defaults to source=library
// count=1 -- this is what the book-detail quick-add form posts and
// what the foundation TestAddCopyHappyPath continues to exercise.
func TestAddCopySourceMissingDefaultsLibrary(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")
	bookID := mustCreateBook(t, dm, "Quick Add Subject", 0)

	rr := postStaffForm(t, router, fmt.Sprintf("/books/%d/copies", bookID), sess, csrf, nil)

	if got := flashCode(rr, "flash_success"); got != "copy_added" {
		t.Fatalf("expected flash_success=copy_added (default library), got %q", got)
	}
	copies, _ := dm.GetCopiesByBookID(bookID)
	if len(copies) != 1 {
		t.Errorf("expected exactly 1 copy from default library add, got %d", len(copies))
	}
}

// ---------- DB methods: AddLibraryCopies + CreateCopyWithBarcode ----------

// TestAddLibraryCopiesMonotonic pins that a single AddLibraryCopies
// call yields a contiguous range of LSF sequences.
func TestAddLibraryCopiesMonotonic(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Seq Subject", 0)

	created, err := dm.AddLibraryCopies(bookID, 5)
	if err != nil {
		t.Fatalf("AddLibraryCopies: %v", err)
	}
	if len(created) != 5 {
		t.Fatalf("expected 5 created, got %d", len(created))
	}
	// All LSF, all distinct, monotonic on the 7-digit sequence portion.
	prev := -1
	for _, cp := range created {
		if !strings.HasPrefix(cp.Barcode, "LSF") || len(cp.Barcode) != 11 {
			t.Errorf("non-LSF barcode in bulk: %q", cp.Barcode)
		}
		seq := 0
		fmt.Sscanf(cp.Barcode[3:10], "%d", &seq)
		if seq <= prev {
			t.Errorf("non-monotonic sequence: %d after %d", seq, prev)
		}
		prev = seq
	}
}

// TestAddLibraryCopiesCapped pins the per-request cap guard.
func TestAddLibraryCopiesCapped(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Cap DB Subject", 0)

	if _, err := dm.AddLibraryCopies(bookID, MaxBulkCopiesPerRequest+1); err == nil {
		t.Errorf("expected error for count > MaxBulkCopiesPerRequest, got nil")
	}
	if _, err := dm.AddLibraryCopies(bookID, 0); err == nil {
		t.Errorf("expected error for count=0, got nil")
	}
}

// TestCreateCopyWithBarcodeHappy verifies the DB method end-to-end
// outside the handler: validation + insert + readback.
func TestCreateCopyWithBarcodeHappy(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Direct DB Subject", 0)

	id, err := dm.CreateCopyWithBarcode(bookID, "9780062316097", "ean13")
	if err != nil {
		t.Fatalf("CreateCopyWithBarcode: %v", err)
	}
	cp, err := dm.GetCopyByID(id)
	if err != nil {
		t.Fatalf("GetCopyByID: %v", err)
	}
	if cp.Barcode != "9780062316097" || cp.BarcodeFormat != "ean13" {
		t.Errorf("readback = %+v, want barcode 9780062316097 format ean13", cp)
	}
}

// TestCreateCopyWithBarcodeDuplicate pins the UNIQUE-violation -> sentinel mapping.
func TestCreateCopyWithBarcodeDuplicate(t *testing.T) {
	dm := setupTestDB(t)
	bookA := mustCreateBook(t, dm, "Book A", 0)
	bookB := mustCreateBook(t, dm, "Book B", 0)

	if _, err := dm.CreateCopyWithBarcode(bookA, "9780141439518", "ean13"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := dm.CreateCopyWithBarcode(bookB, "9780141439518", "ean13"); err != ErrBarcodeAlreadyExists {
		t.Errorf("second insert: expected ErrBarcodeAlreadyExists, got %v", err)
	}
}

// TestCreateCopyWithBarcodeInvalidFormat verifies the front-loaded
// validation: an unknown format returns ErrBarcodeFormatInvalid
// without touching the DB.
func TestCreateCopyWithBarcodeInvalidFormat(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Invalid Fmt Subject", 0)

	if _, err := dm.CreateCopyWithBarcode(bookID, "anything", "morse"); err != ErrBarcodeFormatInvalid {
		t.Errorf("expected ErrBarcodeFormatInvalid, got %v", err)
	}
	copies, _ := dm.GetCopiesByBookID(bookID)
	if len(copies) != 0 {
		t.Errorf("no copy should have been inserted; got %d", len(copies))
	}
}
