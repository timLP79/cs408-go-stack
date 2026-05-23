// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mustCreateBook seeds a book and N library-format copies, then returns
// the book id. Pass copies=0 to create a catalog entry with no
// inventory (e.g. for tests that exercise empty-inventory paths).
func mustCreateBook(t *testing.T, dm *DatabaseManager, title string, copies int) int {
	t.Helper()
	book := &Book{Title: title}
	id, err := dm.CreateBook(book, []string{"Test Author"})
	if err != nil {
		t.Fatalf("CreateBook(%q): %v", title, err)
	}
	for i := 0; i < copies; i++ {
		if _, _, err := dm.AddLibraryCopy(id); err != nil {
			t.Fatalf("AddLibraryCopy(book %d, copy %d/%d): %v", id, i+1, copies, err)
		}
	}
	return id
}

// mustCreatePatron seeds a patron (and its linked user row via the
// transactional CreatePatron path). Returns the patron id.
func mustCreatePatron(t *testing.T, dm *DatabaseManager, name string) int {
	t.Helper()
	id, _, err := dm.CreatePatron(name, "", "", "", "fake-hash")
	if err != nil {
		t.Fatalf("CreatePatron(%q): %v", name, err)
	}
	return id
}

// firstAvailableCopyOf returns the lowest-id available copy of the
// given book (status='available' and not currently on loan). Fails the
// test if no eligible copy exists.
func firstAvailableCopyOf(t *testing.T, dm *DatabaseManager, bookID int) int {
	t.Helper()
	var copyID int
	err := dm.db.QueryRow(`
		SELECT c.id FROM copies c
		WHERE c.book_id = ? AND c.status = 'available'
		  AND c.id NOT IN (SELECT copy_id FROM loans WHERE returned_at IS NULL)
		ORDER BY c.id LIMIT 1`, bookID).Scan(&copyID)
	if err != nil {
		t.Fatalf("firstAvailableCopyOf(book %d): %v", bookID, err)
	}
	return copyID
}

// firstCopyOf returns the lowest-id copy of the given book without
// regard to status or loan state. Use for tests that want a copy id
// for a historical (returned) loan.
func firstCopyOf(t *testing.T, dm *DatabaseManager, bookID int) int {
	t.Helper()
	var copyID int
	err := dm.db.QueryRow(`SELECT id FROM copies WHERE book_id = ? ORDER BY id LIMIT 1`, bookID).Scan(&copyID)
	if err != nil {
		t.Fatalf("firstCopyOf(book %d): %v", bookID, err)
	}
	return copyID
}

