// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	_ "modernc.org/sqlite"
)

const (
	DefaultLoanTermDays     = 14
	MaxActiveLoansPerPatron = 5
)

type Book struct {
	ID            int
	Title         string
	ISBN          *string
	CoverFilename *string
	Year          *int
	Publisher     *string
	Description   *string
	Genre         *string
	Dewey         *string
	Authors       string
	// AvailableCopies and TotalCopies are derived from the copies and
	// loans tables at query time and are not stored on the books row.
	// AvailableCopies counts copies whose status = 'available' and that
	// are not currently checked out. TotalCopies counts copies whose
	// status != 'withdrawn'.
	AvailableCopies int
	TotalCopies     int
}

// Copy represents one physical book on the shelf. See DEC-037.
type Copy struct {
	ID            int
	BookID        int
	Barcode       string
	BarcodeFormat string
	Status        string
	NeedsRelabel  bool
	AcquiredAt    string
}

// Copy status values.
const (
	CopyStatusAvailable = "available"
	CopyStatusLost      = "lost"
	CopyStatusDamaged   = "damaged"
	CopyStatusWithdrawn = "withdrawn"
)

// Copy barcode_format values.
const (
	BarcodeFormatCode128 = "code128"
	BarcodeFormatCode39  = "code39"
	BarcodeFormatEAN13   = "ean13"
	BarcodeFormatUPCA    = "upca"
)

type Author struct {
	ID   int
	Name string
}

type LoanRecord struct {
	ID           int
	PatronName   string
	CheckedOutAt string
	DueDate      string
	ReturnedAt   *string
	Status       string
}

type LoanListRow struct {
	LoanID      int
	BookID      int
	BookTitle   string
	PatronID    int
	PatronName  string
	DueDate     string
	DaysOverdue int
}

type OverdueNoticeRow struct {
	BookID      int
	Title       string
	Authors     string
	DueDate     string
	DaysOverdue int
}

type StaffMember struct {
	ID        int
	Username  string
	Role      string
	CreatedAt string
}

type Patron struct {
	ID              int
	Name            string
	Email           *string
	Phone           *string
	Address         *string
	JoinedDate      string
	Metadata        *string
	Username        string
	HasTempPassword bool
}

func (dm *DatabaseManager) GetAllStaff() ([]StaffMember, error) {
	rows, err := dm.db.Query(`
	SELECT id, username, role, created_at
	FROM users
	WHERE role != 'patron'
	ORDER BY CASE role WHEN 'admin' THEN 0 ELSE 1 END, username`)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var staff []StaffMember
	for rows.Next() {
		var s StaffMember
		if err := rows.Scan(&s.ID, &s.Username, &s.Role, &s.CreatedAt); err != nil {
			return nil, err
		}
		staff = append(staff, s)
	}
	return staff, rows.Err()
}

// bookSelectCols is the column list used by GetAllBooks, GetBookByID,
// and GetOutOfStockBooks. AvailableCopies and TotalCopies are derived
// at query time from the copies + loans tables; see DEC-037.
const bookSelectCols = `
	b.id, b.title, b.isbn, b.cover_filename, b.year, b.publisher,
	b.description, b.genre, b.dewey,
	(SELECT COUNT(*) FROM copies c WHERE c.book_id = b.id
		AND c.status = 'available'
		AND c.id NOT IN (SELECT copy_id FROM loans WHERE returned_at IS NULL)
	) AS available_copies,
	(SELECT COUNT(*) FROM copies c WHERE c.book_id = b.id
		AND c.status != 'withdrawn'
	) AS total_copies`

func (dm *DatabaseManager) GetAllBooks() ([]Book, error) {
	rows, err := dm.db.Query(`
		SELECT ` + bookSelectCols + `,
		       GROUP_CONCAT(a.name, ', ') AS authors
		FROM books b
		LEFT JOIN book_authors ba ON b.id = ba.book_id
		LEFT JOIN authors a ON ba.author_id = a.id
		GROUP BY b.id
		ORDER BY b.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var book Book
		var authors *string
		if err := rows.Scan(&book.ID, &book.Title, &book.ISBN, &book.CoverFilename,
			&book.Year, &book.Publisher, &book.Description, &book.Genre,
			&book.Dewey, &book.AvailableCopies, &book.TotalCopies, &authors); err != nil {
			return nil, err
		}
		if authors != nil {
			book.Authors = *authors
		}
		books = append(books, book)
	}
	return books, rows.Err()
}

// GetOutOfStockBooks returns books with zero available copies (every
// non-withdrawn copy is either checked out, lost, or damaged). Used by
// HandleCatalog when invoked with ?filter=out so the dashboard's
// Out-of-Stock card can deep-link into a filtered catalog view.
//
// A book with TotalCopies == 0 (no inventory at all) is excluded so
// the view shows titles that exist physically but are unavailable,
// not titles for which no copies have been added yet.
func (dm *DatabaseManager) GetOutOfStockBooks() ([]Book, error) {
	rows, err := dm.db.Query(`
		SELECT ` + bookSelectCols + `,
		       GROUP_CONCAT(a.name, ', ') AS authors
		FROM books b
		LEFT JOIN book_authors ba ON b.id = ba.book_id
		LEFT JOIN authors a ON ba.author_id = a.id
		GROUP BY b.id
		HAVING total_copies > 0 AND available_copies = 0
		ORDER BY b.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var book Book
		var authors *string
		if err := rows.Scan(&book.ID, &book.Title, &book.ISBN, &book.CoverFilename,
			&book.Year, &book.Publisher, &book.Description, &book.Genre,
			&book.Dewey, &book.AvailableCopies, &book.TotalCopies, &authors); err != nil {
			return nil, err
		}
		if authors != nil {
			book.Authors = *authors
		}
		books = append(books, book)
	}
	return books, rows.Err()
}

