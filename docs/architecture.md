# LibreShelf Architecture

LibreShelf is a self-hostable library management system. A small library (school, office, or
personal collection) manages books, patrons, and loans through a simple web UI. A public kiosk
lets anyone browse the catalog without logging in; patrons may optionally log in to favorite
books and request holds. All checkout and return transactions are handled by staff.

This document is the architecture and design reference. For security details see
[`security.md`](./security.md). For deployment instructions see [`deployment.md`](./deployment.md).
For numbered design decisions see [`../DECISIONS.md`](../DECISIONS.md).

---

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.25.9+ |
| Web framework | Gin (`github.com/gin-gonic/gin`) |
| Templating | Go `html/template` with layout pattern |
| Database | SQLite via `modernc.org/sqlite` (pure Go, no CGo) |
| CSS | Bootstrap 5.3 (served locally; no CDN) |
| Deployment | systemd + nginx |

---

## Routes

| Route | Page | Access |
|-------|------|--------|
| `GET /login` | Login | Public |
| `POST /login` | Login action | Public |
| `POST /logout` | Logout action | Any logged-in user |
| `GET /` | Dashboard | Any logged-in user |
| `GET /catalog` | Catalog | Any logged-in user |
| `GET /books/:id` | Book detail | Any logged-in user |
| `GET /kiosk` | Public anonymous catalog | Public |
| `GET /kiosk/books/:id` | Public read-only book detail | Public |
| `GET /loans` | Active / overdue loan list with filter | Staff + admin |
| `POST /books/:id/checkout` | Check out a specific copy to a patron (barcode required) | Staff + admin |
| `POST /loans/:id/return` | Return a checked-out copy | Staff + admin |
| `GET /my/loans` | Patron's own active loans | Patron |
| `GET /books/:id/copies` | Per-book Manage Copies page (DEC-037) | Staff + admin |
| `POST /books/:id/copies` | Add one library-format copy (LSF barcode allocated server-side) | Staff + admin |
| `GET /inventory` | Top-level inventory listing with status + needs-relabel filters | Staff + admin |
| `POST /copies/:id/status` | Mark copy lost / damaged / withdrawn / available | Staff + admin |
| `POST /copies/:id/delete` | Delete a copy (rejected if it has loan history) | Staff + admin |
| `GET /patrons` | Patron list | Staff + admin |
| `POST /patrons` | Create patron | Staff + admin |
| `POST /patrons/:id/edit` | Edit patron | Staff + admin |
| `POST /patrons/:id/delete` | Delete patron | Staff + admin |
| `GET /staff` | Staff management | Admin |
| `POST /staff` | Create staff / admin | Admin |
| `POST /staff/:id/edit` | Edit staff / admin | Admin |
| `POST /staff/:id/delete` | Delete staff / admin | Admin |
| `POST /staff/:id/password` | Reset staff password | Admin |
| `GET /books/new` | Add-book form | Staff + admin |
| `POST /books` | Create book | Staff + admin |
| `GET /books/:id/edit` | Edit-book form | Staff + admin |
| `POST /books/:id/edit` | Update book | Staff + admin |
| `POST /books/:id/delete` | Delete book | Admin |
| `GET /api/openlibrary/isbn/:isbn` | Open Library ISBN lookup proxy (JSON) | Staff + admin |
| `GET /admin` | Admin tools index | Admin |
| `GET /admin/backup` | Backup admin page (stats + export + restore modal) | Admin |
| `GET /admin/backup/export` | ZIP backup download | Admin |
| `POST /admin/backup/import` | ZIP backup restore (atomic swap with `.bak` rollback) | Admin |
| `GET /admin/settings` | System-wide toggles | Admin |
| `POST /admin/settings` | Update toggles | Admin |
| `GET /admin/patrons/import` | CSV patron import form | Admin always; staff when `staff_can_import_patrons` is on |
| `POST /admin/patrons/import` | Parse + dedup + preview the upload | Same |
| `POST /admin/patrons/import/confirm` | Commit the import (token-keyed) | Same |
| `GET /admin/patrons/import/download/:token` | Single-use credentials / errors CSV download | Same |
| `GET /patrons/:id/login-credentials` | Reveal a patron's temp password for distribution | Staff + admin |
| `POST /patrons/:id/dismiss-temp` | Clear the stored temp ("Mark as Delivered") | Staff + admin |
| `POST /patrons/:id/regenerate-temp` | Regenerate temp + invalidate prior sessions | Staff + admin |
| `GET /account/change-password` | Forced-change form for users with `must_change_password=1` | Any logged-in user |
| `POST /account/change-password` | Set new password, clear flag, redirect to /login | Any logged-in user |