// mustInsertLoan bypasses CheckoutBook to seed loans directly. Needed
// for tests that exercise guard conditions (overdue, at-limit) and
// filters (GetActiveLoans, GetOverdueLoans). bookID is resolved to a
// copy_id internally: for an active loan (returnedAt == ""), the
// lowest-id copy with no current active loan is picked; for a returned
// loan, copy id is the lowest-id copy of the book.
func mustInsertLoan(t *testing.T, dm *DatabaseManager, bookID, patronID int, dueDate, returnedAt string) int {
	t.Helper()
	var copyID int
	if returnedAt == "" {
		copyID = firstAvailableCopyOf(t, dm, bookID)
	} else {
		copyID = firstCopyOf(t, dm, bookID)
	}
	var (
		res sql.Result
		err error
	)
	if returnedAt == "" {
		res, err = dm.db.Exec(
			`INSERT INTO loans (copy_id, patron_id, due_date) VALUES (?, ?, ?)`,
			copyID, patronID, dueDate)
	} else {
		res, err = dm.db.Exec(
			`INSERT INTO loans (copy_id, patron_id, due_date, returned_at) VALUES (?, ?, ?, ?)`,
			copyID, patronID, dueDate, returnedAt)
	}
	if err != nil {
		t.Fatalf("insert loan: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return int(id)
}

// mustCreateBookWithAvailable seeds a book with `total` library-format
// copies, then marks (total - available) of them as 'lost' so the
// derived available_copies count equals `available`. This mirrors the
// pre-copies-refactor `Book{QuantityTotal: total, QuantityAvailable:
// available}` shape without needing the test to set up loan rows.
//
// Use this when the test cares about the availability count surface
// (out-of-stock filters, dashboard counters) but not about which
// specific patron holds which copy. For tests that need real loans,
// use mustInsertLoan instead.
func mustCreateBookWithAvailable(t *testing.T, dm *DatabaseManager, title string, total, available int) int {
	t.Helper()
	if available < 0 || available > total {
		t.Fatalf("mustCreateBookWithAvailable(%q): invalid total/available %d/%d", title, total, available)
	}
	id := mustCreateBook(t, dm, title, total)
	if available == total {
		return id
	}
	rows, err := dm.db.Query(`SELECT id FROM copies WHERE book_id = ? ORDER BY id LIMIT ?`,
		id, total-available)
	if err != nil {
		t.Fatalf("query copies for %q: %v", title, err)
	}
	var copyIDs []int
	for rows.Next() {
		var cid int
		if err := rows.Scan(&cid); err != nil {
			rows.Close()
			t.Fatalf("scan copy id: %v", err)
		}
		copyIDs = append(copyIDs, cid)
	}
	rows.Close()
	for _, cid := range copyIDs {
		if _, err := dm.db.Exec(`UPDATE copies SET status = 'lost' WHERE id = ?`, cid); err != nil {
			t.Fatalf("mark copy %d lost: %v", cid, err)
		}
	}
	return id
}

// mustCheckout calls CheckoutBook against the first available copy of
// the given book. Used by tests that want the happy path without
// caring about which specific copy is borrowed. Fails the test if
// CheckoutBook returns an error.
func mustCheckout(t *testing.T, dm *DatabaseManager, bookID, patronID int, dueDate time.Time) {
	t.Helper()
	copyID := firstAvailableCopyOf(t, dm, bookID)
	if err := dm.CheckoutBook(copyID, patronID, dueDate); err != nil {
		t.Fatalf("CheckoutBook(copy %d): %v", copyID, err)
	}
}

// tryCheckoutBook is the bookID-keyed shim around CheckoutBook for
// tests that want to inspect the returned error. Picks the lowest-id
// available copy and calls CheckoutBook(copyID, ...). When the book
// has no available copies at all, returns ErrNoCopiesAvailable
// without calling CheckoutBook -- preserving the pre-refactor semantic
// where "book is out of stock" was the bookID-level guard.
func tryCheckoutBook(t *testing.T, dm *DatabaseManager, bookID, patronID int, dueDate time.Time) error {
	t.Helper()
	var copyID int
	err := dm.db.QueryRow(`
		SELECT c.id FROM copies c
		WHERE c.book_id = ? AND c.status = 'available'
		  AND c.id NOT IN (SELECT copy_id FROM loans WHERE returned_at IS NULL)
		ORDER BY c.id LIMIT 1`, bookID).Scan(&copyID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoCopiesAvailable
	}
	if err != nil {
		t.Fatalf("look up copy for book %d: %v", bookID, err)
	}
	return dm.CheckoutBook(copyID, patronID, dueDate)
}

// firstAvailableBarcodeOf returns the barcode of the lowest-id
// available copy of the given book. Convenience helper for HTTP
// handler tests that need to populate the "barcode" form field on
// /books/:id/checkout.
func firstAvailableBarcodeOf(t *testing.T, dm *DatabaseManager, bookID int) string {
	t.Helper()
	var barcode string
	err := dm.db.QueryRow(`
		SELECT c.barcode FROM copies c
		WHERE c.book_id = ? AND c.status = 'available'
		  AND c.id NOT IN (SELECT copy_id FROM loans WHERE returned_at IS NULL)
		ORDER BY c.id LIMIT 1`, bookID).Scan(&barcode)
	if err != nil {
		t.Fatalf("firstAvailableBarcodeOf(book %d): %v", bookID, err)
	}
	return barcode
}

// availableCopiesOf returns the derived available-copies count for the
// given book. Replacement for the pre-refactor pattern that read the
// quantity_available column directly.
func availableCopiesOf(t *testing.T, dm *DatabaseManager, bookID int) int {
	t.Helper()
	var n int
	if err := dm.db.QueryRow(`
		SELECT COUNT(*) FROM copies c
		WHERE c.book_id = ? AND c.status = 'available'
		  AND c.id NOT IN (SELECT copy_id FROM loans WHERE returned_at IS NULL)`, bookID).Scan(&n); err != nil {
		t.Fatalf("availableCopiesOf(book %d): %v", bookID, err)
	}
	return n
}

// TestCheckoutBookHappyPath pins the baseline success path: a loan row
// is created against an available copy, and the derived available count
// drops by one.
func TestCheckoutBookHappyPath(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Test Book", 2)
	patronID := mustCreatePatron(t, dm, "Jane Doe")
	dueDate := time.Now().AddDate(0, 0, DefaultLoanTermDays)

	if err := tryCheckoutBook(t, dm, bookID, patronID, dueDate); err != nil {
		t.Fatalf("CheckoutBook: %v", err)
	}

	var count int
	if err := dm.db.QueryRow(`
		SELECT COUNT(*) FROM loans l
		JOIN copies c ON l.copy_id = c.id
		WHERE c.book_id = ? AND l.patron_id = ? AND l.returned_at IS NULL`,
		bookID, patronID).Scan(&count); err != nil {
		t.Fatalf("count loans: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 active loan, got %d", count)
	}

	if got := availableCopiesOf(t, dm, bookID); got != 1 {
		t.Errorf("expected 1 available copy, got %d", got)
	}
}

// TestCheckoutBookNoCopiesAvailable pins the "book is out of stock"
// guard. A book with zero copies (or all copies on loan / unavailable)
// must reject checkout with ErrNoCopiesAvailable.
func TestCheckoutBookNoCopiesAvailable(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Zero Copies", 0)
	patronID := mustCreatePatron(t, dm, "Alice")
	dueDate := time.Now().AddDate(0, 0, DefaultLoanTermDays)

	err := tryCheckoutBook(t, dm, bookID, patronID, dueDate)
	if err != ErrNoCopiesAvailable {
		t.Errorf("expected ErrNoCopiesAvailable, got %v", err)
	}
}

// TestCheckoutBookBlockedByOverdue pins the overdue guard. A patron with
// even one overdue book cannot check out anything new, regardless of
// book availability.
func TestCheckoutBookBlockedByOverdue(t *testing.T) {
	dm := setupTestDB(t)
	bookA := mustCreateBook(t, dm, "Overdue Book", 1)
	bookB := mustCreateBook(t, dm, "Wanted Book", 5)
	patronID := mustCreatePatron(t, dm, "Overdue Olivia")

	yesterday := time.Now().AddDate(0, 0, -1).UTC().Format("2006-01-02")
	mustInsertLoan(t, dm, bookA, patronID, yesterday, "")

	dueDate := time.Now().AddDate(0, 0, DefaultLoanTermDays)
	err := tryCheckoutBook(t, dm, bookB, patronID, dueDate)
	if err != ErrPatronHasOverdue {
		t.Errorf("expected ErrPatronHasOverdue, got %v", err)
	}
}

// TestCheckoutBookAtLoanLimit pins the max-active-loans guard at the
// exact threshold. MaxActiveLoansPerPatron active loans must cause the
// next checkout to fail with ErrPatronAtLoanLimit.
func TestCheckoutBookAtLoanLimit(t *testing.T) {
	dm := setupTestDB(t)
	patronID := mustCreatePatron(t, dm, "Maxed Max")
	nextWeek := time.Now().AddDate(0, 0, 7).UTC().Format("2006-01-02")

	for i := range MaxActiveLoansPerPatron {
		bookID := mustCreateBook(t, dm, "Loan Filler Book "+string(rune('A'+i)), 1)
		mustInsertLoan(t, dm, bookID, patronID, nextWeek, "")
	}

	oneMore := mustCreateBook(t, dm, "The Straw", 1)
	err := tryCheckoutBook(t, dm, oneMore, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays))
	if err != ErrPatronAtLoanLimit {
		t.Errorf("expected ErrPatronAtLoanLimit, got %v", err)
	}
}

// TestCheckoutBookAtLimitBoundary pins the other side of the limit: a
// patron with MaxActiveLoansPerPatron - 1 active loans CAN still check
// out one more. Regression guard against an off-by-one in the comparator.
func TestCheckoutBookAtLimitBoundary(t *testing.T) {
	dm := setupTestDB(t)
	patronID := mustCreatePatron(t, dm, "One Under Max")
	nextWeek := time.Now().AddDate(0, 0, 7).UTC().Format("2006-01-02")

	for i := range MaxActiveLoansPerPatron - 1 {
		bookID := mustCreateBook(t, dm, "Loan Filler Book "+string(rune('A'+i)), 1)
		mustInsertLoan(t, dm, bookID, patronID, nextWeek, "")
	}

	final := mustCreateBook(t, dm, "The Last One", 1)
	if err := tryCheckoutBook(t, dm, final, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays)); err != nil {
		t.Errorf("expected success at boundary, got %v", err)
	}
}

