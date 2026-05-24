// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

const (
	flashMaxAgeSeconds    = 60
	flashKindError        = "error"
	flashKindSuccess      = "success"
	flashDetailCookieName = "flash_detail"
	flashDetailMaxBytes   = 255
)

// flashMessages maps a stable code slug to the human-visible banner text.
// The cookie only carries the slug; the map lives server-side so that
// operator-visible strings never transit the cookie jar, access logs, or
// request referers. Add a new entry here whenever a handler emits a new
// code.
var flashMessages = map[string]string{
	"password_mismatch":                  "Password and confirmation did not match.",
	"weak_password":                      "Password does not meet complexity requirements.",
	"invalid_username":                   "Username may only contain letters, numbers, and underscores.",
	"invalid_role":                       "Role must be either 'admin' or 'staff'.",
	"duplicate_username":                 "That username is already taken.",
	"staff_created":                      "Staff account created.",
	"cannot_demote_self":                 "You cannot demote your own account.",
	"cannot_demote_last_admin":           "At least one admin account must remain.",
	"staff_updated":                      "Account updated.",
	"cannot_delete_self":                 "You cannot delete your own account.",
	"cannot_delete_last_admin":           "At least one admin account must remain.",
	"staff_deleted":                      "Account deleted.",
	"password_reset":                     "Password reset. Share the new password with the user through a trusted channel.",
	"book_created":                       "Added to the catalog:",
	"book_created_cover_skipped_offline": "Book created. Cover URL was skipped because offline mode is on.",
	"book_updated":                       "Updated:",
	"book_updated_cover_skipped_offline": "Book updated. Cover URL was skipped because offline mode is on.",
	"book_deleted":                       "Removed from the catalog:",
	"book_has_loans":                     "This book cannot be deleted while it has loan history.",
	"patron_created":                     "Patron added:",
	"patron_updated":                     "Patron updated:",
	"patron_deleted":                     "Patron removed:",
	"patron_has_loans":                   "This patron cannot be removed while they have loan history.",
	"patron_name_required":               "Patron name is required.",
	"patron_name_unusable":               "Could not derive a username from the provided name. Use letters or digits.",
	"loan_checkout_success":              "Checked out:",
	"loan_return_success":                "Book returned.",
	"loan_blocked_overdue":               "Cannot check out. This patron has overdue loans that must be returned first.",
	"loan_blocked_limit":                 "Cannot check out. This patron has reached the maximum of 5 active loans.",
	"loan_no_copies":                     "No copies available to check out.",
	"loan_already_returned":              "This loan has already been returned.",
	"loan_patron_required":               "Please select a patron before checking out.",
	"loan_barcode_required":              "Please scan or type the copy barcode before checking out.",
	"loan_barcode_unknown":               "No copy found with that barcode.",
	"loan_barcode_book_mismatch":         "That barcode belongs to a different title.",
	"copy_added":                         "Copy added. Barcode:",
	"copy_status_updated":                "Copy status updated:",
	"copy_status_invalid":                "Status must be available, lost, damaged, or withdrawn.",
	"copy_deleted":                       "Copy deleted:",
	"copy_has_loans":                     "This copy has loan history and cannot be deleted.",
	"copy_not_found":                     "That copy no longer exists.",
	"backup_imported":                    "Backup restored. The previous database and covers are preserved as .bak files for one-step rollback.",
	"settings_saved":                     "Settings saved.",
	"settings_save_failed":               "Could not save settings. Please try again.",
	"temp_password_dismissed":            "Temporary password marked as delivered.",
	"temp_password_regenerated":          "New temporary password generated.",
	"temp_password_unavailable":          "No temporary password is set for that patron.",
}

func flashCookieName(kind string) string {
	switch kind {
	case flashKindError:
		return "flash_error"
	case flashKindSuccess:
		return "flash_success"
	}
	return ""
}

// setFlash writes a short-lived, HttpOnly, SameSite=Strict cookie
// carrying an error-code slug. The paired readAndClearFlash on the next
// request resolves the slug to a message and clears the cookie so a
// browser refresh does not re-show the banner.
func setFlash(c *gin.Context, kind, code string) {
	name := flashCookieName(kind)
	if name == "" {
		return
	}
	secure := os.Getenv("APP_ENV") == "production"
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(name, code, flashMaxAgeSeconds, "/", "", secure, true)
}

// readAndClearFlash returns the banner message for the kind's flash
// cookie, or "" when no cookie is present. It always clears the cookie
// (MaxAge=-1) in the same response. An unknown code is logged and
// returns "" so a typo surfaces in server logs rather than silently
// showing a blank banner in production.
func readAndClearFlash(c *gin.Context, kind string) string {
	name := flashCookieName(kind)
	if name == "" {
		return ""
	}
	code, err := c.Cookie(name)
	if err != nil || code == "" {
		return ""
	}
	secure := os.Getenv("APP_ENV") == "production"
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(name, "", -1, "/", "", secure, true)
	msg, ok := flashMessages[code]
	if !ok {
		log.Printf("flash: unknown code %q for kind %q", code, kind)
		return ""
	}
	return msg
}

// setFlashDetail writes a short-lived, HttpOnly, SameSite=Strict companion
// cookie carrying free-text detail to pair with a flash message (e.g. the
// title of a just-created book). Gin's c.SetCookie URL-escapes the value
// itself (and c.Cookie decodes it on read), so special characters
// (quotes, UTF-8, spaces) survive the round trip without manual encoding
// here. The caller is responsible for ensuring the detail is already
// normalized; html/template auto-escapes on render so the content is
// XSS-safe when dropped into a banner.
func setFlashDetail(c *gin.Context, detail string) {
	if detail == "" {
		return
	}
	if len(detail) > flashDetailMaxBytes {
		detail = detail[:flashDetailMaxBytes]
	}
	secure := os.Getenv("APP_ENV") == "production"
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(flashDetailCookieName, detail, flashMaxAgeSeconds, "/", "", secure, true)
}

func readAndClearFlashDetail(c *gin.Context) string {
	detail, err := c.Cookie(flashDetailCookieName)
	if err != nil || detail == "" {
		return ""
	}
	secure := os.Getenv("APP_ENV") == "production"
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(flashDetailCookieName, "", -1, "/", "", secure, true)
	if len(detail) > flashDetailMaxBytes {
		detail = detail[:flashDetailMaxBytes]
	}
	return detail
}