func (dm *DatabaseManager) GetBookByID(id int) (*Book, error) {
	book := &Book{}
	err := dm.db.QueryRow(`
		SELECT `+bookSelectCols+`
		FROM books b WHERE b.id = ?`, id).Scan(
		&book.ID, &book.Title, &book.ISBN, &book.CoverFilename,
		&book.Year, &book.Publisher, &book.Description, &book.Genre,
		&book.Dewey, &book.AvailableCopies, &book.TotalCopies)
	if err != nil {
		return nil, err
	}

	rows, err := dm.db.Query(`
                SELECT a.name FROM authors a
                JOIN book_authors ba ON a.id = ba.author_id
                WHERE ba.book_id = ?
                ORDER BY ba.position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	book.Authors = strings.Join(names, ", ")

	return book, nil

}

func (dm *DatabaseManager) GetBookByISBN(isbn string) (*Book, error) {
	book := &Book{}
	err := dm.db.QueryRow(
		"SELECT id, title FROM books WHERE isbn = ?", isbn,
	).Scan(&book.ID, &book.Title)
	if err != nil {
		return nil, err
	}
	return book, nil
}

// CheckoutBook creates a loan for a specific physical copy. Three guards
// run inside the transaction (DEC-031 TOCTOU pattern): the patron must
// have zero overdue loans, fewer than MaxActiveLoansPerPatron active
// loans, and the copy must be available (status = 'available' and not
// currently on loan).
func (dm *DatabaseManager) CheckoutBook(copyID, patronID int, dueDate time.Time) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var overdueCount int
	if err := tx.QueryRow(`
			SELECT COUNT(*) FROM loans
			WHERE patron_id = ?
				AND returned_at IS NULL
				AND due_date < DATE('now')`,
		patronID).Scan(&overdueCount); err != nil {
		return err
	}
	if overdueCount > 0 {
		return ErrPatronHasOverdue
	}

	var activeCount int
	if err := tx.QueryRow(`
			SELECT COUNT(*) FROM loans
			WHERE patron_id = ?
				AND returned_at IS NULL`,
		patronID).Scan(&activeCount); err != nil {
		return err
	}
	if activeCount >= MaxActiveLoansPerPatron {
		return ErrPatronAtLoanLimit
	}

	var status string
	var onLoan int
	if err := tx.QueryRow(`
			SELECT c.status,
			       (SELECT COUNT(*) FROM loans l
			        WHERE l.copy_id = c.id AND l.returned_at IS NULL)
			FROM copies c WHERE c.id = ?`,
		copyID).Scan(&status, &onLoan); err != nil {
		return err
	}
	if status != CopyStatusAvailable || onLoan > 0 {
		return ErrNoCopiesAvailable
	}

	if _, err := tx.Exec(
		`INSERT INTO loans (copy_id, patron_id, due_date) VALUES (?, ?, ?)`,
		copyID, patronID, dueDate.UTC().Format("2006-01-02")); err != nil {
		return err
	}

	return tx.Commit()
}

// ActiveLoanInfo is the join shape returned by GetActiveLoanByBarcode:
// enough information for the rapid-scan checkin portal to render a
// scan-table row (book title, patron name, due date) and to call
// ReturnBook / undo paths (loan id).
type ActiveLoanInfo struct {
	LoanID       int
	CopyID       int
	BookID       int
	BookTitle    string
	PatronID     int
	PatronName   string
	Barcode      string
	CheckedOutAt string
	DueDate      string
}

// ErrNoActiveLoanForBarcode means a copy with that barcode exists but
// is not currently checked out. Distinct from ErrCopyNotFound (no copy
// at all). The rapid-scan checkin handler surfaces these as different
// flash codes so staff know whether they scanned the wrong barcode or
// a barcode for an already-returned copy.
var ErrNoActiveLoanForBarcode = errors.New("db: copy is not currently on loan")

// GetActiveLoanByBarcode looks up the active (not-yet-returned) loan
// for the copy with the given barcode, joining through copies + books
// + patrons to fill ActiveLoanInfo. Returns ErrCopyNotFound when no
// copy has that barcode, ErrNoActiveLoanForBarcode when the copy
// exists but is on the shelf.
func (dm *DatabaseManager) GetActiveLoanByBarcode(barcode string) (*ActiveLoanInfo, error) {
	cp, err := dm.GetCopyByBarcode(barcode)
	if err != nil {
		return nil, err
	}
	info := &ActiveLoanInfo{
		CopyID:  cp.ID,
		BookID:  cp.BookID,
		Barcode: cp.Barcode,
	}
	err = dm.db.QueryRow(`
		SELECT l.id, l.checked_out_at, l.due_date, l.patron_id, p.name, b.title
		FROM loans l
		JOIN copies c ON l.copy_id = c.id
		JOIN books b ON c.book_id = b.id
		JOIN patrons p ON l.patron_id = p.id
		WHERE c.barcode = ? AND l.returned_at IS NULL
		ORDER BY l.checked_out_at DESC LIMIT 1`,
		barcode).Scan(&info.LoanID, &info.CheckedOutAt, &info.DueDate,
		&info.PatronID, &info.PatronName, &info.BookTitle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoActiveLoanForBarcode
	}
	if err != nil {
		return nil, err
	}
	return info, nil
}

// DeleteLoanIfActive removes a loan row only when the loan has not
// been returned yet. Used by the rapid-scan checkout portal's undo
// button to back out a just-created loan. Returns ErrLoanAlreadyReturned
// if returned_at is non-NULL (preserves history; an already-returned
// loan is not a typo that can be undone, it's a completed cycle).
// Returns sql.ErrNoRows when no loan with that id exists.
func (dm *DatabaseManager) DeleteLoanIfActive(loanID int) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var returnedAt sql.NullString
	if err := tx.QueryRow(`SELECT returned_at FROM loans WHERE id = ?`, loanID).Scan(&returnedAt); err != nil {
		return err
	}
	if returnedAt.Valid {
		return ErrLoanAlreadyReturned
	}
	if _, err := tx.Exec(`DELETE FROM loans WHERE id = ?`, loanID); err != nil {
		return err
	}
	return tx.Commit()
}

// ReopenReturnedLoan clears the returned_at column on a previously-
// returned loan. Used by the rapid-scan checkin portal's undo button
// to back out a mistaken return (the patron didn't actually bring the
// book back). Returns sql.ErrNoRows when the loan id doesn't exist
// and ErrLoanAlreadyReturned... wait, the opposite: this is for loans
// that ARE returned. Returns nil on success; if returned_at is
// already NULL the UPDATE is a no-op but still commits cleanly.
func (dm *DatabaseManager) ReopenReturnedLoan(loanID int) error {
	res, err := dm.db.Exec(`UPDATE loans SET returned_at = NULL WHERE id = ?`, loanID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (dm *DatabaseManager) ReturnBook(loanID int) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var returnedAt sql.NullString
	if err := tx.QueryRow(
		`SELECT returned_at FROM loans WHERE id = ?`,
		loanID).Scan(&returnedAt); err != nil {
		return err
	}
	if returnedAt.Valid {
		return ErrLoanAlreadyReturned
	}

	if _, err := tx.Exec(
		`UPDATE loans SET returned_at = CURRENT_TIMESTAMP WHERE id = ?`,
		loanID); err != nil {
		return err
	}

	return tx.Commit()
}

func (dm *DatabaseManager) GetLoanHistory(bookID int) ([]LoanRecord, error) {
	rows, err := dm.db.Query(`                         
                SELECT l.id, p.name, l.checked_out_at, l.due_date, l.returned_at                                                                                                              
                FROM loans l
                JOIN copies c ON l.copy_id = c.id
                JOIN patrons p ON l.patron_id = p.id
                WHERE c.book_id = ?
                ORDER BY l.checked_out_at DESC`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []LoanRecord
	for rows.Next() {
		var r LoanRecord
		if err := rows.Scan(&r.ID, &r.PatronName, &r.CheckedOutAt, &r.DueDate, &r.ReturnedAt); err != nil {
			return nil, err
		}
		if r.ReturnedAt != nil {
			r.Status = "returned"
		} else if r.DueDate < time.Now().Format("2006-01-02 15:04:05") {
			r.Status = "overdue"
		} else {
			r.Status = "active"
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (dm *DatabaseManager) GetActiveLoans() ([]LoanListRow, error) {
	rows, err := dm.db.Query(`
			SELECT l.id, b.id, b.title, p.id, p.name, l.due_date,
				CAST(julianday('now') - julianday(l.due_date) AS INTEGER) AS days_overdue
			FROM loans l
			JOIN copies c ON l.copy_id = c.id
			JOIN books b ON c.book_id = b.id
			JOIN patrons p ON l.patron_id = p.id
			WHERE l.returned_at IS NULL
				AND l.due_date >= DATE('now')
			ORDER BY l.due_date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var loans []LoanListRow
	for rows.Next() {
		var r LoanListRow
		if err := rows.Scan(&r.LoanID, &r.BookID, &r.BookTitle,
			&r.PatronID, &r.PatronName, &r.DueDate, &r.DaysOverdue); err != nil {
			return nil, err
		}
		loans = append(loans, r)
	}
	return loans, rows.Err()
}

func (dm *DatabaseManager) GetOverdueLoans() ([]LoanListRow, error) {
	rows, err := dm.db.Query(`
			SELECT l.id, b.id, b.title, p.id, p.name, l.due_date,
				CAST(julianday('now') - julianday(l.due_date) AS INTEGER) AS days_overdue
			FROM loans l
			JOIN copies c ON l.copy_id = c.id
			JOIN books b ON c.book_id = b.id
			JOIN patrons p ON l.patron_id = p.id
			WHERE l.returned_at IS NULL
				AND l.due_date < DATE('now')
			ORDER BY l.due_date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var loans []LoanListRow
	for rows.Next() {
		var r LoanListRow
		if err := rows.Scan(&r.LoanID, &r.BookID, &r.BookTitle,
			&r.PatronID, &r.PatronName, &r.DueDate, &r.DaysOverdue); err != nil {
			return nil, err
		}
		loans = append(loans, r)
	}
	return loans, rows.Err()
}

func (dm *DatabaseManager) GetPatronActiveLoans(patronID int) ([]LoanListRow, error) {
	rows, err := dm.db.Query(`
			SELECT l.id, b.id, b.title, p.id, p.name, l.due_date,
				CAST(julianday('now') - julianday(l.due_date) AS INTEGER) AS days_overdue
			FROM loans l
			JOIN copies c ON l.copy_id = c.id
			JOIN books b ON c.book_id = b.id
			JOIN patrons p ON l.patron_id = p.id
			WHERE l.returned_at IS NULL
				AND l.patron_id = ?
			ORDER BY l.due_date ASC`, patronID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var loans []LoanListRow
	for rows.Next() {
		var r LoanListRow
		if err := rows.Scan(&r.LoanID, &r.BookID, &r.BookTitle,
			&r.PatronID, &r.PatronName, &r.DueDate, &r.DaysOverdue); err != nil {
			return nil, err
		}
		loans = append(loans, r)
	}
	return loans, rows.Err()
}

func (dm *DatabaseManager) GetPatronOverdueLoansForNotice(patronID int) ([]OverdueNoticeRow, error) {
	rows, err := dm.db.Query(`
		SELECT b.id, b.title,
		       COALESCE(GROUP_CONCAT(a.name, ', '), '') AS authors,
		       l.due_date,
		       CAST(julianday('now') - julianday(l.due_date) AS INTEGER) AS days_overdue
		FROM loans l
		JOIN copies c ON l.copy_id = c.id
		JOIN books b ON c.book_id = b.id
		LEFT JOIN book_authors ba ON b.id = ba.book_id
		LEFT JOIN authors a ON ba.author_id = a.id
		WHERE l.returned_at IS NULL
		  AND l.due_date < DATE('now')
		  AND l.patron_id = ?
		GROUP BY l.id
		ORDER BY l.due_date ASC`, patronID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var loans []OverdueNoticeRow
	for rows.Next() {
		var r OverdueNoticeRow
		if err := rows.Scan(&r.BookID, &r.Title, &r.Authors, &r.DueDate, &r.DaysOverdue); err != nil {
			return nil, err
		}
		loans = append(loans, r)
	}
	return loans, rows.Err()
}

func (dm *DatabaseManager) CountActiveLoans() (int, error) {
	var count int
	err := dm.db.QueryRow(`
			SELECT COUNT(*) FROM loans
			WHERE returned_at IS NULL
				AND due_date >= DATE('now')`).Scan(&count)
	return count, err
}

func (dm *DatabaseManager) CountOverdueLoans() (int, error) {
	var count int
	err := dm.db.QueryRow(`
			SELECT COUNT(*) FROM loans
			WHERE returned_at IS NULL
				AND due_date < DATE('now')`).Scan(&count)
	return count, err
}

// CountOutOfStock returns the number of books that have at least one
// non-withdrawn copy but zero copies currently available for checkout
// (every copy is checked out, lost, or damaged). Books with no inventory
// at all are excluded; see GetOutOfStockBooks for the rationale.
func (dm *DatabaseManager) CountOutOfStock() (int, error) {
	var count int
	err := dm.db.QueryRow(`
		SELECT COUNT(*) FROM books b
		WHERE (SELECT COUNT(*) FROM copies c WHERE c.book_id = b.id AND c.status != 'withdrawn') > 0
		  AND (SELECT COUNT(*) FROM copies c WHERE c.book_id = b.id
		         AND c.status = 'available'
		         AND c.id NOT IN (SELECT copy_id FROM loans WHERE returned_at IS NULL)
		      ) = 0`).Scan(&count)
	return count, err
}

func (dm *DatabaseManager) CountBooks() (int, error) {
	var count int
	err := dm.db.QueryRow(`SELECT COUNT(*) FROM books`).Scan(&count)
	return count, err
}

func (dm *DatabaseManager) CountPatrons() (int, error) {
	var count int
	err := dm.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'patron'`).Scan(&count)
	return count, err
}

func (dm *DatabaseManager) CountTotalLoans() (int, error) {
	var count int
	err := dm.db.QueryRow(`SELECT COUNT(*) FROM loans`).Scan(&count)
	return count, err
}

// SnapshotTo writes a consistent point-in-time copy of the database to
// destPath using SQLite's VACUUM INTO. destPath must NOT already exist.
// VACUUM INTO does not accept parameter bindings for the destination,
// so the path is escaped and inlined. Callers should construct destPath
// from a process-controlled source (e.g. os.MkdirTemp).
func (dm *DatabaseManager) SnapshotTo(destPath string) error {
	escaped := strings.ReplaceAll(destPath, "'", "''")
	_, err := dm.db.Exec(fmt.Sprintf("VACUUM INTO '%s'", escaped))
	return err
}

type DatabaseManager struct {
	mu     sync.RWMutex
	db     *sql.DB
	dbPath string
}

func NewDatabaseManager(dbPath string) *DatabaseManager {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	db, err := openDB(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	dm := &DatabaseManager{db: db, dbPath: dbPath}
	dm.createSchema()
	return dm
}

// openDB opens a SQLite database with foreign keys + WAL mode, returning
// any error rather than calling log.Fatal. Used by HandleBackupImport
// when reopening after a swap, where a non-fatal failure must be
// recoverable rather than killing the server.
func openDB(dbPath string) (*sql.DB, error) {
	// PRAGMAs and txlock are passed in the DSN so the modernc.org/sqlite
	// driver applies them to *every* connection it opens, not just the
	// first one a `db.Exec("PRAGMA ...")` call happens to grab from the
	// pool.
	//
	//   foreign_keys   per-connection. Default off; must be set on each
	//                  new connection.
	//   journal_mode   database-level (persisted in the file once set),
	//                  but harmless to assert per-connection.
	//   busy_timeout   per-connection. Default 0 -- a losing concurrent
	//                  writer returns SQLITE_BUSY immediately. With 5s,
	//                  the loser queues on the journal/WAL lock and then
	//                  re-evaluates its guards inside its own transaction.
	//   _txlock        every non-readonly Begin() issues "BEGIN IMMEDIATE"
	//                  rather than "BEGIN DEFERRED". DEFERRED starts as a
	//                  reader, reads a snapshot, then tries to upgrade to
	//                  writer on the first write -- if another tx
	//                  committed in between, the upgrade fails with
	//                  SQLITE_BUSY_SNAPSHOT (code 517). IMMEDIATE takes
	//                  the write lock at BEGIN time, so other writers
	//                  queue on the lock (with busy_timeout) instead of
	//                  racing for the snapshot. Every dm.db.Begin() call
	//                  in this file is a write transaction.
	//
	// Verified load-bearing by TestCheckoutBookConcurrentRace in
	// db_loans_test.go (cs408-go-stack-7an, DEC-031). CI initially
	// surfaced SQLITE_BUSY (5) on db.Exec-style PRAGMAs (only one
	// connection got them); then SQLITE_BUSY_SNAPSHOT (517) on
	// BEGIN DEFERRED. _txlock=immediate is what closes both classes.
	dsn := dbPath + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)&_pragma=journal_mode(WAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	// Touch the DB so the driver opens a connection and surfaces any
	// PRAGMA failure (bad path, permissions) here rather than at first
	// query time.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return db, nil
}

type sessionRow struct {
	Token     string
	UserID    int
	CSRFToken string
	ExpiresAt string
}

// DumpSessions returns every row of the sessions table. Used by the
// import handler to preserve live sessions across a database swap.
func (dm *DatabaseManager) DumpSessions() ([]sessionRow, error) {
	rows, err := dm.db.Query(`SELECT token, user_id, csrf_token, expires_at FROM sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sessionRow
	for rows.Next() {
		var s sessionRow
		if err := rows.Scan(&s.Token, &s.UserID, &s.CSRFToken, &s.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RestoreSessions truncates the sessions table and re-inserts the given
// rows. Sessions whose user_id no longer exists in the (possibly
// imported) users table are skipped to avoid foreign key violations.
func (dm *DatabaseManager) RestoreSessions(sessions []sessionRow) error {
	if _, err := dm.db.Exec(`DELETE FROM sessions`); err != nil {
		return err
	}
	for _, s := range sessions {
		var exists int
		err := dm.db.QueryRow(`SELECT 1 FROM users WHERE id = ?`, s.UserID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := dm.db.Exec(
			`INSERT INTO sessions (token, user_id, csrf_token, expires_at) VALUES (?, ?, ?, ?)`,
			s.Token, s.UserID, s.CSRFToken, s.ExpiresAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (dm *DatabaseManager) createSchema() {
	schema := `CREATE TABLE IF NOT EXISTS books (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		title          TEXT NOT NULL,
		isbn           TEXT UNIQUE,
		cover_filename TEXT,
		year           INTEGER,
		publisher      TEXT,
		description    TEXT,
		genre          TEXT,
		dewey          TEXT
	);

	CREATE TABLE IF NOT EXISTS authors (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE
	);

	CREATE TABLE IF NOT EXISTS book_authors (
		book_id   INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
		author_id INTEGER NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
		position  INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (book_id, author_id)
	);

	CREATE TABLE IF NOT EXISTS copies (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		book_id        INTEGER NOT NULL REFERENCES books(id),
		barcode        TEXT NOT NULL UNIQUE,
		barcode_format TEXT NOT NULL DEFAULT 'code128',
		status         TEXT NOT NULL DEFAULT 'available',
		needs_relabel  INTEGER NOT NULL DEFAULT 0,
		acquired_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_copies_book_id ON copies(book_id);
	CREATE INDEX IF NOT EXISTS idx_copies_status ON copies(status);

	CREATE TABLE IF NOT EXISTS patrons (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		name        TEXT NOT NULL,
		email       TEXT,
		phone       TEXT,
		address     TEXT,
		joined_date DATETIME DEFAULT CURRENT_TIMESTAMP,
		metadata    TEXT
	);

	CREATE TABLE IF NOT EXISTS loans (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		copy_id        INTEGER NOT NULL REFERENCES copies(id),
		patron_id      INTEGER NOT NULL REFERENCES patrons(id),
		checked_out_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		due_date       TEXT NOT NULL,
		returned_at    DATETIME
	);

	CREATE INDEX IF NOT EXISTS idx_loans_copy_id ON loans(copy_id);
	CREATE INDEX IF NOT EXISTS idx_loans_patron_id ON loans(patron_id);

	CREATE TABLE IF NOT EXISTS users (
		id                   INTEGER PRIMARY KEY AUTOINCREMENT,
		username             TEXT NOT NULL UNIQUE,
		password_hash        TEXT NOT NULL,
		role                 TEXT NOT NULL CHECK(role IN('admin', 'staff', 'patron')),
		patron_id            INTEGER REFERENCES patrons(id),
		created_at           DATETIME DEFAULT CURRENT_TIMESTAMP,
		must_change_password INTEGER NOT NULL DEFAULT 0,
		temp_password        TEXT
	);

	CREATE TABLE IF NOT EXISTS sessions (
		token      TEXT PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id),
		csrf_token TEXT NOT NULL,
		expires_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS settings (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_by INTEGER REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS label_settings (
		id              INTEGER PRIMARY KEY CHECK (id = 1),
		preset          TEXT NOT NULL DEFAULT 'avery-5160',
		offset_top_mm   REAL NOT NULL DEFAULT 0.0,
		offset_left_mm  REAL NOT NULL DEFAULT 0.0
	);

	INSERT OR IGNORE INTO label_settings (id, preset, offset_top_mm, offset_left_mm)
		VALUES (1, 'avery-5160', 0.0, 0.0);`

	if _, err := dm.db.Exec(schema); err != nil {
		log.Fatalf("Failed to create schema: %v", err)
	}

	// Additive migrations for tables that pre-existed the column.
	// SQLite raises "duplicate column" when the column is already
	// present; that's the idempotent path on existing deployments.
	if _, err := dm.db.Exec(`ALTER TABLE patrons ADD COLUMN address TEXT`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		log.Fatalf("Failed to add patrons.address column: %v", err)
	}
	if _, err := dm.db.Exec(`ALTER TABLE books ADD COLUMN dewey TEXT`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		log.Fatalf("Failed to add books.dewey column: %v", err)
	}

	log.Println("Database schema ready")
}

type Session struct {
	User      *User
	CSRFToken string
}

type User struct {
	ID                 int
	Username           string
	PasswordHash       string
	Role               string
	PatronID           *int
	MustChangePassword bool
	TempPassword       *string
}

func (dm *DatabaseManager) GetUserByUsername(username string) (*User, error) {
	user := &User{}
	err := dm.db.QueryRow(
		"SELECT id, username, password_hash, role, patron_id, must_change_password, temp_password FROM users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.PatronID, &user.MustChangePassword, &user.TempPassword)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (dm *DatabaseManager) GetUserByID(id int) (*User, error) {
	user := &User{}
	err := dm.db.QueryRow(
		"SELECT id, username, password_hash, role, patron_id, must_change_password, temp_password FROM users where id = ?", id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.PatronID, &user.MustChangePassword, &user.TempPassword)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (dm *DatabaseManager) CreateUser(username, passwordHash, role string, patronID *int) error {
	_, err := dm.db.Exec(
		"INSERT INTO users (username, password_hash, role, patron_id) VALUES (?, ?, ?, ?)",
		username, passwordHash, role, patronID,
	)
	return err
}

func (dm *DatabaseManager) UpdateStaffUser(id int, username, role string) error {
	_, err := dm.db.Exec(
		"UPDATE users SET username = ?, role = ? WHERE id = ?",
		username, role, id,
	)
	return err
}

func (dm *DatabaseManager) UpdateUserPassword(id int, passwordHash string) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		"UPDATE users SET password_hash = ?, must_change_password = 0, temp_password = NULL WHERE id = ?",
		passwordHash, id,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"DELETE FROM sessions WHERE user_id = ?", id,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (dm *DatabaseManager) SetMustChangePassword(userID int) error {
	_, err := dm.db.Exec(
		"UPDATE users SET must_change_password = 1 WHERE id = ?", userID,
	)
	return err
}

func (dm *DatabaseManager) ClearTempPassword(userID int) error {
	_, err := dm.db.Exec(
		"UPDATE users SET temp_password = NULL WHERE id = ?", userID,
	)
	return err
}

// RegenerateTempPassword generates a new temp, hashes it, swaps both the
// hash and stored plaintext, sets must_change_password=1, and wipes the
// user's existing sessions so any in-flight session under the old hash
// is invalidated.
func (dm *DatabaseManager) RegenerateTempPassword(userID int) (string, error) {
	tempPassword, err := generateTempPassword()
	if err != nil {
		return "", fmt.Errorf("db: generate temp password: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("db: hash temp password: %w", err)
	}

	tx, err := dm.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE users SET password_hash = ?, temp_password = ?, must_change_password = 1 WHERE id = ?`,
		string(hash), tempPassword, userID,
	); err != nil {
		return "", err
	}
	if _, err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return tempPassword, nil
}

func (dm *DatabaseManager) DeleteUser(id int) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM users WHERE id = ?", id); err != nil {
		return err
	}
	return tx.Commit()
}

func (dm *DatabaseManager) CountAdmins() (int, error) {
	var count int
	err := dm.db.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&count)
	return count, err
}

func (dm *DatabaseManager) CreateSession(token string, userID int, csrfToken string, expiresAt time.Time) error {
	_, err := dm.db.Exec(
		"INSERT INTO sessions (token, user_id, csrf_token, expires_at) VALUES (?, ?, ?, ?)",
		token, userID, csrfToken, expiresAt.UTC().Format("2006-01-02 15:04:05"),
	)
	return err
}

func (dm *DatabaseManager) GetSession(token string) (*Session, error) {
	session := &Session{User: &User{}}
	err := dm.db.QueryRow(`
		SELECT u.id, u.username, u.password_hash, u.role, u.patron_id, u.must_change_password, u.temp_password, s.csrf_token
		FROM sessions s
		JOIN users u on s.user_id = u.id
		WHERE s.token = ? AND datetime(s.expires_at) > datetime('now')`,
		token,
	).Scan(&session.User.ID, &session.User.Username, &session.User.PasswordHash, &session.User.Role, &session.User.PatronID, &session.User.MustChangePassword, &session.User.TempPassword, &session.CSRFToken)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (dm *DatabaseManager) DeleteSession(token string) error {
	_, err := dm.db.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

func (dm *DatabaseManager) GetSetting(key string) (string, error) {
	var value string
	err := dm.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

func (dm *DatabaseManager) SetSetting(key, value string, byUserID int) error {
	_, err := dm.db.Exec(
		`INSERT INTO settings (key, value, updated_at, updated_by)
		 VALUES (?, ?, CURRENT_TIMESTAMP, ?)
		 ON CONFLICT(key) DO UPDATE SET
		     value = excluded.value,
		     updated_at = excluded.updated_at,
		     updated_by = excluded.updated_by`,
		key, value, byUserID,
	)
	return err
}

func (dm *DatabaseManager) GetSettingBool(key string, defaultValue bool) bool {
	v, err := dm.GetSetting(key)
	if err != nil || v == "" {
		return defaultValue
	}
	return strings.EqualFold(v, "true")
}

func (dm *DatabaseManager) SeedDefaultUsers() {
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = "Admin123!"
	}
	if err := ValidatePassword(adminPassword); err != nil {
		log.Fatalf("ADMIN_PASSWORD does not meet requirements: %v", err)
	}

	var count int

	if err := dm.db.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'admin'").Scan(&count); err != nil {
		log.Fatalf("Failed to check for admin user: %v", err)
	}
	if count == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("Failed to hash admin password: %v", err)
		}
		if err := dm.CreateUser("admin", string(hash), "admin", nil); err != nil {
			log.Fatalf("Failed to seed admin user: %v", err)
		}
		log.Println("Seeded admin user")
	}

	if err := dm.db.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'patron1'").Scan(&count); err != nil {
		log.Fatalf("Failed to check for patron1 user: %v", err)
	}
	if count == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte("Patron123!"), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("Failed to hash patron 1 password: %v", err)
		}
		if err := dm.seedLinkedPatron("patron1", string(hash), "Seed Patron"); err != nil {
			log.Fatalf("Failed to seed patron1: %v", err)
		}
		log.Println("Seeded patron1 user and linked patrons row")
	}

	if err := dm.db.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'staff1'").Scan(&count); err != nil {
		log.Fatalf("Failed to check for staff1 user: %v", err)
	}
	if count == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte("Staff123!"), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("Failed to hash staff1 password: %v", err)
		}
		if err := dm.CreateUser("staff1", string(hash), "staff", nil); err != nil {
			log.Fatalf("Failed to seed staff1 user: %v", err)
		}
		log.Println("Seeded staff1 user")
	}
}

