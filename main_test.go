// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// setupTestRouter builds a router that mirrors the production middleware
// chain from main.go: public routes, an auth group with RequireAuth +
// CSRFProtect, a staff group with RequireAuth + RequireStaff + CSRFProtect,
// and an admin group with RequireAuth + RequireAdmin + CSRFProtect. Tests
// that need auth call loginAs to create a user + session and get the
// cookie and CSRF token to send with requests. Closes #35.
func setupTestRouter(t *testing.T) (*gin.Engine, *DatabaseManager) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "libreshelf-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	// Route DATA_DIR into the per-test tmp dir so SaveUploadedCover and
	// coversDir() write under tmpDir/covers/ rather than polluting ./data/
	// in the repo. t.Setenv auto-restores on test end.
	t.Setenv("DATA_DIR", tmpDir)

	dm := NewDatabaseManager(tmpDir + "/test.sqlite")
	dm.SeedBooks()

	// brokenDM is a second DatabaseManager whose *sql.DB has been closed.
	// Tests that need to drive a handler's generic DB-error branch send
	// the X-Test-Break-Handler-DB: 1 header; a middleware appended after
	// the auth/CSRF/DBReadLock chain swaps the request-scoped "db" key
	// to point at brokenDM. The handler's first DB call then returns
	// "sql: database is closed" and the err != nil branch fires.
	brokenDM := NewDatabaseManager(tmpDir + "/broken.sqlite")
	if err := brokenDM.db.Close(); err != nil {
		t.Fatalf("close brokenDM: %v", err)
	}
	breakDBIfHeaderSet := func(c *gin.Context) {
		if c.GetHeader("X-Test-Break-Handler-DB") == "1" {
			c.Set("db", brokenDM)
		}
		c.Next()
	}

	funcMap := template.FuncMap{
		"deref": func(v interface{}) interface{} {
			switch p := v.(type) {
			case *string:
				if p != nil {
					return *p
				}
			case *int:
				if p != nil {
					return *p
				}
			}
			return ""
		},
		"add": func(a, b int) int { return a + b },
	}

	templates = make(map[string]*template.Template)
	templateNames := []string{
		"index", "catalog", "book_detail", "book_form",
		"patrons", "admin", "staff", "loans", "my_loans", "error",
		"reports_overdue", "overdue_notice",
		"backup_admin", "admin_settings",
		"admin_patrons_import", "admin_patrons_import_preview", "admin_patrons_import_result",
		"patron_login_credentials",
	}
	for _, name := range templateNames {
		templates[name] = template.Must(template.New("layout").Funcs(funcMap).ParseFiles(
			"templates/layout.html",
			"templates/"+name+".html",
		))
	}
	kioskTemplateNames := []string{"kiosk", "kiosk_book_detail"}
	for _, name := range kioskTemplateNames {
		templates[name] = template.Must(template.New("kiosk_layout").Funcs(funcMap).ParseFiles(
			"templates/kiosk_layout.html",
			"templates/"+name+".html",
		))
	}
	templates["login"] = template.Must(template.ParseFiles("templates/login.html"))
	templates["account_change_password"] = template.Must(template.ParseFiles("templates/account_change_password.html"))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := router.SetTrustedProxies([]string{"127.0.0.1"}); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
	router.Use(SecurityHeaders)
	router.Use(DatabaseMiddleware(dm))

	// Public routes (also covered by the header-gated swap so coverage
	// tests can exercise their DB-error branches).
	public := router.Group("/")
	public.Use(breakDBIfHeaderSet)
	public.GET("/login", HandleLogin)
	public.POST("/login", LoginCSRFProtect, HandleLoginPost)
	public.GET("/kiosk", HandleKiosk)
	public.GET("/kiosk/books/:id", HandleKioskBookDetail)

	// Authenticated routes -- any logged in user
	auth := router.Group("/")
	auth.Use(RequireAuth, RequirePasswordCurrent, CSRFProtect, DBReadLock, breakDBIfHeaderSet)
	auth.GET("/", HandleIndex)
	auth.GET("/catalog", HandleCatalog)
	auth.GET("/books/:id", HandleBookDetail)

	// Account routes -- no RequirePasswordCurrent; must stay reachable
	// while the flag is set.
	account := router.Group("/")
	account.Use(RequireAuth, CSRFProtect, DBReadLock, breakDBIfHeaderSet)
	account.GET("/account/change-password", HandleChangePassword)
	account.POST("/account/change-password", HandleChangePasswordPost)
	account.POST("/logout", HandleLogout)

	// Patron-only routes
	patron := router.Group("/")
	patron.Use(RequireAuth, RequirePasswordCurrent, RequirePatron, CSRFProtect, DBReadLock, breakDBIfHeaderSet)
	patron.GET("/my/loans", HandleMyLoans)

	// Staff routes -- admin + staff
	staff := router.Group("/")
	staff.Use(RequireAuth, RequirePasswordCurrent, RequireStaff, CSRFProtect, DBReadLock, breakDBIfHeaderSet)
	staff.GET("/patrons", HandlePatronList)
	staff.POST("/patrons", HandlePatronCreate)
	staff.POST("/patrons/:id/edit", HandlePatronEdit)
	staff.POST("/patrons/:id/delete", HandlePatronDelete)
	staff.GET("/patrons/:id/login-credentials", HandlePatronLoginCredentials)
	staff.POST("/patrons/:id/dismiss-temp", HandlePatronDismissTemp)
	staff.POST("/patrons/:id/regenerate-temp", HandlePatronRegenerateTemp)
	staff.GET("/api/openlibrary/isbn/:isbn", HandleOpenLibraryLookup)
	staff.GET("/books/new", HandleBookNew)
	staff.POST("/books", HandleBookCreate)
	staff.GET("/books/:id/edit", HandleBookEdit)
	staff.POST("/books/:id/edit", HandleBookUpdate)
	staff.POST("/books/:id/checkout", HandleCheckout)
	staff.POST("/loans/:id/return", HandleReturn)
	staff.GET("/loans", HandleLoansList)
	staff.GET("/reports/overdue", HandleReportsOverdue)
	staff.GET("/reports/overdue/patron/:id/notice", HandleOverdueNotice)

	// Admin-only routes (read-locked)
	admin := router.Group("/")
	admin.Use(RequireAuth, RequirePasswordCurrent, RequireAdmin, CSRFProtect, DBReadLock, breakDBIfHeaderSet)
	admin.GET("/staff", HandleStaffList)
	admin.POST("/staff", HandleStaffCreate)
	admin.POST("/staff/:id/edit", HandleStaffEdit)
	admin.POST("/staff/:id/delete", HandleStaffDelete)
	admin.POST("/staff/:id/password", HandleStaffResetPassword)
	admin.POST("/books/:id/delete", HandleBookDelete)
	admin.GET("/admin", HandleAdmin)
	admin.GET("/admin/backup", HandleBackupAdmin)
	admin.GET("/admin/backup/export", HandleBackupExport)
	admin.GET("/admin/settings", HandleSettings)
	admin.POST("/admin/settings", HandleSettingsPost)

	// Patron import (mirror)
	patronImport := router.Group("/")
	patronImport.Use(RequireAuth, RequirePasswordCurrent, RequireStaffImportAccess, CSRFProtect, DBReadLock, breakDBIfHeaderSet)
	patronImport.GET("/admin/patrons/import", HandlePatronImportForm)
	patronImport.POST("/admin/patrons/import", HandlePatronImportPreview)
	patronImport.POST("/admin/patrons/import/confirm", HandlePatronImportCommit)
	patronImport.GET("/admin/patrons/import/download/:token", HandleImportDownload)

	// Admin write routes -- no DBReadLock; takes write lock directly.
	adminWrite := router.Group("/")
	adminWrite.Use(RequireAuth, RequirePasswordCurrent, RequireAdmin, CSRFProtect, breakDBIfHeaderSet)
	adminWrite.POST("/admin/backup/import", HandleBackupImport)

	router.NoRoute(HandleNotFound)

	return router, dm
}

