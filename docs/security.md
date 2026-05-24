# LibreShelf -- Security Reference

This document covers the security model, known risks, and mitigations for LibreShelf.
See [`architecture.md`](./architecture.md) for the broader design context and
[`../DECISIONS.md`](../DECISIONS.md) for numbered design decisions referenced below.

---

## Threat Model

LibreShelf is designed for **trusted internal networks** -- a school, office, or home library.

**Assumed environment:**
- Deployed behind nginx on a private or semi-private network
- Staff-facing routes require login; unauthenticated users are redirected to `/login`
- Three roles: **admin** (full access including staff management and system settings), **staff** (day-to-day operations: book/patron CRUD, checkouts, exports), and **patron** (optional login for kiosk features)
- The kiosk is fully public; no login required to browse. Patrons may optionally log in to save searches, favorite books, and request holds on checked-out titles. Checkout and return are handled by admin and staff.

---

## Protections Built Into the Stack

### XSS (Cross-Site Scripting)
Go's `html/template` package **automatically escapes all output**. Any string rendered
in a template is HTML-encoded — `<script>` becomes `&lt;script&gt;`. This protection
is on by default and covers every template in LibreShelf.

**What to avoid:**
```go
// UNSAFE — bypasses escaping entirely
tmpl.Execute(w, template.HTML(userInput))

// SAFE — escaping applied automatically
tmpl.Execute(w, userInput)
```

Never use `template.HTML()`, `template.JS()`, or `template.URL()` on user-supplied data.

---

### SQL Injection
Go's `database/sql` package uses **parameterized queries** with `?` placeholders.
The driver sends the query and parameters separately — user input can never be
interpreted as SQL.

**Always do this:**
```go
// SAFE — parameterized query
row := dm.db.QueryRow("SELECT * FROM books WHERE id = ?", id)

// NEVER do this — SQL injection vulnerability
row := dm.db.QueryRow("SELECT * FROM books WHERE id = " + id)
```

This rule applies to every query in `db.go` without exception.

---

### Transactional Integrity Under Concurrency

The checkout flow is the only place in the system where two staff actions
could race on the same row (the last available copy of a book). The design
that makes this safe (post-DEC-037, now operating on a specific copy id):

1. `CheckoutBook` reads the copy's `status` and counts its active loans
   inside a single transaction, then inserts the loan row in the same tx.
   No "check now, write later" gap.
2. SQLite serializes writers via the journal/WAL lock. With
   `PRAGMA busy_timeout = 5000` (set in `openDB`), a losing concurrent
   writer queues on the lock until the winner commits, then re-evaluates
   the availability guard inside its own transaction and returns
   `ErrNoCopiesAvailable`.

`TestCheckoutBookConcurrentRace` in `db_loans_test.go` pins this with
N=10 goroutines racing to check out a single available copy on behalf of
10 distinct patrons (they all target the same `copy_id`). The assertion
is "exactly one success, exactly nine `ErrNoCopiesAvailable`, zero
other errors, and the derived `available_copies` count for the book
ends at 0." The test passes 100x in a row under
`go test -race -count=100`. If a future refactor moves the
status / on-loan guard outside the transaction, this test catches it
because the success count would exceed one.

`DeleteCopy` follows the same pattern: existence check + loan-history
count + delete are all in one transaction, so a concurrent loan insert
cannot slip between the guard and the delete.

See DEC-031 for the busy-timeout design choice.

---

### CDN Supply Chain
Bootstrap CSS and JS are served from `static/` — not from a CDN. A compromised
CDN cannot inject malicious scripts into LibreShelf pages.

---

## File Upload Security (Cover Images)

When accepting image uploads for book covers:

1. **Validate MIME type** — check the actual file content, not just the extension:
   ```go
   // Read first 512 bytes and detect content type
   buffer := make([]byte, 512)
   n, _ := file.Read(buffer)
   contentType := http.DetectContentType(buffer[:n])
   if contentType != "image/jpeg" && contentType != "image/png" {
       // reject
   }
   ```

2. **Restrict extensions** — only allow `.jpg`, `.jpeg`, `.png`, `.webp`