// seedLinkedPatron inserts a patrons row and a linked users row in a
// single transaction, mirroring CreatePatron's two-row write (DEC-022)
// but with an explicit username instead of auto-generation. Used by
// SeedDefaultUsers for patron1 so the seed account appears in the
// admin /patrons list and gives CP6 checkout / return something to
// target. Separate from CreatePatron because that function derives
// the username from a name via generateBaseUsername, and the seed
// needs to keep the canonical "patron1" credential for pre-existing
// auth tests.
func (dm *DatabaseManager) seedLinkedPatron(username, passwordHash, patronName string) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec("INSERT INTO patrons (name) VALUES (?)", patronName)
	if err != nil {
		return err
	}
	patronID64, err := res.LastInsertId()
	if err != nil {
		return err
	}
	patronID := int(patronID64)

	if _, err := tx.Exec(
		"INSERT INTO users (username, password_hash, role, patron_id) VALUES (?, ?, 'patron', ?)",
		username, passwordHash, patronID,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func findOrCreateAuthor(tx *sql.Tx, name string) (int, error) {
	var id int
	err := tx.QueryRow("SELECT id FROM authors WHERE name = ?", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	result, err := tx.Exec("INSERT INTO authors (name) VALUES (?)", name)
	if err != nil {
		return 0, err
	}
	id64, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id64), nil
}

func (dm *DatabaseManager) CreateBook(book *Book, authors []string) (int, error) {
	tx, err := dm.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
			INSERT INTO books (title, isbn, cover_filename, year, publisher, description, genre, dewey)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		book.Title, book.ISBN, book.CoverFilename, book.Year,
		book.Publisher, book.Description, book.Genre, book.Dewey)
	if err != nil {
		return 0, err
	}

	bookID64, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	bookID := int(bookID64)

	for i, name := range authors {
		authorID, err := findOrCreateAuthor(tx, name)
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(
			"INSERT INTO book_authors (book_id, author_id, position) VALUES (?, ?, ?)",
			bookID, authorID, i+1,
		); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return bookID, nil
}

func (dm *DatabaseManager) UpdateBook(id int, book *Book, authors []string) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE books
		SET title = ?, isbn = ?, cover_filename = ?, year = ?, publisher = ?,
		    description = ?, genre = ?, dewey = ?
		WHERE id = ?`,
		book.Title, book.ISBN, book.CoverFilename, book.Year,
		book.Publisher, book.Description, book.Genre, book.Dewey, id); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM book_authors WHERE book_id = ?", id); err != nil {
		return err
	}

	for i, name := range authors {
		authorID, err := findOrCreateAuthor(tx, name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			"INSERT INTO book_authors (book_id, author_id, position) VALUES (?, ?, ?)",
			id, authorID, i+1,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (dm *DatabaseManager) UpdateBookCover(id int, filename string) error {
	_, err := dm.db.Exec("UPDATE books SET cover_filename = ? WHERE id = ?", filename, id)
	return err
}

var ErrBookHasLoans = errors.New("db: book has loan history, cannot delete")

func (dm *DatabaseManager) DeleteBook(id int) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var loanCount int
	if err := tx.QueryRow(`
		SELECT COUNT(*) FROM loans l
		JOIN copies c ON l.copy_id = c.id
		WHERE c.book_id = ?`, id).Scan(&loanCount); err != nil {
		return err
	}
	if loanCount > 0 {
		return ErrBookHasLoans
	}

	if _, err := tx.Exec("DELETE FROM copies WHERE book_id = ?", id); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM books WHERE id = ?", id); err != nil {
		return err
	}

	return tx.Commit()
}

var ErrPatronHasLoans = errors.New("db: patron has loan history, cannot delete")

var (
	ErrNoCopiesAvailable   = errors.New("db: no copies available for check out")
	ErrLoanAlreadyReturned = errors.New("db: loan already returned")
	ErrPatronHasOverdue    = errors.New("db: patron has overdue loans, cannot check out")
	ErrPatronAtLoanLimit   = errors.New("db: patron at max active loans")
	ErrCopyNotFound        = errors.New("db: copy not found")
	ErrCopyBookMismatch    = errors.New("db: barcode does not belong to this book")
	ErrCopyHasLoans        = errors.New("db: copy has loan history, cannot delete")
	ErrCopyStatusInvalid   = errors.New("db: copy status must be available, lost, damaged, or withdrawn")
)

// CopyDetail enriches Copy with display-only fields derived at query
// time: the book's title (so the inventory page can show titles
// without an N+1), the latest checked_out_at across all loans for the
// copy ("LastLoanAt"; null if never loaned), and whether the copy is
// currently on loan ("OnLoan"). Used by the Manage Copies and
// Inventory pages.
type CopyDetail struct {
	Copy
	BookTitle  string
	LastLoanAt sql.NullString
	OnLoan     bool
}

// IsValidCopyStatus reports whether s is one of the four constants
// (available / lost / damaged / withdrawn). Handlers use this to
// validate the form payload before calling UpdateCopyStatus.
func IsValidCopyStatus(s string) bool {
	switch s {
	case CopyStatusAvailable, CopyStatusLost, CopyStatusDamaged, CopyStatusWithdrawn:
		return true
	}
	return false
}

// GetCopyByID looks up a copy by primary key. Returns ErrCopyNotFound
// when no row exists. Used by status / delete handlers to verify the
// copy before mutating it.
func (dm *DatabaseManager) GetCopyByID(copyID int) (*Copy, error) {
	c := &Copy{}
	err := dm.db.QueryRow(`
		SELECT id, book_id, barcode, barcode_format, status, needs_relabel, acquired_at
		FROM copies WHERE id = ?`, copyID).Scan(
		&c.ID, &c.BookID, &c.Barcode, &c.BarcodeFormat, &c.Status, &c.NeedsRelabel, &c.AcquiredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCopyNotFound
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// UpdateCopyStatus sets the status of a single copy. Rejects values
// outside the four constants with ErrCopyStatusInvalid; surfaces
// ErrCopyNotFound when the copy id does not exist. Status changes
// while the copy has an active loan are allowed (real libraries mark
// a copy lost while it's still checked out to the patron who reported
// it lost); the loan stays linked.
func (dm *DatabaseManager) UpdateCopyStatus(copyID int, status string) error {
	if !IsValidCopyStatus(status) {
		return ErrCopyStatusInvalid
	}
	res, err := dm.db.Exec(`UPDATE copies SET status = ? WHERE id = ?`, status, copyID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCopyNotFound
	}
	return nil
}

// DeleteCopy removes a copy after verifying it has no loan history.
// Wraps the existence check + history check + delete in a transaction
// so a concurrent loan insert cannot slip in between the guard and
// the delete.
func (dm *DatabaseManager) DeleteCopy(copyID int) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM copies WHERE id = ?`, copyID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrCopyNotFound
	}

	var loanCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM loans WHERE copy_id = ?`, copyID).Scan(&loanCount); err != nil {
		return err
	}
	if loanCount > 0 {
		return ErrCopyHasLoans
	}

	if _, err := tx.Exec(`DELETE FROM copies WHERE id = ?`, copyID); err != nil {
		return err
	}

	return tx.Commit()
}

// copyDetailSelectCols is shared by GetCopiesByBookIDWithLoanInfo and
// GetAllCopiesWithFilters. LastLoanAt is the latest checked_out_at
// across the copy's loan history (null when never loaned); OnLoan is
// 1 when at least one loan for this copy has no returned_at.
const copyDetailSelectCols = `
	c.id, c.book_id, c.barcode, c.barcode_format, c.status, c.needs_relabel, c.acquired_at,
	b.title,
	(SELECT MAX(l.checked_out_at) FROM loans l WHERE l.copy_id = c.id) AS last_loan_at,
	(SELECT EXISTS (SELECT 1 FROM loans l WHERE l.copy_id = c.id AND l.returned_at IS NULL)) AS on_loan`

func scanCopyDetail(rows interface {
	Scan(...interface{}) error
}, d *CopyDetail) error {
	var onLoan int
	if err := rows.Scan(&d.ID, &d.BookID, &d.Barcode, &d.BarcodeFormat,
		&d.Status, &d.NeedsRelabel, &d.AcquiredAt,
		&d.BookTitle, &d.LastLoanAt, &onLoan); err != nil {
		return err
	}
	d.OnLoan = onLoan != 0
	return nil
}

// GetCopiesByBookIDWithLoanInfo lists copies of a single book enriched
// with last-loan timestamp and current-on-loan flag.
func (dm *DatabaseManager) GetCopiesByBookIDWithLoanInfo(bookID int) ([]CopyDetail, error) {
	rows, err := dm.db.Query(`
		SELECT `+copyDetailSelectCols+`
		FROM copies c JOIN books b ON c.book_id = b.id
		WHERE c.book_id = ?
		ORDER BY c.id`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var copies []CopyDetail
	for rows.Next() {
		var d CopyDetail
		if err := scanCopyDetail(rows, &d); err != nil {
			return nil, err
		}
		copies = append(copies, d)
	}
	return copies, rows.Err()
}

// GetAllCopiesWithFilters returns every copy in the catalog ordered
// by book title then copy id. The statusFilter argument, when one of
// the four status constants, narrows to copies in that status; any
// other value (including "all" and "") returns every status.
// needsRelabelOnly, when true, narrows further to copies whose
// needs_relabel flag is set.
func (dm *DatabaseManager) GetAllCopiesWithFilters(statusFilter string, needsRelabelOnly bool) ([]CopyDetail, error) {
	where := ""
	args := []interface{}{}
	if IsValidCopyStatus(statusFilter) {
		where += " AND c.status = ?"
		args = append(args, statusFilter)
	}
	if needsRelabelOnly {
		where += " AND c.needs_relabel = 1"
	}

	rows, err := dm.db.Query(`
		SELECT `+copyDetailSelectCols+`
		FROM copies c JOIN books b ON c.book_id = b.id
		WHERE 1=1`+where+`
		ORDER BY b.title, c.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var copies []CopyDetail
	for rows.Next() {
		var d CopyDetail
		if err := scanCopyDetail(rows, &d); err != nil {
			return nil, err
		}
		copies = append(copies, d)
	}
	return copies, rows.Err()
}