// TestReturnBookHappyPath pins the return round-trip: returned_at is
// stamped and the derived available count is restored.
func TestReturnBookHappyPath(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Return Target", 1)
	patronID := mustCreatePatron(t, dm, "Returnee")

	if err := tryCheckoutBook(t, dm, bookID, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays)); err != nil {
		t.Fatalf("CheckoutBook: %v", err)
	}

	var loanID int
	if err := dm.db.QueryRow(`
		SELECT l.id FROM loans l
		JOIN copies c ON l.copy_id = c.id
		WHERE c.book_id = ? AND l.patron_id = ?`,
		bookID, patronID).Scan(&loanID); err != nil {
		t.Fatalf("query loan id: %v", err)
	}

	if err := dm.ReturnBook(loanID); err != nil {
		t.Fatalf("ReturnBook: %v", err)
	}

	var returnedAt sql.NullString
	if err := dm.db.QueryRow(`SELECT returned_at FROM loans WHERE id = ?`, loanID).Scan(&returnedAt); err != nil {
		t.Fatalf("query returned_at: %v", err)
	}
	if !returnedAt.Valid {
		t.Errorf("expected returned_at set, got NULL")
	}

	if got := availableCopiesOf(t, dm, bookID); got != 1 {
		t.Errorf("expected available copies restored to 1, got %d", got)
	}
}

