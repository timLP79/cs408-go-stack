# LibreShelf -- Design Decisions

Architectural and design decisions made during development. Each entry records the context,
the decision, and the reasoning so future sessions start with full understanding.

---

## DEC-001: Bootstrap served locally, not via CDN

**Date:** 2026-02-01 (CP1)
**Context:** The product specification listed "Bootstrap 5 via CDN." The app is designed to be
self-hostable and offline-capable.
**Decision:** Serve Bootstrap from `static/stylesheets/` and `static/javascripts/` instead of a CDN.
**Rationale:** Eliminates CDN supply chain risk and supports offline-first architecture. The spec
PDF cannot be changed but this divergence is intentional.

---

## DEC-002: Pure-Go SQLite driver (no CGo)

**Date:** 2026-02-01 (CP1)
**Context:** Two main SQLite options in Go: `mattn/go-sqlite3` (CGo) and `modernc.org/sqlite` (pure Go).
**Decision:** Use `modernc.org/sqlite`, which registers as driver `"sqlite"` (not `"sqlite3"`).
**Rationale:** No CGo means no C compiler dependency, simpler cross-compilation, and easier
deployment. Pure Go binary with zero system library requirements.

---

## DEC-003: Flat package structure

**Date:** 2026-02-01 (CP1)
**Context:** Go projects can use sub-packages (`internal/handlers/`, `internal/db/`, etc.) or keep
everything in `package main`.
**Decision:** All `.go` files in `package main`, split by concern using filename suffixes
(`handlers_books.go`, `handlers_patrons.go`, etc.).
**Rationale:** The app is medium-sized. Sub-packages would add import indirection without meaningful
benefit at this scale.

---

## DEC-004: Session cookies over JWT

**Date:** 2026-03-01 (CP2)
**Context:** Two common approaches for web app auth: server-side sessions with cookies, or
stateless JWT tokens.
**Decision:** Server-side sessions stored in the `sessions` DB table, with `HttpOnly`,
`SameSite=Strict` cookies. Token generated with `crypto/rand`.
**Rationale:** Server-side sessions can be invalidated immediately (logout, compromise). JWTs
cannot be revoked without maintaining a blocklist, which negates the stateless benefit. For a
server-rendered app with no separate API clients, sessions are simpler and more secure.

