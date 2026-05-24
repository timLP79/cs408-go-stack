# Per-Physical-Copy Inventory with Multi-Format Barcodes (CP8 scope)

Status: Design approved 2026-05-23. Implementation sliced into 6 bd issues (see Slicing section). 2 of 6 shipped as of 2026-05-24.
Owner: Tim Palacios
Related bd issues:
- [x] cs408-go-stack-e9a (1, Foundation) -- shipped via PR #89, merged 2026-05-23
- [ ] cs408-go-stack-stb (2) -- multi-format copy entry + bulk add (ready)
- [x] cs408-go-stack-zbi (3) -- Manage Copies + status editing -- shipped via PR #90, merged 2026-05-24
- [ ] cs408-go-stack-l9m (4) -- Print Labels + boombuler/barcode + Avery 5160 (ready)
- [ ] cs408-go-stack-8vi (5) -- Dewey enrichment chain extension (ready)
- [ ] cs408-go-stack-1v5 (6) -- rapid-scan portal rebuild (blocked on 2; closes cs408-go-stack-yu3)
- cs408-go-stack-yu3 -- paused rapid-scan portal, unblocked by this chain
Supersedes (partially): the prior LSF-only barcode sketch captured in bd memory `inventory-copies-barcodes-design`. This spec expands the format set and the label content per the 2026-05-23 conversation.

## Context

Today the `books` table is catalog-level: one row per ISBN with
`quantity_total` and `quantity_available` as counters, and `loans`
references `book_id`. That model works for a small library but is
structurally wrong for circulation: a real library checks books out by
the barcode on the physical item, not by the ISBN of the title. Each
copy needs its own identity so that lost / damaged / withdrawn states
can be tracked per item, and so that a checkout transaction names the
specific copy that left the shelf.

The gap surfaced 2026-05-22 during the rapid-scan portal work
(cs408-go-stack-yu3). That branch is paused on the wrong-model
implementation; this refactor is the prerequisite, and yu3 gets rebuilt
on top of the new model at the end of the chain.

DEC-036 (2026-05-16) covers idempotent additive `ALTER TABLE` migrations
in `createSchema` and was originally driven by the CS408 EC2 deployment's
production data. That deployment was retired post-class; LibreShelf has
no live deployment as of 2026-05-23. DEC-037 (this spec) takes advantage
of the no-deployment-today state and ships the reshape via a local DB
wipe rather than a migration function. Any future deployment will need
a real migration for the next non-additive change; that design lives
in DEC-037's "future deployment" note.

## Goals

- Each physical book becomes a row in a new `copies` table with its own
  unique barcode. A patron checkout names the specific copy, not the
  title.
- Staff can choose at add-a-copy time whether to (a) scan an existing
  publisher barcode already printed on the book (EAN-13 / UPC-A) and
  store it as-is, or (b) generate a library-format barcode (LSF prefix
  + 7-digit sequence + Luhn check digit) and print a label.
- Printable labels follow the standard library spine-label pattern:
  barcode image + first 3 chars of the first author's last name +
  Dewey number, laid out for a configurable Avery sheet (default 5160,
  30 labels per sheet).
- The legacy book-detail Check Out form keeps working but now requires
  the staffer to scan / type the barcode of the specific copy leaving
  the shelf (no auto-pick).
- No live deployment exists today (CS408 EC2 retired post-class), so
  the schema reshape ships via local DB wipe + re-seed, not a
  migration function. The `needs_relabel` column is still added to
  `copies` for future use: it carries the "auto-generated, please
  re-label" flag for any code path that bulk-creates copies (e.g. a
  future "import existing inventory" flow), even though the foundation
  issue does not have such a path.

## Non-goals

- No migration function. No live deployment exists; the reshape ships
  via local DB wipe + re-seed. A future deployment that needs to
  preserve data across a non-additive change will need a migration
  function designed then, per the DEC-037 "future deployment" note.
  We are not adopting goose, dbmate, or any framework.