// loginAs creates a user with the given role and a valid session, and
// returns the session cookie plus the CSRF token to send on POSTs.
// The bcrypt hash is computed against a fixed test password; callers
// never need the password because they use the returned cookie directly.
func loginAs(t *testing.T, dm *DatabaseManager, username, role string) (*http.Cookie, string) {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte("TestPass1!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if err := dm.CreateUser(username, string(hash), role, nil); err != nil {
		t.Fatalf("CreateUser(%q, %q): %v", username, role, err)
	}
	user, err := dm.GetUserByUsername(username)
	if err != nil {
		t.Fatalf("GetUserByUsername(%q): %v", username, err)
	}

	sessionToken := "test-session-" + username
	csrfToken := "test-csrf-" + username
	if err := dm.CreateSession(sessionToken, user.ID, csrfToken, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	return &http.Cookie{Name: "session", Value: sessionToken}, csrfToken
}

// logoutHelper performs POST /logout through the router with the given
// session cookie and CSRF token. Returns the response recorder so the
// caller can assert on status, cookie clearing, etc.
func logoutHelper(t *testing.T, router *gin.Engine, sessionCookie *http.Cookie, csrfToken string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{}
	form.Set("csrf_token", csrfToken)
	req := httptest.NewRequest("POST", "/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func TestIndexRoute(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Dashboard") {
		t.Errorf("Expected body to contain 'Dashboard'")
	}
}

// TestDashboardStaffShowsCounts pins the staff/admin card set: Overdue,
// Active Loans, Out of Stock all render with the correct counts pulled
// from the seeded fixture. Active count must exclude overdue (the cards
// are disjoint by design -- see CountActiveLoans semantics).
func TestDashboardStaffShowsCounts(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "dash_admin", "admin")

	bookA := mustCreateBook(t, dm, "Active Book", 1)
	bookB := mustCreateBook(t, dm, "Overdue Book", 1)
	mustCreateBook(t, dm, "Sold Out Book", 0)
	patronID := mustCreatePatron(t, dm, "Dash Patron")

	nextWeek := time.Now().AddDate(0, 0, 7).UTC().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).UTC().Format("2006-01-02")
	mustInsertLoan(t, dm, bookA, patronID, nextWeek, "")  // active
	mustInsertLoan(t, dm, bookB, patronID, yesterday, "") // overdue

	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, label := range []string{"Overdue", "Active Loans", "Out of Stock"} {
		if !strings.Contains(body, label) {
			t.Errorf("expected staff dashboard to contain card label %q", label)
		}
	}
	// Patron-only card must not leak into staff view.
	if strings.Contains(body, "My Active Loans") {
		t.Errorf("staff dashboard must not render the patron-only card")
	}
}