// TestReturnBookAlreadyReturned pins the idempotency guard. A second
// ReturnBook call must fail with ErrLoanAlreadyReturned and not mutate
// derived availability counts.
func TestReturnBookAlreadyReturned(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Already Returned", 1)
	patronID := mustCreatePatron(t, dm, "Quick Returner")

	if err := tryCheckoutBook(t, dm, bookID, patronID, time.Now().AddDate(0, 0, DefaultLoanTermDays)); err != nil {
		t.Fatalf("CheckoutBook: %v", err)
	}
	var loanID int
	if err := dm.db.QueryRow(`
		SELECT l.id FROM loans l
		JOIN copies c ON l.copy_id = c.id
		WHERE c.book_id = ?`, bookID).Scan(&loanID); err != nil {
		t.Fatalf("query loan id: %v", err)
	}
	if err := dm.ReturnBook(loanID); err != nil {
		t.Fatalf("first ReturnBook: %v", err)
	}

	err := dm.ReturnBook(loanID)
	if err != ErrLoanAlreadyReturned {
		t.Errorf("expected ErrLoanAlreadyReturned on second return, got %v", err)
	}

	if got := availableCopiesOf(t, dm, bookID); got != 1 {
		t.Errorf("expected available copies to remain 1 after double-return attempt, got %d", got)
	}
}