- No automatic format detection from a scanner emit. The staffer picks
  the format at add-a-copy time; the system validates the digits match
  the chosen format (length + checksum). A future enhancement could
  auto-classify on scan, but it is out of scope here.
- No GS1-128 / ITF-14 / UPC-E / EAN-8 support. The screenshot listed
  them; in a small-library context they add complexity without
  matching real-world inventory. The supported set is: Code 128,
  Code 39, EAN-13, UPC-A.
- No barcode validation against external registries (UPC / EAN
  databases). We validate format and checksum, not registry
  membership.
- No per-copy variation of Dewey / classification. Dewey lives on
  `books`, not `copies`. If a copy belongs to a different physical
  shelf section (e.g. reference vs circulating), that is a `status` or
  `location` concern, not a Dewey concern. Out of scope for now.

## Schema changes

### New table `copies`

```sql
CREATE TABLE copies (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id         INTEGER NOT NULL REFERENCES books(id),
    barcode         TEXT NOT NULL UNIQUE,
    barcode_format  TEXT NOT NULL DEFAULT 'code128',
    status          TEXT NOT NULL DEFAULT 'available',
    needs_relabel   INTEGER NOT NULL DEFAULT 0,
    acquired_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_copies_book_id ON copies(book_id);
CREATE INDEX idx_copies_status ON copies(status);
```

- `barcode_format` values: `code128`, `code39`, `ean13`, `upca`. Stored
  literal so we know how to render the SVG on a re-print and how to
  validate on edit.
- `status` values: `available`, `lost`, `damaged`, `withdrawn`. A copy
  with `status != 'available'` cannot be checked out even if it has no
  active loan. Status transitions are admin-only.
- `needs_relabel` is a boolean flag set on auto-generated copies during
  migration and cleared when the librarian re-prints a label for that
  copy via the Print Labels page.

### New column on `books`

```sql
ALTER TABLE books ADD COLUMN dewey TEXT;
```

- Populated by the OL/GB enrichment chain (extends DEC-032 / DEC-035)
  with manual override in the book edit form. OL has a `dewey_decimal`
  field on some edition records; GB has a partial fallback via the
  industry identifiers / classification surface.
- Plain TEXT, no validation. Some libraries use Dewey numbers like
  `813.54`, others use full call numbers like `813.54 FIT`. We store
  whatever the librarian enters and treat the author-prefix on the
  label as a separate computed field.

### `loans` shape change

The new `loans` table is created directly in the new shape (no `book_id`
column, `copy_id INTEGER NOT NULL REFERENCES copies(id)`). On a fresh
DB this is a one-shot CREATE; there is no rename / rebuild dance
because there is no pre-existing data to preserve.

```sql
CREATE TABLE loans (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    copy_id         INTEGER NOT NULL REFERENCES copies(id),
    patron_id       INTEGER NOT NULL REFERENCES patrons(id),
    checked_out_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    due_date        TEXT NOT NULL,
    returned_at     DATETIME
);
```

### `books` shape change

The CREATE TABLE statement for `books` ships without `quantity_total`
and `quantity_available`, and includes the new `dewey` column. Existing
counter logic is removed from `CheckoutBook`, `ReturnBook`, `CreateBook`,
`UpdateBook`, `GetBookByID`, `GetAllBooks`, etc.; those handlers move
to deriving availability from `copies` instead.

`quantity_available` becomes a derived count:
`SELECT COUNT(*) FROM copies WHERE book_id = ? AND status = 'available' AND id NOT IN (SELECT copy_id FROM loans WHERE returned_at IS NULL)`.
`quantity_total` becomes
`SELECT COUNT(*) FROM copies WHERE book_id = ? AND status != 'withdrawn'`.
We do not cache these; the per-book display query takes the join hit.

### Schema-reshape mechanics

No migration function. The foundation issue ships:

1. Updated `CREATE TABLE` statements in `createSchema` for the new
   `books`, `loans`, and `copies` shapes (plus indexes).