// TestDashboardPatronShowsMyLoansCard pins the patron card: count plus
// "Next due:" secondary text taken from the soonest active loan.
func TestDashboardPatronShowsMyLoansCard(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _, patronID := loginAsPatron(t, dm, "Dash Patron")

	bookA := mustCreateBook(t, dm, "Soon Due Book", 1)
	bookB := mustCreateBook(t, dm, "Later Due Book", 1)
	soon := time.Now().AddDate(0, 0, 3).UTC().Format("2006-01-02")
	later := time.Now().AddDate(0, 0, 10).UTC().Format("2006-01-02")
	mustInsertLoan(t, dm, bookA, patronID, soon, "")
	mustInsertLoan(t, dm, bookB, patronID, later, "")

	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "My Active Loans") {
		t.Errorf("expected patron dashboard to render My Active Loans card")
	}
	if !strings.Contains(body, "Next due: "+soon) {
		t.Errorf("expected Next due to show soonest due_date %q in body", soon)
	}
	// Staff cards must not leak into patron view.
	for _, label := range []string{"Overdue", "Out of Stock"} {
		if strings.Contains(body, label) {
			t.Errorf("patron dashboard must not render staff card %q", label)
		}
	}
}

// TestDashboardPatronZeroLoans pins the empty patron state: card renders
// with "0", no "Next due:" line, no crash.
func TestDashboardPatronZeroLoans(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _, _ := loginAsPatron(t, dm, "No Loans Patron")

	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "My Active Loans") {
		t.Errorf("expected patron card even with zero loans")
	}
	if strings.Contains(body, "Next due:") {
		t.Errorf("must not render Next due line when patron has zero loans")
	}
}