---

## Database Schema

```sql
CREATE TABLE books (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    isbn           TEXT UNIQUE,                       -- null for books without ISBN
    title          TEXT NOT NULL,
    publisher      TEXT,
    year           INTEGER,
    description    TEXT,
    cover_filename TEXT,                              -- filename only; file lives in DATA_DIR/covers/
    genre          TEXT,
    dewey          TEXT                               -- Dewey decimal / call number; populated manually or by OL enrichment (DEC-037)
);

-- books has no quantity columns post-DEC-037. Availability is derived
-- from the copies + loans tables at query time:
--   total_copies     = COUNT(*) FROM copies WHERE book_id = ? AND status != 'withdrawn'
--   available_copies = COUNT(*) FROM copies WHERE book_id = ? AND status = 'available'
--                                                            AND id NOT IN (active loans)

CREATE TABLE authors (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE COLLATE NOCASE              -- case-insensitive uniqueness
);

CREATE TABLE book_authors (
    book_id   INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    author_id INTEGER NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
    position  INTEGER NOT NULL DEFAULT 0,                 -- 1-indexed author order on the book jacket
    PRIMARY KEY (book_id, author_id)
);

-- copies is the per-physical-book inventory table introduced in DEC-037.
-- Each row is one physical copy with its own barcode and status.
CREATE TABLE copies (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id        INTEGER NOT NULL REFERENCES books(id),
    barcode        TEXT NOT NULL UNIQUE,                  -- LSF library code OR publisher EAN-13/UPC-A
    barcode_format TEXT NOT NULL DEFAULT 'code128',       -- one of: code128, code39, ean13, upca
    status         TEXT NOT NULL DEFAULT 'available',     -- available / lost / damaged / withdrawn
    needs_relabel  INTEGER NOT NULL DEFAULT 0,            -- 1 = label needs printing/reprinting (DEC-037)
    acquired_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_copies_book_id ON copies(book_id);
CREATE INDEX idx_copies_status ON copies(status);

CREATE TABLE patrons (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    email       TEXT,
    phone       TEXT,
    address     TEXT,                                     -- street address for printed overdue notices (DEC-036)
    joined_date DATETIME DEFAULT CURRENT_TIMESTAMP,
    metadata    TEXT                                      -- JSON, nullable; UI deferred (DEC-016)
);

CREATE TABLE loans (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    copy_id        INTEGER NOT NULL REFERENCES copies(id),  -- per-copy, not per-title, post-DEC-037
    patron_id      INTEGER NOT NULL REFERENCES patrons(id),
    checked_out_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    due_date       TEXT NOT NULL,                           -- TEXT, not DATE: modernc.org/sqlite driver auto-parses DATE columns
    returned_at    DATETIME                                 -- NULL while still checked out
);
CREATE INDEX idx_loans_copy_id ON loans(copy_id);
CREATE INDEX idx_loans_patron_id ON loans(patron_id);

-- Overdue is never stored; computed at query time:
--   returned_at IS NULL AND due_date < DATE('now')

CREATE TABLE users (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    username             TEXT NOT NULL UNIQUE,
    password_hash        TEXT NOT NULL,                       -- bcrypt
    role                 TEXT NOT NULL CHECK(role IN ('admin', 'staff', 'patron')),
    patron_id            INTEGER REFERENCES patrons(id),      -- NULL for admin and staff
    created_at           DATETIME DEFAULT CURRENT_TIMESTAMP,
    must_change_password INTEGER NOT NULL DEFAULT 0,          -- 1 forces /account/change-password on next request (DEC-030)
    temp_password        TEXT                                 -- plaintext temp for per-row reveal on /patrons; cleared on successful change or admin dismissal (DEC-030)
);

CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,                          -- crypto/rand
    user_id    INTEGER NOT NULL REFERENCES users(id),
    csrf_token TEXT NOT NULL,                             -- crypto/rand, bound to session (DEC-017)
    expires_at DATETIME NOT NULL                          -- canonical UTC "YYYY-MM-DD HH:MM:SS" (DEC-018)
);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,                          -- key/value store for admin toggles (DEC-030)
    value      TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_by INTEGER REFERENCES users(id)
);
```