3. **Limit file size** — reject files over a reasonable limit (e.g. 5MB):
   ```go
   file, err := c.FormFile("cover")
   if file.Size > 5*1024*1024 {
       c.Status(http.StatusRequestEntityTooLarge)
       return
   }
   ```

4. **Sanitize the filename** — never use the user-supplied filename directly:
   ```go
   // Generate a safe filename
   filename := fmt.Sprintf("%d%s", bookID, filepath.Ext(header.Filename))
   savePath := filepath.Join("static", "images", "covers", filename)
   ```

5. **Store outside web root if possible** — or ensure the upload directory has no
   execute permissions.

---

## ZIP Import Security

**Zip Slip** is a vulnerability where a malicious ZIP contains entries with paths like
`../../etc/passwd`. Extracting naively overwrites files outside the intended directory.
LibreShelf's admin import handler routes all extraction through `internal/safezip`, a
purpose-built package whose `SafeExtract(zipPath, destDir)` and
`SafeExtractWithLimits(zipPath, destDir, limits)` enforce six rules per entry **before
any byte hits disk**:

1. **Reject symlinks.** Any ZIP entry with the symlink mode bit fails fast. Symlinks
   are the classic privilege-escalation primitive (extract a symlink `evil -> /etc`,
   then a later entry writes through it).
2. **Reject backslash in names.** Defensive against Windows-style paths that bypass
   `filepath.Join`'s separator on Linux. Locked Q2 decision in DEC-027.
3. **Reject absolute paths.** `filepath.IsAbs(name)` catches `/etc/passwd`-style.
4. **Zip Slip via `filepath.Rel`.** Compute the would-be target with
   `filepath.Join(absDest, name)`, then check that
   `filepath.Rel(absDest, target)` is neither `..` nor begins with `../`.
5. **Per-file size cap.** `MaxFileSize` (default 100 MB) checked from the entry's
   declared `UncompressedSize64`.
6. **Total size cap.** `MaxTotalSize` (default 500 MB) accumulated across all entries
   in pass 1.

**Two-pass atomicity.** `SafeExtract` runs every check in pass 1 against every entry
before pass 2 writes anything. A malicious entry at any position causes the entire
extraction to abort with **no partial state on disk**, which means the import handler
has nothing to roll back if validation fails.

**Zip-bomb defense (lying header).** `extractEntry` wraps each entry's reader in
`io.LimitReader(rc, declaredSize+1)` so a ZIP that lies about its uncompressed size
(small header, gigabyte payload) is caught during the copy via `n > declared`.
Defense-in-depth: Go 1.21+'s `archive/zip` also rejects oversize decompression in
`checksumReader.Read` (returns `archive/zip.ErrFormat`); the safezip layer fires
first on older runtimes or future stdlib regressions.

**Test coverage.** `internal/safezip/extract_test.go` ships 13 cases at 87.5%
coverage: happy paths, all 5 rejection sentinels, atomicity (bad entry at index 5 of
10 leaves zero files on disk), per-file/total/zero-as-unlimited size cases, and a
hand-rolled lying-header bomb fixture that patches both the central directory and
the data descriptor `UncompressedSize` fields. `TestBackupImport_RejectsZipSlip`
exercises the integration path through the admin handler.

The package is exported under `internal/` so the deferred offline cover sync workflow
(see open backlog) can reuse it without a refactor.

See [DEC-027](../DECISIONS.md) for the design rationale, including why the package
lives at `internal/safezip` and why the import does in-process file swapping rather
than a process restart.

---

## HTTP Security Headers

`SecurityHeaders` middleware in `handlers.go` sets defensive headers on every response,
including 404 / 500 error pages. Applied router-wide via `router.Use(SecurityHeaders)` in
`main.go` before any route group, and mirrored in `setupTestRouter` so the test surface
matches production. See [DEC-028](../DECISIONS.md) for the full rationale.

```go
func SecurityHeaders(c *gin.Context) {
    h := c.Writer.Header()
    h.Set("X-Content-Type-Options", "nosniff")
    h.Set("X-Frame-Options", "DENY")
    h.Set("Referrer-Policy", "same-origin")
    h.Set("Content-Security-Policy",
        "default-src 'self'; "+
            "style-src 'self' 'unsafe-inline'; "+
            "img-src 'self' data: https://covers.openlibrary.org https://archive.org https://*.archive.org; "+
            "frame-ancestors 'none'; "+
            "base-uri 'self'; "+
            "form-action 'self'")
    if os.Getenv("APP_ENV") == "production" {
        h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
    }
    c.Next()
}
```