// TestPublicRoutesReturn200 verifies routes registered outside the auth
// group serve 200 without any session cookie.
func TestPublicRoutesReturn200(t *testing.T) {
	router, _ := setupTestRouter(t)

	for _, route := range []string{"/kiosk", "/login"} {
		req, _ := http.NewRequest("GET", route, nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", route, rr.Code)
		}
	}
}

// TestAuthedRoutesReturn200AsAdmin verifies the auth group routes (dashboard,
// catalog, book detail) serve 200 when the caller holds a valid session.
func TestAuthedRoutesReturn200AsAdmin(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	for _, route := range []string{"/", "/catalog", "/books/1"} {
		req, _ := http.NewRequest("GET", route, nil)
		req.AddCookie(sess)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", route, rr.Code)
		}
	}
}

// TestStaffRoutesReturn200AsStaff verifies the staff group routes serve
// 200 for a staff-role session. /admin moved to the admin group when the
// backup tools landed; staff role no longer has access there (covered by
// TestStaffRoleCannotAccessAdminRoutes).
func TestStaffRoutesReturn200AsStaff(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff1", "staff")

	for _, route := range []string{"/patrons"} {
		req, _ := http.NewRequest("GET", route, nil)
		req.AddCookie(sess)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", route, rr.Code)
		}
	}
}

// TestProtectedRoutesRedirectWithoutAuth asserts that every non-public
// route redirects to /login when no session cookie is present. This is
// the regression pin for #35: if a future edit forgets to attach
// RequireAuth to a route group, one of these hits will return 200
// instead of 302 and the test fires.
func TestProtectedRoutesRedirectWithoutAuth(t *testing.T) {
	router, _ := setupTestRouter(t)

	for _, route := range []string{"/", "/catalog", "/books/1", "/books/new", "/books/1/edit", "/patrons", "/admin", "/staff", "/api/openlibrary/isbn/9780553213119"} {
		req, _ := http.NewRequest("GET", route, nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound {
			t.Errorf("%s: expected 302 redirect, got %d", route, rr.Code)
			continue
		}
		if loc := rr.Header().Get("Location"); loc != "/login" {
			t.Errorf("%s: expected redirect to /login, got %q", route, loc)
		}
	}
}

// TestPatronCannotAccessStaffRoutes asserts the RequireStaff middleware
// actually rejects a patron-role session with 403, not 200 or redirect.
// Regression pin for the role chain, separate from the auth chain.
// Covers staff-group routes (/patrons) and admin-group routes (/staff,
// /admin) -- patrons must be rejected by both.
func TestPatronCannotAccessStaffRoutes(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "patron1", "patron")

	for _, route := range []string{"/patrons", "/admin", "/staff", "/books/new", "/books/1/edit", "/api/openlibrary/isbn/9780553213119"} {
		req, _ := http.NewRequest("GET", route, nil)
		req.AddCookie(sess)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d", route, rr.Code)
		}
	}
}

// TestStaffRoleCannotAccessAdminRoutes asserts that a staff-role session
// is rejected by the admin-group middleware chain with 403. /admin moved
// here when the backup tools landed; the admin-only group also covers
// /staff and the backup paths. Regression pin for the RequireAdmin
// boundary: if a future edit accidentally drops RequireAdmin from the
// admin group, a staff session would start passing through and one of
// these hits would fire.
func TestStaffRoleCannotAccessAdminRoutes(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff1", "staff")

	for _, route := range []string{"/staff", "/admin", "/admin/backup"} {
		req, _ := http.NewRequest("GET", route, nil)
		req.AddCookie(sess)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("%s: expected 403 for staff-role session on admin route, got %d", route, rr.Code)
		}
	}
}