**Addendum (2026-04-10, CP4):** `SameSite=Strict` was documented as a decision here but the
original CP2 code never actually called `c.SetSameSite(http.SameSiteStrictMode)` before
`c.SetCookie`, so the browser used its default (Lax). Fixed while working on CSRF (#32) in CP4.
`HandleLoginPost` now explicitly sets Strict mode on both the session cookie and the
`csrf_login` pre-session cookie.

---

## DEC-005: bcrypt for password hashing

**Date:** 2026-03-01 (CP2)
**Context:** Password hashing options include bcrypt, scrypt, argon2.
**Decision:** Use `golang.org/x/crypto/bcrypt` with default cost factor.
**Rationale:** bcrypt is well-understood, widely supported, and the cost factor provides natural
brute-force resistance. Good enough for this application; argon2 would be overkill.

---

## DEC-006: Public kiosk with optional login

**Date:** 2026-03-13 (CP2 design update)
**Context:** Original design had the kiosk as an authenticated page. Reconsidered whether public
browsing should require login.
**Decision:** `GET /kiosk` is fully public (no auth required). Patrons can optionally log in to
access favorites and holds. Checkout and return remain staff-only on the book detail page.
**Rationale:** A kiosk in a real library should let anyone walk up and browse. Requiring login
defeats the purpose. Patron features (favorites, holds) are value-adds that justify optional login.

---

## DEC-007: SSE for real-time availability (not WebSocket)

**Date:** 2026-03-01 (deferred post-submission as of the 2026-04-19 CP6 v2 trim)
**Context:** Real-time availability updates need to push from server to browser.
**Decision:** Use Server-Sent Events (`GET /events`) instead of WebSocket when the feature is built.
**Rationale:** SSE is one-way (server to browser), which is exactly what this use case needs.
Simpler than WebSocket, works over HTTP/1.1, auto-reconnects, and requires no additional
dependencies.
**Status update 2026-04-19:** The SSE feature itself is deferred post-submission. CP6 ships
loans without live availability broadcast; a page reload is the signal. The technology choice
recorded here stands for whenever the feature is built.

---

## DEC-008: Open Library API via server-side proxy

**Date:** 2026-03-01 (planned for CP4)
**Context:** ISBN lookup needs to call the Open Library API to auto-fill book metadata.
**Decision:** Server-side proxy endpoint (`GET /api/openlibrary?isbn=...`) fetches data and
returns clean JSON to the client.
**Rationale:** Avoids CORS issues with direct browser requests. Keeps client JS simple. Allows
server-side ISBN format validation before making the outbound request.

---

## DEC-009: ZIP export/import with standard library

**Date:** 2026-03-01 (planned for CP7)
**Context:** Admin needs to export and import the full database and cover images.
**Decision:** Use Go's `archive/zip` standard library package.
**Rationale:** No third-party dependency needed. ZIP contains the SQLite file and cover images
from `data/covers/` (the DATA_DIR-relative path since cover storage moved in #20).

---

## DEC-010: WAL mode for SQLite

**Date:** 2026-03-01 (CP2)
**Context:** SQLite default journal mode blocks readers during writes.
**Decision:** Enable WAL mode on startup (`PRAGMA journal_mode=WAL`).
**Rationale:** Required by the product specification. WAL allows concurrent reads during writes,
which matters when SSE connections are reading while staff actions are writing.

---

## DEC-011: Pointer types for nullable database columns

**Date:** 2026-04-04 (CP3)
**Context:** Book fields like ISBN, publisher, description, and genre are optional (nullable in SQLite).
Scanning NULL into a Go `string` or `int` causes a runtime error.
**Decision:** Use pointer types (`*string`, `*int`) for nullable columns in Go structs. Register a
`deref` template function to safely unwrap pointers in templates.
**Rationale:** Pointer types make nullability explicit at the type level. The `deref` helper keeps
templates clean without requiring nil checks on every optional field.

---

## DEC-012: Client-side catalog filtering (temporary)

**Date:** 2026-04-04 (CP3)
**Context:** The catalog needs search, genre filtering, and availability filtering. Options: server-side
query params with page reloads, or client-side filtering over the full dataset.
**Decision:** All books are rendered server-side into the page. JavaScript in `app.js` filters by
toggling `display: none` on card elements using data attributes.
**Rationale:** Fastest path to a working catalog for CP3. This approach does not scale to larger
collections (thousands of books would hurt page load and DOM performance). Server-side pagination
and query-based filtering should replace this before production use.

---

## DEC-013: Template loading pattern

**Date:** 2026-02-01 (CP1), updated 2026-04-04 (CP3)
**Context:** Go templates can be loaded individually, as a global set, or as page-specific pairs.
**Decision:** `map[string]*template.Template` with one entry per page, each paired with
`layout.html`. The `renderTemplate()` helper executes the `"layout"` template which pulls in
the page's `"content"` block. As of CP3, templates are created with `template.New("layout").Funcs(funcMap)`
to register custom functions before parsing.
**Rationale:** Simple, explicit, and avoids the global template namespace collisions that come
from `template.ParseGlob()`. Each page knows exactly which templates it includes.

---

## DEC-014: Three-role access model (admin, staff, patron)

**Date:** 2026-04-06 (CP4)
**Context:** The original two-role model (admin/patron) had no way to give operational access
without full admin privileges. Different deployment environments need a middle tier for
day-to-day workers who can handle books, view patrons, and run exports, but should not
manage user accounts or system settings.
**Decision:** Three roles with chained middleware. `RequireAuth` validates the session and sets
the user in context. `RequireStaff` checks that the role is not patron (allows admin + staff).
`RequireAdmin` checks that the role is admin. Route groups chain these:
`RequireAuth, RequireStaff` for operational routes, `RequireAuth, RequireAdmin` for admin-only routes.
**Rationale:** Chaining keeps each middleware single-purpose and avoids duplicating session lookup
logic. The staff role fills the gap between full admin and patron without overcomplicating the
permission model.

---

## DEC-015: Admin page shared with role-based template conditionals

**Date:** 2026-04-06 (CP4)
**Context:** Both admin and staff need access to the admin tools page (export/import, system stats),
but admin has additional privileges (staff management, system settings).
**Decision:** Single `/admin` route in the staff group (accessible to admin + staff). The template
uses `{{if and .User (eq .User.Role "admin")}}` to conditionally show admin-only sections.
Admin-only POST endpoints are registered in the admin route group, not the staff group.
**Rationale:** Template conditionals control what the user sees; route group middleware enforces
what the user can do. This avoids duplicate pages and is consistent with the pattern used in the
sidebar, dashboard, and book detail templates. Security does not depend on the template -- even
if a staff user crafted a direct request to an admin-only endpoint, the middleware would return 403.

---

## DEC-016: Patron metadata as JSON TEXT column

**Date:** 2026-04-06 (CP5 design)
**Context:** Different deployment environments need different extra fields on patron records
(external IDs, housing units, departments, etc.). Options: EAV table (patron_custom_fields),
JSON column, or hardcoded extra columns.
**Decision:** Add a nullable `metadata TEXT` column to the patrons table. Stores JSON for
environment-specific fields. NULL for manually-added patrons. CSV imports store extra columns
as JSON in this field.
**Rationale:** JSON columns are the modern standard for flexible metadata. SQLite has built-in
JSON functions (`json_extract`) for querying. Avoids the EAV anti-pattern and keeps the schema
clean. If the project ever migrates to PostgreSQL, JSONB provides even richer support.

---

## DEC-017: Session-bound CSRF tokens with double-submit cookie for login

**Date:** 2026-04-10 (CP4)
**Context:** Every state-changing form needs CSRF protection. Several patterns exist:
double-submit cookie (non-HttpOnly cookie with a token, form embeds the same token, middleware
compares); synchronizer token bound to the server-side session (token stored on the session
row, template reads it, middleware validates); per-form tokens (new token on each render,
tracked in a pool).
**Decision:** **Session-bound synchronizer tokens** for authenticated routes, generated at
login and stored on the `sessions` row in a new `csrf_token NOT NULL` column. The token is
injected into the request context by `RequireAuth`/`LoadUser` alongside the user, then
auto-injected into template data by `renderTemplate` (same pattern as `User`), so forms
only need `<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">`. A new
`CSRFProtect` middleware validates unsafe-method requests against the context token using
`subtle.ConstantTimeCompare`. For `POST /login` where no session exists yet, a separate
**pre-session double-submit cookie** named `csrf_login` is set on `GET /login` (HttpOnly,
SameSite=Strict, 10-minute lifetime) and compared against the form field by a dedicated
`LoginCSRFProtect` middleware. The pre-session cookie is cleared once a real session exists.
**Rationale:** Session-bound tokens are the textbook-correct pattern for server-rendered apps
with sessions and bind CSRF state to authentication state for free (logout deletes both).
Adding a column to `sessions` was a 1:1 extension with no extra joins. Per-form tokens would
add complexity (token pool, tracking, expiry) without meaningful security gain for LibreShelf's
threat model. The double-submit cookie for login is scoped narrowly to the authentication
chicken-and-egg and does not bleed into the normal request path. Defense in depth: the
SameSite=Strict cookie attribute (also implemented in CP4, see DEC-004 addendum) provides a
first line of defense before middleware ever runs.

---

## DEC-018: Canonical UTC datetime format for session expiry

**Date:** 2026-04-10 (CP4, discovered during CSRF integration testing)
**Context:** `CreateSession` originally passed `time.Time` directly to `dm.db.Exec`. The
modernc.org/sqlite driver serialized this as ISO 8601 with the local timezone offset and
nanosecond precision (e.g. `2026-04-10T22:22:05.168055574-06:00`). SQLite's `CURRENT_TIMESTAMP`
returns canonical UTC in `YYYY-MM-DD HH:MM:SS` format. The `GetSession` WHERE clause used
`expires_at > CURRENT_TIMESTAMP`, which is a **lexicographic string comparison** of two
differently-formatted strings. On non-UTC systems (e.g. local dev in MDT) the string comparison
failed even when the absolute times were correct, so freshly-created sessions were reported as
expired. The UTC EC2 deployment was accidentally masking this because the date portions matched.
Additionally, SQLite's `datetime()` normalization function could not parse the driver's
9-digit fractional seconds, so wrapping the comparison in `datetime()` returned NULL.
**Decision:** `CreateSession` formats `expires_at` explicitly as
`expiresAt.UTC().Format("2006-01-02 15:04:05")` before insert, producing a canonical UTC
string that is byte-identical in format to `CURRENT_TIMESTAMP`. String comparison then works
correctly as chronological comparison. `GetSession` keeps `datetime()` wrappers on both sides
of the comparison as defense in depth against future callers that might bypass `CreateSession`.
**Rationale:** Storing datetimes in a canonical format at write time is more robust than
normalizing at every read. Formatting in UTC removes the timezone-comparison trap entirely.
The `"2006-01-02 15:04:05"` layout is Go's reference time; it produces exactly the format
SQLite's `CURRENT_TIMESTAMP` returns. Discovered via `TestAuthenticatedPOSTWithCSRF`, which
was the first test to exercise the full `CreateSession` to `GetSession` round trip and caught
the latent bug immediately.

---

## DEC-019: Type-to-confirm over delayed deletion queue for staff accounts

**Date:** 2026-04-16 (CP5 design)
**Context:** Admin staff deletion needs a safety net against accidental or mistaken deletion.
Proposed design was a 48-hour delayed-deletion queue with cancellation. Evaluated against
LibreShelf's actual threat model (single-admin class deployment) and the engineering cost.
**Decision:** Use a type-to-confirm modal instead of a delayed queue. The delete button opens
a Bootstrap modal containing the target username. The confirm button stays disabled until
the admin types the username exactly. Deletion is immediate on confirm. No background job,
no pending state, no cancellation flow. Last-admin and self-deletion checks run server-side
and also gate the UI (delete button is disabled on rows that violate either rule).
**Rationale:** A 48-hour queue would require a new pending-deletions table, a startup
catch-up pass, a periodic scheduler goroutine, a cancellation UI, and a dual last-admin
check (at schedule time and execution time, because two admins could schedule each other
and leave zero admins). Each of those is a non-trivial lift and would need its own tests.
Type-to-confirm captures most of the "oops" protection value in one modal with roughly
fifty lines of client JS. Consistent with the Absolute Code philosophy: build the simpler
thing first; revisit if the threat model changes.

---

## DEC-020: Combined username + role edit, restricted role transitions

**Date:** 2026-04-16 (CP5 design)
**Context:** Staff management needs to support renames and role changes. Options: separate
endpoints per field (cleaner REST but more handlers), one combined edit endpoint, or
promote/demote as explicit actions.
**Decision:** One `POST /staff/:id/edit` endpoint updates both username and role in a
single call. Allowed role transitions are `staff <-> admin` only. Patron role changes are
out of scope and will be handled in `/patrons` (#21). Self role changes are forbidden
(cannot demote yourself, prevents accidental admin lockout). The last admin cannot be
demoted (same `CountAdmins() > 1` check used for deletion). The UI disables the `staff`
option in the role dropdown when the target is the last admin, and disables the role
field entirely when the target is the current user.
**Rationale:** Combining username and role into one form matches the UX of a standard
edit dialog and keeps the handler count low. Restricting role transitions to `staff <->
admin` is a consequence of the CP4 three-role model: the patron role has a `patron_id`
foreign key relationship and a different lifecycle, so co-mingling it with staff edits
would invite bugs. Server-side checks are authoritative; the UI restrictions are UX
polish only.

---

## DEC-021: Password complexity policy, enforced everywhere passwords are set

**Date:** 2026-04-17 (CP5)
**Context:** Until now the seed accounts used short, lowercase passwords (`admin123`,
`staff123`, `patron123`) and there was no policy on operator-chosen passwords for the
`ADMIN_PASSWORD` env override or future staff/patron create handlers. Staff management
(#39) needs to validate passwords at every entry point, not just at the handler layer,
or a seeded/override account can sidestep the rule.
**Decision:** Enforce a single `ValidatePassword` function in `validators.go` that
requires 8+ characters with at least one uppercase letter, one digit, and one special
character (anything `unicode.IsPunct` or `unicode.IsSymbol`). Seed passwords bumped to
`Admin123!`, `Staff123!`, `Patron123!`. `SeedDefaultUsers` calls `ValidatePassword` on
the resolved `ADMIN_PASSWORD` before hashing and `log.Fatalf`s on failure so an
invalid env override fails fast at startup. Every future password-setting path
(staff create, staff password reset, patron create, admin password change) must pass
through the same validator.
**Rationale:** Centralizing the rule in one function prevents drift. Fatal-on-invalid
for the env override is the right failure mode: silent-accept would let weak passwords
through, and warn-and-fallback would silently swap the admin password without notice.
Username format uses a separate `ValidateUsername` helper with lighter rules (3-32
chars, alphanumeric + underscore) because username complexity serves a different
purpose (collision avoidance, URL/log sanity) from password complexity.

---

## DEC-022: Multi-statement DB writes must run inside a transaction

**Date:** 2026-04-17 (CP5)
**Context:** CP5 introduces several operations that touch more than one table in a
single logical write: staff `DeleteUser` removes sessions + user rows, patron create
(#21) will insert into `patrons` + `users`, book create (#20) will insert into
`books` + `authors` + `book_authors`, CP6 checkout will touch `loans` +
`books.quantity_available`. The existing `SeedBooks` code did the book + author +
book_author inserts as independent `Exec` calls with no atomicity. A failure partway
through would leave a book with no authors, or partially-linked authors.
**Decision:** Any multi-statement DB write is wrapped in a `sql.Tx` using the
standard idiom:
```go
tx, err := dm.db.Begin()
if err != nil { return err }
defer tx.Rollback() // no-op after successful Commit
// ... tx.Exec / tx.QueryRow for every write ...
return tx.Commit()
```
`SeedBooks` was retrofitted into a loop that calls `seedOneBook(b seedBook) error`,
with the transaction scoped per book (so a failure on one book still seeds the
rest). `DeleteUser` already followed the pattern. Future multi-table writes in CP5
and CP6 (`CreatePatron` with its linked user, `CreateBook` with authors, checkout
and return, ZIP import) will use the same idiom.
**Rationale:** The `defer tx.Rollback()` + explicit `tx.Commit()` pattern is the
shortest correct way to handle transactions in Go: any early return rolls back
automatically, and a successful commit makes the deferred rollback a no-op. Partial
writes are the most common source of latent data-integrity bugs, and the cost of
wrapping in a transaction is a single extra function call per write. Adopting this
as a project standard now means every new handler in CP5-CP7 inherits the guarantee
by default.

---

## DEC-023: Variant B two-button submit for bulk-entry forms

**Date:** 2026-04-18 (CP5)
**Context:** After shipping book Create with a PRG redirect to `/books/:id`, it became
clear the single-destination flow hurt bulk-entry use cases (processing a donation
stack, manually importing a shelf of titles). Three options on the table: always
redirect back to `/books/new` after save, keep the single `/books/:id` redirect but
add a "View book" link in the success banner, or give the admin an explicit choice
at the moment of save with two submit buttons.
**Decision:** Two submit buttons on the create form. "Save" posts with
`submit_action=save` and redirects to `/books/:id` (detail page). "Save and Add
Another" posts with `submit_action=add_another` and redirects back to `/books/new`
with the form cleared. Flash cookies (`book_created` code plus the title in
`flash_detail`) fire on both paths so either redirect target shows
"Added to the catalog: **Title**" in the banner. The edit form shows only
"Save Changes" -- add-another is a create-only affordance. Any unexpected
`submit_action` value falls through to the default `/books/:id` redirect. The same
pattern will apply to Patrons (#21) where the bulk-entry vs single-add tension also
exists (a receptionist processing a stack of sign-up sheets vs. adding one patron
to immediately check their record).
**Rationale:** Django-admin pattern, battle-tested across years of CRUD UX. Gives
both workflows without sacrificing either, which the single-button alternatives
could not. The handler-side branch is one string comparison; the template-side
addition is one extra `<button>` gated on `{{if .ShowAddAnother}}`. Surface area is
small, the affordance is obvious to admins familiar with any other admin console,
and it generalizes cleanly to the next CRUD surface.

---

## DEC-024: Loan state expressed via `due_date + returned_at`, no status column

**Date:** 2026-04-20 (CP6 design session)
**Context:** CP6 introduces the loan lifecycle. A loan has three observable states
(active, returned, overdue). The naive approach is a `status` TEXT column updated
on checkout and return, but that denormalizes data already expressible from two
timestamp columns and creates drift risk: a row could read `status='active'` while
`due_date` is in the past, or `status='returned'` while `returned_at IS NULL`.
**Decision:** The `loans` table carries `due_date DATE NOT NULL` and
`returned_at DATETIME` (nullable). No `status` column. The three states are derived
at query time:
- **Active:** `returned_at IS NULL AND due_date >= DATE('now')`
- **Overdue:** `returned_at IS NULL AND due_date < DATE('now')`
- **Returned:** `returned_at IS NOT NULL`

`due_date` is a DATE (not DATETIME) because loan terms are whole-day granularity;
a book due "April 30" is due at end-of-day April 30, and fractional hours never
matter. `returned_at` stays DATETIME because the exact return moment is useful in
audit history. The existing `loans` table schema from CP1 (DATETIME `due_date`
nullable, no `fine_cents`) is rewritten in `createSchema`; local dev re-seeds via
`rm data/database.sqlite*` and EC2 gets a clean DB at CP7 deploy.
**Rationale:** One source of truth per state. No branch where a handler might
forget to update `status` and leave the row inconsistent. The `DATE('now')`
comparison is trivial in SQL and readable in both queries and tests. If a future
CP adds a fourth state (lost, on-hold, etc.) that is genuinely not derivable from
existing columns, adding a status column then costs one migration -- cheap.

---

## DEC-025: Loan management via `/loans` with filter query param, 14-day loan term

**Date:** 2026-04-20 (CP6 design session)
**Context:** CP6 needs a staff-facing way to see and act on loans. The v1 planning
doc proposed a rapid-scan checkout/checkin portal, a separate `/reports/overdue`
table, and per-patron printable notices. The v2 reality-check trimmed all of that
to fit the calendar. The minimum shippable surface is: one list view that staff
can filter between active and overdue loans, with a return action per row, plus
checkout initiated from the book-detail page's existing scaffold. Loan term
default needed a number.
**Decision:** Single `/loans` route, staff + admin only, with
`?filter=active|overdue` query param (default `active`). Table rows show book
title, patron name, due date, and (when overdue) days overdue; each row has a
Return button posting to `POST /loans/:id/return`. Default loan term is 14 days,
defined as a single package-level constant (`const DefaultLoanTermDays = 14`)
passed to `CheckoutBook` by the handler. Per-book or per-patron overrides deferred
post-submission. The book-detail page keeps its existing checkout scaffold as the
checkout UX; no separate `/checkout` portal in CP6. The rapid-scan portal design
is preserved in bead `cs408-go-stack-yu3` for a future un-defer when transaction
volume justifies it.
**Rationale:** Two states expressible by one query param keeps the URL shareable,
the template single, and the tests trivial. Fourteen days is the standard public
library default (Boise Public, Ball State Bracken, most US library systems) and
shorter terms produce more overdue rows for demo data. Keeping the book-detail
form as the checkout UX means session 3 is "wire existing scaffold to handler"
instead of "build new portal template" -- ~2h saved.

---

## DEC-026: Checkout guardrails: block on any overdue, cap active loans at 5

**Date:** 2026-04-20 (CP6 design session)
**Context:** Checkout can be unrestricted (trust staff) or guarded. Unrestricted
is simpler but lets a patron with six overdue books walk out with a seventh.
Guardrails are the stronger library posture. Two guardrails on the table: block
checkout when the patron has overdue loans, and/or cap the number of concurrent
active loans per patron.
**Decision:** Both guardrails in CP6. Enforced inside `CheckoutBook` before the
insert, as part of the same transaction that decrements `quantity_available`:

1. **Block on overdue:** if the patron has one or more loans where
   `returned_at IS NULL AND due_date < DATE('now')`, refuse the checkout and
   return `ErrPatronHasOverdue`.
2. **Max active loans:** if the patron has 5 or more loans where
   `returned_at IS NULL`, refuse the checkout and return `ErrPatronAtLoanLimit`.

The limit is a single package-level constant (`const MaxActiveLoansPerPatron = 5`).
Handlers map both sentinels to flash codes (`loan_blocked_overdue`,
`loan_blocked_limit`) surfaced as a banner on the book-detail page.
**Rationale:** "Any overdue blocks" is the simplest rule to explain to both staff
and patrons, and matches typical small-library practice (return what you have,
then borrow more). Five active loans per patron is the sweet spot between too
restrictive (3 feels stingy for families) and too liberal (10 makes the dashboard
"Active Loans" counter less meaningful). Both numbers are single constants, so
tuning them later is one-line change. Catching these inside `CheckoutBook` keeps
the check and the insert atomic; validating in the handler would open a TOCTOU
race between check and insert when multiple staff check out books concurrently.

---

## DEC-027: Admin backup/restore -- ZIP scope, consistency, swap strategy, safety interlocks

**Date:** 2026-04-26 (CP7 design session)
**Context:** CP7 ships an admin panel feature (#23) that lets an admin export
the library to a portable backup ZIP and import a backup ZIP to restore state.
The export must produce a consistent point-in-time copy under concurrent
writes; the import must not silently corrupt operator data, must reject
malicious archives (Zip Slip), and must be reversible if the operator picks
the wrong file. Seven sub-decisions, captured together because they are
intertwined.

**Decision:**

1. **ZIP scope.** Backup contains exactly two paths: `data/database.sqlite`
   (the entire DB including the sessions table) and the `data/covers/`
   directory. Logs, config, the binary, templates, static assets, and
   `.beads/` are explicitly out of scope -- they come from the deploy and
   from git, not from operator data.

2. **Export consistency.** Use SQLite's `VACUUM INTO '/tmp/snapshot.sqlite'`
   to produce a consistent point-in-time copy before zipping. ZIP the
   snapshot file (not the live DB), then delete the snapshot. This rules
   out torn-write corruption when a checkout/return happens during export.

3. **Import strategy.** In-process swap with a global `sync.RWMutex`. The
   import handler takes the write lock, closes `*sql.DB`, swaps files,
   reopens. All other handlers take the read lock when accessing the DB.
   No restart required. Lock window is ~200-500ms on a small DB; invisible
   to a single-library deployment with ~5 concurrent users.

4. **Pre-swap backup.** Before overwriting, rename existing files
   to `data/database.sqlite.bak` and `data/covers.bak/` (atomic on the
   same filesystem). On reopen failure, rename `.bak` back and reopen the
   original. On reopen success, keep the most recent `.bak` indefinitely
   (next import overwrites it). Exclude `*.bak` and `*.bak/` from the
   export walk so backups do not recursively contain previous backups.

5. **Zip Slip protection.** Carved into a new `internal/safezip/` package
   (first use of `internal/` in this repo). `SafeExtract(zipPath, destDir)`
   validates every entry: `filepath.Rel(absDest, targetPath)` must not
   start with `..` and must not be absolute; entries with the symlink mode
   bit are rejected outright. Unblocks future reuse by the deferred `#63`
   offline cover sync workflow without a refactor.

6. **No CLI counterpart in CP7.** Web UI export only. A CLI subcommand
   (`go run . export-backup --out=...`) was considered for SSH/cron
   workflows and for shared plumbing with `#63`'s `cmd/cover-fetcher/`,
   but deferred to keep CP7 scope tight. Operators who need scripted
   backups can `curl -b cookies.txt /admin/export -o backup.zip` against
   the same handler.

7. **Import confirmation interlock.** Medium friction: Bootstrap modal +
   a checkbox labeled "I have a backup. I understand this replaces the
   current database." The Confirm button is disabled until the box is
   ticked. Lighter (modal-only) was rejected because users blow past
   reflexive Confirm dialogs; heavier (type the word "REPLACE") was
   rejected because the `.bak` rollback (decision 4) gives a recovery
   path even if the operator clicks through.

**Rationale:** Each of the seven was a balance between security posture
and complexity tax. The pattern is: pick the safer option whenever the
implementation cost is small. `VACUUM INTO` is one statement; safezip is
a small package; pre-swap rename is two `os.Rename` calls. The cumulative
result is an admin panel where the destructive operation has three
independent layers of protection (Zip Slip rejection, write-lock atomic
swap, `.bak` rollback) and the operator has to deliberately tick a box
to fire it. The choice to skip the CLI and to pick medium-not-heavy
confirmation explicitly trades small marginal protection for keeping
CP7 scope ship-able by the 2026-04-30 target. The `internal/safezip/`
package extraction is the one place where we paid future-tax now (against
the "don't design for hypothetical future requirements" rule) because
`#63`'s design explicitly depends on it and the alternative is a refactor
on the day we need it.

---

## DEC-028: Defensive HTTP headers + trusted-proxy pin + Go 1.25.9 stdlib bump

**Date:** 2026-05-01 (CP7 #24 implementation)
**Context:** Default Gin trusts every proxy for `X-Forwarded-For`, the bare
binary sends no defensive HTTP headers, and `govulncheck` on the Go
1.25.0 toolchain reported 19 stdlib CVEs (net/url, encoding/pem,
crypto/tls, crypto/x509, etc.). All three are zero-cost to fix together
before the final EC2 deploy.

**Decision:**

1. **`SecurityHeaders` middleware applied router-wide before everything
   else.** Sets `X-Content-Type-Options: nosniff`, `X-Frame-Options:
   DENY`, `Referrer-Policy: same-origin`, and a Content-Security-Policy
   of `default-src 'self'; style-src 'self' 'unsafe-inline'; img-src
   'self' data: https://covers.openlibrary.org https://archive.org
   https://*.archive.org; frame-ancestors 'none'; base-uri 'self';
   form-action 'self'`. Applied via `router.Use(SecurityHeaders)` so
   even 404 / 500 responses carry the headers. HSTS is gated on
   `APP_ENV=production` because the bare-IP EC2 deploy is HTTP-only
   and HSTS over HTTP is harmful. The `img-src` exceptions for the
   OL/IA hosts were added post-CP7 so the OL Lookup cover preview
   can render in the Add/Edit Book form. OL covers HTTP-302 redirect
   to the Internet Archive CDN, and CSP applies to the final URL
   after redirect, so both `covers.openlibrary.org` and `archive.org`
   (plus `*.archive.org` for the numbered IA hosts) must be present.
   Saving worked without these (server-side fetch in
   `SaveCoverFromURL`), but the in-form preview required the browser
   to load the image directly. Strictly smaller trust extension than
   what the server already does on the same hosts.

2. **`'unsafe-inline'` allowed for `style-src` only, not `script-src`.**
   Templates rely on inline `style="..."` attributes in 22+ places, and
   tightening that without breaking the visual design is a multi-day
   refactor. The single inline `<script>` block in `backup_admin.html`
   was extracted to `static/javascripts/admin_backup.js` so `script-src`
   can stay tight at `'self'` (no inline, no eval). XSS protection
   therefore lives in `script-src`; `style-src 'unsafe-inline'` is the
   accepted residual risk.

3. **`router.SetTrustedProxies([]string{"127.0.0.1"})` in `main.go`.**
   On the EC2 deploy, nginx fronts the Go app on localhost:3000, so
   only nginx should be allowed to set `X-Forwarded-For`. Default Gin
   trust-everyone behavior would let any client spoof their IP. The
   test router in `setupTestRouter` mirrors this so the test surface
   matches production (per the #35 gotcha).

4. **Pin Go 1.25.9 in `.tool-versions` and `go.mod`.** `govulncheck`
   on 1.25.0 reported CVEs all fixed in 1.25.x patch releases. 1.25.9
   was the latest available via asdf at deploy time. Re-running
   `govulncheck` on 1.25.9 returns no findings. Pinning in
   `.tool-versions` makes `asdf install` reproducible; bumping the
   `go` directive in `go.mod` makes anyone running a stale toolchain
   fail at compile time rather than silently shipping vulnerable
   binaries.

5. **`govulncheck` and `go mod verify` are pre-deploy gates, not CI
   gates.** Documented in `docs/deployment.md` Step 1 and
   Redeploying section. CP7 did not set up GitHub Actions CI (#17
   remains open); the gates run locally on the developer machine
   before each `scp`. If/when #17 lands, the gates move into the CI
   workflow.

**Rationale:** Each of the four was a few lines of code (or zero, for
the toolchain pin) with high security upside, and they cluster naturally
into one PR (#75). The decision NOT to refactor inline styles is the
load-bearing trade -- it keeps CSP headers shippable today instead of
deferring all of them indefinitely waiting on a perfect template
refactor. The `script-src 'self'` constraint required moving one
small JS file, which surfaced as a nice forcing function: any future
inline script will fail loudly via a CSP violation in DevTools console
rather than slip in unnoticed.

---

## DEC-029: Admin tools-index pattern (heavy tools as cards, light settings inline)

**Date:** 2026-05-01 (CP7 mid-implementation, after DEC-027 backup
shipped)
**Context:** DEC-027 shipped `/admin/backup` as a dedicated page,
leaving `/admin` as the placeholder "Admin panel coming soon" stub.
With more admin features likely to land post-CP7 (patron self-
registration toggle `cs408-go-stack-o1x`, offline cover sync
`cs408-go-stack-0eh`), the question was whether to fold backup into
`/admin` (one big page) or build out `/admin` as a tools dashboard.

**Decision:**

1. **`/admin` is a tools-index page**, not a stub and not a single
   monolithic admin form. The page has labeled sections; each section
   is either a card grid linking out to a dedicated tool page, or an
   inline block of lightweight controls.

2. **Heavy tools get their own card on `/admin` and drill into a
   dedicated `/admin/<tool>` page.** "Heavy" means: multi-section
   layout, modal interlocks, file uploads, multi-step flows, or
   substantial per-tool state. Backup is the canonical heavy tool --
   stats panel + export + import-with-modal -- and it lives at
   `/admin/backup`.

3. **Light tools live inline as sections directly on `/admin`.** "Light"
   means: a single toggle, a single text field, a single button. The
   patron self-registration toggle (when it ships) is the canonical
   light example: one boolean setting, one form, one button. No need
   for a dedicated `/admin/registration` route.

4. **`/admin` moved from staff-accessible to admin-only access.** The
   pre-CP7 placeholder was in the staff route group, but every actual
   admin tool is admin-only. The mismatch (staff sees an Admin link to
   a page with no tools they can use) is bad UX. Mirrored in
   `setupTestRouter`; `TestStaffRoleCannotAccessAdminRoutes` was
   updated to assert the new boundary.

5. **The standalone "Backup" link in the sidebar was removed.** Only
   the "Admin" link remains under the admin-only block. Drilling from
   `/admin` -> Backup card -> `/admin/backup` is the path. New tools
   add a card on `/admin`, not a sidebar entry.

**Rationale:** Naive future-proofing said "build a sidebar group with
Admin > Backup > Future Thing". That visual nesting was overkill for
the actual backlog (probably 3-4 admin tools total over the project
lifetime). The tools-index pattern scales linearly without sidebar
clutter and makes the access-tier boundary explicit at the route
level, not just the template level. The card-vs-inline split is a
judgment call per tool; the criterion ("does this need its own
page?") is loose on purpose so future tools don't get force-fit
into the wrong category.


## DEC-030: CSV patron import + force-change-on-first-login

**Date:** 2026-05-05 (design) through 2026-05-10 (implementation +
manual verification, branch `csv-patron-import`).
**Context:** Sales targets (Idaho Department of Corrections,
mom-and-pop libraries) need bulk patron onboarding from existing
records. Manual single-patron create scales to maybe 20-50; a
corrections customer arrives with 500+ inmate records on day one.
Eight inter-locked sub-decisions, captured together because the CSV
import, the force-change flow, the settings infrastructure, and the
per-row credential retrieval path are tightly coupled.

**Decision:**

1. **Standard columns + JSON catch-all, not a mapping UI.** The patron
   schema has `name`, `email`, `phone` as columns and a single
   `metadata TEXT` (JSON) catch-all (the column was introduced in
   DEC-016 for exactly this case). CSV headers go through a
   normalization pass (lowercase + strip non-alphanumeric) and then a
   small alias table maps common variants -- "Email Address",
   "E-mail", "mail" all collapse to `email`; "IDOC Number",
   "inmate_id", "library card", "card number", "student_id",
   "patron_id", "external_id" all collapse to the reserved JSON key
   `metadata.external_id`. Everything else flows through to
   `metadata.<original_header>` keyed verbatim. No interactive column
   mapper -- the alias table covers the 90% case and metadata
   absorbs the rest.

2. **`metadata.external_id` is a reserved JSON key, not a dedicated
   column.** Considered adding `external_id TEXT` to the `users` or
   `patrons` table; rejected because:
   - Future customers each have their own identifier name (IDOC #,
     library card, student ID, employee ID). A generic `external_id`
     column works for all but adds schema bloat.
   - SQLite supports `json_extract(metadata, '$.external_id') = ?`
     for dedup and indexing. Performance is fine at our scale (<50k
     patrons).
   - Application-layer dedup was already the plan, so we don't lose
     a DB-enforced uniqueness constraint we were going to use.

3. **Two-step preview-then-commit flow.** Upload posts to
   `POST /admin/patrons/import`, which parses, dedupes, stashes the
   parsed result in a process-local `sync.Map` keyed by a 16-byte
   random token, and renders a preview page. The admin sees row
   counts (insertable, duplicate, empty-name, malformed) plus a
   first-10 sample. Confirming posts the token to
   `POST /admin/patrons/import/confirm` which retrieves the parsed
   data and runs the inserts. TTL sweep on each set keeps the map
   bounded (30 min). Restart loses pending imports by design.

4. **Account-mode radio: records-only OR with-logins.** Per-import
   choice (not per-row CSV column). Records-only creates a
   `patrons` row with no linked `users` row -- patron exists in the
   system, staff can transact for them, but they cannot log in.
   `GetAllPatrons` / `GetPatronByID` use `LEFT JOIN users` so
   no-login patrons appear in the list; `patrons.html` renders the
   empty Username column as a muted "no login" hint. With-logins
   path inserts both rows in one transaction, generates a random
   temp password server-side, sets `must_change_password=1`, and
   returns the plaintext temp via the function return value.

5. **Random temps with force-change-on-first-login.** Each
   with-login import generates a 12-char temp via
   `generateTempPassword` (crypto/rand, ambiguous chars 0/O/1/l/I
   excluded, satisfies `ValidatePassword`). `must_change_password`
   column added to `users` (default 0). `RequirePasswordCurrent`
   middleware runs after `RequireAuth` on every route group except
   the `account` group, which hosts `/account/change-password` and
   `/logout` (escape hatch -- a user must always be able to bail
   out). A flagged user is redirected to the change page on any
   other request. `UpdateUserPassword` clears the flag and the
   stored temp in the same UPDATE.

6. **`temp_password` column persists plaintext until claimed.**
   Initial implementation stored the temp only in an in-memory
   token map for the single-use credentials CSV download. The
   first manual test surfaced a hazard: clicking away from the
   result page lost the temps with no recovery short of wiping the
   DB or manually resetting each patron's password. Three
   architectural options considered (`cs408-go-stack-fut`):
   - Shape 1: persist plaintext `temp_password` column on `users`;
     per-row reveal UI on `/patrons`; clear on first login OR
     admin "Mark as Delivered".
   - Shape 2: regenerate per-patron on demand; no plaintext stored;
     painful at 500-row scale.
   - Shape 3: Shape 1 + AES encryption with app-key; added
     complexity for marginal real-world security gain (DB-file
     access usually implies shell access).
   - Picked Shape 1. Patron rows on `/patrons` show a yellow
     "Login Setup" button when `temp_password IS NOT NULL`. Click
     opens `/patrons/:id/login-credentials` with the patron name,
     username, temp password, and three actions: copy-to-clipboard,
     "Mark as Delivered" (`ClearTempPassword`), and "Regenerate
     Temporary Password" (`RegenerateTempPassword`, which generates
     a fresh temp, swaps the hash, sets the flag, wipes sessions).
     The reveal page sets `Cache-Control: no-store, no-cache,
     must-revalidate, private` + `Pragma: no-cache`.

7. **Settings table for admin toggles.** New `settings (key, value,
   updated_at, updated_by)` key/value table. First inhabitant:
   `staff_can_import_patrons` (default false). When true, staff
   gain access to the CSV import tool only -- other admin-only
   features (backup, settings, staff management) stay locked.
   `RequireStaffImportAccess` middleware (admin always, staff when
   the setting is true) gates the import routes. Future toggles
   add a row to `/admin/settings`, a key to `GetSettingBool`, and
   their own gating middleware. The deferred `cs408-go-stack-o1x`
   (patron self-registration toggle) is the canonical next use.

8. **Patron import entry point lives on `/patrons`, not `/admin`.**
   First implementation put the "Patron Import" card on the
   `/admin` tools-index. Staff with the toggle on couldn't see it
   (the `/admin` page is admin-only). Moved the entry point to
   `/patrons` -> "Import CSV" button (visible to admin always,
   staff when toggle on). Mirrors the same gating as the route's
   middleware. The card was removed from `/admin` to avoid two
   entry points to the same tool.

**Security review findings addressed mid-implementation:**

- **CSV formula injection** in the downloaded credentials CSV: an
  attacker with import rights could embed `=HYPERLINK(...)`,
  `=IMPORTXML(...)`, or `=WEBSERVICE(...)` in the patron Name
  column, which would execute in Excel/LibreOffice/Sheets when a
  different admin later opens the file -- exfiltrating the
  plaintext temp password from the adjacent column. Fixed by
  defanging every output cell whose first byte is in `=+-@\t\r`
  with a leading single quote. Defang happens at CSV write time;
  the patron's true `name` stays clean in the DB.

- **Plaintext credentials CSV cached by the browser**: the
  download endpoint shipped without `Cache-Control: no-store`,
  meaning the response body sat in the browser's disk cache after
  the single-use server-side token was consumed. Fixed by adding
  `Cache-Control: no-store, no-cache, must-revalidate, private` +
  `Pragma: no-cache` on `HandleImportDownload`, matching the
  per-row reveal page that already had the same headers.

Both findings were caught by running the `security-review` skill
on the branch diff before merging. The process is now codified
(see CLAUDE.md "Session Completion" step 3 and AGENTS.md "Security
Review Requirements"): every non-test/docs/beads-only branch must
pass a security review before a `bd close`.

**Rationale:** The shape of the feature is driven by the
asymmetric scale of the target customers. Corrections imports
hundreds of inmates in a single shot, with no email and a hard
external identifier (IDOC number). Mom-and-pop libraries import
tens of patrons with emails and library card numbers. One CSV
format serves both because the alias table absorbs naming
differences and the JSON catch-all preserves whatever
institution-specific fields each customer brings. Force-change is
the right default for any temp-password flow; persisting the
plaintext temp in the DB is the right trade-off vs. losing temps
on a misclick or session boundary, given the DB is already
trusted and the data is intentionally short-lived. The settings
table avoids re-inventing the toggle pattern each time and
provides an audit trail (`updated_by`).

---

## DEC-031: SQLite `busy_timeout=5000` + `_txlock=immediate` (set per-connection via DSN) to make WAL-mode writers queue cleanly

**Date:** 2026-05-12 (cs408-go-stack-7an, commits `ac06305` and two follow-ups).

**Context:** `openDB` already set `journal_mode=WAL` (DEC-002 era),
which lets readers and writers proceed without blocking each other in
the common case. But WAL still serializes *writers* via the journal
lock: when two writers race, the loser sees `SQLITE_BUSY` immediately
because SQLite's default `busy_timeout` is `0`. The
`TestCheckoutBookConcurrentRace` test (added in the same commit) made
this concrete -- with 10 goroutines racing to check out the last copy
of a book, the losers came back with `database is locked (5)
(SQLITE_BUSY)` instead of the in-transaction
`ErrNoCopiesAvailable` we want.

In production this would have shown up as flaky "could not check out
book" errors any time two staff hit checkout within the same handful
of milliseconds. The fix is one of those SQLite settings that every
other client (the `sqlite3` CLI, the mattn driver) sets by default;
`modernc.org/sqlite` does not.

**Decision:** Set `busy_timeout=5000` (along with `foreign_keys=on`
and `journal_mode=WAL`) in the *DSN* passed to `sql.Open`, not via a
post-Open `db.Exec("PRAGMA ...")` call. The modernc.org/sqlite driver
reads the DSN's `_pragma=...` query parameters and applies each
PRAGMA on **every new connection** it opens. The DSN form is:

    file.sqlite?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)&_pragma=journal_mode(WAL)

Five seconds is the wait budget for the journal lock: well above any
real LibreShelf transaction (the slowest one writes a single loan row
and decrements a count, sub-millisecond) and below any user-perceptible
request timeout.

**Why per-connection matters.** `database/sql` is a connection *pool*.
When you call `db.Exec("PRAGMA busy_timeout = 5000")`, the pool hands
out one connection, runs the statement on it, and returns it to the
pool. Other connections opened later -- e.g. when goroutines race for
checkout -- start with `busy_timeout=0`. The first version of this
change used the post-Open `db.Exec` form and passed locally because my
machine reused a single connection often enough to avoid the issue;
CI's scheduler opened multiple connections under contention and the
losing goroutines bounced off the journal lock with `SQLITE_BUSY`.
The DSN form fixes this because the driver runs `applyQueryParams` in
`newConn` for every connection it opens (see
`modernc.org/sqlite/conn.go`).

`foreign_keys` is also per-connection in SQLite and was subject to the
same bug pre-fix; this change closes that hole too. `journal_mode=WAL`
is persisted in the database file once set, so it survived the
single-connection misconfiguration, but it's still cleaner to assert
it on every connection.

**`_txlock=immediate` -- the second-layer fix.** With the PRAGMAs
landed via DSN, the next CI run failed again -- this time with
`database is locked (517)`, SQLite error code `SQLITE_BUSY_SNAPSHOT`.
That is a different race:

1. Two goroutines call `db.Begin()`. Go's `database/sql` does not
   surface SQLite's BEGIN modes directly; the driver default is
   `BEGIN DEFERRED`, which starts each transaction as a reader.
2. Both goroutines read from the same snapshot.
3. The first goroutine writes (`INSERT`/`UPDATE`), upgrading its
   transaction to a writer, and commits. The DB advances to a new
   snapshot.
4. The second goroutine tries to write, but its read snapshot is now
   stale. SQLite aborts that transaction with `SQLITE_BUSY_SNAPSHOT`
   rather than serving a write against an out-of-date view.

`busy_timeout` does not help here -- the loser isn't waiting for a
lock; it has a stale snapshot. The fix is `BEGIN IMMEDIATE`, which
takes the database's RESERVED (write-intent) lock at BEGIN time. With
IMMEDIATE, concurrent writers queue on that lock via `busy_timeout`
and then re-evaluate their guards inside their own fresh transaction.
No snapshot staleness.

The modernc.org/sqlite driver supports `_txlock=immediate` as a DSN
parameter (see `applyQueryParams` in `sqlite.go` and `newTx` in
`tx.go`): every non-readonly `Begin()` becomes `BEGIN IMMEDIATE`.
Every `dm.db.Begin()` in `db.go` is a write transaction (13 sites at
time of writing -- CheckoutBook, ReturnBook, UpdateUserPassword,
CreatePatron, CreateBook, etc.), so this is the right global default.

**Performance trade-off.** `BEGIN IMMEDIATE` serializes all write
transactions, even if they touch different tables. For LibreShelf's
scale (single Go process, small library, a few staff writing at most
once every few seconds), this is the right call: correctness over
micro-throughput. Concurrent *readers* are unaffected -- they still
take only a shared lock under WAL.

**Final DSN form:**

    dbPath?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)&_pragma=journal_mode(WAL)&_txlock=immediate

**Effect on the checkout TOCTOU claim.** `CheckoutBook` already did
the right thing structurally: the availability read and the decrement
ran inside one transaction (DEC-022's rule). What was missing was a
test that proved the structure held under contention. With WAL +
busy_timeout the losing goroutine queues on the journal lock, waits
for the winner to commit, then re-evaluates the availability guard
inside its own transaction and returns `ErrNoCopiesAvailable`. The
test asserts exactly one success and `N-1` `ErrNoCopiesAvailable`
results -- if a future refactor moved the guard outside the
transaction, `quantity_available` would go negative and the test
would fail. Passes 100x in a row under `go test -race -count=100`.

**Verified load-bearing.** During implementation we temporarily
removed the PRAGMA and re-ran the test; it failed with multiple
goroutines reporting `database is locked (5) (SQLITE_BUSY)`. So the
PRAGMA is the actual fix, not cosmetic.

**Why 5000 ms (not 1000, not 30000).** Five seconds is the conventional
default that the SQLite shell uses. One second is too short under
sustained write contention (the WAL can grow large and slow writers).
Thirty seconds is long enough that the front end would have already
given up; pinning it lower forces us to see the contention as an
error rather than mask it as a slow response. Five seconds is the
sweet spot.

**Single-process app.** LibreShelf runs in one Go process per
deployment; concurrent writes only originate from concurrent staff
HTTP requests handled by Gin goroutines. There is no second process
or replica that could starve the timeout, and no cron/external writer
to compete with. The 5s budget is for in-process goroutine races
only.

**Security implications:** None new. The PRAGMA strengthens
transactional integrity under concurrent load (a positive). It is
not a path from user input to SQL and does not change any auth/authz
boundary. The security review on `ac06305` confirmed this.

**Alternatives considered:**

- `db.SetMaxOpenConns(1)` -- serialize at the connection-pool layer.
  This would have hidden the race entirely (one goroutine at a time
  ever talks to SQLite), so the test would not have proved anything
  about the in-transaction guard. Rejected.
- Adding a `_pragma=busy_timeout=5000` DSN parameter -- works for
  some drivers but `modernc.org/sqlite` syntax is awkward and harder
  to read than an explicit `db.Exec("PRAGMA ...")` next to the
  existing PRAGMAs. Rejected for legibility.
- Larger timeout (e.g. 30s) -- only meaningful if we expected a
  long-running writer to hold the lock for many seconds, which would
  itself be a bug in LibreShelf. Rejected.

---

## DEC-032: Open Library metadata enrichment chain (no Wikipedia)

**Date:** 2026-05-13 (cs408-go-stack-4xq, dy3, 7iu, m5y, 2a8 -- single
session, commits `a1b33b7` through `de3ef64`).

**Context:** The OL Lookup endpoint was returning very thin prefill
payloads for the staff "Add Book" / "Edit Book" form. Most books got
title and cover but no description; some got no authors either. The
seed-cover backfill could only recover about half of the 100 seed
books' covers from OL, even though OL clearly *had* covers for many
of the missing ones when you looked at openlibrary.org in a browser.

The root cause turned out to be OL's record sharding. Each book has
two record layers: an **edition** (one specific printing, queried by
ISBN) and a **work** (the abstract book; one work has many editions).
Different fields live on different layers depending on which OL
editor added what when:

- Edition records sometimes carry full back-cover descriptions
  (Penguin Classics' P&P has one) but sometimes don't (Dell's
  Rule of Four has none).
- Edition records sometimes carry an `authors: [{name}]` array,
  sometimes a `author: ["Last, First, year-year."]` catalog-card
  string array, sometimes neither -- the latter case requires the
  caller to resolve refs on the linked work record.
- OL frequently has **duplicate work records** for the same book
  (Charlotte's Web has at least three, with covers spread across
  some of them). An edition can link to a sibling work that happens
  to be missing what we want.

The first pass at this used the OL primary API plus a Wikipedia
synopsis fallback (`cs408-go-stack-g5x`). It worked technically but
the Wikipedia REST `/api/rest_v1/page/summary/{title}` endpoint
returns the article's *lead section*, which for most book articles
is meta-content ("X is a 1965 novel by Y") rather than jacket
synopsis. Tim's example "The Rule of Four" returned a paragraph
about Caldwell and Thomason's biographies. That work got reverted in
the same session.

**Decision:** Stay entirely within OL for descriptions, authors, and
covers; chain across OL's record layers to compensate for the
sharding.

### The fetch chain (`FetchOpenLibraryBook` in `openlibrary.go`)

Primary call: `GET /api/books?bibkeys=ISBN:<isbn>&jscmd=details&format=json`.
This returns the edition record, which carries description, authors
(in either of two shapes), publisher, publish date, and cover IDs.

**Description fallbacks** in priority order:

1. Edition's `description` field (string or `{type, value}` object;
   handled by a custom `olDescription.UnmarshalJSON` that tolerates
   either shape and ignores junk).
2. Work record's `description` field. Fetched via a second call to
   `<host>/<works[0].key>.json`. Combined with the cover fallback
   below into a single HTTP call (`fetchOLWork`).

**Author fallbacks** in priority order:

1. Edition's `authors: [{key, name}]` structured array (canonical
   "First Last" form).
2. Edition's `author: ["Last, First, year-year."]` catalog-card
   form, flipped to "First Last" by `normalizeOLAuthorString`. The
   trailing period is dropped only if preceded by a digit (so
   "Tolkien, J. R. R." keeps its final initial).
3. A third call to `/api/books?...&jscmd=data`, which OL transforms
   to resolve work-record author refs into `[{name, url}]` pairs.
   Fires only when both edition shapes were empty.

**Cover fallbacks** in priority order:

1. Edition's `covers: [int]` array, first non-zero entry, formatted
   into `https://covers.openlibrary.org/b/id/<id>-L.jpg`.
2. Work record's `covers: [int]` array (combined into the same
   `fetchOLWork` call as the description fallback).
3. HEAD-probe `https://covers.openlibrary.org/b/isbn/<isbn>-L.jpg
   ?default=false`. OL's ISBN-based covers endpoint resolves a cover
   whenever *any* work or edition has one indexed under that ISBN,
   abstracting over the duplicate-work problem. The `?default=false`
   query is essential -- without it the endpoint returns a 1x1
   placeholder image rather than 404, and we'd save placeholders as
   real covers. The probe is HEAD-only (not GET) so it doesn't
   stream the image just to check resolvability; this also keeps
   broken-image URLs out of the form preview.

All fallbacks are **non-fatal**: if a secondary call fails, the
function logs and continues with whatever the primary call yielded.

### Storage and offline behavior

Cover URLs returned by the chain are *transient*. `SaveCoverFromURL`
downloads to `data/covers/<hash>.jpg` (filename is a content hash)
and persists only the filename in the books row. The catalog and
book-detail pages serve covers from local disk via Gin's static
handler -- no upstream call at render time, so the app works fully
offline once a book has been ingested. The OL endpoints are only
touched during the OL Lookup admin action and during the one-shot
seed-cover backfill at first startup.

### Why no Wikipedia, Google Books, etc.

The cover-fallback chain backlog (`cs408-go-stack-069`, `l2h`, `fcb`,
`8gj`) was originally scoped to extend coverage with Wikipedia /
Internet Archive / Google Books. After this session's OL-only chain
shipped, the seed backfill recovers 100/100 covers (was 98/100 with
just the work fallback; was somewhat lower before the chain) and the
manual OL Lookup returns jacket-style descriptions for every book OL
catalogs. Google Books (`8gj`) is still useful for the small minority
of books OL doesn't catalog at all -- it remains open. Wikipedia
specifically (`g5x`) is closed as "tried, wrong content shape." The
OLID/LCCN/OCLC endpoint exhaustion (`069`) is partially obviated by
the ISBN-cover probe; it remains open but probably yields little.

### Observability

The handler logs upstream errors with `log.Printf` lines tagged with
the function name and the ISBN or work key. Successful fallback
firings are not logged (the result tells the staff member via the
"Prefilled from Open Library" status banner, and the seed backfill
emits a final tally `saved N/M cover(s)` regardless of which layer
each came from).

### Tests

`openlibrary_test.go` covers:

- Edition shapes: structured authors, catalog-card authors, cover-IDs
  vs no covers, string-vs-object description.
- Work fallback: description-only, covers-only, both, skipped when
  edition is already complete, silent on 404.
- `jscmd=data` author fallback: happy path, silent on 404.
- ISBN-cover probe: happy path (200 after redirect), missing (404)
  stays empty, skipped when edition cover already present.
- Author-string normalizer table tests, including the
  "Tolkien, J. R. R." trailing-period edge case.

The fake server router (`startFakeOLRouter`) dispatches by path and
`jscmd` query so a single httptest.Server stands in for all four OL
endpoints in one test.

### Live verification snapshot (2026-05-13, against real OL)

- ISBN 9780141439518 (Pride and Prejudice, Penguin Classics ed.):
  edition has all three fields; no fallbacks fire. Description is
  the publisher's back-cover blurb ending in `--back cover`.
- ISBN 9780441013593 (Dune, Ace ed.): edition description empty,
  work fallback supplies the full novel synopsis.
- ISBN 9780440241355 (Rule of Four, Dell ed.): edition has nothing;
  work supplies description, `jscmd=data` supplies authors
  (Ian Caldwell, Dustin Thomason, Eiko Kakinuma).
- ISBN 9780064400558 (Charlotte's Web): edition+work covers both
  empty due to duplicate-work indexing; ISBN-cover probe resolves.
- ISBN 9780735211292 (Atomic Habits): same shape as Charlotte's Web,
  recovered by the ISBN probe.

Full seed backfill on a fresh install: 100/100 covers, ~70s total.

---

## DEC-033: Operator-declared offline mode (env var + admin toggle, no auto-detect)

**Date:** 2026-05-15 (cs408-go-stack-ahq, branch `feat/offline-mode-a0`, commits `4db0070` through `0b6b574`).

**Superseded in part by DEC-034 (2026-05-15):** the precedence rule was flipped from "settings row wins over env var" to "env var=true acts as a deployment lock that overrides the DB row." See DEC-034 for current behavior. The rest of this entry (env var name, settings key, why-not-auto-detect, call sites) still applies.

**Decision:** External HTTP calls (Open Library, future Google Books, future Internet Archive)
are gated by an operator-declared offline-mode predicate. Sources: `LIBRESHELF_OFFLINE` env
var as startup default, `offline_mode` row in the existing `settings` table as runtime
override. The settings row wins when present. Admin-only toggle on the existing Settings
page.

**Why not auto-detect:** Restricted networks are inconsistent. Some allow outbound HTTPS
to some hosts and block others; transparent proxies can return success codes for blocked
URLs; air-gapped facilities have no reliable probe target. The operator knows the
deployment's network policy. Asking the app to guess from inside the network creates
false positives and negatives that are worse than an explicit declaration.

**Call sites gated by `IsExternalAllowed`:**
- `FetchOpenLibraryBookGated` (admin OL Lookup path)
- `FetchAndStoreSeedCovers` (startup seed backfill)

The un-gated `FetchOpenLibraryBook` remains exported so the existing httptest-driven tests
in `openlibrary_test.go` keep working without needing a DB.

**Future use:** Subproject A (Google Books) reads the same predicate before any GB HTTP
attempt. Same pattern applies to Internet Archive, Wikidata, and any future external
source -- one gate, one toggle.

**Related:** spec at `docs/specs/2026-05-15-google-books-fallback-design.md`; bd issues
cs408-go-stack-8gj (next subproject) and cs408-go-stack-0eh (offline workflow context).

---

## DEC-034: LIBRESHELF_OFFLINE acts as a deployment lock (precedence flip)

**Date:** 2026-05-15 (cs408-go-stack-85j, branch `feat/offline-mode-env-lock`).

**Decision:** Flip the precedence rule established in DEC-033. `LIBRESHELF_OFFLINE=true` at startup now overrides any `offline_mode` row in the settings table. Setting the env var to any other value (false, 1, yes, unset) leaves the DB row in control. Only the case-insensitive string `"true"` locks. The admin Settings UI renders the offline-mode toggle as checked+disabled with an explanatory note when locked; the handler skips writing the offline_mode row while locked. Other settings (currently `staff_can_import_patrons`) are unaffected by the lock.

**Why the flip:** During manual test of PR #78 (A0), Tim hit the precedence trap that the original DEC-033 design created. The admin Settings handler writes every known setting on every POST (treating absent checkboxes as off). A Save-while-exploring with the offline_mode checkbox unchecked persisted `offline_mode=false` to the DB. On the next restart with `LIBRESHELF_OFFLINE=true`, the DB row silently overrode the env var; OL Lookup returned 200 instead of 503. No log line, no warning. For the documented audience (prisons, secure facilities, air-gapped deployments) the operator needs confidence that an env-var declaration cannot be silently undone by a prior UI Save.

**Asymmetric lock:** Only `=true` locks. There is no "lock online" use case. An operator who deploys in a connected environment simply does not set the env var; the runtime UI is the source of truth.

**Predicate behavior:**
- `offlineEnvDefault=true` (env locks): `IsExternalAllowed` returns false. DB is not consulted. A DB fault during the lock period does not change behavior.
- `offlineEnvDefault=false` (no lock): `IsExternalAllowed` consults the DB row. Empty row defaults to allow (true). DB fault defaults to allow (true) -- fail-open, since there is no env signal to fall back to.

**UI behavior:**
- Locked: toggle rendered as `checked disabled`, help copy reads "Locked by the LIBRESHELF_OFFLINE env var. Clear the env var and restart the server to edit this setting from the UI."
- Unlocked: toggle rendered from the DB row, help copy reads the standard offline-mode description.

**Handler behavior:**
- Locked: `HandleSettingsPost` skips writing the offline_mode row regardless of the submitted form value. The `staff_can_import_patrons` write path is unaffected.
- Unlocked: `HandleSettingsPost` writes every known setting on every POST per the original DEC-033 design (and the cross-toggle non-interference test in PR #78 continues to pin that behavior).

**Startup log:** When locked, `main.go` logs a single line at startup: "LIBRESHELF_OFFLINE=true is locking offline mode; runtime DB setting will be ignored until the env var is unset."

**Related:**
- DEC-033 -- the original A0 precedence decision being superseded in part.
- bd issue `cs408-go-stack-85j` -- the bug report and four-option brainstorm.
- bd issue `cs408-go-stack-ahq` -- parent A0 work that shipped DEC-033 and surfaced this issue.
- Spec: `docs/specs/2026-05-15-offline-mode-env-lock-design.md`.

---

## DEC-035: Google Books as fallback + field-level enrichment over Open Library

**Date:** 2026-05-15 (cs408-go-stack-8gj, branch `feat/google-books-fallback`).

**Decision:** When `GOOGLE_BOOKS_API_KEY` is set at startup and the Open Library edition response has any gap (TrimSpace-empty title, authors, year, publisher, cover URL, or description), the OL chain fans out to Google Books in parallel with the existing OL work-record and jscmd=data fallbacks. After all concurrent calls complete, `mergePrefill(ol, gb)` applies the rule "OL wins, GB fills gaps" -- each field uses the OL value if non-empty after TrimSpace, otherwise the GB value. Per-field source labels (`DescriptionSource`, `CoverSource`) carry "googlebooks" when GB filled the gap, "openlibrary" when OL filled it. On GB error, the OL data flows through unchanged and `GoogleBooksError = true` so the JS banner can show a "Google Books unavailable" note.

**Why fallback + enrichment, not replacement:** Open Library is the open-data primary that aligns with LibreShelf's self-hostable framing. The DEC-032 chain already covers 100% of OL-catalogued books. The remaining gap is books OL does not catalog at all (mostly new releases and niche presses); GB has substantially better coverage there. For OL-catalogued books with sparse editions, GB's descriptions and covers are often richer than OL's, so enriching gap-by-gap gives noticeable quality improvement without abandoning OL as the source of truth for bibliographic data.

**Why parallel, not sequential:** The OL chain already runs work-record and jscmd=data concurrently (commit 316e31c). Adding GB to that fan-out keeps end-to-end latency dominated by the slowest of the three calls (~700ms typical) rather than their sum (~1500ms+ if GB were tacked on after).

**Asymmetric trigger:** GB fires only when the OL edition response has at least one gap. An entirely-populated OL response (rare but possible) skips the GB call entirely. The gap check happens before the OL work/data fallbacks complete, so it is conservative -- GB may fire even when the work record would have filled the gap. That trade is acceptable: the latency win of the parallel fan-out outweighs the occasional unused GB call.

**API key handling:** `GOOGLE_BOOKS_API_KEY` is opt-in. Unset means GB is disabled; the OL chain runs alone with no error, no warning. Get a key from Google Cloud Console; free tier 1000 requests/day is sufficient for typical small-library volume.

**Error policy:**
- `ErrGoogleBooksDisabled` (key unset) -- silent short-circuit; no flag.
- `ErrGoogleBooksNotFound` (GB has no record) -- silent short-circuit; no flag.
- Other errors (network, 5xx, decode, timeout) -- `GoogleBooksError = true` on the response; OL data still flows through; JS renders a small "Google Books unavailable" banner note.

**Attribution:** A site-wide footer renders on every page through the shared layout. When offline-locked: no attribution (no external data is being used). When GB configured: "Book data from Open Library and Google Books." Otherwise: "Book data from Open Library." State captured once at startup; templates do not re-check on every render.

**Cover downloads:** A GB-sourced cover URL flows through the same `cover_url` form field that OL covers use. On submit, `HandleBookCreate` / `HandleBookUpdate` call `SaveCoverFromURLGated` to download. No new download path is added; the existing gated wrapper handles any URL the prefill returns.

**Struct change:** `OpenLibraryBook` is renamed to `BookPrefill` (PR commit 1 on this branch). The struct now represents the merged-source payload, not OL alone. New fields: `CoverSource` (parallel to `DescriptionSource`), `GoogleBooksError`. Existing JSON consumers (the admin Lookup JS) ignore the new fields when zero-valued via `omitempty`.

**Related:**
- DEC-032 -- OL enrichment chain that this design layers on top of.
- DEC-033 -- offline-mode predicate that gates the GB call.
- DEC-034 -- env-var-as-lock precedence flip that GB consumes via IsExternalAllowed.
- bd issue `cs408-go-stack-8gj` -- the parent issue for this work.
- Spec: `docs/specs/2026-05-15-google-books-fallback-design.md`.

---

## DEC-036: Idempotent additive ALTER TABLE migrations in createSchema

**Date:** 2026-05-16 (cs408-go-stack-ygu, PR #85, branch `feat/patron-address-field`).

**Context:** Until now, LibreShelf's `createSchema` used only `CREATE TABLE IF NOT EXISTS` and any schema change required wiping `data/database.sqlite` -- documented as a CLAUDE.md gotcha. That was acceptable while the project was pre-deployment; once production data exists on EC2, dropping the DB to add a column is no longer acceptable. ygu (patron address field for printed overdue notices) was the first additive change that needed a real migration path.

**Decision:** For additive schema changes (`ADD COLUMN`), run the `ALTER TABLE` statement after the main `CREATE TABLE IF NOT EXISTS` block, and ignore the SQLite `"duplicate column"` error so the path is idempotent on every startup. The migration block lives inline in `createSchema`, not in a separate framework.

Pattern:

```go
if _, err := dm.db.Exec(`ALTER TABLE patrons ADD COLUMN address TEXT`); err != nil &&
    !strings.Contains(err.Error(), "duplicate column") {
    log.Fatalf("Failed to add patrons.address column: %v", err)
}
```

The same column is also included in the `CREATE TABLE` block above. That way:
- **Fresh DBs**: CREATE TABLE creates the column; ALTER returns "duplicate column"; ignored.
- **Existing DBs**: CREATE TABLE IF NOT EXISTS no-ops; ALTER adds the column.
- **Re-running** at startup: ALTER always returns "duplicate column" after the first success; ignored.

**Why inline, not a migration framework:** LibreShelf has one production deployment and a small, well-known schema. A real framework (versioned files, applied-migrations table, up/down scripts) is more machinery than the change rate justifies. The inline pattern handles the common case (additive column) idempotently and explicitly. When a non-additive change is needed -- column rename, NOT NULL with no default, table drop -- we revisit then.

**What this pattern does NOT support:**
- `DROP COLUMN` (SQLite supports it since 3.35, but we don't have a tested pattern yet).
- Column rename. Requires a multi-step ALTER + UPDATE + ALTER dance.
- Adding `NOT NULL` without `DEFAULT`. SQLite rejects this for existing rows.
- Changing a column type. Requires the standard SQLite rebuild dance (create temp table, copy, drop, rename).
- Composite migrations that touch multiple tables atomically. The current pattern Execs ALTERs one at a time; if a later one fails the earlier ones are already committed.

If a change needs any of those, file a bd issue, design the migration as a one-off in the same `createSchema` block (or escalate to a framework), and document in DECISIONS.

**Error-string match is brittle but acceptable:** `strings.Contains(err.Error(), "duplicate column")` matches `modernc.org/sqlite`'s wording, which mirrors upstream SQLite's `SQLITE_ERROR` for that case. The error code itself is not exposed cleanly by the driver. If `modernc` ever changes the wording (rare for SQLite error strings -- they are stable across major versions), the migration would refuse to run on existing DBs, which is a noisy fail-fast at startup. Acceptable risk.

**Updates the CLAUDE.md "Schema changes don't migrate" gotcha:** Partially solved. Additive `ADD COLUMN` is now first-class; non-additive changes still require the local-wipe escape hatch.

**Related:**
- CLAUDE.md gotcha "Schema changes don't migrate" -- updated to reflect this pattern.
- bd issue `cs408-go-stack-ygu` -- the first use; adds `patrons.address TEXT`.
- bd issue `cs408-go-stack-wdy` -- next consumer; the printed-notice route reads this column.
- bd memory `schema-migrations` -- pattern reference for future additive changes.

---

## DEC-037: Per-physical-copy inventory + schema reshape via local wipe

**Date:** 2026-05-23 (CP8 scope, foundation bd issue `cs408-go-stack-e9a`, spec at `docs/specs/2026-05-23-inventory-copies-design.md`).

**Context:** Until now, `books` is catalog-level (one row per ISBN with `quantity_total` + `quantity_available` counters), and `loans` references `book_id`. The rapid-scan checkout portal (cs408-go-stack-yu3) surfaced the structural gap on 2026-05-22: a real library checks books out by the BARCODE on the physical item, not by the ISBN of the title. yu3 is paused on the wrong-model implementation; this decision documents the data-model refactor that unblocks it.

**Decision:** Introduce a `copies` table that represents each physical book with its own unique barcode and status. Replace `loans.book_id` with `loans.copy_id`. Drop `books.quantity_total` and `books.quantity_available`; both become derived counts over `copies`. Add `books.dewey` for spine-label printing. Support four barcode formats: `code128` (library-generated LSF prefix + 7-digit + Luhn), `code39` (legacy compatibility), `ean13` and `upca` (publisher barcodes already printed on books). Each `copies` row carries its `barcode_format` so we know how to render or re-validate.

The full design including label content, print layout, UI surfaces, enrichment-chain extension, and the six-issue slicing lives in `docs/specs/2026-05-23-inventory-copies-design.md`. That document is the source of truth; this DECISIONS entry records the architectural shift, the mechanism for getting there, and the reasoning that's specific to LibreShelf's state today.

**Why now:** yu3 cannot ship without it. The existing counter-based model produces incorrect data for any operation that needs to identify a specific physical book (lost-copy tracking, condition state, accurate label printing). Earlier checkpoints worked around the gap because they only needed availability counts; circulation at volume needs per-copy identity.

**Mechanism: schema reshape via local wipe, no migration code shipped.** The CS408 EC2 deployment was retired post-class. No live deployment exists as of 2026-05-23, so there is no production data to preserve. The new code ships with `CREATE TABLE` statements in their final shape (books without quantity columns + with `dewey`, loans with `copy_id NOT NULL` + no `book_id`, `copies` table + indexes); local developer databases are deleted (`rm data/database.sqlite*`) and re-seeded from `SeedBooks` on next startup. No `migrateToCopies` function is shipped.

This intentionally departs from DEC-036's "no more wipes once deployment exists" direction because the deployment that drove that decision no longer exists. The wipe is a one-time inconvenience for two developer machines, not a data-loss event. The DEC-036 pattern itself remains the correct path for any future additive change against a live deployment; only the historical-context paragraph in DEC-036 ("once production data exists on EC2") is overtaken by events.

**What this implies for any future deployment with data:** if LibreShelf gets redeployed (DigitalOcean droplet, Hetzner, anywhere) and accrues data that has to be preserved across the next non-additive change, that change will need a one-off migration function. The pattern would be: gate on schema-shape detection, wrap the transformation in a transaction, ship the function in the binary forever as a one-shot. We are not implementing this speculatively; the code-debt is documented here so a future session knows to design it when the deployment actually exists.

**Library barcode format (LSF):** `LSF` + 7-digit zero-padded sequence + 1 Luhn check digit (total 11 chars). Rendered as Code 128 by the print pipeline. Code 128 accepts the full ASCII set including letters, so the LSF prefix encodes cleanly. The 7-digit sequence allocator is decided in the foundation issue (likely `MAX(barcode) WHERE barcode LIKE 'LSF%'` until we hit a concurrent-add race, which is theoretical at library volume).

**Why these four formats and not the full retail set:** Code 128 and Code 39 cover any alphanumeric library ID; EAN-13 and UPC-A cover publisher barcodes already printed on most modern books. UPC-E, EAN-8, ITF-14, and GS1-128 add validation complexity without matching the inventory a small library actually has. The screenshot that listed all six retail formats was the prompting source; the decision after design review was to keep the set tight.

**Why we accept publisher barcodes alongside library ones:** Forcing every book to wear a library sticker is real labor. Modern books arrive with EAN-13 / UPC-A already printed; the scanner reads them fine. The trade is mixed-format inventory data (different copies of the same title may have different formats), which we handle by storing the format alongside the barcode and rendering re-prints in whatever format the copy was added with.

**Why no per-copy Dewey:** The librarian's mental model is one Dewey per title, regardless of how many physical copies exist. If a copy is shelved differently (reference vs circulating), that is a `location` or `status` concern, not classification. We keep Dewey on `books` and treat per-copy section as a backlog item.

**Derived counts, not denormalized counters:** `quantity_available` becomes `COUNT(*) FROM copies WHERE book_id = ? AND status = 'available' AND id NOT IN (active loans)`. We take the join hit on the per-book display rather than maintain a counter that can drift from the truth. Catalog list views may need an index review later; the foundation issue will benchmark.

**Updates the CLAUDE.md "Schema changes don't migrate" gotcha:** ships a one-time wipe instruction for the dev machines, but the DEC-036 additive-ALTER pattern remains the default for future schema work. The gotcha entry gets a "non-additive changes today require a local wipe; if a deployment exists, design a migration first" line.

**Related:**
- DEC-036 -- additive-only ALTER pattern. Still the default for additive changes; this decision documents the one-off wipe path for the non-additive case while no deployment exists.
- DEC-032, DEC-035 -- enrichment chains gaining the Dewey field in issue 5 of the slicing.
- `docs/specs/2026-05-23-inventory-copies-design.md` -- full spec including UI, label layout, test plan, and issue slicing.
- bd issue `cs408-go-stack-yu3` -- paused rapid-scan portal; rebuilt and closed by issue 6.
- bd memory `inventory-copies-barcodes-design` -- prior single-format sketch from 2026-05-22; superseded.

## DEC-038: Soft-fail CSRF on logout

**Date:** 2026-05-28
**Issue:** cs408-go-stack-q14

**Context:** POST `/logout` returned 403 "CSRF validation failed -- token mismatch" when the rendered logout form held a CSRF token from a session that was no longer the one the cookie pointed to. The trigger is a stale page: `HandleLoginPost` mints a fresh session (new token + new CSRF) on every login and does not delete the user's prior session rows, so re-logging in (or logging in in a second tab) rotates the `session` cookie while an already-open page still carries the old session's CSRF token. Clicking logout on that older page submits the stale token; `RequireAuth` loads the current session and the constant-time compare fails. A `GetSession` miss (expired/wiped) redirects to `/login` instead, so a *mismatch* specifically means a live session with a non-matching token.

**Decision:** Logout soft-fails CSRF. `/logout` moves off the strict `CSRFProtect` chain into its own group wired `RequireAuth, DBReadLock`. `HandleLogout` still reads the form token and logs any mismatch for visibility, but proceeds to delete the session, clear the cookie, and redirect to `/login` regardless. `/account/change-password` and all other state-changing POSTs keep strict `CSRFProtect`.

**Why:** Logout is idempotent and destroys the session no matter what, so a CSRF rejection there is pure friction with little security upside. The only thing CSRF-on-logout defends against is a forced-logout (an attacker's cross-site POST logging the victim out). For a staff library tool that is a low-risk annoyance, not a breach. We accept that exposure; it is identical to simply exempting logout from CSRF, but the soft-fail shape keeps the clean redirect-to-login and still tears down the session server-side.

**Rejected alternatives:**
- *Leave strict:* correct CSRF behavior but recurring friction every time a tab goes stale; hard-refresh-before-logout is not a reasonable ask of staff.
- *Exempt entirely (drop CSRFProtect from the route):* nearly identical security posture; soft-fail chosen for the explicit, logged, documented handling.
- *Single session per user (delete prior sessions on login):* a larger session-model change that still leaves the stale-token-in-an-open-tab edge unresolved. Filed as future work if multi-session semantics ever matter.

**Tests:** `TestAuthenticatedPOSTWithCSRF` repointed at a dummy CSRF-protected route to keep guarding "RequireAuth populates csrfToken." `TestLogoutSoftFailsCSRF` asserts POST `/logout` with no token returns 302, clears the session cookie, and deletes the session row.

**Related:**
- DEC-028 -- security hardening (SecurityHeaders, session/CSRF model).
- `handlers_auth.go` `CSRFProtect` / `HandleLogout`; `main.go` route groups.