// GetCopyByBarcode looks up a copy by its barcode. Returns
// ErrCopyNotFound when no copy with that barcode exists.
func (dm *DatabaseManager) GetCopyByBarcode(barcode string) (*Copy, error) {
	c := &Copy{}
	err := dm.db.QueryRow(`
		SELECT id, book_id, barcode, barcode_format, status, needs_relabel, acquired_at
		FROM copies WHERE barcode = ?`, barcode).Scan(
		&c.ID, &c.BookID, &c.Barcode, &c.BarcodeFormat, &c.Status, &c.NeedsRelabel, &c.AcquiredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCopyNotFound
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetCopiesByBookID returns all copies of the given book ordered by id.
func (dm *DatabaseManager) GetCopiesByBookID(bookID int) ([]Copy, error) {
	rows, err := dm.db.Query(`
		SELECT id, book_id, barcode, barcode_format, status, needs_relabel, acquired_at
		FROM copies WHERE book_id = ? ORDER BY id`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var copies []Copy
	for rows.Next() {
		var c Copy
		if err := rows.Scan(&c.ID, &c.BookID, &c.Barcode, &c.BarcodeFormat,
			&c.Status, &c.NeedsRelabel, &c.AcquiredAt); err != nil {
			return nil, err
		}
		copies = append(copies, c)
	}
	return copies, rows.Err()
}

// CreatedCopy is a (id, barcode) tuple returned by the bulk creation
// path so handlers can flash a useful detail string (e.g. the first
// and last allocated barcodes in a range).
type CreatedCopy struct {
	ID      int
	Barcode string
}

// MaxBulkCopiesPerRequest caps a single AddLibraryCopies call to
// guard against accidental runaway requests. 50 has headroom for a
// real shipment of duplicates without inviting "add a million"
// fat-fingers.
const MaxBulkCopiesPerRequest = 50

// AddLibraryCopies allocates `count` consecutive LSF barcodes for the
// book and inserts that many copy rows in a single transaction. The
// MAX-query allocator runs once at the start of the tx; subsequent
// inserts use seq+1, seq+2, ... so they are unique within the tx and
// against any pre-existing rows. Returns the created copies in
// allocation order so the caller can show the range to the user.
//
// Concurrent callers serialize on the SQLite writer lock (PRAGMA
// busy_timeout queues losers until the winner commits), so each
// call sees a fresh MAX before allocating its range.
func (dm *DatabaseManager) AddLibraryCopies(bookID, count int) ([]CreatedCopy, error) {
	if count < 1 {
		return nil, fmt.Errorf("AddLibraryCopies: count must be >= 1, got %d", count)
	}
	if count > MaxBulkCopiesPerRequest {
		return nil, fmt.Errorf("AddLibraryCopies: count %d exceeds cap %d", count, MaxBulkCopiesPerRequest)
	}

	tx, err := dm.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var nextSeq int
	if err := tx.QueryRow(`
		SELECT COALESCE(MAX(CAST(substr(barcode, 4, 7) AS INTEGER)), 0) + 1
		FROM copies
		WHERE barcode LIKE 'LSF%' AND length(barcode) = 11`).Scan(&nextSeq); err != nil {
		return nil, err
	}

	created := make([]CreatedCopy, 0, count)
	for i := 0; i < count; i++ {
		barcode, err := MakeLSFBarcode(nextSeq + i)
		if err != nil {
			return nil, err
		}
		res, err := tx.Exec(`
			INSERT INTO copies (book_id, barcode, barcode_format)
			VALUES (?, ?, ?)`, bookID, barcode, BarcodeFormatCode128)
		if err != nil {
			return nil, err
		}
		id64, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		created = append(created, CreatedCopy{ID: int(id64), Barcode: barcode})
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

// AddLibraryCopy is a thin back-compat wrapper around
// AddLibraryCopies(bookID, 1). Returns the new copy id and barcode.
func (dm *DatabaseManager) AddLibraryCopy(bookID int) (int, string, error) {
	created, err := dm.AddLibraryCopies(bookID, 1)
	if err != nil {
		return 0, "", err
	}
	return created[0].ID, created[0].Barcode, nil
}

// CreateCopyWithBarcode inserts a copy with a user-supplied barcode
// in the named format (one of code128 / code39 / ean13 / upca).
// Validates the value against the format up front; surfaces
// ErrBarcodeFormatInvalid for unknown formats,
// ErrBarcodeFailsValidation when the value does not match, and
// ErrBarcodeAlreadyExists when the UNIQUE constraint on
// copies.barcode trips.
func (dm *DatabaseManager) CreateCopyWithBarcode(bookID int, barcode, format string) (int, error) {
	if err := ValidateBarcode(barcode, format); err != nil {
		return 0, err
	}
	res, err := dm.db.Exec(`
		INSERT INTO copies (book_id, barcode, barcode_format)
		VALUES (?, ?, ?)`, bookID, barcode, format)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") &&
			strings.Contains(err.Error(), "copies.barcode") {
			return 0, ErrBarcodeAlreadyExists
		}
		return 0, err
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id64), nil
}

func (dm *DatabaseManager) GetAllPatrons() ([]Patron, error) {
	rows, err := dm.db.Query(`
		SELECT p.id, p.name, p.email, p.phone, p.address, p.joined_date, p.metadata,
		       COALESCE(u.username, ''), (u.temp_password IS NOT NULL)
		FROM patrons p
		LEFT JOIN users u ON u.patron_id = p.id
		ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patrons []Patron
	for rows.Next() {
		var p Patron
		if err := rows.Scan(&p.ID, &p.Name, &p.Email, &p.Phone, &p.Address, &p.JoinedDate, &p.Metadata, &p.Username, &p.HasTempPassword); err != nil {
			return nil, err
		}
		patrons = append(patrons, p)
	}
	return patrons, rows.Err()
}

func (dm *DatabaseManager) GetPatronByID(id int) (*Patron, error) {
	p := &Patron{}
	err := dm.db.QueryRow(`
		SELECT p.id, p.name, p.email, p.phone, p.address, p.joined_date, p.metadata,
		       COALESCE(u.username, ''), (u.temp_password IS NOT NULL)
		FROM patrons p
		LEFT JOIN users u ON u.patron_id = p.id
		WHERE p.id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Email, &p.Phone, &p.Address, &p.JoinedDate, &p.Metadata, &p.Username, &p.HasTempPassword)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// CreatePatron inserts a patrons row and a linked users row in a single
// transaction per DEC-022. The username is auto-generated inside the
// transaction: generateBaseUsername produces a starting form, and we
// retry with "base", "base2", "base3", ... until SELECT COUNT returns
// zero. The COUNT uses COLLATE NOCASE so "jsmith" does not collide
// with an existing "JSmith"; the users.username column itself is not
// NOCASE today (pre-dates #21), so this check is the belt-and-
// suspenders until that schema fix lands. Returns the final username
// so the handler can flash it to the admin.
//
// Email and phone are passed as plain strings; empty string converts
// to a nil pointer before INSERT so the column stores NULL rather
// than a zero-length string. This keeps the DB shape honest about
// "not provided" vs "provided as empty".
func (dm *DatabaseManager) CreatePatron(name, email, phone, address, passwordHash string) (int, string, error) {
	tx, err := dm.db.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()

	var emailPtr, phonePtr, addressPtr *string
	if email != "" {
		emailPtr = &email
	}
	if phone != "" {
		phonePtr = &phone
	}
	if address != "" {
		addressPtr = &address
	}

	res, err := tx.Exec(
		"INSERT INTO patrons (name, email, phone, address) VALUES (?, ?, ?, ?)",
		name, emailPtr, phonePtr, addressPtr,
	)
	if err != nil {
		return 0, "", err
	}
	patronID64, err := res.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	patronID := int(patronID64)

	base := generateBaseUsername(name)
	if base == "" {
		return 0, "", errors.New("db: cannot derive a username from the provided name")
	}

	username := base
	for suffix := 2; ; suffix++ {
		var count int
		if err := tx.QueryRow(
			"SELECT COUNT(*) FROM users WHERE username = ? COLLATE NOCASE",
			username,
		).Scan(&count); err != nil {
			return 0, "", err
		}
		if count == 0 {
			break
		}
		username = fmt.Sprintf("%s%d", base, suffix)
	}

	if _, err := tx.Exec(
		"INSERT INTO users (username, password_hash, role, patron_id) VALUES (?, ?, 'patron', ?)",
		username, passwordHash, patronID,
	); err != nil {
		return 0, "", err
	}

	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return patronID, username, nil
}

func (dm *DatabaseManager) CreatePatronNoLogin(name, email, phone, metadataJSON string) (int, error) {
	var emailPtr, phonePtr, metaPtr *string
	if email != "" {
		emailPtr = &email
	}
	if phone != "" {
		phonePtr = &phone
	}
	if metadataJSON != "" {
		metaPtr = &metadataJSON
	}
	res, err := dm.db.Exec(
		"INSERT INTO patrons (name, email, phone, metadata) VALUES (?, ?, ?, ?)",
		name, emailPtr, phonePtr, metaPtr,
	)
	if err != nil {
		return 0, err
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id64), nil
}

// CreatePatronWithLogin returns the plaintext temp password as the
// fourth return value. Never log it; it leaves this function only via
// the credentials CSV download once.
func (dm *DatabaseManager) CreatePatronWithLogin(name, email, phone, metadataJSON string) (int, int, string, string, error) {
	tempPassword, err := generateTempPassword()
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("db: generate temp password: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("db: hash temp password: %w", err)
	}

	tx, err := dm.db.Begin()
	if err != nil {
		return 0, 0, "", "", err
	}
	defer tx.Rollback()

	var emailPtr, phonePtr, metaPtr *string
	if email != "" {
		emailPtr = &email
	}
	if phone != "" {
		phonePtr = &phone
	}
	if metadataJSON != "" {
		metaPtr = &metadataJSON
	}

	res, err := tx.Exec(
		"INSERT INTO patrons (name, email, phone, metadata) VALUES (?, ?, ?, ?)",
		name, emailPtr, phonePtr, metaPtr,
	)
	if err != nil {
		return 0, 0, "", "", err
	}
	patronID64, err := res.LastInsertId()
	if err != nil {
		return 0, 0, "", "", err
	}
	patronID := int(patronID64)

	base := generateBaseUsername(name)
	if base == "" {
		return 0, 0, "", "", errors.New("db: cannot derive a username from the provided name")
	}

	username := base
	for suffix := 2; ; suffix++ {
		var count int
		if err := tx.QueryRow(
			"SELECT COUNT(*) FROM users WHERE username = ? COLLATE NOCASE",
			username,
		).Scan(&count); err != nil {
			return 0, 0, "", "", err
		}
		if count == 0 {
			break
		}
		username = fmt.Sprintf("%s%d", base, suffix)
	}

	userRes, err := tx.Exec(
		`INSERT INTO users (username, password_hash, role, patron_id, must_change_password, temp_password)
		 VALUES (?, ?, 'patron', ?, 1, ?)`,
		username, string(hash), patronID, tempPassword,
	)
	if err != nil {
		return 0, 0, "", "", err
	}
	userID64, err := userRes.LastInsertId()
	if err != nil {
		return 0, 0, "", "", err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, "", "", err
	}
	return patronID, int(userID64), username, tempPassword, nil
}

func (dm *DatabaseManager) FindPatronByExternalID(externalID string) (*Patron, error) {
	if externalID == "" {
		return nil, nil
	}
	p := &Patron{}
	var username sql.NullString
	err := dm.db.QueryRow(`
		SELECT p.id, p.name, p.email, p.phone, p.joined_date, p.metadata, COALESCE(u.username, '')
		FROM patrons p
		LEFT JOIN users u ON u.patron_id = p.id
		WHERE json_extract(p.metadata, '$.external_id') = ?
		LIMIT 1`, externalID,
	).Scan(&p.ID, &p.Name, &p.Email, &p.Phone, &p.JoinedDate, &p.Metadata, &username)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Username = username.String
	return p, nil
}

func (dm *DatabaseManager) FindPatronByEmail(email string) (*Patron, error) {
	if email == "" {
		return nil, nil
	}
	p := &Patron{}
	var username sql.NullString
	err := dm.db.QueryRow(`
		SELECT p.id, p.name, p.email, p.phone, p.joined_date, p.metadata, COALESCE(u.username, '')
		FROM patrons p
		LEFT JOIN users u ON u.patron_id = p.id
		WHERE p.email = ?
		LIMIT 1`, email,
	).Scan(&p.ID, &p.Name, &p.Email, &p.Phone, &p.JoinedDate, &p.Metadata, &username)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Username = username.String
	return p, nil
}

func (dm *DatabaseManager) UpdatePatron(id int, name, email, phone, address string) error {
	var emailPtr, phonePtr, addressPtr *string
	if email != "" {
		emailPtr = &email
	}
	if phone != "" {
		phonePtr = &phone
	}
	if address != "" {
		addressPtr = &address
	}
	_, err := dm.db.Exec(
		"UPDATE patrons SET name = ?, email = ?, phone = ?, address = ? WHERE id = ?",
		name, emailPtr, phonePtr, addressPtr, id,
	)
	return err
}

// DeletePatron removes the patron + their linked users + sessions rows
// in a single transaction per DEC-022. Guard fires if any loans row
// references this patron (active or returned) so history survives;
// admin's recovery path for a truly departed patron is to wait until
// the loan rows are archived or -- post-submission -- use a soft-
// delete flag. Order of deletes matters: sessions first (while users
// still exists for the subquery), then users, then patrons.
func (dm *DatabaseManager) DeletePatron(id int) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var loanCount int
	if err := tx.QueryRow("SELECT COUNT(*) FROM loans WHERE patron_id = ?", id).Scan(&loanCount); err != nil {
		return err
	}
	if loanCount > 0 {
		return ErrPatronHasLoans
	}

	if _, err := tx.Exec(
		"DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE patron_id = ?)", id,
	); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM users WHERE patron_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM patrons WHERE id = ?", id); err != nil {
		return err
	}

	return tx.Commit()
}

// FetchAndStoreSeedCovers scans the books table for rows that have an
// ISBN but no cover_filename and opportunistically backfills covers
// from Open Library. Safe to call every startup: after a successful
// first run the SELECT returns zero rows and the function exits fast.
// Per-book failures (OL not-found, network, bad content, DB update)
// log a warning and continue -- never panic, never block the server
// from starting. Called from main.go after SeedBooks so a fresh DB
// gets real cover art for the seed books instead of placeholder slots.
//
// The network budget comes from the ctx the caller passes, plus a
// 10s per-request timeout inside FetchOpenLibraryBook and
// SaveCoverFromURL. If ctx fires, remaining books get skipped and
// their covers can be backfilled on the next startup.
func (dm *DatabaseManager) FetchAndStoreSeedCovers(ctx context.Context) {
	if !IsExternalAllowed(dm) {
		log.Printf("FetchAndStoreSeedCovers: offline mode -- skipping seed cover backfill")
		return
	}

	rows, err := dm.db.Query(`
		SELECT id, isbn FROM books
		WHERE cover_filename IS NULL AND isbn IS NOT NULL AND isbn != ''`)
	if err != nil {
		log.Printf("FetchAndStoreSeedCovers: SELECT: %v", err)
		return
	}

	type missing struct {
		id   int
		isbn string
	}
	var pending []missing
	for rows.Next() {
		var m missing
		if err := rows.Scan(&m.id, &m.isbn); err != nil {
			log.Printf("FetchAndStoreSeedCovers: scan: %v", err)
			rows.Close()
			return
		}
		pending = append(pending, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("FetchAndStoreSeedCovers: rows.Err: %v", err)
		return
	}
	if len(pending) == 0 {
		return
	}

	log.Printf("FetchAndStoreSeedCovers: backfilling %d cover(s) from Open Library", len(pending))
	start := time.Now()
	saved := 0
	for _, m := range pending {
		if err := ctx.Err(); err != nil {
			log.Printf("FetchAndStoreSeedCovers: context cancelled, skipping remaining %d book(s): %v", len(pending)-saved, err)
			break
		}
		book, err := FetchOpenLibraryBook(ctx, m.isbn)
		if err != nil {
			log.Printf("FetchAndStoreSeedCovers: OL fetch for ISBN %s: %v", m.isbn, err)
			continue
		}
		if book.CoverURL == "" {
			log.Printf("FetchAndStoreSeedCovers: no cover URL from OL for ISBN %s", m.isbn)
			continue
		}
		filename, err := SaveCoverFromURL(book.CoverURL)
		if err != nil {
			log.Printf("FetchAndStoreSeedCovers: SaveCoverFromURL for ISBN %s: %v", m.isbn, err)
			continue
		}
		if err := dm.UpdateBookCover(m.id, filename); err != nil {
			log.Printf("FetchAndStoreSeedCovers: UpdateBookCover for book %d: %v", m.id, err)
			continue
		}
		saved++
	}
	log.Printf("FetchAndStoreSeedCovers: saved %d/%d cover(s) in %v", saved, len(pending), time.Since(start).Round(time.Millisecond))
}