// TestLogoutClearsSessionAndRedirectsProtectedRoutes verifies that after
// a successful logout, the session row is gone and protected routes that
// were previously accessible now redirect to /login. Exercises the full
// RequireAuth -> CSRFProtect -> HandleLogout chain via logoutHelper and
// then probes the downstream effect.
func TestLogoutClearsSessionAndRedirectsProtectedRoutes(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin", "admin")

	logoutRR := logoutHelper(t, router, sess, csrf)
	if logoutRR.Code != http.StatusFound {
		t.Fatalf("logout: expected 302, got %d. body: %s", logoutRR.Code, logoutRR.Body.String())
	}

	// Old cookie should now be rejected since the session row is gone.
	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound || rr.Header().Get("Location") != "/login" {
		t.Errorf("after logout, / should redirect to /login; got status=%d location=%q",
			rr.Code, rr.Header().Get("Location"))
	}
}

func TestNotFoundReturns404(t *testing.T) {
	router, _ := setupTestRouter(t)

	req, _ := http.NewRequest("GET", "/doesnotexist", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rr.Code)
	}
}

func TestBookDetailNotFoundReturns404(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	req, _ := http.NewRequest("GET", "/books/9999", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rr.Code)
	}
}

func TestBookDetailNonNumericReturns404(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	req, _ := http.NewRequest("GET", "/books/abc", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rr.Code)
	}
}

// TestResponseContentTypeIsHTML verifies that the renderTemplate helper
// sets Content-Type explicitly (#31). Previously we relied on Go's body
// sniffing, which worked accidentally; the buffer-based rewrite sets it
// explicitly and this test pins that behavior.
func TestResponseContentTypeIsHTML(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin", "admin")

	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Expected Content-Type 'text/html; charset=utf-8', got %q", ct)
	}
}

// setupAuthTestRouter builds a router with only the auth routes and the
// templates they need (login, error). It seeds the default users so that
// realistic login attempts can be made against real bcrypt hashes.
func setupAuthTestRouter(t *testing.T) *gin.Engine {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "libreshelf-auth-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dm := NewDatabaseManager(tmpDir + "/test.sqlite")
	dm.SeedDefaultUsers()

	funcMap := template.FuncMap{
		"deref": func(v interface{}) interface{} {
			switch p := v.(type) {
			case *string:
				if p != nil {
					return *p
				}
			case *int:
				if p != nil {
					return *p
				}
			}
			return ""
		},
	}

	templates = make(map[string]*template.Template)
	templates["login"] = template.Must(template.ParseFiles("templates/login.html"))
	templates["error"] = template.Must(template.New("layout").Funcs(funcMap).ParseFiles(
		"templates/layout.html",
		"templates/error.html",
	))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(DatabaseMiddleware(dm))
	router.GET("/login", HandleLogin)
	router.POST("/login", LoginCSRFProtect, HandleLoginPost)
	return router
}

func postLogin(t *testing.T, router *gin.Engine, username, password string) *httptest.ResponseRecorder {
	t.Helper()

	// Preflight GET /login to obtain the csrf_login cookie and its token value.
	getReq := httptest.NewRequest("GET", "/login", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)

	var csrfCookie *http.Cookie
	for _, cookie := range getRR.Result().Cookies() {
		if cookie.Name == "csrf_login" {
			csrfCookie = cookie
			break
		}
	}
	if csrfCookie == nil {
		t.Fatalf("GET /login did not set csrf_login cookie")
	}

	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)
	form.Set("csrf_token", csrfCookie.Value)
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// TestLoginErrorIsGeneric asserts that both "user does not exist" and
// "user exists but password is wrong" render the same generic error
// message in the response body. Prevents regressions where a future
// edit accidentally leaks which branch was taken.
func TestLoginErrorIsGeneric(t *testing.T) {
	router := setupAuthTestRouter(t)

	const expected = "Invalid username or password"

	fakeRR := postLogin(t, router, "does-not-exist", "irrelevant")
	if !strings.Contains(fakeRR.Body.String(), expected) {
		t.Errorf("fake-user response missing %q in body", expected)
	}

	realRR := postLogin(t, router, "staff1", "wrong-password")
	if !strings.Contains(realRR.Body.String(), expected) {
		t.Errorf("wrong-password response missing %q in body", expected)
	}
}

