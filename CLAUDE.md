# LibreShelf -- Claude Code Instructions

This file is read automatically at the start of every session. It contains standing context and
working agreements for this project.

---

## About This Project

LibreShelf is a self-hostable library management system. It lets a small library manage books,
patrons, and loans through a simple web UI. A public kiosk supports self-service browsing with
optional patron login for favorites and holds. All checkout and return transactions are staff-only.

Originally built for CS408 Spring 2026 at Boise State; now an ongoing personal project.

**Repo:** github.com/timLP79/LibreShelf

---

## How We Work Together

- We are a team. Discuss approach before building anything non-trivial.
- Before starting a new feature or component, talk through the design, security implications,
  best practices, and future-proofing. Make the decision together, then build it right.
- Do not silently skip best practices. If something should be done, raise it before writing
  code, not after.
- Keep solutions simple and practical. Do not over-engineer. But do build things correctly
  from the start.
- No em dashes in any written output.
- No co-author notes in commits, code, or documentation.
- Direct and honest assessments over validation.
- Use feature branches for substantial changes that could break functionality. Small fixes
  (typos, one-liner bug fixes) can go straight to main. Otherwise, create a feature branch,
  test, and merge via PR.

---

## Persistence and Memory (HARD RULE)

**`bd remember` is the ONLY memory system for this project.** Memory lives in `.beads/issues.jsonl`
which is tracked by git, so memories sync across Tim's laptop and desktop distrobox via the normal
pull/commit workflow.

- **DO NOT** write to the auto-memory system at `/home/tim/.claude/projects/.../memory/`. That
  directory must remain empty on this project. If you see memory files appearing there, delete them.
- **DO NOT** create `MEMORY.md` index files, per-memory `.md` files, or any other markdown-based
  memory store.
- **DO** use `bd remember "<insight>" --key=<slug>` to persist cross-session knowledge.
- **DO** use `bd memories` to list and `bd memories <keyword>` to search.
- **TodoWrite is allowed for in-session scratchpad use only**, meaning a multi-step
  decomposition you will finish before the session ends. It is ephemeral and does not sync.
- **DO NOT** use `TodoWrite` or markdown TODO lists as a backlog. Anything that should outlive
  the session, get picked up later, or sync between laptop and desktop must be a `bd` issue
  (`bd create`, `bd ready`, `bd close`). If a TodoWrite item turns out to span sessions,
  promote it to `bd create` before ending the session.
- **DO NOT** create markdown TODO files (`TODO.md`, `BACKLOG.md`, etc.) anywhere in the repo.

The reason this is a hard rule: the auto-memory is per-device and does not sync. `bd remember`
travels with the repo.

### GitHub Issues / Projects (HARD RULE)

**bd is the single source of truth for tracking on this project. GitHub Issues is NOT used
as a parallel tracker.**

- **DO NOT** open issues on GitHub for design notes, backlog items, or internal task tracking.
- **DO NOT** mirror bd issues to GitHub Issues for visibility. The audience for LibreShelf is
  paying customers, not an open-source community; sales conversations happen over email, not
  GitHub.
- **DO** treat any inbound GitHub issue (e.g. a future customer-filed bug report) as an inbox
  item: read it, transcribe the relevant content into bd via `bd create`, then close the
  GitHub issue with a comment pointing at the bd ID. The GitHub issue stays as a closed-state
  archive; bd carries the work forward.