**What each header does:**

| Header | Protection |
|--------|-----------|
| `X-Content-Type-Options: nosniff` | Prevents browser from guessing MIME types -- stops MIME-sniffing attacks where a `.txt` upload renders as HTML |
| `X-Frame-Options: DENY` | Prevents the app from being embedded in an iframe -- stops clickjacking |
| `Referrer-Policy: same-origin` | Stops leaking the current URL to external sites when an admin clicks an outbound link |
| `Content-Security-Policy` | Restricts where scripts/styles/images load from. `default-src 'self'` locks everything to this origin; `frame-ancestors 'none'` mirrors `X-Frame-Options: DENY`; `form-action 'self'` blocks form submissions to other origins |
| `Strict-Transport-Security` | Forces HTTPS for one year. Gated on `APP_ENV=production` because the bare-IP EC2 deploy is HTTP-only -- HSTS over HTTP is harmful (a single insecure response would otherwise pin the browser to HTTPS forever) |

**Trade-off, documented:** `style-src` allows `'unsafe-inline'` because the templates rely
on inline `style="..."` attributes in 22+ places. Tightening this is a multi-day refactor
tracked on the open backlog. `script-src` stays tight at `'self'` (no inline, no eval);
the one inline `<script>` block in `backup_admin.html` was extracted to
`/static/javascripts/admin_backup.js` so this constraint can hold. Any future inline
script will fail loudly with a CSP violation in DevTools console rather than slip in
silently.

**`img-src` exceptions for `https://covers.openlibrary.org` and `https://archive.org`
(plus IA subdomains):** the OL Lookup button on the Add/Edit Book form prefills a
hidden `cover_url` field with the OL cover URL and also stages an `<img>` preview
pointing at the same URL. OL serves cover URLs like
`https://covers.openlibrary.org/b/id/12345-L.jpg` that HTTP-302 redirect to the
Internet Archive CDN at `https://archive.org/download/...` (and occasionally to
`https://ia######.us.archive.org/...`). CSP applies to the final URL after redirect,
so both `covers.openlibrary.org` and `archive.org` (with `*.archive.org` for the
numbered IA hosts) must be allowlisted. Without these the preview renders as a
broken image (saving still works because `SaveCoverFromURL` runs server-side, where
CSP does not apply). Strictly smaller trust extension than what the server already
does -- the Go code already fetches cover bytes from these hosts on every book
create that uses the OL flow. No other external image hosts are allowed.

Coverage: see `handlers_security_test.go` -- pins headers on a public page, on a 404
response, and HSTS on/off based on `APP_ENV`.

---

## HTTPS / TLS

Let's Encrypt does not issue certificates for bare IP addresses; a domain name is required.
For deployments without a domain (development, intranet on raw IP), LibreShelf runs on HTTP
only and the `Secure` cookie flag must stay disabled, otherwise the session cookie is never
sent back to the server and login breaks silently.

For any deployment exposed to the public internet, terminate TLS at nginx with Let's Encrypt:

```nginx
server {
    listen 443 ssl;
    server_name your-domain.com;

    ssl_certificate     /etc/letsencrypt/live/your-domain/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/your-domain/privkey.pem;

    location / {
        proxy_pass http://localhost:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}

# Redirect HTTP to HTTPS
server {
    listen 80;
    return 301 https://$host$request_uri;
}
```

---

## Trusted Proxies

Default Gin trusts every proxy for `X-Forwarded-For` and emits the warning *"You trusted
all proxies, this is NOT safe."* on startup. The fix is in `main.go`, applied at router
construction:

```go
router := gin.Default()
if err := router.SetTrustedProxies([]string{"127.0.0.1"}); err != nil {
    log.Fatalf("failed to set trusted proxies: %v", err)
}
```