// TestLoginTimingIsConstant asserts that login requests for nonexistent
// users take roughly the same wall-clock time as login requests for
// existing users with a wrong password. If the handler skips bcrypt when
// the username is missing, the fake-user path will be ~1ms while the
// real-user path will be ~60ms (default bcrypt cost), leaking username
// existence via timing (#33).
func TestLoginTimingIsConstant(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in short mode")
	}

	router := setupAuthTestRouter(t)

	measure := func(username, password string) time.Duration {
		start := time.Now()
		postLogin(t, router, username, password)
		return time.Since(start)
	}

	// Warm up once per path to avoid first-call overhead skewing the first sample.
	measure("warmup-nobody", "x")
	measure("staff1", "x")

	const samples = 15
	fakeDurations := make([]time.Duration, samples)
	realDurations := make([]time.Duration, samples)
	for i := 0; i < samples; i++ {
		fakeDurations[i] = measure("does-not-exist", "irrelevant")
		realDurations[i] = measure("staff1", "wrong-password")
	}

	fakeMedian := medianDuration(fakeDurations)
	realMedian := medianDuration(realDurations)

	t.Logf("fake-user median: %v", fakeMedian)
	t.Logf("real-user median: %v", realMedian)

	// Fail if the fake-user path is less than half the real-user path.
	// With the bug present: fake ~1ms, real ~60ms, ratio ~0.017 -> fail.
	// With the fix: both ~60ms, ratio ~1.0 -> pass.
	if fakeMedian*2 < realMedian {
		t.Errorf("login timing leaks username existence: fake=%v real=%v (fake should be at least half of real)",
			fakeMedian, realMedian)
	}
}

func medianDuration(ds []time.Duration) time.Duration {
	sorted := make([]time.Duration, len(ds))
	copy(sorted, ds)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}

// TestLoginCSRFRejectsMissingCookie asserts that POST /login without a
// csrf_login cookie is rejected by LoginCSRFProtect with 403, even if a
// csrf_token form field is present.
func TestLoginCSRFRejectsMissingCookie(t *testing.T) {
	router := setupAuthTestRouter(t)

	form := url.Values{}
	form.Set("username", "staff1")
	form.Set("password", "irrelevant")
	form.Set("csrf_token", "any-value")
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for missing csrf_login cookie, got %d", rr.Code)
	}
}

// TestLoginCSRFRejectsMismatchedToken asserts that POST /login with a
// csrf_login cookie and a csrf_token form field that don't match is
// rejected with 403.
func TestLoginCSRFRejectsMismatchedToken(t *testing.T) {
	router := setupAuthTestRouter(t)

	form := url.Values{}
	form.Set("username", "staff1")
	form.Set("password", "irrelevant")
	form.Set("csrf_token", "wrong-value")
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_login", Value: "correct-value"})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for mismatched CSRF tokens, got %d", rr.Code)
	}
}

