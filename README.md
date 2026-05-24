# LibreShelf

A self-hostable library management system, built in Go.

LibreShelf lets a small library -- a school, office, or personal collection -- manage books,
patrons, and loans through a simple web UI. A public kiosk lets anyone browse the catalog
without logging in. All checkout and return transactions are handled by staff.

## Features

- Book catalog with search, genre filter, and availability filter
- Open Library ISBN lookup with cover image preview on add/edit
- Per-physical-copy inventory: every book has one or more `copies` rows, each with its own barcode and status (DEC-037)
- Library-format barcode generator (`LSF` prefix + 7-digit sequence + Luhn check digit, rendered as Code 128); publisher EAN-13 / UPC-A barcodes also storable
- Manage Copies page per book + top-level Inventory listing with status filters (available / lost / damaged / withdrawn) and a needs-relabel filter
- Barcode-driven checkout: staff scans / types the copy barcode and the handler validates it belongs to the title before creating a loan
- Loan transactions with overdue tracking; due dates derived from `due_date` and `returned_at`, not stored
- Three-role access model: admin, staff, patron
- Public kiosk for anonymous catalog browsing
- ZIP backup export and import with Zip Slip protection and atomic `.bak` rollback
- CSV patron import with column auto-mapping (IDOC / inmate / library card / student ID), records-only or with-logins mode, dedup, and per-row temp-password recovery on the patron list
- Force-change-on-first-login enforced for any patron with a server-generated temp password
- CSRF protection, session-bound tokens, bcrypt password hashing
- Defensive HTTP headers (CSP, X-Frame-Options, HSTS gated on production)
- Pure-Go SQLite via `modernc.org/sqlite` -- no CGo, no system libraries

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.25.9+ |
| Web framework | [Gin](https://github.com/gin-gonic/gin) |
| Templating | Go `html/template` with layout pattern |
| Database | SQLite via [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) (pure Go, no CGo) |
| CSS | Bootstrap 5.3 (served locally; no CDN) |
| Deployment | systemd + nginx |

## Quick Start

```bash
git clone https://github.com/timLP79/LibreShelf.git
cd LibreShelf
go mod download
go run .
```

Visit `http://localhost:3000`. The schema is created on first run, and three default accounts
are seeded (see below). The catalog starts empty; add books through the UI or set
`LIBRESHELF_SEED_DEV_BOOKS=1` to populate a fixed dev fixture (~10 well-known titles, one
library-format copy per quantity slot) on first run.

## Default Accounts

Created on first run if they don't already exist.

| Username | Password | Role |
|----------|----------|------|
| `admin` | `Admin123!` | Admin -- full access |
| `staff1` | `Staff123!` | Staff -- day-to-day operations |
| `patron1` | `Patron123!` | Patron -- catalog and book detail |

Override the admin password by setting the `ADMIN_PASSWORD` environment variable. Passwords
must be 8+ characters with at least one uppercase letter, one digit, and one special character;
the policy is enforced at startup. See [DEC-021](./DECISIONS.md) for the rationale.

## Pages and Access

| Route | Page | Access |
|-------|------|--------|
| `/` | Dashboard with role-differentiated stat cards | Any logged-in user |
| `/catalog` | Searchable book grid | Any logged-in user |
| `/books/:id` | Book detail with availability and loan history | Any logged-in user |
| `/books/new`, `/books/:id/edit` | Add or edit a book, with Open Library lookup | Staff + admin |
| `/books/:id/copies` | Per-book Manage Copies page with status / delete actions | Staff + admin |
| `/inventory` | Top-level inventory listing with status + needs-relabel filters | Staff + admin |
| `/patrons` | Patron management with add / edit / delete modals | Staff + admin |
| `/staff` | Staff management with add / edit / delete / reset-password modals | Admin |
| `/admin` | Admin tools index | Admin |
| `/admin/backup` | Library statistics, ZIP export, restore-from-backup modal | Admin |
| `/loans` | Active and overdue loans, with role filter | Staff + admin |
| `/my/loans` | Patron's own active loans | Patron |
| `/kiosk`, `/kiosk/books/:id` | Public anonymous browse | Public |

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `3000` | HTTP server port |
| `DATA_DIR` | `data` | Directory for SQLite database and cover images |
| `DB_NAME` | `database.sqlite` | Database filename |
| `ADMIN_PASSWORD` | `Admin123!` | Override the seeded admin password. Validated against the password policy at startup |
| `APP_ENV` | (unset) | Set to `production` to enable the `Secure` cookie flag and HSTS |
| `LIBRESHELF_SEED_DEV_BOOKS` | (unset) | Set to any non-empty value to populate the dev fixture catalog on first run. Production installs leave this unset and start with an empty catalog |
| `LIBRESHELF_OFFLINE` | (unset) | Set to `true` to lock offline mode (skip all external API calls); runtime DB setting is ignored until unset |
| `GOOGLE_BOOKS_API_KEY` | (unset) | Optional. When set, the OL enrichment chain fans out to Google Books as a fallback for books OL does not catalog (DEC-035) |

## Documentation

- [Architecture and design reference](./docs/architecture.md) -- routes, schema, directory layout, design decisions
- [Security model](./docs/security.md) -- threat model, mitigations, auth, CSRF, headers
- [Deployment guide](./docs/deployment.md) -- build, systemd, nginx
- [Product specification](./docs/product-spec/libreshelf-product-specification.pdf) (PDF)
- [UI wireframes](./docs/product-spec/wireframes/) (PDF)
- [Design decisions log](./DECISIONS.md)

## Status

CS408 v1 scope shipped; CP8 (per-physical-copy inventory + multi-format barcodes + label
printing) is the current focus. 2 of 6 CP8 issues merged as of 2026-05-24 (Foundation +
Manage Copies); remaining: multi-format copy entry, Print Labels page, Dewey enrichment,
rapid-scan portal rebuild. See `docs/specs/2026-05-23-inventory-copies-design.md` for the
chain and `bd list --status=open` for the live backlog.

## License

LibreShelf is proprietary software. Copyright (c) 2026 Tim Palacios. See
[LICENSE](./LICENSE) for the full terms.

**Free for individual personal use.** You may install and run LibreShelf
on your own hardware at no charge to manage your own household-scale book
collection.

**Commercial license required** for any organizational use -- including
businesses, churches, nonprofits, schools, public libraries, government
agencies, and correctional facilities -- and for hosting LibreShelf on
behalf of others.

To inquire about a commercial license, open an issue at
[github.com/timLP79/LibreShelf/issues](https://github.com/timLP79/LibreShelf/issues)
with the title `Commercial license inquiry`, or contact the author via
their [GitHub profile](https://github.com/timLP79).
