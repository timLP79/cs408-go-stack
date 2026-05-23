// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"bytes"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// SecurityHeaders sets defensive HTTP response headers on every reply.
// Applied at the router level so even error responses (404, 500) carry
// these headers.
//
// CSP allows 'unsafe-inline' for style-src because the templates rely
// on inline style="..." attrs in many places; tightening that requires
// a template-wide refactor. script-src defaults to 'self' (no inline,
// no eval) since all JS is loaded from /javascripts. img-src allows
// data: for the small data URLs Bootstrap embeds, plus
// covers.openlibrary.org and archive.org (and IA subdomains) for OL
// covers and books.google.com for Google Books covers, so both Lookup
// chains' cover previews can render in the Add/Edit Book form. OL
// covers HTTP-302 redirect to archive.org (Internet Archive's CDN),
// and CSP applies to the final URL after redirect, so both OL hosts
// must be allowlisted. The server already trusts these hosts when
// SaveCoverFromURL fetches the bytes server-side.
//
// HSTS is only useful over HTTPS, which the bare-IP EC2 deploy does
// not have. Gated on APP_ENV=production so a future HTTPS-fronted
// deploy turns it on without code changes.
func SecurityHeaders(c *gin.Context) {
	h := c.Writer.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Content-Security-Policy",
		"default-src 'self'; "+
			"style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data: https://covers.openlibrary.org https://archive.org https://*.archive.org https://books.google.com; "+
			"frame-ancestors 'none'; "+
			"base-uri 'self'; "+
			"form-action 'self'")
	if os.Getenv("APP_ENV") == "production" {
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
	c.Next()
}

func DatabaseMiddleware(dm *DatabaseManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("db", dm)
		c.Next()
	}
}

func getDB(c *gin.Context) *DatabaseManager {
	return c.MustGet("db").(*DatabaseManager)
}

// DBReadLock holds the DatabaseManager's read lock for the duration of
// the request. Allows many concurrent readers but blocks while
// HandleBackupImport holds the write lock during a database swap.
// Routes that need exclusive DB access (only the import endpoint) must
// be in a group that does NOT include this middleware, and must take
// dm.mu.Lock() themselves.
func DBReadLock(c *gin.Context) {
	dm := getDB(c)
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	c.Next()
}

func HandleIndex(c *gin.Context) {
	dm := getDB(c)
	user, _ := c.Get("user")
	u := user.(*User)

	data := gin.H{"Title": "Dashboard"}

	if u.Role == "patron" {
		if u.PatronID == nil {
			log.Printf("HandleIndex: patron user %d has nil patron_id", u.ID)
			c.String(http.StatusInternalServerError, "Internal Server Error")
			return
		}
		loans, err := dm.GetPatronActiveLoans(*u.PatronID)
		if err != nil {
			log.Printf("HandleIndex: GetPatronActiveLoans(%d): %v", *u.PatronID, err)
			c.String(http.StatusInternalServerError, "Internal Server Error")
			return
		}
		data["MyLoanCount"] = len(loans)
		if len(loans) > 0 {
			data["NextDueDate"] = loans[0].DueDate
		}
	} else {
		active, err := dm.CountActiveLoans()
		if err != nil {
			log.Printf("HandleIndex: CountActiveLoans: %v", err)
			c.String(http.StatusInternalServerError, "Internal Server Error")
			return
		}
		overdue, err := dm.CountOverdueLoans()
		if err != nil {
			log.Printf("HandleIndex: CountOverdueLoans: %v", err)
			c.String(http.StatusInternalServerError, "Internal Server Error")
			return
		}
		oos, err := dm.CountOutOfStock()
		if err != nil {
			log.Printf("HandleIndex: CountOutOfStock: %v", err)
			c.String(http.StatusInternalServerError, "Internal Server Error")
			return
		}
		data["ActiveLoans"] = active
		data["OverdueLoans"] = overdue
		data["OutOfStock"] = oos
	}

	renderTemplate(c, "index", data)
}

func HandleAdmin(c *gin.Context) {
	renderTemplate(c, "admin", gin.H{
		"Title": "Admin Tools",
	})
}

func HandleStaffTools(c *gin.Context) {
	renderTemplate(c, "staff_tools", gin.H{
		"Title": "Staff Tools",
	})
}

func HandleNotFound(c *gin.Context) {
	// router.NoRoute fires outside the auth middleware groups, so the
	// "user" context key would be empty even for logged-in users hitting
	// an unknown URL. Resolve the session here so the error template
	// can pick the right CTAs. Silent fail-through to the anonymous
	// branch on any error (no cookie, expired session, etc).
	if _, exists := c.Get("user"); !exists {
		if token, err := c.Cookie("session"); err == nil {
			if session, err := getDB(c).GetSession(token); err == nil {
				c.Set("user", session.User)
			}
		}
	}
	c.Status(http.StatusNotFound)
	renderTemplate(c, "error", gin.H{
		"Title":   "Not Found",
		"Status":  404,
		"Message": "Page not found",
	})
}

func renderTemplate(c *gin.Context, name string, data gin.H) {
	tmpl, ok := templates[name]
	if !ok {
		log.Printf("template not found: %q", name)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if user, exists := c.Get("user"); exists {
		data["User"] = user
	}
	if token, exists := c.Get("csrfToken"); exists {
		data["CSRFToken"] = token
	}
	if _, ok := data["SiteFooter"]; !ok {
		data["SiteFooter"] = siteFooter
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		log.Printf("template execution failed for %q: %v", name, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := c.Writer.Write(buf.Bytes()); err != nil {
		log.Printf("response write failed for %q: %v", name, err)
	}
}

func renderKioskTemplate(c *gin.Context, name string, data gin.H) {
	tmpl, ok := templates[name]
	if !ok {
		log.Printf("template not found: %q", name)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if _, ok := data["SiteFooter"]; !ok {
		data["SiteFooter"] = siteFooter
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "kiosk_layout", data); err != nil {
		log.Printf("template execution failed for %q: %v", name, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := c.Writer.Write(buf.Bytes()); err != nil {
		log.Printf("response write failed for %q: %v", name, err)
	}
}

func renderPage(c *gin.Context, name string, data gin.H) {
	tmpl, ok := templates[name]
	if !ok {
		log.Printf("template not found: %q", name)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("template execution failed for %q: %v", name, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := c.Writer.Write(buf.Bytes()); err != nil {
		log.Printf("response write failed for %q: %v", name, err)
	}
}
