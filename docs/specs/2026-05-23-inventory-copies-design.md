# Per-Physical-Copy Inventory with Multi-Format Barcodes (CP8 scope)

Status: Design approved 2026-05-23. Implementation sliced into 6 bd issues (see Slicing section).
Owner: Tim Palacios
Related bd issues:
- cs408-go-stack-e9a (1, Foundation) -- schema + migration + library-format add
- cs408-go-stack-stb (2) -- multi-format copy entry + bulk add
- cs408-go-stack-zbi (3) -- Manage Copies + status editing
- cs408-go-stack-l9m (4) -- Print Labels + boombuler/barcode + Avery 5160
- cs408-go-stack-8vi (5) -- Dewey enrichment chain extension
- cs408-go-stack-1v5 (6) -- rapid-scan portal rebuild (closes cs408-go-stack-yu3)
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
in `createSchema`. The decision body explicitly says: "When a
non-additive change is needed -- column rename, NOT NULL with no
default, table drop -- we revisit then." This refactor is the revisit.
DEC-037 (this spec) documents the non-additive migration approach.

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
- Existing data on the EC2 deployment migrates without manual cleanup:
  every current `quantity_available` slot becomes a copy row with an
  auto-generated LSF barcode and `needs_relabel=true` so the librarian
  can pull a re-label report later.

## Non-goals

- No real migration framework. The non-additive migration is a one-off
  function gated on `copies`-table-absent, called from `createSchema`
  before normal table setup. We are not adopting goose, dbmate, etc.
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

### Migration of `loans`

```sql
ALTER TABLE loans ADD COLUMN copy_id INTEGER REFERENCES copies(id);
-- backfill copy_id from each loan's current book_id
-- drop book_id column (SQLite 3.35+ supports DROP COLUMN)
ALTER TABLE loans DROP COLUMN book_id;
```

Backfill detail: for each loan, find the lowest-id copy of the loan's
`book_id` and link. Since migration auto-creates exactly
`quantity_total` copies per book and there is at most one active loan
per quantity slot, this assignment is deterministic. Returned loans
also link cleanly; the copy may have been returned and re-loaned since,
but the historical row links to the original physical copy.

### Drop on `books`

```sql
ALTER TABLE books DROP COLUMN quantity_total;
ALTER TABLE books DROP COLUMN quantity_available;
```

`quantity_available` becomes a derived count: `SELECT COUNT(*) FROM
copies WHERE book_id = ? AND status = 'available' AND id NOT IN
(SELECT copy_id FROM loans WHERE returned_at IS NULL)`. `quantity_total`
becomes `SELECT COUNT(*) FROM copies WHERE book_id = ? AND status !=
'withdrawn'`. We do not cache these; the per-book display query takes
the join hit.

### Migration mechanics

A new function `migrateToCopies(db *sql.DB) error` lives in `db.go` and
is called from `createSchema` BEFORE the normal `CREATE TABLE IF NOT
EXISTS` block. The function:

1. Checks if `copies` table exists via `sqlite_master`. If yes, returns
   nil (migration already ran).
2. Opens a transaction.
3. Runs the schema changes above.
4. For each book row, generates `quantity_total` LSF barcodes (each
   `LSF` + 7-digit sequence + Luhn check digit, sequence is monotonic
   across the whole migration), inserts copy rows with
   `needs_relabel=1`.
5. Backfills `loans.copy_id`.
6. Drops the old columns.
7. Commits.

If any step fails, the transaction rolls back and the app fails to
start. We do not partial-apply this migration; the surface is small
enough to wrap in one transaction.

The migration runs exactly once per database (gate on table existence).
Subsequent app restarts skip the function and proceed to normal
`CREATE TABLE IF NOT EXISTS copies` (no-op on existing tables) plus
the additive DEC-036 block. This means the migration code lives in the
binary forever as a one-shot; that is acceptable given the
single-deployment shape of LibreShelf.

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

Each label has three lines (top to bottom):

1. Barcode SVG (server-rendered via `boombuler/barcode`), sized to fit
   the label cell width.