// TestReturnBookNotFound pins the "loan does not exist" path. Must
// surface sql.ErrNoRows so the handler can 404 instead of 500.
func TestReturnBookNotFound(t *testing.T) {
	dm := setupTestDB(t)

	err := dm.ReturnBook(99999)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestGetActiveLoansFiltersReturnedAndOverdue pins the active filter:
// only loans where returned_at IS NULL and due_date >= today appear.
// Returned loans and overdue loans must be excluded.
func TestGetActiveLoansFiltersReturnedAndOverdue(t *testing.T) {
	dm := setupTestDB(t)
	bookA := mustCreateBook(t, dm, "Active Book", 1)
	bookB := mustCreateBook(t, dm, "Returned Book", 1)
	bookC := mustCreateBook(t, dm, "Overdue Book", 1)
	patronID := mustCreatePatron(t, dm, "Pat")

	nextWeek := time.Now().AddDate(0, 0, 7).UTC().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).UTC().Format("2006-01-02")

	mustInsertLoan(t, dm, bookA, patronID, nextWeek, "")                    // active
	mustInsertLoan(t, dm, bookB, patronID, nextWeek, "2026-04-01 12:00:00") // returned
	mustInsertLoan(t, dm, bookC, patronID, yesterday, "")                   // overdue

	loans, err := dm.GetActiveLoans()
	if err != nil {
		t.Fatalf("GetActiveLoans: %v", err)
	}
	if len(loans) != 1 {
		t.Fatalf("expected 1 active loan, got %d", len(loans))
	}
	if loans[0].BookID != bookA {
		t.Errorf("expected book %d, got %d", bookA, loans[0].BookID)
	}
}

// TestGetOverdueLoansOnlyPastDue pins two things: only loans with
// due_date < today are returned, and DaysOverdue is computed correctly.
func TestGetOverdueLoansOnlyPastDue(t *testing.T) {
	dm := setupTestDB(t)
	bookA := mustCreateBook(t, dm, "3 Days Overdue", 1)
	bookB := mustCreateBook(t, dm, "Active", 1)
	patronID := mustCreatePatron(t, dm, "Pat")

	threeDaysAgo := time.Now().AddDate(0, 0, -3).UTC().Format("2006-01-02")
	nextWeek := time.Now().AddDate(0, 0, 7).UTC().Format("2006-01-02")

	mustInsertLoan(t, dm, bookA, patronID, threeDaysAgo, "")
	mustInsertLoan(t, dm, bookB, patronID, nextWeek, "")

	loans, err := dm.GetOverdueLoans()
	if err != nil {
		t.Fatalf("GetOverdueLoans: %v", err)
	}
	if len(loans) != 1 {
		t.Fatalf("expected 1 overdue loan, got %d", len(loans))
	}
	if loans[0].DaysOverdue != 3 {
		t.Errorf("expected DaysOverdue=3, got %d", loans[0].DaysOverdue)
	}
}

// TestGetPatronActiveLoansScopedToPatron pins that the patron-scoped
// filter returns only the given patron's active loans, not everyone's.
func TestGetPatronActiveLoansScopedToPatron(t *testing.T) {
	dm := setupTestDB(t)
	book := mustCreateBook(t, dm, "Shared Book Title Space", 3)
	pat1 := mustCreatePatron(t, dm, "Patron One")
	pat2 := mustCreatePatron(t, dm, "Patron Two")
	nextWeek := time.Now().AddDate(0, 0, 7).UTC().Format("2006-01-02")

	mustInsertLoan(t, dm, book, pat1, nextWeek, "")
	mustInsertLoan(t, dm, book, pat1, nextWeek, "")
	mustInsertLoan(t, dm, book, pat2, nextWeek, "")

	loans, err := dm.GetPatronActiveLoans(pat1)
	if err != nil {
		t.Fatalf("GetPatronActiveLoans: %v", err)
	}
	if len(loans) != 2 {
		t.Errorf("expected 2 loans for patron 1, got %d", len(loans))
	}
	for _, l := range loans {
		if l.PatronID != pat1 {
			t.Errorf("unexpected patron id %d in scoped result", l.PatronID)
		}
	}
}