### Seed Accounts

Created on first run if they don't exist:

| Username | Password | Role | Notes |
|----------|----------|------|-------|
| `admin` | `Admin123!` | admin | Overridable via `ADMIN_PASSWORD` (validated on startup; DEC-021) |
| `staff1` | `Staff123!` | staff | |
| `patron1` | `Patron123!` | patron | `users.patron_id` is NULL; the seed user can log in but has no `patrons` row. New patrons created via `HandlePatronCreate` get a linked `patrons` row per DEC-022 |

---

## Directory Structure

```
libreshelf/
├── main.go                       # Entry point: router, template loading, middleware, server
├── main_test.go                  # HTTP boundary + role tests; setupTestRouter + loginAs helpers
├── db.go                         # DatabaseManager: schema creation + all CRUD methods
├── db_test.go                    # DB method tests
├── handlers.go                   # renderTemplate / renderPage helpers + dashboard / admin / kiosk / not-found
├── handlers_auth.go              # Login, logout, session middleware, RequireAuth / RequireStaff / RequireAdmin, CSRFProtect
├── handlers_books.go             # Catalog, detail, Create / Edit / Update / Delete, Open Library proxy
├── handlers_books_test.go
├── handlers_patrons.go           # Patron list, create, edit, delete
├── handlers_patrons_test.go
├── handlers_staff.go             # Staff list, create, edit, delete, reset-password
├── handlers_staff_test.go
├── handlers_loans.go             # Checkout / return + /loans list view + /my/loans
├── handlers_loans_test.go
├── handlers_copies.go            # Manage Copies + Inventory + status / delete actions (DEC-037)
├── handlers_copies_test.go
├── barcodes.go                   # LSF library barcode format + Luhn check digit (DEC-037)
├── barcodes_test.go
├── seed_books.go                 # Dev-only seed fixture (LIBRESHELF_SEED_DEV_BOOKS env gate)
├── handlers_kiosk_test.go
├── db_loans_test.go
├── handlers_admin.go             # Admin tools index + backup export / import (DEC-027, DEC-029)
├── handlers_admin_test.go        # Backup export / import tests including Zip Slip rejection
├── handlers_security_test.go     # SecurityHeaders + HSTS gating tests (DEC-028)
├── handlers_auth_test.go
├── openlibrary.go                # Open Library API client (DEC-008)
├── openlibrary_test.go
├── covers.go                     # Cover upload + OL-URL download with MIME / size / extension validation
├── covers_test.go
├── flash.go                      # HttpOnly flash cookies (success + error + detail companion)
├── validators.go                 # IsValidISBN, ValidateUsername, ValidatePassword, generateBaseUsername
├── validators_test.go
├── validators_isbn_test.go
├── internal/safezip/             # ZIP extraction with Zip Slip / symlink / size protections (DEC-027)
│   ├── doc.go
│   ├── extract.go
│   └── extract_test.go           # 13 cases at 87.5% coverage including hand-rolled zip-bomb fixture
├── templates/
│   ├── layout.html               # Base layout with responsive offcanvas sidebar
│   ├── login.html                # Standalone login (no sidebar)
│   ├── index.html                # Dashboard
│   ├── catalog.html              # 4-wide book grid; search / filter
│   ├── book_detail.html          # Single book view + Edit / Delete (type-to-confirm delete)
│   ├── book_form.html            # Shared new / edit form: ISBN + OL Lookup, cover preview
│   ├── patrons.html              # Patron list + Add / Edit / Delete modals
│   ├── staff.html                # Staff list + Add / Edit / Delete / Reset Password modals
│   ├── admin.html                # Admin tools index (DEC-029)
│   ├── backup_admin.html         # Backup admin: stats + export + restore-from-backup modal (DEC-027)
│   ├── kiosk.html                # Public catalog grid
│   ├── kiosk_layout.html         # Public kiosk shell (no sidebar, "Public Terminal" header)
│   ├── kiosk_book_detail.html    # Public read-only book detail
│   ├── loans.html                # Active / overdue loan list with filter
│   ├── my_loans.html             # Patron's own active loans (RequirePatron)
│   └── error.html                # 404 / 500 error page
├── static/
│   ├── stylesheets/
│   │   ├── bootstrap.min.css
│   │   └── style.css
│   ├── javascripts/
│   │   ├── bootstrap.bundle.min.js
│   │   ├── app.js                # Catalog filter, modal init, OL Lookup, cover preview, live validation
│   │   └── admin_backup.js       # Backup-restore modal interlock (extracted for CSP)
│   └── images/
│       └── favicon.svg
├── data/                         # Gitignored; holds database.sqlite, covers/, WAL files
├── deploy/
│   └── libreshelf.service        # systemd unit
├── docs/                         # Architecture, security, deployment, archived class artifacts
├── .tool-versions                # Go toolchain pin (asdf / mise)
├── CLAUDE.md                     # Internal working notes
├── DECISIONS.md                  # Numbered architectural decisions
├── README.md
├── go.mod
├── go.sum
└── .gitignore
```