2. Author prefix: first 3 characters of the first author's last name,
   uppercased. "Jane Austen" -> `AUS`; "F. Scott Fitzgerald" -> `FIT`;
   "Ursula K. Le Guin" -> `LEG` (we treat "Le Guin" as one token by
   taking the last whitespace-separated token, so "Le Guin" -> "Guin"
   -> "GUI"; this is imperfect for compound surnames and is fine).
   No author -> empty.
3. Dewey number, displayed verbatim from `books.dewey`. Empty if not
   set.

Default sheet: Avery 5160 (30 labels per sheet, 1" x 2 5/8"). The
print page accepts a layout query parameter for future sheet support
(`5163`, `5167`, etc.) but only `5160` ships in CP8.

Print CSS: `@media print` block in `static/stylesheets/style.css` (or
a new `print.css` linked only on the print page). Hides sidebar and
non-printable UI; positions labels in a CSS Grid matching the Avery
template; sets `page-break-inside: avoid` on each label cell.

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
- Sheet layout selector (defaults to 5160; only 5160 ships in CP8).
- "Generate" button renders an HTML page with all selected labels
  laid out for printing. User triggers browser Print (Ctrl+P) to
  send to a label printer.
- When the librarian prints a label for a copy with `needs_relabel=1`,
  the page POSTs back to clear the flag once printing is confirmed
  (a "Mark as relabeled" button below the print preview).

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

### Migration tests (foundation issue)

- Fresh DB: `migrateToCopies` runs, creates table, no books exist,
  zero copies created. Subsequent restart: gate skips migration.
- Seeded DB: each seeded book gets `quantity_total` copies with
  `needs_relabel=1`. LSF codes are unique and monotonic. All
  existing loans link to a copy of the right `book_id`.
- DB with returned loans: returned loans link to a copy. Active
  loans link to an available-marked copy of the same book.
- Idempotency: running the migration function twice (forced) returns
  early on the second call without error.

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

1. **Foundation** (`cs408-go-stack-e9a`, P1). Schema, `migrateToCopies`, copies table,
   `books.dewey` column, LSF barcode generator, single-copy
   library-format add on book detail, book-detail Check Out barcode
   prompt, migration tests, schema tests. Blocks everything else.
2. **Multi-format copy entry + bulk add** (`cs408-go-stack-stb`, P2). Format selector in
   add-a-copy modal (Code 128 / Code 39 / EAN-13 / UPC-A) with
   per-format validation, "Add N copies" bulk variant for library
   format. Depends on (1).
3. **Manage Copies UI + status editing** (`cs408-go-stack-zbi`, P2). `/books/:id/copies`
   page, mark lost / damaged / withdrawn actions, needs-relabel
   filter, sidebar Inventory section. Depends on (1).
4. **Label printing** (`cs408-go-stack-l9m`, P2). `boombuler/barcode` dependency, Avery
   5160 layout, `/admin/inventory/print-labels` page, label content
   (barcode SVG + author prefix + Dewey), `@media print` CSS,
   re-print single label from Manage Copies, clear `needs_relabel`
   on confirmed reprint. Depends on (1); preferred after (3) so the
   Manage Copies re-print link has a destination.
5. **Dewey enrichment chain** (`cs408-go-stack-8vi`, P3). Extend OL enrichment to fetch
   `dewey_decimal_class`, manual override in book edit form. Can
   land in parallel with (2), (3), (4). Depends on (1) for the
   column.
6. **Rebuild rapid-scan portal on copies** (`cs408-go-stack-1v5`, P2). Reclaim and close
   cs408-go-stack-yu3. Swap `GetActiveLoanByISBN` for
   `GetActiveLoanByBarcode`, rebuild the portal from main with the
   copies-model backing. Depends on (1) and (2). The existing
   `feat/rapid-scan-portal` branch is reference-only; do not
   salvage.

## Open questions deferred

- LSF sequence allocation: dedicated `sequences` table or
  `MAX(barcode) WHERE barcode LIKE 'LSF%'` query? Foundation issue
  decides. The simpler `MAX` query is fine unless we hit a
  concurrent-add race; with library volumes that race is
  theoretical.
- Print sheet sizes beyond 5160 (5163, 5167, etc.). Backlog.
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
