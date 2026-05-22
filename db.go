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
	ID                int
	Title             string
	ISBN              *string
	CoverFilename     *string
	Year              *int
	Publisher         *string
	Description       *string
	Genre             *string
	QuantityTotal     int
	QuantityAvailable int
	Authors           string
}

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

func (dm *DatabaseManager) GetAllBooks() ([]Book, error) {
	rows, err := dm.db.Query(`
			SELECT b.id, b.title, b.isbn, b.cover_filename, b.year, b.publisher,
                   b.description, b.genre, b.quantity_total, b.quantity_available,                                                                                                        
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
			&book.QuantityTotal, &book.QuantityAvailable, &authors); err != nil {
			return nil, err
		}
		if authors != nil {
			book.Authors = *authors
		}
		books = append(books, book)
	}
	return books, rows.Err()
}

// GetOutOfStockBooks returns books whose quantity_available is 0
// (every copy is checked out). Used by HandleCatalog when invoked
// with ?filter=out so the dashboard's Out-of-Stock card can deep-link
// into a filtered catalog view. The shape matches GetAllBooks so the
// same template renders the result.
func (dm *DatabaseManager) GetOutOfStockBooks() ([]Book, error) {
	rows, err := dm.db.Query(`
		SELECT b.id, b.title, b.isbn, b.cover_filename, b.year, b.publisher,
		       b.description, b.genre, b.quantity_total, b.quantity_available,
		       GROUP_CONCAT(a.name, ', ') AS authors
		FROM books b
		LEFT JOIN book_authors ba ON b.id = ba.book_id
		LEFT JOIN authors a ON ba.author_id = a.id
		WHERE b.quantity_available = 0
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
			&book.QuantityTotal, &book.QuantityAvailable, &authors); err != nil {
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
                SELECT id, title, isbn, cover_filename, year, publisher,
                       description, genre, quantity_total, quantity_available
                FROM books WHERE id = ?`, id).Scan(
		&book.ID, &book.Title, &book.ISBN, &book.CoverFilename,
		&book.Year, &book.Publisher, &book.Description, &book.Genre,
		&book.QuantityTotal, &book.QuantityAvailable)
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

func (dm *DatabaseManager) CheckoutBook(bookID, patronID int, dueDate time.Time) error {
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

	var available int
	if err := tx.QueryRow(
		`SELECT quantity_available FROM books WHERE id = ?`,
		bookID).Scan(&available); err != nil {
		return err
	}
	if available <= 0 {
		return ErrNoCopiesAvailable
	}

	if _, err := tx.Exec(
		`INSERT INTO loans (book_id, patron_id, due_date) VALUES (?, ?, ?)`,
		bookID, patronID, dueDate.UTC().Format("2006-01-02")); err != nil {
		return err
	}

	if _, err := tx.Exec(
		`UPDATE books SET quantity_available = quantity_available - 1 WHERE id = ?`,
		bookID); err != nil {
		return err
	}

	return tx.Commit()
}

func (dm *DatabaseManager) ReturnBook(loanID int) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var bookID int
	var returnedAt sql.NullString
	if err := tx.QueryRow(
		`SELECT book_id, returned_at FROM loans WHERE id = ?`,
		loanID).Scan(&bookID, &returnedAt); err != nil {
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

	if _, err := tx.Exec(
		`UPDATE books SET quantity_available = quantity_available + 1 WHERE id = ?`,
		bookID); err != nil {
		return err
	}

	return tx.Commit()
}

func (dm *DatabaseManager) GetLoanHistory(bookID int) ([]LoanRecord, error) {
	rows, err := dm.db.Query(`                         
                SELECT l.id, p.name, l.checked_out_at, l.due_date, l.returned_at                                                                                                              
                FROM loans l             
                JOIN patrons p ON l.patron_id = p.id
                WHERE l.book_id = ?                  
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
			JOIN books b ON l.book_id = b.id
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
			JOIN books b ON l.book_id = b.id
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
			JOIN books b ON l.book_id = b.id
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
		JOIN books b ON l.book_id = b.id
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

func (dm *DatabaseManager) CountOutOfStock() (int, error) {
	var count int
	err := dm.db.QueryRow(`
			SELECT COUNT(*) FROM books
			WHERE quantity_available = 0`).Scan(&count)
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
		id                 INTEGER PRIMARY KEY AUTOINCREMENT,
		title              TEXT NOT NULL,
		isbn               TEXT UNIQUE,
		cover_filename     TEXT,
		year               INTEGER,
		publisher          TEXT,
		description        TEXT,
		genre              TEXT,
		quantity_total     INTEGER DEFAULT 1,
		quantity_available INTEGER DEFAULT 1
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
		book_id        INTEGER NOT NULL REFERENCES books(id),
		patron_id      INTEGER NOT NULL REFERENCES patrons(id),
		checked_out_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		due_date       TEXT NOT NULL,
		returned_at    DATETIME
	);

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
	);`

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

type seedBook struct {
	title       string
	isbn        string
	year        int
	publisher   string
	description string
	genre       string
	quantity    int
	authors     []string
}

func (dm *DatabaseManager) SeedBooks() {
	var count int
	if err := dm.db.QueryRow("SELECT COUNT(*) FROM books").Scan(&count); err != nil {
		log.Fatalf("Failed to check book count: %v", err)
	}
	if count > 0 {
		return
	}

	books := []seedBook{
		{
			title:       "Pride and Prejudice",
			isbn:        "9780141439518",
			year:        1813,
			publisher:   "Penguin Classics",
			description: "A romantic novel of manners that chronicles the emotional development of Elizabeth Bennet.",
			genre:       "Classic Literature",
			quantity:    3,
			authors:     []string{"Jane Austen"},
		},
		{
			title:       "To Kill a Mockingbird",
			isbn:        "9780061120084",
			year:        1960,
			publisher:   "Harper Perennial",
			description: "A novel about racial injustice in the American South, seen through the eyes of a young girl.",
			genre:       "Classic Literature",
			quantity:    4,
			authors:     []string{"Harper Lee"},
		},
		{
			title:       "1984",
			isbn:        "9780451524935",
			year:        1949,
			publisher:   "Signet Classics",
			description: "A dystopian novel set in a totalitarian society ruled by Big Brother.",
			genre:       "Science Fiction",
			quantity:    2,
			authors:     []string{"George Orwell"},
		},
		{
			title:       "The Great Gatsby",
			isbn:        "9780743273565",
			year:        1925,
			publisher:   "Scribner",
			description: "A story of wealth, class, and the American Dream in the Jazz Age.",
			genre:       "Classic Literature",
			quantity:    2,
			authors:     []string{"F. Scott Fitzgerald"},
		},
		{
			title:       "Good Omens",
			isbn:        "9780060853983",
			year:        1990,
			publisher:   "William Morrow",
			description: "An angel and a demon team up to prevent the apocalypse.",
			genre:       "Fantasy",
			quantity:    3,
			authors:     []string{"Neil Gaiman", "Terry Pratchett"},
		},
		{
			title:       "Dune",
			isbn:        "9780441013593",
			year:        1965,
			publisher:   "Ace Books",
			description: "An epic science fiction novel set on the desert planet Arrakis.",
			genre:       "Science Fiction",
			quantity:    2,
			authors:     []string{"Frank Herbert"},
		},
		{
			title:       "The Catcher in the Rye",
			isbn:        "9780316769488",
			year:        1951,
			publisher:   "Little, Brown and Company",
			description: "A disillusioned teenager wanders New York after being expelled from prep school.",
			genre:       "Classic Literature",
			quantity:    3,
			authors:     []string{"J.D. Salinger"},
		},
		{
			title:       "Brave New World",
			isbn:        "9780060850524",
			year:        1932,
			publisher:   "Harper Perennial",
			description: "A dystopian society engineered for stability through conditioning and pleasure.",
			genre:       "Science Fiction",
			quantity:    3,
			authors:     []string{"Aldous Huxley"},
		},
		{
			title:       "Jane Eyre",
			isbn:        "9780141441146",
			year:        1847,
			publisher:   "Penguin Classics",
			description: "An orphaned governess falls for her brooding employer and uncovers his secret.",
			genre:       "Classic Literature",
			quantity:    2,
			authors:     []string{"Charlotte Bronte"},
		},
		{
			title:       "Wuthering Heights",
			isbn:        "9780141439556",
			year:        1847,
			publisher:   "Penguin Classics",
			description: "A passionate, destructive love story set on the windswept Yorkshire moors.",
			genre:       "Classic Literature",
			quantity:    2,
			authors:     []string{"Emily Bronte"},
		},
	}

	for _, b := range books {
		if err := dm.seedOneBook(b); err != nil {
			log.Fatalf("Failed to seed book %q: %v", b.title, err)
		}
	}

	log.Printf("Seeded %d books", len(books))
}

func (dm *DatabaseManager) seedOneBook(b seedBook) error {
	tx, err := dm.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		INSERT INTO books (title, isbn, year, publisher, description, genre,
		                   quantity_total, quantity_available)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		b.title, b.isbn, b.year, b.publisher, b.description, b.genre,
		b.quantity, b.quantity)
	if err != nil {
		return err
	}
	bookID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	for _, authorName := range b.authors {
		var authorID int64
		err := tx.QueryRow("SELECT id FROM authors WHERE name = ?", authorName).Scan(&authorID)
		if err == sql.ErrNoRows {
			res, execErr := tx.Exec("INSERT INTO authors (name) VALUES (?)", authorName)
			if execErr != nil {
				return execErr
			}
			authorID, err = res.LastInsertId()
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		if _, err := tx.Exec(
			"INSERT INTO book_authors (book_id, author_id) VALUES (?, ?)",
			bookID, authorID,
		); err != nil {
			return err
		}
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
			INSERT INTO books (title, isbn, cover_filename, year, publisher, description, genre, quantity_total, quantity_available)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		book.Title, book.ISBN, book.CoverFilename, book.Year,
		book.Publisher, book.Description, book.Genre,
		book.QuantityTotal, book.QuantityAvailable)
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
		    description = ?, genre = ?, quantity_total = ?, quantity_available = ?
		WHERE id = ?`,
		book.Title, book.ISBN, book.CoverFilename, book.Year,
		book.Publisher, book.Description, book.Genre,
		book.QuantityTotal, book.QuantityAvailable, id); err != nil {
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
	if err := tx.QueryRow("SELECT COUNT(*) FROM loans WHERE book_id = ?", id).Scan(&loanCount); err != nil {
		return err
	}
	if loanCount > 0 {
		return ErrBookHasLoans
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
)

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