---

## Access Control

Three roles enforced via middleware chains (see DEC-014):

| Middleware | Routes |
|-----------|--------|
| `RequireAuth` + `DBReadLock` | `/`, `/catalog`, `/books/:id`, `POST /logout` |
| `RequirePatron` + `DBReadLock` | `/my/loans` |
| `RequireStaff` + `DBReadLock` | `/patrons` + patron CRUD, book add/edit, OL lookup proxy, `/loans`, checkout, return |
| `RequireAdmin` + `DBReadLock` | `/staff` + staff CRUD + password reset, book delete, `/admin`, `/admin/backup`, backup export |
| `RequireAdmin` (no `DBReadLock`) | `POST /admin/backup/import` -- takes the write lock directly to swap the DB file (DEC-027) |
| `LoadUser` (optional auth) | `/kiosk`, `/kiosk/books/:id` |
| Public | `GET /login`, `POST /login`, static files |

Permissions:

| Capability | Admin | Staff | Patron |
|---|---|---|---|
| Dashboard, catalog, book detail | Yes | Yes | Yes |
| Loan history on book detail | Yes | Yes | No |
| Checkout / return | Yes | Yes | No |
| Book add / edit | Yes | Yes | No |
| Book delete | Yes | No | No |
| Open Library ISBN lookup | Yes | Yes | No |
| Patron list and CRUD | Yes | Yes | No |
| Staff management (list, add, edit, delete, reset password) | Yes | No | No |
| Admin tools (`/admin`) | Yes | No | No |
| ZIP export / import | Yes | No | No |

Patrons cannot self-checkout; their login exists for browsing, favorites, and (deferred) holds
on the kiosk.

---

## Key Design Decisions

For the full numbered design decisions log see [`../DECISIONS.md`](../DECISIONS.md). Below
are the highlights of the architecture-shaping calls.

### Bootstrap served locally

Bootstrap is bundled in `static/stylesheets/` and `static/javascripts/` rather than loaded
from a CDN. This keeps the app fully offline-capable (a primary deployment target) and
eliminates CDN supply-chain risk.

### Flat package structure

All `.go` files in `package main` at the repo root, split by concern via filename suffix
(`handlers_books.go`, `handlers_patrons.go`, etc.). Sub-packages would add indirection
without benefit at this scale. The one exception is `internal/safezip`, pulled out because
it is reusable, has its own boundary, and merits isolated tests.