2. An additive `ALTER TABLE books ADD COLUMN dewey TEXT` line in the
   DEC-036 idempotent block so that re-seeding a fresh DB always
   has the column even if the CREATE TABLE somehow ran an older
   string (defense in depth; on a wiped DB the CREATE TABLE always
   wins, the ALTER fails with "duplicate column" and is swallowed).
3. A README / CLAUDE.md gotcha note: `rm data/database.sqlite*`
   before running the new code on a machine that has the pre-DEC-037
   schema. After the wipe, `SeedBooks` re-populates the seed catalog
   in the new shape.

There is no `migrateToCopies` function. If LibreShelf gains a live
deployment in the future and the next non-additive change needs to
preserve data, that migration is designed then, per the DEC-037
"future deployment" note.

## Library barcode format (LSF)

Library-format barcodes are: `LSF` + 7-digit zero-padded sequence + 1
Luhn check digit, total 11 characters. Examples: `LSF00000017`,
`LSF12345678`.

- The 7-digit sequence comes from a new `library_barcode_seq` row in a
  small `sequences` table (or a simple `MAX(sequence)+1` query on
  `copies` rows with `barcode_format='code128'` whose barcode matches
  the LSF prefix). Implementation TBD in the foundation issue.
- Luhn check digit follows the standard credit-card algorithm,
  computed on the 10 digits of the sequence after stripping the LSF
  prefix.
- Rendered as Code 128 by the print pipeline. Code 128 accepts the
  full ASCII set including letters, so the LSF prefix encodes cleanly.

## Label content + layout

Conceptual model: a label is a **combined spine label**. It goes on
the book's spine (or, for thin paperbacks, on the lower front cover
near the spine) and serves both shelving and circulation. The layout
visually prioritizes shelving information at the top so a librarian
scanning a shelf can find the book quickly; the barcode sits at the
bottom for the scanner.

Single label content, top to bottom:

1. Dewey number, displayed verbatim from `books.dewey`, large and
   bold. Empty if not set (rendered as blank space, keeps label
   geometry consistent).
2. Author prefix: first 3 characters of the first author's last name,
   uppercased, slightly smaller than Dewey. "Jane Austen" -> `AUS`;
   "F. Scott Fitzgerald" -> `FIT`; "Ursula K. Le Guin" -> `LEG` (we
   treat "Le Guin" as one token by taking the last whitespace-
   separated token, so "Le Guin" -> "Guin" -> "GUI"; this is
   imperfect for compound surnames and is fine).
   No author -> empty line.
3. Barcode SVG (server-rendered via `boombuler/barcode`) in the
   copy's stored `barcode_format` (Code 128 / Code 39 / EAN-13 /
   UPC-A). Sized to fit the label cell width with a small margin.
4. Human-readable copy identifier under the barcode (the barcode
   string, e.g. `LSF00000178`), small monospace, so a librarian can
   read it without a scanner.

### Sheet presets (4 ship in CP8)

| Preset | Paper | Layout | Label size | Use case |
|---|---|---|---|---|
| `avery-5160` (default) | US letter | 30 / sheet, 3 cols x 10 rows | 1" x 2 5/8" | Spines of hardbacks and trade paperbacks |
| `avery-5161` | US letter | 20 / sheet, 2 cols x 10 rows | 1" x 4" | Thicker books and AV media; more room for long Dewey numbers |
| `avery-l7160` | A4 | 21 / sheet, 3 cols x 7 rows | 1" x 2.625" | International parity with 5160 |
| `roll-1x2.5` | Continuous roll | 1 / page | 1" x 2.5" | Brother QL / Dymo / Zebra thermal label printers |

The smallest spine-only sizes (e.g. Avery 5167 at 0.5" x 1.75") are
intentionally NOT in the CP8 set: 0.5" of height is too cramped to
include a reliable barcode. If a library wants traditional text-only
spine labels in addition to combined labels, that is a backlog item
(separate "spine-only label mode").

### Per-deployment default + calibration

A small `label_settings` table holds one row keyed on `id=1`:

```sql
CREATE TABLE label_settings (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    preset          TEXT NOT NULL DEFAULT 'avery-5160',
    offset_top_mm   REAL NOT NULL DEFAULT 0.0,
    offset_left_mm  REAL NOT NULL DEFAULT 0.0
);
```

- `preset` is one of the four preset slugs above (validated app-side).
- `offset_top_mm` and `offset_left_mm` apply printer drift correction
  before the first label cell. Adjustable in the admin UI in
  millimeters, typical range -3 to +3 mm.
- A "Print test page" button on `/admin/inventory/label-settings`
  generates a calibration sheet with crosshair marks at known
  positions so the librarian can measure drift and dial in the
  offsets.

### Print CSS

A new `static/stylesheets/print.css` is linked only on the print
page. It contains:
- `@media print` block hiding sidebar, navbar, and all non-print UI.
- Per-preset CSS Grid layouts matching the physical label positions.
- `@page` rules for paper size (letter / A4) and zero browser
  margins (the preset defines its own).
- For the roll preset, `@page` size matches the label dimensions
  exactly, and the page contains a single label cell.
- `page-break-inside: avoid` on every label cell.
- Custom properties for the calibration offsets, injected inline by
  the handler so they apply at render time.

## UI surfaces

### Book detail page

- Top action bar gets a new "Manage Copies" link (staff/admin) next
  to Edit and Delete.
- The existing Check Out form (currently a patron picker + Submit)
  gains a new "Barcode" text input above the patron picker. Staff
  scans / types the barcode; the handler validates the barcode
  belongs to this book and is available. Mismatched barcodes flash an
  error and stay on the page.
- The availability card now shows "X of Y available" computed from
  copies, plus a small badge if any copies have `needs_relabel=1`.

### Manage Copies page (`/books/:id/copies`)

- Table of copies for this title: barcode, format, status, last loan
  date (or "in stock since"), needs-relabel flag, actions.
- Actions per row: mark lost / damaged / withdrawn (admin only), edit
  barcode (admin only, validates uniqueness + format), re-print this
  label (deep-links to the Print Labels page with this copy
  pre-selected), delete (admin only, only allowed if the copy has no
  loan history).
- "Add a copy" button at the top opens a modal with the source toggle
  described next.

### Add-a-copy modal

Source toggle (radio):
- **Scan existing** -- format dropdown (Code 128 / Code 39 / EAN-13 /
  UPC-A) + barcode text input. The input validates per format:
  EAN-13 / UPC-A check digit, Code 128 / Code 39 character set.
  Submitting stores the barcode as entered.
- **Generate library label** -- shows a preview of the next LSF code
  that would be allocated. Submitting allocates the code, creates the
  copy with `barcode_format='code128'` and `needs_relabel=0` (the
  librarian will print it).

Bulk variant: a checkbox "Add N identical copies" with a number
input. For "Scan existing", bulk is disabled (each copy needs its
own barcode); for "Generate library label", N LSF codes are allocated
and N copies are created in one transaction.

### Print Labels page (`/admin/inventory/print-labels`)

- Source filter (radio): all books, books with `needs_relabel=1`
  copies (the "needs relabel" report from the migration backlog),
  specific book (search by title or ISBN), specific barcode (for
  the per-copy re-print link from Manage Copies).
- Sheet preset selector defaults to the value in `label_settings`.
  Librarian can override per print run without changing the default.
  Sheet presets in CP8: `avery-5160`, `avery-5161`, `avery-l7160`,
  `roll-1x2.5`. The selector renders an inline preview of the
  preset's dimensions and label count.
- "Generate" button renders an HTML page with all selected labels
  laid out for printing using the chosen preset. User triggers
  browser Print (Ctrl+P) to send to a printer. For roll presets,
  the print job emits one label per page; for sheet presets, labels
  pack into a grid.
- When the librarian prints labels for copies with `needs_relabel=1`,
  the page POSTs back to clear the flag once printing is confirmed
  (a "Mark as relabeled" button below the print preview).