This ensures `X-Forwarded-For` and `X-Real-IP` headers are only honored when the
inbound TCP connection comes from `127.0.0.1` (the local nginx reverse proxy).
External clients can still send those headers, but Gin ignores them and uses the
actual TCP source IP for `c.ClientIP()`. The test router in `setupTestRouter` mirrors
this so logging, rate-limiting hooks, and any future IP-based middleware behave
identically in tests and in production.

**If the deployment topology changes** (e.g. a load balancer in front of nginx), this
list must be updated to include the LB's IPs or CIDR. Behind any other topology the
default-trust-everything behavior would re-emerge.

---

## Authentication

LibreShelf uses server-side sessions with three roles: `admin`, `staff`, and `patron`
(see DEC-014 for the role model).

### Password Hashing — bcrypt

Passwords are hashed with `bcrypt` from `golang.org/x/crypto/bcrypt`.
**Never store plain text, MD5, or SHA passwords.**

```go
import "golang.org/x/crypto/bcrypt"

// Hashing (on account creation or password change)
hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

// Verification (on login)
err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(inputPassword))
if err != nil {
    // wrong password — err is bcrypt.ErrMismatchedHashAndPassword
}
```

`bcrypt.DefaultCost` (currently 10) makes hashing intentionally slow — it takes ~100ms.
This is a feature: it makes brute-force attacks expensive.

### Password Complexity Policy (DEC-021)

Every password-setting path must run the candidate through `ValidatePassword` in
`validators.go` before hashing. The rules:

- 8 or more characters
- at least one uppercase letter
- at least one digit
- at least one special character (`unicode.IsPunct` or `unicode.IsSymbol`)

Enforcement points:

- `SeedDefaultUsers` validates `ADMIN_PASSWORD` (env override) at startup and
  `log.Fatalf`s on failure. Seed defaults (`Admin123!`, `Staff123!`, `Patron123!`)
  all satisfy the rule.
- Staff create / password-reset handlers.
- Patron create.
- Any future admin password-change handler.

`ValidateUsername` lives in the same file and enforces 3-32 characters with the
alphanumeric + underscore character class. Username rules are deliberately separate
from password rules because the goals differ (collision avoidance and URL/log
sanity vs. brute-force resistance).

### Session Flow

1. User POSTs credentials to `/login`
2. Server verifies password with `bcrypt.CompareHashAndPassword`
3. On success: generate a cryptographically random session token:
   ```go
   token := make([]byte, 32)
   _, err := crypto_rand.Read(token)
   sessionToken := hex.EncodeToString(token)
   ```
4. Store `(token, user_id, expires_at)` in the `sessions` table
5. Set cookie on the response (see Cookie Security below)
6. On subsequent requests: read cookie, look up token in `sessions`, load user

### Cookie Security
```go
// Secure flag requires HTTPS; stays false for HTTP-only deployments.
// If set to true on an HTTP server, the browser will never send the cookie
// back and login will silently break.
secure := os.Getenv("APP_ENV") == "production"

c.SetSameSite(http.SameSiteStrictMode)  // must be called before SetCookie
c.SetCookie(
    "session",     // name
    sessionToken,  // value
    28800,         // max age (8 hours)
    "/",           // path
    "",            // domain
    secure,        // Secure; true only when HTTPS is available
    true,          // HttpOnly; always true, blocks JS access
)
```

| Attribute | Purpose |
|-----------|---------|
| `HttpOnly` | Prevents JavaScript from reading the cookie; blocks XSS-based session theft |
| `Secure` | Cookie only sent over HTTPS; toggled by `APP_ENV=production` |
| `SameSite=Strict` | Cookie not sent on cross-site requests; first line of defense against CSRF |

> **Note:** for HTTP-only deployments the `Secure` flag is intentionally off; `HttpOnly`
> and `SameSite=Strict` still defend against XSS-based theft and CSRF. Set
> `APP_ENV=production` once HTTPS is in front of the app to flip it on.

### Session Hijacking Prevention
- Tokens generated with `crypto/rand` (cryptographically secure) — never `math/rand`
- Sessions stored server-side in the `sessions` DB table — invalidated on logout
- Session token regenerated after login — prevents session fixation attacks
- 8-hour expiry — user must re-login after expiry
- On logout: delete the session row from the DB immediately