### Open Library API: server-side proxy with multi-endpoint enrichment chain

ISBN metadata is fetched server-side at `GET /api/openlibrary/isbn/:isbn` to avoid CORS,
keep client JS simple, and allow server-side validation (`IsValidISBN`) before any outbound
request. The handler returns clean JSON to the form for prefill.

`FetchOpenLibraryBook` (in `openlibrary.go`) chains across multiple OL endpoints because
OL splits metadata across edition records (per printing) and work records (per abstract
book), and the splits are inconsistent. The chain in priority order:

| Layer | Endpoint | Provides |
|---|---|---|
| 1. Edition | `/api/books?jscmd=details` | description (sometimes), structured authors (sometimes), cover IDs (sometimes), publisher, year |
| 2. Work | `/works/<key>.json` | description fallback + cover-ID fallback (single call covers both) |
| 3. ISBN-cover probe | `HEAD /b/isbn/<isbn>-L.jpg?default=false` on `covers.openlibrary.org` | cover URL when edition+work both empty -- abstracts over OL's duplicate-work indexing |
| 4. jscmd=data | `/api/books?jscmd=data` | resolved author names when neither edition shape carried them |

All fallbacks are non-fatal: a failed secondary call logs and the function returns
whatever the primary call yielded. The cover URLs are transient -- `SaveCoverFromURL`
downloads to `data/covers/<hash>.jpg` and stores only the local filename, so the app works
offline post-ingest. See DEC-032 for the full design including why Wikipedia was tried
and reverted.

### ZIP backup using the standard library

`archive/zip` from the standard library is sufficient. Extraction goes through the
purpose-built `internal/safezip` package, which enforces six rules (no symlinks, no
backslash, no absolute paths, Zip Slip via `filepath.Rel`, per-file size cap, total size
cap) in pass 1 before any byte hits disk in pass 2. See DEC-027.

### Template loading

`map[string]*template.Template`, one entry per page, each paired with `layout.html`. The
`renderTemplate` helper executes `"layout"`, which pulls in the page's `"content"` block.
Templates are parsed once at startup via `template.Must` -- there is no hot reload; a
template edit takes effect only after a process restart.

### SQLite driver

`modernc.org/sqlite` registers as `"sqlite"`, not `"sqlite3"`. Pure Go -- no CGo, no system
library dependency. This means a single static binary for any GOOS/GOARCH target.

`openDB` passes three PRAGMAs plus `_txlock=immediate` in the DSN so the driver applies
them to every connection it opens. WAL lets readers and writers proceed without blocking
each other; the 5s busy timeout makes a losing concurrent writer queue on the journal lock
instead of returning `SQLITE_BUSY`; and `_txlock=immediate` makes every write transaction
take the RESERVED lock at BEGIN time rather than upgrade-on-first-write (which would race
on the snapshot and return `SQLITE_BUSY_SNAPSHOT` under contention). Together they let two
staff members hit checkout simultaneously without spurious errors, and they make the
in-transaction availability guard in `CheckoutBook` provably TOCTOU-safe under contention
(see `TestCheckoutBookConcurrentRace` in `db_loans_test.go` and DEC-031).

### Loan state via columns, not a status field

The three loan states (active, returned, overdue) are derivable from `returned_at` and
`due_date`, so there is no `status` column to keep in sync. Overdue is computed at query
time: `returned_at IS NULL AND due_date < DATE('now')`. See DEC-024.

### Single `/loans` page with a filter

Active and overdue loans share one list view template with a query-param filter
(`/loans?filter=active|overdue`). No per-patron grouping, no print CSS in v1. See DEC-025.

### Admin tools index pattern

`/admin` is a card-grid landing page that drills into dedicated `/admin/<tool>` routes
(currently just `/admin/backup`). Lightweight settings can land inline as the toolset grows.
See DEC-029.

### Schema changes do not migrate

`createSchema()` uses `CREATE TABLE IF NOT EXISTS`. Altering an existing column requires
either an `ALTER TABLE` migration or wiping `data/database.sqlite` locally. There is no
migration framework yet -- the schema has been small enough to manage by hand.