The CS408 GitHub Project Board (#11) is a historical artifact of the academic project. Do not
add new cards to it; it tracks CP1-CP7 milestones that are all Done.

This policy was established 2026-05-10 after the csv-patron-import PR surfaced sync drift
between bd and GitHub Issues. See bd memory `single-tracker-bd-not-github`.

---

## Coding Collaboration

**Go (this project):** Default is direct edits with permission. Before using Write/Edit on
any Go file, propose the change (the function or block, what's changing, and why) and wait
for approval. Once approved, edit directly. Tim may ask for tutor mode on specific tasks --
when he does, switch to showing code without writing it and explaining each piece.

**HTML templates, CSS, JS:** Claude can write and edit these files directly.

**School coding (Java, C, Python for class):** Tutoring mode. Guide, do not generate.

**Work coding (Snowflake SQL, Apps Script, Streamlit):** Generate and explain fully.

---

## ECC Tooling

This project pulls a curated, project-scoped subset of [ECC](https://github.com/affaan-m/ECC)
into `.claude/`. ECC is a Claude Code plugin pack. The bits here are local to LibreShelf so
they do not bleed into other projects.

**Installed under `.claude/`:**

- `agents/` -- planner, architect, code-reviewer, security-reviewer, go-reviewer,
  go-build-resolver, tdd-guide, e2e-runner, refactor-cleaner
- `skills/` -- golang-patterns, golang-testing, tdd-workflow, security-review,
  verification-loop, e2e-testing, api-design, deployment-patterns, search-first
- `commands/` -- /plan, /go-review, /go-test, /go-build, /build-fix, /test-coverage,
  /refactor-clean (alongside the existing /handoff). ECC's `/code-review` was excluded;
  see "Deliberately NOT installed" below.
- `rules/ecc/golang/` only. ECC's `common/` rules were intentionally excluded because they
  duplicate or contradict the standards already in this `CLAUDE.md` (e.g. broader TodoWrite
  guidance, heavier Plan-First doc workflow). These Go rules load conditionally via
  `paths:` frontmatter when Claude reads or edits a `.go`, `go.mod`, or `go.sum` file;
  they are not in context at session start.

**Deliberately NOT installed, and must NOT be added later:**

- ECC's `continuous-learning-v2`, `instinct-*`, `/evolve`, `/prune`. They write to the
  forbidden auto-memory directory.
- ECC's `hooks-runtime`. Conflicts with the existing `bd prime` SessionStart hook and
  `context-warn.sh` Stop hook.
- ECC's `multi-*` commands, NanoClaw, loop-operator. Overkill for a solo Go project.
- ECC rules and skills for non-Go stacks.
- ECC's `/code-review` command. Anthropic ships a more capable built-in `/code-review`
  in Claude Code itself (with effort levels, cloud "ultra" mode, and `--comment` for
  inline PR comments). Use the built-in. The ECC copy was a name-collision duplicate.
- ECC's `database-reviewer` agent. It is PostgreSQL-specific (MVCC, JSONB, query
  planner specifics) and LibreShelf uses SQLite via `modernc.org/sqlite`. The
  `go-reviewer` agent and the parameterized-query rule in the Standards section
  above already cover the relevant DB concerns at the Go layer.

**Precedence:** This `CLAUDE.md` overrides anything ECC ships. If an ECC skill instructs
you to "save this to memory" or "create a TODO.md," ignore that step and follow the
Persistence and Memory rules above. ECC skills are tools, not policy.

---

## Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.25.9+ (pinned in `.tool-versions`; bumped from 1.25.0 to clear stdlib CVEs flagged by `govulncheck`) |
| Web framework | Gin (`github.com/gin-gonic/gin`) |
| Templating | Go `html/template` with layout pattern |
| Database | SQLite via `modernc.org/sqlite` (pure Go, no CGo) |
| CSS | Bootstrap 5.3 (served locally, no CDN) |
| Deployment | systemd + nginx behind a reverse proxy (host-agnostic VPS; CS408 ran on EC2, retired post-class) |

---

## Commands

```bash
go run .                # start the app on :3000 (PORT env to override)
go build -o libreshelf .
go test ./...           # full suite (35 passing on cp5-crud)
go test -v -run TestX   # run a specific test
sqlite3 data/database.sqlite  # inspect the local DB
```

Deploy guide: `docs/deployment.md` (stale, EC2-specific recipe from CS408; host-agnostic rewrite pending). `deploy/libreshelf.service` is the systemd unit and remains the canonical reference for service shape.

---

## Dev Environments

- **Laptop:** Ubuntu 24.04
- **Desktop:** Ubuntu 24.04

---

## Infrastructure

- systemd service unit at `deploy/libreshelf.service`
- Reverse proxy: nginx
- Secrets: environment variables (PORT, DATA_DIR, DB_NAME, ADMIN_PASSWORD)
- Database file: `data/database.sqlite` (gitignored)
- HTTPS: expected via the reverse proxy when behind a domain; HSTS gated on `APP_ENV=production`
- Host: VPS-agnostic (CS408 ran on EC2, retired post-class; future deploy could be a DigitalOcean droplet, Hetzner, etc.). No live deployment as of 2026-05-23

---

## Current State

All CS408 checkpoints complete. CP1-CP4 shipped over weeks 3-5. CP5 closed 2026-04-18 (6 days early). CP6 closed 2026-04-25 (PR #42, 169 tests). CP7 closed 2026-05-01 across PRs #74 / #75 / #76; the CS408 EC2 deploy went live the same day and was retired post-class. No live deployment exists today; new work targets a host-agnostic redeploy when a new VPS is chosen. Test coverage 74.9% on `libreshelf`, 87.5% on `internal/safezip`, 75.2% overall after the al3 follow-up landed 2026-05-12 (header-gated handler DB-fault scaffolding in `setupTestRouter`).

For per-checkpoint detail:
- `bd memories cp5-architecture` -- staff / book / patron CRUD, OL integration, cover validation
- `bd memories cp6-architecture` -- loans + dashboard + kiosk + role-differentiated routing
- `DECISIONS.md` -- DEC-001 through DEC-037 (DEC-027: backup design; DEC-028: security hardening; DEC-029: admin tools-index pattern; DEC-030: CSV patron import; DEC-031: SQLite busy_timeout + provable TOCTOU safety; DEC-032: Open Library metadata enrichment chain -- no Wikipedia; DEC-033/034: offline-mode predicate + env-var-as-lock precedence flip; DEC-035: Google Books fallback + field-level enrichment over Open Library; DEC-036: idempotent additive ALTER TABLE migrations in createSchema; DEC-037: per-physical-copy inventory + schema reshape via local wipe)
- `git log` -- implementation history

---

## Standards

- Handle all errors explicitly in Go -- never ignore returned errors
- Log errors server-side, return generic messages to clients
- Use environment variables for all secrets -- never hardcode
- Return correct HTTP status codes
- Validate and sanitize inputs server-side on every endpoint
- Defensive headers (X-Frame-Options DENY, CSP locked to local assets, X-Content-Type-Options nosniff) are applied router-wide via `SecurityHeaders` middleware. Rate limiting and CORS were de-scoped from CP7; if a future endpoint needs either, add per-route middleware.
- Always use parameterized queries (`?` placeholders) -- never string concatenation
- Commits should be descriptive and reference issue numbers where applicable
- Keep solutions lightweight -- consistent with the Absolute Code philosophy

---

## Gotchas

- **SQLite driver name.** `modernc.org/sqlite` registers as `"sqlite"`, not `"sqlite3"`. Don't copy-paste snippets from `mattn/go-sqlite3` docs (DEC-002).
- **Seed passwords are fresh-install-only.** `SeedDefaultUsers` skips users that already exist. Bumping a seed value does NOT update existing rows; `rm data/database.sqlite*` to re-seed locally.
- **Test router uses the production middleware chain** (fixed in #35). `setupTestRouter` returns `(router, dm)` and mirrors `main.go` route groups exactly. Use `loginAs(t, dm, username, role)` to get a session cookie + CSRF token, then `req.AddCookie(sess)` and set `csrf_token` on POSTs. `logoutHelper` exists for the logout path.
- **Schema changes: additive only, no framework.** `createSchema` uses `CREATE TABLE IF NOT EXISTS` plus an idempotent `ALTER TABLE ... ADD COLUMN` block (DEC-036). Adding a new column: put it in both the `CREATE TABLE` body and an `ALTER TABLE` line that ignores the "duplicate column" error. Works on fresh and existing DBs without a wipe. Non-additive changes (rename, drop, NOT NULL with no default, type change) are NOT supported by the additive pattern. While no live deployment exists (true as of 2026-05-23), the path for a non-additive change is to update the `CREATE TABLE` strings to the new shape and `rm data/database.sqlite*` on each dev machine before running the new code (DEC-037 codifies this for the CP8 inventory refactor). If LibreShelf gets redeployed and accrues data, the next non-additive change will need a one-off migration function designed at that time.
- **Go has no hot-reload.** Template edits take effect only after a process restart (templates are parsed once at startup via `template.Must` in `main.go`). Go source edits take effect only after re-running `go run .`. Symptom of forgetting: the browser sees the old behavior with no errors. If something "didn't do anything," restart the server first.
- **Static assets cache aggressively in the browser.** `router.Static` serves `static/javascripts/app.js` and `static/stylesheets/style.css` without cache-busting query strings or asset fingerprinting, so after a JS/CSS edit the browser may still hold the prior version. Symptom: `typeof initWhateverIJustAdded` is `"undefined"` in the console even though the file on disk is current. Fix during dev: hard refresh (Ctrl+Shift+R) or keep DevTools open with "Disable cache" checked in the Network tab. Proper fix (a `?v=<build-time>` query or content-hash URL) was de-scoped from CP7; it lives in the deferred backlog.

---

## Key References

- [Technical plan and architecture](./docs/plan.md) -- single source of truth for design
- [Product specification](./docs/product-spec/libreshelf-product-specification.pdf)
- [UI wireframes](./docs/product-spec/wireframes/)
- [Security plan](./docs/security.md)
- [Deployment guide](./docs/deployment.md)
- [Design decisions log](./DECISIONS.md)

---

## Open Issues / Current Focus

CP1-CP7 closed; CS408 submission complete 2026-05-01. CP7 EC2 deploy retired post-class. See `bd memories cp5-architecture` / `cp6-architecture` / `inventory-copies-cp8-chain` and `git log` for retrospective detail.

### CP8 -- Per-copy inventory + multi-format barcodes + label printing (DEC-037, closed 2026-07-25)

All six issues shipped across PRs #89 / #90 / #91 / #92 / #94 / #95 (see
`docs/specs/2026-05-23-inventory-copies-design.md` for full design):

- [x] `cs408-go-stack-e9a` (P1) -- Foundation. Shipped via PR #89 (merged 2026-05-23). Schema reshape (copies table, books.dewey, loans uses copy_id, no quantity columns), LSF barcode generator + tests, `AddLibraryCopy` / `GetCopyByBarcode` / `GetCopiesByBookID`, `HandleAddCopy` (POST /books/:id/copies), book-detail Check Out barcode prompt, `SeedBooks` gated behind `LIBRESHELF_SEED_DEV_BOOKS`. Security review clean.
- [x] `cs408-go-stack-zbi` (P2) -- Manage Copies page + status editing. Shipped via PR #90 (merged 2026-05-24). `/books/:id/copies` + `/inventory` pages, POST status / delete endpoints, `GetCopyByID` / `UpdateCopyStatus` / `DeleteCopy` / `GetCopiesByBookIDWithLoanInfo` / `GetAllCopiesWithFilters`, Bootstrap dropdown per row, sidebar Inventory section, new `dict` template helper. Spec said /inventory admin-only; relaxed to staff (matches Add Copy / Edit Book pattern). Security review clean. `needs_relabel` UI is forward-compatible scaffolding for issue 4 (l9m) which owns the set/clear paths.
- [x] `cs408-go-stack-stb` (P2) -- Multi-format copy entry + bulk add. Shipped via PR #91 (merged 2026-05-24). Add Copy modal on Manage Copies with source toggle (library / scan), per-format barcode validators (EAN-13 / UPC-A check digits, Code 128 / Code 39 charsets), `AddLibraryCopies(book, count)` capped at 50, `CreateCopyWithBarcode` mapping UNIQUE violation to `ErrBarcodeAlreadyExists`. Book detail `+ Add Copy` button removed in favor of the modal-only path through Manage Copies (no state mutation on one click). app.js include added to Manage Copies + Inventory templates. Security review clean.
- [x] `cs408-go-stack-l9m` (P2) -- Print Labels + boombuler/barcode + 4 presets (sheet + roll) + calibration. Shipped via PR #94 (merged 2026-05-29). `label_settings` single-row table, `GetLabelDataByCopyIDs` / `MarkCopiesRelabeled`, `RenderBarcodeSVG`, print picker + render pages, `print.css`, and the `needs_relabel` set/clear paths that zbi scaffolded. The `template.HTML` cast on the barcode SVG (`handlers_labels.go:133`) is safe because `svgFromBarcode` builds from numeric coordinates and fixed markup only, never user-controlled strings; preserve that invariant if it changes. Also carried `cs408-go-stack-q14` / DEC-038 (soft-fail CSRF on logout).
- [x] `cs408-go-stack-8vi` (P3) -- Dewey enrichment via OL chain. Shipped via PR #95 (merged 2026-07-25). OL `dewey_decimal_class` parse (first non-empty trimmed entry) into `BookPrefill.Dewey`, prefill JS via `.value`, manual override on the book form always wins. Dewey is OL-only: GB never supplies it, enforced explicitly in `gbOnly` (`merge.go`) rather than relying on `normalizeGoogleBook` leaving the field zero. Security review clean. Follow-ups filed: `340` (skips `normalizeFreeText`), `mux` (no length cap), `62y` (no polymorphic-shape unmarshaler), `a5x` (no e2e lookup-JSON test), `gxx` (CI double-run from the `branches:` filter this PR dropped).
- [x] `cs408-go-stack-1v5` (P2) -- Rapid-scan portal rebuild. Shipped via PR #92 (merged 2026-05-26). `/checkout` and `/checkin` staff pages with patron-selected-once + barcode-rapid-entry flow, `GetActiveLoanByBarcode` replacing the prior ISBN-based lookup, session-table UX (recent scans, undo), `initCheckoutPortal` / `initCheckinPortal` in app.js. 19 new tests covering unknown barcode, checkin path, cross-patron warn, undo-last-scan. Sidebar Check Out + Check In links from PR #88 verified. Closes paused `cs408-go-stack-yu3` (replaced rather than reused -- yu3's `feat/rapid-scan-portal` branch was wrong-model ISBN-based; 1v5 rebuilt from main on the copies infrastructure).

One-time wipe was required when the foundation landed: `rm data/database.sqlite*` on each dev machine. If your local DB still has the pre-DEC-037 schema (books with quantity columns, loans with book_id), wipe before running current code. No migration function ships because no live deployment exists.

### CP7 -- Admin Panel + Security Hardening + Deploy (closed 2026-05-01)

- [x] #23 -- Admin panel: ZIP export and import (DEC-027). Shipped via PR #74. `internal/safezip` package handles Zip Slip / symlink / absolute-path / size limits with two-pass validation. Export uses `VACUUM INTO`; import uses an in-process swap under a global `sync.RWMutex` with `.bak` rollback and live-session preservation.
- [x] #24 -- Testing, polish, and deploy. Shipped via PR #75 + follow-up `af31e3d`. `SecurityHeaders` middleware (X-Frame-Options DENY, CSP locked to local assets, X-Content-Type-Options nosniff, Referrer-Policy same-origin, HSTS gated on APP_ENV=production), `SetTrustedProxies([]string{"127.0.0.1"})`, Go 1.25.0 -> 1.25.9 toolchain bump (cleared 19 stdlib CVEs flagged by `govulncheck`), nginx `client_max_body_size 100M` for backup imports, deployed to the CS408 EC2 instance and verified end-to-end (instance retired post-class).
- [x] #62 (cs408-go-stack-al3) -- Test coverage push. Shipped via PR #76 (initial 67.6%) and follow-up 2026-05-12 reaching 75.2% overall. Header-gated handler DB-fault middleware added to `setupTestRouter`: requests with `X-Test-Break-Handler-DB: 1` get a closed `*DatabaseManager` injected after auth/CSRF/DBReadLock, so the handler's first DB call hits its `err != nil` branch while middleware sees a healthy DB. Mandatory items shipped: Zip Slip rejection test, `httptest.NewServer` for OL paths, `httptest.NewServer` for cover-URL paths. No 0% in `handlers_auth.go`, `validators.go`, `openlibrary.go`, `covers.go`.

### Deferred post-submission backlog

Quick index. Run `bd list --status=open` and `bd show <id>` for full design notes per item.

Cut from earlier CPs:
- CSV patron import (cs408-go-stack-8ap, from #21)
- Patron reset-password modal (cs408-go-stack-m39, from #21)
- Patron metadata column UI (cs408-go-stack-6gp, from #21)
- Patron activate / deactivate (cs408-go-stack-epe, from #21)
- Staff/Patron/Book table responsive polish (cs408-go-stack-9vv, from #39)
- Password-reset Variant 2 server-generated temp + force-change (cs408-go-stack-rn0, from #39)
- Orphan cover cleanup on post-cover-save validation failure (cs408-go-stack-owj, from #20)
- Offline detection for OL Lookup button (cs408-go-stack-zcs, from #20)

Cut from CP6 v2 trim or raised post-CP6:
- SSE live availability (cs408-go-stack-198, from #22)
- Patron holds on checked-out books (cs408-go-stack-wdf, from #22)
- Favorites (cs408-go-stack-4bn, from #22)
- Loan history view (cs408-go-stack-eig, post-CP6)
- Server-side catalog pagination Path 2 (cs408-go-stack-3d0, #37)
- Checkout/checkin rapid-scan portal (cs408-go-stack-yu3)
- Sidebar restructure by domain not access tier (cs408-go-stack-650)
- Fuller dashboard redesign with mini-lists (cs408-go-stack-ak6, also covers patron richer view)
- Overdue notice print system (cs408-go-stack-wdy)
- Patron address field for printed notices (cs408-go-stack-ygu)

OL metadata enrichment chain (post-DEC-032; mostly obviated by the 2026-05-13 OL-only chain that recovers 100/100 seed covers and jacket descriptions for every book OL catalogs):
- Google Books API as fallback for books OL doesn't catalog at all (cs408-go-stack-8gj) -- still useful, OL has gaps for new releases.
- OL OLID/LCCN/OCLC cover endpoint exhaustion (cs408-go-stack-069) -- partially obviated by the ISBN-cover probe; small marginal value left.
- Internet Archive cover fallback (cs408-go-stack-l2h) -- IA and OL share infrastructure; uncertain marginal value.
- Placeholder cover generator (cs408-go-stack-fcb) -- last-resort SVG when no upstream has anything.
- Bulk cover upload + needs-cover report (cs408-go-stack-iqr) -- operations tool for fixing the long tail manually.
- Thin-description report + bulk description edit UI (cs408-go-stack-uf3) -- companion to bulk cover for descriptions.

Restricted-network deployments:
- Patron self-registration toggle, admin-controlled (cs408-go-stack-o1x, #61)
- Offline cover sync via flash drive, 3-machine workflow (cs408-go-stack-0eh, #63)

Other:
- Automate deployment via GitHub Actions (#17)


<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for DURABLE task tracking. TodoWrite is allowed only as an in-session scratchpad; promote anything spanning sessions to a `bd` issue. See the Persistence and Memory section above for the full rule.
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge. Do NOT use MEMORY.md files or the auto-memory directory.

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Run security review** (if code changed and not test/docs-only) - Invoke the `security-review` skill on the branch diff before closing any beads issue that touched handlers, DB methods, middleware, auth/session logic, templates rendering user data, or anything related to credentials/permissions. Address findings before closing. Skip for docs-only, beads-only, or test-only changes.
4. **Update issue status** - Close finished work, update in-progress items
5. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
