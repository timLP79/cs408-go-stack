// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"database/sql"
	"fmt"
	"log"
)

// seedBook is the dev-only fixture shape used by SeedBooks. Production
// installs do not call SeedBooks; the catalog starts empty.
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

// SeedBooks populates the catalog with a fixed set of well-known
// titles and one library-format copy per quantity slot. Idempotent:
// no-op when the books table already has rows.
//
// This is a developer / test convenience and is NOT called from the
// production startup path. See main.go for the LIBRESHELF_SEED_DEV_BOOKS
// env gate; tests call it directly via setupTestRouter.
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
		INSERT INTO books (title, isbn, year, publisher, description, genre)
		VALUES (?, ?, ?, ?, ?, ?)`,
		b.title, b.isbn, b.year, b.publisher, b.description, b.genre)
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

	// Allocate b.quantity library-format copies for this book. Sequence
	// is monotonic across the whole seed call; the MAX query sees rows
	// inserted by earlier iterations in this same transaction via
	// SQLite's read-your-own-writes semantics.
	for i := 0; i < b.quantity; i++ {
		var nextSeq int
		if err := tx.QueryRow(`
			SELECT COALESCE(MAX(CAST(substr(barcode, 4, 7) AS INTEGER)), 0) + 1
			FROM copies
			WHERE barcode LIKE 'LSF%' AND length(barcode) = 11`).Scan(&nextSeq); err != nil {
			return fmt.Errorf("allocate LSF sequence: %w", err)
		}
		barcode, err := MakeLSFBarcode(nextSeq)
		if err != nil {
			return fmt.Errorf("format LSF barcode for seq %d: %w", nextSeq, err)
		}
		if _, err := tx.Exec(`
			INSERT INTO copies (book_id, barcode, barcode_format)
			VALUES (?, ?, 'code128')`, bookID, barcode); err != nil {
			return fmt.Errorf("insert copy %s for book %q: %w", barcode, b.title, err)
		}
	}

	return tx.Commit()
}
