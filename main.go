// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

var templates map[string]*template.Template

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "database.sqlite"
	}

	dm := NewDatabaseManager(dataDir + "/" + dbName)
	dm.SeedDefaultUsers()
	initSiteFooter()

	if IsOfflineEnvLocked() {
		log.Printf("LIBRESHELF_OFFLINE=true is locking offline mode; runtime DB setting will be ignored until the env var is unset.")
	}

	// LIBRESHELF_SEED_DEV_BOOKS opt-in seeds the dev fixture catalog
	// (~10 well-known titles + one library-format copy per quantity
	// slot) and runs the Open Library cover backfill on those seeds.
	// Production installs leave this unset and start with an empty
	// catalog; the admin adds books through the CRUD UI. Tests call
	// SeedBooks directly via setupTestRouter, independent of this gate.
	if os.Getenv("LIBRESHELF_SEED_DEV_BOOKS") != "" {
		dm.SeedBooks()

		// Opportunistically backfill covers from Open Library for any book
		// that has an ISBN but no cover file yet. 60s total budget so a
		// slow OL (or network block) cannot wedge the server at boot --
		// the inner HTTP client also has its own 10s per-request timeout.
		seedCoverCtx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		dm.FetchAndStoreSeedCovers(seedCoverCtx)
		cancel()
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
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict requires an even number of arguments")
			}
			m := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict keys must be strings, got %T at position %d", values[i], i)
				}
				m[key] = values[i+1]
			}
			return m, nil
		},
	}

	templates = make(map[string]*template.Template)
	templateNames := []string{
		"index", "catalog", "book_detail", "book_form",
		"book_copies", "inventory",
		"patrons", "admin", "staff", "staff_tools", "loans", "my_loans",
		"reports_overdue", "overdue_notice",
		"backup_admin", "admin_settings",
		"admin_patrons_import", "admin_patrons_import_preview", "admin_patrons_import_result",
		"patron_login_credentials",
	}
	for _, name := range templateNames {
		files := []string{
			"templates/layout.html",
			"templates/" + name + ".html",
		}
		if name == "book_copies" || name == "inventory" {
			files = append(files, "templates/_copy_partials.html")
		}
		templates[name] = template.Must(template.New("layout").Funcs(funcMap).ParseFiles(files...))
	}

	kioskTemplateNames := []string{"kiosk", "kiosk_book_detail"}
	for _, name := range kioskTemplateNames {
		templates[name] = template.Must(template.New("kiosk_layout").Funcs(funcMap).ParseFiles(
			"templates/kiosk_layout.html",
			"templates/"+name+".html",
		))
	}

	templates["login"] = template.Must(template.ParseFiles(
		"templates/login.html",
	))

	templates["account_change_password"] = template.Must(template.ParseFiles(
		"templates/account_change_password.html",
	))

	templates["error"] = template.Must(template.New("layout").Funcs(funcMap).ParseFiles(
		"templates/layout.html",
		"templates/error.html",
	))

	router := gin.Default()

	// Trust only the local nginx reverse proxy for X-Forwarded-* headers.
	// Default Gin trusts every proxy, which lets any client spoof their
	// IP via X-Forwarded-For. The EC2 deployment fronts the Go app with
	// nginx on localhost; behind any other topology this list needs to
	// change.
	if err := router.SetTrustedProxies([]string{"127.0.0.1"}); err != nil {
		log.Fatalf("failed to set trusted proxies: %v", err)
	}

	// Defensive headers on every response, including 404/500.
	router.Use(SecurityHeaders)

	router.Static("/stylesheets", "static/stylesheets")
	router.Static("/javascripts", "static/javascripts")
	router.Static("/images", "static/images")
	if err := os.MkdirAll(coversDir(), 0o755); err != nil {
		log.Fatalf("failed to create covers dir: %v", err)
	}
	router.Static("/covers", coversDir())

	router.Use(DatabaseMiddleware(dm))

	// Public routes
	router.GET("/login", HandleLogin)
	router.POST("/login", LoginCSRFProtect, HandleLoginPost)
	router.GET("/kiosk", HandleKiosk)
	router.GET("/kiosk/books/:id", HandleKioskBookDetail)

	// Authenticated routes -- any logged in user
	auth := router.Group("/")
	auth.Use(RequireAuth, RequirePasswordCurrent, CSRFProtect, DBReadLock)
	auth.GET("/", HandleIndex)
	auth.GET("/catalog", HandleCatalog)
	auth.GET("/books/:id", HandleBookDetail)

	// Account routes -- no RequirePasswordCurrent here so the change-
	// password page and logout remain reachable while the flag is set,
	// otherwise users with must_change_password=1 can't unstick themselves.
	account := router.Group("/")
	account.Use(RequireAuth, CSRFProtect, DBReadLock)
	account.GET("/account/change-password", HandleChangePassword)
	account.POST("/account/change-password", HandleChangePasswordPost)
	account.POST("/logout", HandleLogout)

	// Patron-only routes
	patron := router.Group("/")
	patron.Use(RequireAuth, RequirePasswordCurrent, RequirePatron, CSRFProtect, DBReadLock)
	patron.GET("/my/loans", HandleMyLoans)

	// Staff routes -- admin + staff
	staff := router.Group("/")
	staff.Use(RequireAuth, RequirePasswordCurrent, RequireStaff, CSRFProtect, DBReadLock)
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
	staff.POST("/books/:id/copies", HandleAddCopy)
	staff.GET("/books/:id/copies", HandleBookCopies)
	staff.GET("/inventory", HandleInventory)
	staff.POST("/copies/:id/status", HandleCopyStatus)
	staff.POST("/copies/:id/delete", HandleCopyDelete)
	staff.POST("/loans/:id/return", HandleReturn)
	staff.GET("/loans", HandleLoansList)
	staff.GET("/reports/overdue", HandleReportsOverdue)
	staff.GET("/reports/overdue/patron/:id/notice", HandleOverdueNotice)
	staff.GET("/staff-tools", HandleStaffTools)

	// Admin-only routes (read-locked like everything else)
	admin := router.Group("/")
	admin.Use(RequireAuth, RequirePasswordCurrent, RequireAdmin, CSRFProtect, DBReadLock)
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

	// Patron import -- gated by RequireStaffImportAccess so admins
	// always reach it, staff only when staff_can_import_patrons is on.
	patronImport := router.Group("/")
	patronImport.Use(RequireAuth, RequirePasswordCurrent, RequireStaffImportAccess, CSRFProtect, DBReadLock)
	patronImport.GET("/admin/patrons/import", HandlePatronImportForm)
	patronImport.POST("/admin/patrons/import", HandlePatronImportPreview)
	patronImport.POST("/admin/patrons/import/confirm", HandlePatronImportCommit)
	patronImport.GET("/admin/patrons/import/download/:token", HandleImportDownload)

	// Admin write routes -- swap the DB out from under everyone else.
	// No DBReadLock; the import handler takes dm.mu.Lock() directly,
	// since Go's sync.RWMutex cannot upgrade a read lock to a write lock.
	adminWrite := router.Group("/")
	adminWrite.Use(RequireAuth, RequirePasswordCurrent, RequireAdmin, CSRFProtect)
	adminWrite.POST("/admin/backup/import", HandleBackupImport)

	router.NoRoute(HandleNotFound)

	router.Run(":" + port)
}