### Force-Change-on-First-Login (DEC-030)

Patrons imported via CSV in "with logins" mode get a server-generated random
temporary password (12 chars, crypto/rand, ambiguous chars excluded, satisfies
`ValidatePassword`). The same flow applies to any future admin-initiated
password reset.

- `users.must_change_password` (default 0). Set to 1 by
  `CreatePatronWithLogin` and `RegenerateTempPassword`. Cleared by
  `UpdateUserPassword` in the same UPDATE that writes the new hash.
- `RequirePasswordCurrent` middleware runs after `RequireAuth` on every route
  group EXCEPT the `account` group (`/account/change-password` and `/logout`).
  When the flag is set, every request redirects to the change-password page.
  Logout is intentionally kept reachable so a stuck patron can sign out and
  retry.
- The change-password page validates against `ValidatePassword`, hashes with
  bcrypt, calls `UpdateUserPassword` (which clears the flag, clears the
  stored temp, and wipes all of the user's sessions), then clears the
  session cookie and redirects to `/login` for re-authentication.

### Temporary Password Storage (DEC-030)

`users.temp_password TEXT` (nullable) stores the plaintext temp for per-row
recovery on `/patrons`. This is a deliberate trade-off: the plaintext sits
in the SQLite file until the patron successfully changes their password
(via `UpdateUserPassword`, which NULLs the column) or until an admin clicks
"Mark as Delivered" (`ClearTempPassword`).

- The reveal page (`GET /patrons/:id/login-credentials`) sets
  `Cache-Control: no-store, no-cache, must-revalidate, private` and
  `Pragma: no-cache` so the response is not retained by the browser cache.
- The credentials CSV download (`GET /admin/patrons/import/download/:token`)
  sets the same headers and is single-use server-side (token is consumed
  on first GET).
- The downloaded credentials CSV and per-row error report defang
  formula-prefix cells (`=`, `+`, `-`, `@`, `\t`, `\r`) by prepending a
  single quote at write time. Prevents attacker-controlled patron names
  from executing as `=HYPERLINK` / `=IMPORTXML` / `=WEBSERVICE` formulas
  in Excel / LibreOffice / Sheets and exfiltrating the adjacent plaintext
  temp password. The patron's true `name` is preserved verbatim in the DB.

`RegenerateTempPassword` is the recovery action when a temp is lost or
believed compromised. It generates a new temp, swaps both the hash and the
stored plaintext, sets `must_change_password=1`, and deletes the user's
existing sessions in a single transaction.

### CSRF Protection

Every state-changing request is protected by a CSRF token. Two separate mechanisms handle
the two cases: authenticated requests use a session-bound synchronizer token; unauthenticated
login requests use a pre-session double-submit cookie. See DEC-017 for the full design
rationale.

**Session-bound token (authenticated routes):**
- On successful login, a cryptographically random token is generated alongside the session
  token and stored on the `sessions` row in the `csrf_token` column.
- `RequireAuth` and `LoadUser` middleware read the session from the DB and set both the
  user and the CSRF token into the gin request context (`c.Set("csrfToken", ...)`).
- `renderTemplate` auto-injects `CSRFToken` into template data (same pattern as `User`).
- Forms embed the token as a hidden field:
  ```html
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  ```
- `CSRFProtect` middleware attached to the auth/staff/admin route groups validates the form
  field against the context token using `subtle.ConstantTimeCompare` on any unsafe-method
  request (POST/PUT/PATCH/DELETE). Safe methods (GET/HEAD/OPTIONS) bypass the check.
- Requests without a valid token are rejected with `403 Forbidden`.

**Pre-session double-submit cookie (`POST /login`):**
- `GET /login` generates a random token, sets it as a `csrf_login` cookie (HttpOnly,
  SameSite=Strict, 10-minute lifetime), and embeds the same token in the login form.
- `LoginCSRFProtect` middleware on `POST /login` reads the cookie and the form field and
  rejects with 403 if either is missing or they do not match.
- On successful login, the `csrf_login` cookie is cleared; the session-bound token takes
  over for all subsequent requests.

**Route wiring:**
```go
router.POST("/login", LoginCSRFProtect, HandleLoginPost)

auth := router.Group("/")
auth.Use(RequireAuth, CSRFProtect)
auth.POST("/logout", HandleLogout)
// ... other auth routes

staff := router.Group("/")
staff.Use(RequireAuth, RequireStaff, CSRFProtect)
// ... staff routes

admin := router.Group("/")
admin.Use(RequireAuth, RequireAdmin, CSRFProtect)
// ... admin routes
```

**Test coverage:** `TestLoginCSRFRejectsMissingCookie`, `TestLoginCSRFRejectsMismatchedToken`,
`TestCSRFProtectAllowsGet`, `TestCSRFProtectRejectsMissingToken`,
`TestCSRFProtectRejectsMismatchedToken`, `TestCSRFProtectAcceptsMatchingToken`, and
`TestAuthenticatedPOSTWithCSRF` (end-to-end `RequireAuth` + `CSRFProtect` + `HandleLogout`).

### Auth Middleware (three roles, chained)

Four middleware functions protect routes in `main.go`. They are chained so each is
single-purpose and stateless (see DEC-014):

- `RequireAuth` validates the session cookie, loads the session, and sets both the user
  and the CSRF token into the request context. Redirects to `/login` on failure.
- `RequireStaff` checks that the role is admin or staff (rejects patron). Must run after
  `RequireAuth`.
- `RequireAdmin` checks that the role is admin. Must run after `RequireAuth`.
- `LoadUser` is the optional-auth variant used on the public kiosk: it attaches the user
  and CSRF token if a session is present but does not redirect otherwise.

```go
// RequireAuth: any logged-in user
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

// RequireStaff: admin or staff (rejects patron)
// RequireAdmin: admin only
// Both must run after RequireAuth in the middleware chain.
```

Applied in `main.go` (see also DEC-017 for the CSRF middleware on each group):
```go
// Public routes
router.GET("/login", HandleLogin)
router.POST("/login", LoginCSRFProtect, HandleLoginPost)
router.GET("/kiosk", HandleKiosk)

// Authenticated (any logged-in user)
auth := router.Group("/")
auth.Use(RequireAuth, CSRFProtect)
auth.GET("/", HandleIndex)
auth.GET("/catalog", HandleCatalog)
auth.GET("/books/:id", HandleBookDetail)
auth.POST("/logout", HandleLogout)

// Staff (admin + staff)
staff := router.Group("/")
staff.Use(RequireAuth, RequireStaff, CSRFProtect)
staff.GET("/patrons", HandlePatrons)
staff.GET("/admin", HandleAdmin)

// Admin-only
admin := router.Group("/")
admin.Use(RequireAuth, RequireAdmin, CSRFProtect)
admin.GET("/staff", HandleStaff)
admin.GET("/admin", HandleAdminIndex)
admin.GET("/admin/backup", HandleBackupAdmin)
```

---

## Data Privacy

- `data/database.sqlite` is **gitignored** — never committed to the repository
- Patron emails are **optional** — never required, never logged
- Set restricted permissions on the data directory on the server:
  ```bash
  chmod 700 data/
  ```
- Do not include patron data in error messages or log output
- **Role-gated views.** Loan history (which includes other patrons' names and checkout dates) and the checkout form on the book detail page are visible only to admin and staff. Patrons see the book info and availability but never see who else has borrowed a book. Template conditionals must match the role model even for disabled or scaffolded controls, because visibility itself leaks information.

---

## Dependency Security

Before any deploy:
```bash
# Verify all dependencies match go.sum checksums
go mod verify

# Check for known vulnerabilities in dependencies
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

Keep `go.sum` committed; it ensures reproducible, tamper-evident builds.

---

## Pre-Deploy Checklist

- [ ] All DB queries use `?` parameterized placeholders -- no string concatenation
- [ ] All user input is validated server-side before use
- [ ] Error responses do not leak internal paths, stack traces, or SQL
- [ ] `go mod verify` passes
- [ ] `govulncheck ./...` clean
- [ ] `data/` directory permissions appropriate for the runtime user (systemd default umask)
- [ ] HTTPS configured in nginx (or `APP_ENV` left unset for HTTP-only intranet deployments)