// TestCountActiveAndOverdueLoans pins the two dashboard count queries
// with the same fixture: their subsets are disjoint (active excludes overdue)
// so the two cards on the dashboard never double-count the same loan.
func TestCountActiveAndOverdueLoans(t *testing.T) {
	dm := setupTestDB(t)
	bookA := mustCreateBook(t, dm, "Book A", 1)
	bookB := mustCreateBook(t, dm, "Book B", 1)
	bookC := mustCreateBook(t, dm, "Book C", 1)
	bookD := mustCreateBook(t, dm, "Book D", 1)
	patronID := mustCreatePatron(t, dm, "Pat")

	nextWeek := time.Now().AddDate(0, 0, 7).UTC().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).UTC().Format("2006-01-02")

	mustInsertLoan(t, dm, bookA, patronID, nextWeek, "")                     // active (not overdue)
	mustInsertLoan(t, dm, bookB, patronID, yesterday, "")                    // overdue (excluded from active)
	mustInsertLoan(t, dm, bookC, patronID, nextWeek, "2026-04-01 12:00:00")  // returned
	mustInsertLoan(t, dm, bookD, patronID, yesterday, "2026-04-01 12:00:00") // returned past due date

	active, err := dm.CountActiveLoans()
	if err != nil {
		t.Fatalf("CountActiveLoans: %v", err)
	}
	if active != 1 {
		t.Errorf("expected 1 active (unreturned, not overdue), got %d", active)
	}

	overdue, err := dm.CountOverdueLoans()
	if err != nil {
		t.Fatalf("CountOverdueLoans: %v", err)
	}
	if overdue != 1 {
		t.Errorf("expected 1 overdue, got %d", overdue)
	}
}

// TestCountOutOfStockReflectsBooks pins CountOutOfStock's derived count
// over the copies + loans tables. Books with at least one non-withdrawn
// copy but zero available copies count; books with no inventory at all
// do not.
func TestCountOutOfStockReflectsBooks(t *testing.T) {
	dm := setupTestDB(t)

	// Seed three books: two with zero available, one with stock.
	mustCreateBookWithAvailable(t, dm, "Zero A", 1, 0)
	mustCreateBookWithAvailable(t, dm, "Zero B", 2, 0)
	mustCreateBookWithAvailable(t, dm, "Has Stock", 3, 3)

	count, err := dm.CountOutOfStock()
	if err != nil {
		t.Fatalf("CountOutOfStock: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 out-of-stock books, got %d", count)
	}
}

// TestGetOutOfStockBooksReturnsOnlyZeroAvailable pins that the catalog
// filter feeding /catalog?filter=out returns ONLY books with derived
// available_copies = 0. In-stock and partial-stock books must not leak.
func TestGetOutOfStockBooksReturnsOnlyZeroAvailable(t *testing.T) {
	dm := setupTestDB(t)

	mustCreateBookWithAvailable(t, dm, "OOS Apple", 1, 0)
	mustCreateBookWithAvailable(t, dm, "OOS Banana", 2, 0)
	mustCreateBookWithAvailable(t, dm, "Has Stock", 3, 3)
	mustCreateBookWithAvailable(t, dm, "Partial Stock", 3, 1)

	got, err := dm.GetOutOfStockBooks()
	if err != nil {
		t.Fatalf("GetOutOfStockBooks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(books) = %d, want 2", len(got))
	}
	// ORDER BY title -> Apple, Banana
	if got[0].Title != "OOS Apple" || got[1].Title != "OOS Banana" {
		t.Errorf("unexpected titles %q, %q", got[0].Title, got[1].Title)
	}
	for _, b := range got {
		if b.AvailableCopies != 0 {
			t.Errorf("book %q leaked into out-of-stock results with available_copies=%d",
				b.Title, b.AvailableCopies)
		}
	}
}

