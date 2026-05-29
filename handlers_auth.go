// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// dummyPasswordHash is compared against in HandleLoginPost when the
// supplied username does not exist, so the response time for an unknown
// username matches the response time for a known one. Skipping bcrypt
// on the unknown-username path leaks user existence via timing.
var dummyPasswordHash []byte

func init() {
	hash, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("failed to generate dummy bcrypt hash: %v", err)
	}
	dummyPasswordHash = hash
}

func generateSessionToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func RequireAuth(c *gin.Context) {
	token, err := c.Cookie("session")
	if err != nil {
		c.Redirect(http.StatusFound, "/login")
		c.Abort()
		return
	}

	dm := getDB(c)
	session, err := dm.GetSession(token)
	if err != nil {
		c.Redirect(http.StatusFound, "/login")
		c.Abort()
		return
	}

	c.Set("user", session.User)
	c.Set("csrfToken", session.CSRFToken)
	c.Next()
}

func RequirePatron(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.Redirect(http.StatusFound, "/login")
		c.Abort()
		return
	}

	if user.(*User).Role != "patron" {
		c.Status(http.StatusForbidden)
		renderTemplate(c, "error", gin.H{
			"Title":   "Forbidden",
			"Status":  403,
			"Message": "You don't have permission to access this page.",
		})
		c.Abort()
		return
	}

	c.Next()
}

func RequireAdmin(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.Redirect(http.StatusFound, "/login")
		c.Abort()
		return
	}

	if user.(*User).Role != "admin" {
		c.Status(http.StatusForbidden)
		renderTemplate(c, "error", gin.H{
			"Title":   "Forbidden",
			"Status":  403,
			"Message": "You don't have permission to access this page.",
		})
		c.Abort()
		return
	}

	c.Next()
}

func RequireStaff(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.Redirect(http.StatusFound, "/login")
		c.Abort()
		return
	}

	if user.(*User).Role == "patron" {
		c.Status(http.StatusForbidden)
		renderTemplate(c, "error", gin.H{
			"Title":   "Forbidden",
			"Status":  403,
			"Message": "You don't have permission to access this page.",
		})
		c.Abort()
		return
	}

	c.Next()
}

func RequirePasswordCurrent(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.Next()
		return
	}
	if user.(*User).MustChangePassword {
		c.Redirect(http.StatusFound, "/account/change-password")
		c.Abort()
		return
	}
	c.Next()
}

func RequireStaffImportAccess(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.Redirect(http.StatusFound, "/login")
		c.Abort()
		return
	}
	u := user.(*User)
	if u.Role == "admin" {
		c.Next()
		return
	}
	if u.Role == "staff" {
		dm := getDB(c)
		if dm.GetSettingBool("staff_can_import_patrons", false) {
			c.Next()
			return
		}
	}
	c.Status(http.StatusForbidden)
	renderTemplate(c, "error", gin.H{
		"Title":   "Forbidden",
		"Status":  403,
		"Message": "You don't have permission to access this page.",
	})
	c.Abort()
}

func renderLoginForm(c *gin.Context, errorMsg string) {
	csrfToken, err := generateSessionToken()
	if err != nil {
		log.Printf("failed to generate per-session CSRF token: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	secure := os.Getenv("APP_ENV") == "production"
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("csrf_login", csrfToken, 600, "/", "", secure, true)

	data := gin.H{
		"Title":     "Login",
		"CSRFToken": csrfToken,
	}
	if errorMsg != "" {
		data["Error"] = errorMsg
	}
	renderPage(c, "login", data)
}

func HandleLogin(c *gin.Context) {
	renderLoginForm(c, "")
}

func HandleLoginPost(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	dm := getDB(c)
	user, err := dm.GetUserByUsername(username)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("login lookup failed for %q: %v", username, err)
		renderLoginForm(c, "Something went wrong.")
		return
	}

	hashToCompare := dummyPasswordHash
	if user != nil {
		hashToCompare = []byte(user.PasswordHash)
	}
	bcryptErr := bcrypt.CompareHashAndPassword(hashToCompare, []byte(password))

	if user == nil || bcryptErr != nil {
		renderLoginForm(c, "Invalid username or password.")
		return
	}

	token, err := generateSessionToken()
	if err != nil {
		log.Printf("failed to generate session token: %v", err)
		renderLoginForm(c, "Something went wrong.")
		return
	}

	csrfToken, err := generateSessionToken()
	if err != nil {
		log.Printf("failed to generate session CSRF token: %v", err)
		renderLoginForm(c, "Something went wrong.")
		return
	}

	expiresAt := time.Now().Add(8 * time.Hour)
	if err := dm.CreateSession(token, user.ID, csrfToken, expiresAt); err != nil {
		log.Printf("Failed to create session: %v", err)
		renderLoginForm(c, "Something went wrong.")
		return
	}

	c.SetCookie("csrf_login", "", -1, "/", "", false, true)

	secure := os.Getenv("APP_ENV") == "production"
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("session", token, 8*60*60, "/", "", secure, true)
	c.Redirect(http.StatusFound, "/")
}

func HandleLogout(c *gin.Context) {
	// Soft-fail CSRF: a stale or missing token (e.g. a re-login in another
	// tab rotated the session) should still log the user out rather than
	// 403. Logout is idempotent and destroys the session regardless. We log
	// mismatches for visibility. See DEC-038.
	if sessionToken, ok := c.Get("csrfToken"); ok {
		if subtle.ConstantTimeCompare([]byte(c.PostForm("csrf_token")), []byte(sessionToken.(string))) != 1 {
			log.Printf("HandleLogout: CSRF token mismatch (soft-fail, proceeding)")
		}
	}

	token, err := c.Cookie("session")
	if err == nil {
		dm := getDB(c)
		dm.DeleteSession(token)
	}

	c.SetCookie("session", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}

func CSRFProtect(c *gin.Context) {
	method := c.Request.Method
	if method == "GET" || method == "HEAD" || method == "OPTIONS" {
		c.Next()
		return
	}

	sessionToken, exists := c.Get("csrfToken")
	if !exists {
		log.Printf("CSRFProtect: no csrfToken in context for %s %s", method, c.Request.URL.Path)
		c.String(http.StatusForbidden, "CSRF validation failed")
		c.Abort()
		return
	}

	formToken := c.PostForm("csrf_token")
	if subtle.ConstantTimeCompare([]byte(formToken), []byte(sessionToken.(string))) != 1 {
		log.Printf("CSRFProtect: token mismatch for %s %s", method, c.Request.URL.Path)
		c.String(http.StatusForbidden, "CSRF validation failed")
		c.Abort()
		return
	}

	c.Next()
}

func LoginCSRFProtect(c *gin.Context) {
	cookieToken, err := c.Cookie("csrf_login")
	if err != nil || cookieToken == "" {
		log.Printf("LoginCSRFProtect: missing csrf_login cookie")
		c.String(http.StatusForbidden, "CSRF validation failed")
		c.Abort()
		return
	}

	formToken := c.PostForm("csrf_token")
	if subtle.ConstantTimeCompare([]byte(formToken), []byte(cookieToken)) != 1 {
		log.Printf("LoginCSRFProtect: token mismatch")
		c.String(http.StatusForbidden, "CSRF validation failed")
		c.Abort()
		return
	}

	c.Next()
}