### Label settings + calibration page (`/admin/inventory/label-settings`)

Admin-only. Two surfaces:

- Default preset dropdown (one of the four CP8 presets). Save updates
  the `label_settings` row.
- Calibration controls: top offset (mm) and left offset (mm) number
  inputs, with a "Print test page" button that renders a
  calibration sheet for the currently-selected preset. The sheet
  has crosshair marks at each label cell's corners so the librarian
  can measure drift against the physical stock and adjust the
  offsets accordingly.

### Sidebar

A new "Inventory" section under "Circulation" (staff/admin only):
- Manage Copies (links to a top-level list -- if the librarian wants
  per-book it is reachable from book detail; the top-level page shows
  global filters like all needs-relabel copies)
- Print Labels

## Enrichment chain extension (Dewey)

The OL/GB enrichment chain (DEC-032 + DEC-035) already populates
title, authors, year, publisher, cover URL, and description. Adding
Dewey:

- OL: each OL edition response may carry `dewey_decimal_class`. If
  present and non-empty, take the first value (an array in some
  responses). Trim whitespace.
- GB: `volumeInfo.industryIdentifiers` rarely carries Dewey, but
  `volumeInfo.categories` and `volumeInfo.mainCategory` can be mapped
  to Dewey ranges heuristically. We skip the heuristic in CP8 and
  only consume an explicit Dewey from OL. If OL has no Dewey, the
  field stays empty and the librarian fills it in manually.
- mergePrefill takes the OL Dewey if non-empty; GB never overrides.
  No `DeweySource` label needed since the policy is OL-only.
- Manual override always wins. The book edit form gets a Dewey input.

## Test plan

### Seed + schema tests (foundation issue)

- Fresh DB on first run: `createSchema` produces the new shape;
  `SeedBooks` populates the catalog with zero copies (seed books
  have no copies until the librarian adds them via the
  add-a-copy flow).
- Re-running on an already-initialized DB: `CREATE TABLE IF NOT EXISTS`
  is a no-op; the additive `ALTER TABLE books ADD COLUMN dewey`
  swallows "duplicate column".
- The wipe-and-re-seed path (delete `data/database.sqlite*`, restart)
  produces the same end state as a brand-new install.

### Schema tests

- `copies` UNIQUE constraint on barcode rejects duplicates.
- `copies.status` constraint check (if we add CHECK) rejects invalid
  values. (TBD whether we use a CHECK constraint or rely on app-level
  validation.)
- `loans.copy_id` FK is enforced (delete a copy with a loan -> rejected).

### Handler tests

- Add-a-copy with library-format: LSF code allocated, copy created,
  flash success.
- Add-a-copy with scanned EAN-13: valid check digit accepted,
  invalid rejected with flash error.
- Add-a-copy with duplicate barcode (across any book): rejected with
  flash error.
- Manage Copies status change: copy marked lost is not eligible for
  checkout.
- Book detail Check Out with mismatched barcode: rejects with flash
  ("Barcode does not belong to this title").
- Book detail Check Out with unavailable barcode: rejects with flash
  ("Copy is checked out / lost / withdrawn").
- Print Labels page renders SVGs for all selected copies; the SVG
  output decodes correctly via a Go barcode decoder in tests (or we
  smoke-test by counting `<svg>` tags in the response).

### Security

- All handlers behind RequireStaff or RequireAdmin per UI surface.
- Barcode input is sanitized (length cap, ASCII-only) before storage.
- Status change actions verify CSRF token (per existing pattern).
- The Print Labels page does not expose any user-input that escapes
  into the SVG; the SVG is generated server-side from validated
  fields.

## Slicing into bd issues

Six issues, dependency ordering noted. The whole chain is roughly
CP8-sized; targeting "ship one issue per week" so the chain lands
across June 2026.