// TestGetOutOfStockBooksEmptyResult pins the zero-row path: when every
// book has at least one available copy, the method returns a nil/empty
// slice and no error (not a sql.ErrNoRows wrapper).
func TestGetOutOfStockBooksEmptyResult(t *testing.T) {
	dm := setupTestDB(t)
	mustCreateBookWithAvailable(t, dm, "Always Available", 1, 1)

	got, err := dm.GetOutOfStockBooks()
	if err != nil {
		t.Fatalf("GetOutOfStockBooks: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(books) = %d, want 0", len(got))
	}
}

// TestCheckoutBookConcurrentRace proves the TOCTOU safety of
// CheckoutBook under contention. With one available copy and N
// goroutines racing to check it out on behalf of N distinct patrons,
// exactly one must succeed and the rest must see ErrNoCopiesAvailable
// -- nothing else.
//
// The design that makes this safe: the status + active-loan read and
// the loan insert happen inside one transaction, and SQLite serializes
// writers via the journal/WAL lock (with PRAGMA busy_timeout the loser
// queues on the lock instead of returning SQLITE_BUSY). If a future
// refactor moves the availability guard outside the transaction, this
// test catches it because the success count would exceed 1.
//
// The N=10 goroutines use 10 distinct patrons so the
// MaxActiveLoansPerPatron guard is not in play.
func TestCheckoutBookConcurrentRace(t *testing.T) {
	dm := setupTestDB(t)
	bookID := mustCreateBook(t, dm, "Race Target", 1)
	copyID := firstAvailableCopyOf(t, dm, bookID)

	const N = 10
	patronIDs := make([]int, N)
	for i := 0; i < N; i++ {
		patronIDs[i] = mustCreatePatron(t, dm, "Racer "+string(rune('A'+i)))
	}

	dueDate := time.Now().AddDate(0, 0, DefaultLoanTermDays)

	var (
		successCount  atomic.Int32
		noCopiesCount atomic.Int32
		otherErrCount atomic.Int32

		otherMu        sync.Mutex
		otherErrSample error // first non-nil non-ErrNoCopiesAvailable for diagnostics
	)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start // line all goroutines up before firing
			err := dm.CheckoutBook(copyID, patronIDs[i], dueDate)
			switch {
			case err == nil:
				successCount.Add(1)
			case errors.Is(err, ErrNoCopiesAvailable):
				noCopiesCount.Add(1)
			default:
				otherErrCount.Add(1)
				otherMu.Lock()
				if otherErrSample == nil {
					otherErrSample = err
				}
				otherMu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := successCount.Load(); got != 1 {
		t.Errorf("success count = %d, want 1", got)
	}
	if got := noCopiesCount.Load(); got != N-1 {
		t.Errorf("ErrNoCopiesAvailable count = %d, want %d", got, N-1)
	}
	if got := otherErrCount.Load(); got != 0 {
		otherMu.Lock()
		sample := otherErrSample
		otherMu.Unlock()
		t.Errorf("unexpected non-nil non-ErrNoCopiesAvailable count = %d (first: %v)", got, sample)
	}

	// Final state: derived availability must be 0 and exactly one loan
	// row exists for the copy.
	if got := availableCopiesOf(t, dm, bookID); got != 0 {
		t.Errorf("available copies = %d, want 0", got)
	}

	var loanCount int
	if err := dm.db.QueryRow(`SELECT COUNT(*) FROM loans WHERE copy_id = ?`, copyID).Scan(&loanCount); err != nil {
		t.Fatalf("query loan count: %v", err)
	}
	if loanCount != 1 {
		t.Errorf("loans count = %d, want 1", loanCount)
	}
}