// setupCSRFTestRouter builds a minimal router with CSRFProtect in front
// of GET and POST routes, with a stub middleware that injects a known
// CSRF token into the context. Used to unit-test CSRFProtect behavior
// in isolation from the full auth flow.
func setupCSRFTestRouter(t *testing.T, knownToken string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("csrfToken", knownToken)
		c.Next()
	})
	router.Use(CSRFProtect)
	router.GET("/protected", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	router.POST("/protected", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return router
}

// TestCSRFProtectAllowsGet asserts that CSRFProtect bypasses validation
// for GET/HEAD/OPTIONS requests, since those methods don't change state.
func TestCSRFProtectAllowsGet(t *testing.T) {
	router := setupCSRFTestRouter(t, "any-token")

	req := httptest.NewRequest("GET", "/protected", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", rr.Code)
	}
}

// TestCSRFProtectRejectsMissingToken asserts that CSRFProtect returns 403
// when an unsafe-method request omits the csrf_token form field.
func TestCSRFProtectRejectsMissingToken(t *testing.T) {
	router := setupCSRFTestRouter(t, "known-token")

	req := httptest.NewRequest("POST", "/protected", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for missing csrf_token, got %d", rr.Code)
	}
}

// TestCSRFProtectRejectsMismatchedToken asserts that CSRFProtect returns
// 403 when the form csrf_token differs from the session's token in context.
func TestCSRFProtectRejectsMismatchedToken(t *testing.T) {
	router := setupCSRFTestRouter(t, "known-token")

	form := url.Values{}
	form.Set("csrf_token", "wrong-token")
	req := httptest.NewRequest("POST", "/protected", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for mismatched csrf_token, got %d", rr.Code)
	}
}

// TestCSRFProtectAcceptsMatchingToken asserts that CSRFProtect lets
// through a POST with a csrf_token field that matches the session's token
// in context.
func TestCSRFProtectAcceptsMatchingToken(t *testing.T) {
	router := setupCSRFTestRouter(t, "known-token")

	form := url.Values{}
	form.Set("csrf_token", "known-token")
	req := httptest.NewRequest("POST", "/protected", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for matching csrf_token, got %d. body: %s", rr.Code, rr.Body.String())
	}
}

// TestAuthenticatedPOSTWithCSRF is an end-to-end check that the full
// RequireAuth -> CSRFProtect -> handler chain works for an authenticated
// POST. Creates a session row with a known CSRF token, then verifies
// that POST /logout without the token returns 403 and with the correct
// token performs the logout (redirect + session cookie cleared).
// Protects against regressions where RequireAuth forgets to populate
// csrfToken in context.
func TestAuthenticatedPOSTWithCSRF(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "libreshelf-integration-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dm := NewDatabaseManager(tmpDir + "/test.sqlite")
	dm.SeedDefaultUsers()

	user, err := dm.GetUserByUsername("staff1")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	const knownSession = "test-session-token"
	const knownCSRF = "test-csrf-token"
	if err := dm.CreateSession(knownSession, user.ID, knownCSRF, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(DatabaseMiddleware(dm))
	authGroup := router.Group("/")
	authGroup.Use(RequireAuth, CSRFProtect)
	authGroup.POST("/logout", HandleLogout)

	// Case 1: POST /logout without csrf_token -> 403
	req1 := httptest.NewRequest("POST", "/logout", nil)
	req1.AddCookie(&http.Cookie{Name: "session", Value: knownSession})
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusForbidden {
		t.Errorf("expected 403 for /logout without csrf_token, got %d", rr1.Code)
	}

	// Case 2: POST /logout with correct csrf_token -> 302 redirect
	form := url.Values{}
	form.Set("csrf_token", knownCSRF)
	req2 := httptest.NewRequest("POST", "/logout", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(&http.Cookie{Name: "session", Value: knownSession})
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusFound {
		t.Errorf("expected 302 for /logout with correct csrf_token, got %d. body: %s", rr2.Code, rr2.Body.String())
	}

	// Verify logout cleared the session cookie in the response.
	var sessionCookieCleared bool
	for _, c := range rr2.Result().Cookies() {
		if c.Name == "session" && c.MaxAge < 0 {
			sessionCookieCleared = true
			break
		}
	}
	if !sessionCookieCleared {
		t.Errorf("expected logout to clear session cookie (MaxAge<0), but no cleared cookie found in response")
	}
}