1. **Foundation** (`cs408-go-stack-e9a`, P1). **SHIPPED via PR #89 (merged 2026-05-23).**
   Schema reshape (new `books` / `loans` / `copies` shapes in
   `createSchema`; local wipe documented in CLAUDE.md), `books.dewey`
   column, LSF barcode generator, rewrite of every handler / DB
   method that previously read or wrote `books.quantity_*` or
   `loans.book_id` to use `copies` instead, single-copy library-format
   add on book detail, book-detail Check Out barcode prompt, seed +
   schema tests, handler tests for the add-a-copy and Check Out
   paths. Blocked everything else; now unblocks 2, 3, 5.
2. **Multi-format copy entry + bulk add** (`cs408-go-stack-stb`, P2). Format selector in
   add-a-copy modal (Code 128 / Code 39 / EAN-13 / UPC-A) with
   per-format validation, "Add N copies" bulk variant for library
   format. Depends on (1) -- ready.
3. **Manage Copies UI + status editing** (`cs408-go-stack-zbi`, P2). **SHIPPED via PR #90 (merged 2026-05-24).**
   `/books/:id/copies` per-book page + `/inventory` top-level page,
   mark lost / damaged / withdrawn actions via per-row Bootstrap
   dropdown, delete with loan-history guard, needs-relabel filter
   chip (forward-compatible; populated by (4) when that ships),
   sidebar Inventory section under Circulation. Spec said
   `/inventory` admin-only; relaxed to staff-actionable post-design
   review to match Add Copy / Edit Book pattern.
4. **Label printing** (`cs408-go-stack-l9m`, P2). `boombuler/barcode` dependency, Avery
   5160 layout, `/admin/inventory/print-labels` page, label content
   (barcode SVG + author prefix + Dewey), `@media print` CSS,
   re-print single label from Manage Copies, clear `needs_relabel`
   on confirmed reprint. Depends on (1); now ready since (3)
   provides the Manage Copies re-print link target.
5. **Dewey enrichment chain** (`cs408-go-stack-8vi`, P3). Extend OL enrichment to fetch
   `dewey_decimal_class`, manual override in book edit form. Can
   land in parallel with (2), (4). Depends on (1) for the column --
   ready.
6. **Rebuild rapid-scan portal on copies** (`cs408-go-stack-1v5`, P2). Reclaim and close
   cs408-go-stack-yu3. Swap `GetActiveLoanByISBN` for
   `GetActiveLoanByBarcode`, rebuild the portal from main with the
   copies-model backing. Depends on (1) and (2). The existing
   `feat/rapid-scan-portal` branch is reference-only; do not
   salvage. Blocked on (2).

## Open questions deferred

- LSF sequence allocation: ~~dedicated `sequences` table or
  `MAX(barcode) WHERE barcode LIKE 'LSF%'` query?~~ **Resolved by
  Foundation (PR #89):** the MAX-query approach inside the
  `AddLibraryCopy` transaction. Concurrent-add race is theoretical
  at library volumes, and a collision would fail the UNIQUE
  constraint and be retried by the caller.
- Print sheet sizes beyond the four CP8 presets (Avery 5163, 5366,
  Dymo 30252, etc.). Backlog; add presets per librarian request.
- Text-only spine labels (small stock like Avery 5167) as a separate
  print mode alongside combined labels. Backlog.
- Admin-defined custom label templates (enter dimensions in admin
  UI rather than pick from a fixed preset set). Backlog.
- Auto-classification of barcode format on scan (read raw scanner
  emit, detect length + checksum, propose format). Backlog.
- Per-copy location / shelf section. Backlog.
- Real Dewey heuristic from GB categories. Backlog.

## Related

- DEC-036 -- additive-only migration pattern that this refactor
  intentionally violates.
- DEC-037 (new, this spec) -- non-additive migration approach for
  the copies refactor.
- DEC-032, DEC-035 -- OL / GB enrichment chains that gain a Dewey
  field in issue 5.
- bd memory `inventory-copies-barcodes-design` -- prior single-format
  sketch from 2026-05-22. Superseded by this spec.
- bd issue `cs408-go-stack-yu3` -- paused rapid-scan portal that
  this chain unblocks (issue 6 closes it).
