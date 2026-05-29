// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"errors"
	"testing"
)

func TestGetLabelSettingsSeededByCreateSchema(t *testing.T) {
	dm := setupTestDB(t)
	s, err := dm.GetLabelSettings()
	if err != nil {
		t.Fatalf("GetLabelSettings: %v", err)
	}
	if s.Preset != "avery-5160" {
		t.Errorf("default preset = %q, want avery-5160", s.Preset)
	}
	if s.OffsetTopMm != 0.0 || s.OffsetLeftMm != 0.0 {
		t.Errorf("default offsets = (%v, %v), want (0, 0)", s.OffsetTopMm, s.OffsetLeftMm)
	}
}

func TestUpdateLabelSettingsRoundTrip(t *testing.T) {
	dm := setupTestDB(t)
	if err := dm.UpdateLabelSettings("avery-l7160", 1.5, -0.75); err != nil {
		t.Fatalf("UpdateLabelSettings: %v", err)
	}
	s, err := dm.GetLabelSettings()
	if err != nil {
		t.Fatalf("GetLabelSettings: %v", err)
	}
	if s.Preset != "avery-l7160" {
		t.Errorf("preset = %q, want avery-l7160", s.Preset)
	}
	if s.OffsetTopMm != 1.5 || s.OffsetLeftMm != -0.75 {
		t.Errorf("offsets = (%v, %v), want (1.5, -0.75)", s.OffsetTopMm, s.OffsetLeftMm)
	}
}

func TestUpdateLabelSettingsRejectsBadPreset(t *testing.T) {
	dm := setupTestDB(t)
	err := dm.UpdateLabelSettings("nonsense", 0, 0)
	if err == nil {
		t.Fatal("UpdateLabelSettings with invalid preset returned nil, want error")
	}
}

func TestUpdateLabelSettingsClampsOffsets(t *testing.T) {
	dm := setupTestDB(t)
	if err := dm.UpdateLabelSettings("avery-5160", 50, -50); err == nil {
		t.Error("expected error for offsets outside -10..+10 range")
	}
}

func TestGetLabelDataByCopyIDs(t *testing.T) {
	dm := setupTestDB(t)
	dewey := "813.54"
	bookID, err := dm.CreateBook(&Book{Title: "The Great Gatsby", Dewey: &dewey}, []string{"F. Scott Fitzgerald"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	id1, barcode1, err := dm.AddLibraryCopy(bookID)
	if err != nil {
		t.Fatalf("AddLibraryCopy 1: %v", err)
	}
	id2, _, err := dm.AddLibraryCopy(bookID)
	if err != nil {
		t.Fatalf("AddLibraryCopy 2: %v", err)
	}

	got, err := dm.GetLabelDataByCopyIDs([]int{id1, id2})
	if err != nil {
		t.Fatalf("GetLabelDataByCopyIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d labels, want 2", len(got))
	}
	for _, ld := range got {
		if ld.BookTitle != "The Great Gatsby" {
			t.Errorf("BookTitle = %q, want The Great Gatsby", ld.BookTitle)
		}
		if ld.Authors != "F. Scott Fitzgerald" {
			t.Errorf("Authors = %q, want F. Scott Fitzgerald", ld.Authors)
		}
		if ld.Dewey != "813.54" {
			t.Errorf("Dewey = %q, want 813.54", ld.Dewey)
		}
		if ld.BarcodeFormat != BarcodeFormatCode128 {
			t.Errorf("BarcodeFormat = %q, want code128", ld.BarcodeFormat)
		}
	}
	// Verify first label has the expected barcode (sanity check on id1)
	if got[0].CopyID == id1 && got[0].Barcode != barcode1 {
		t.Errorf("first label barcode = %q, want %q", got[0].Barcode, barcode1)
	}
}

func TestGetLabelDataByCopyIDsEmpty(t *testing.T) {
	dm := setupTestDB(t)
	got, err := dm.GetLabelDataByCopyIDs(nil)
	if err != nil {
		t.Fatalf("GetLabelDataByCopyIDs(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d labels, want 0", len(got))
	}
}

func TestGetLabelDataByCopyIDsMissingCopyIgnored(t *testing.T) {
	dm := setupTestDB(t)
	bookID, err := dm.CreateBook(&Book{Title: "Real Book"}, []string{"Jane Author"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	realID, _, err := dm.AddLibraryCopy(bookID)
	if err != nil {
		t.Fatalf("AddLibraryCopy: %v", err)
	}
	got, err := dm.GetLabelDataByCopyIDs([]int{realID, 99999})
	if err != nil {
		t.Fatalf("GetLabelDataByCopyIDs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d labels, want 1 (missing id ignored)", len(got))
	}
	if got[0].CopyID != realID {
		t.Errorf("CopyID = %d, want %d", got[0].CopyID, realID)
	}
}

func TestMarkCopiesRelabeledClearsFlag(t *testing.T) {
	dm := setupTestDB(t)
	bookID, err := dm.CreateBook(&Book{Title: "Relabel Test"}, []string{"Auth Or"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	copyID, _, err := dm.AddLibraryCopy(bookID)
	if err != nil {
		t.Fatalf("AddLibraryCopy: %v", err)
	}
	// Force needs_relabel = 1 directly so we have a row to clear.
	if _, err := dm.db.Exec(`UPDATE copies SET needs_relabel = 1 WHERE id = ?`, copyID); err != nil {
		t.Fatalf("seed needs_relabel: %v", err)
	}

	if err := dm.MarkCopiesRelabeled([]int{copyID}); err != nil {
		t.Fatalf("MarkCopiesRelabeled: %v", err)
	}
	c, err := dm.GetCopyByID(copyID)
	if err != nil {
		t.Fatalf("GetCopyByID: %v", err)
	}
	if c.NeedsRelabel {
		t.Error("NeedsRelabel still true after MarkCopiesRelabeled")
	}
}

func TestMarkCopiesRelabeledEmptyNoop(t *testing.T) {
	dm := setupTestDB(t)
	if err := dm.MarkCopiesRelabeled(nil); err != nil {
		t.Errorf("MarkCopiesRelabeled(nil) = %v, want nil", err)
	}
	if err := dm.MarkCopiesRelabeled([]int{}); err != nil {
		t.Errorf("MarkCopiesRelabeled([]) = %v, want nil", err)
	}
}

func TestMarkCopiesRelabeledMissingIDsTolerated(t *testing.T) {
	dm := setupTestDB(t)
	// Should not error on non-existent IDs (UPDATE just affects 0 rows).
	if err := dm.MarkCopiesRelabeled([]int{99999, 88888}); err != nil {
		t.Errorf("MarkCopiesRelabeled with nonexistent ids = %v, want nil", err)
	}
}

func TestFlagCopyForRelabelSetsFlag(t *testing.T) {
	dm := setupTestDB(t)
	bookID, err := dm.CreateBook(&Book{Title: "Flag Test"}, []string{"Auth Or"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	copyID, _, err := dm.AddLibraryCopy(bookID)
	if err != nil {
		t.Fatalf("AddLibraryCopy: %v", err)
	}
	if c, _ := dm.GetCopyByID(copyID); c.NeedsRelabel {
		t.Fatalf("precondition: new copy unexpectedly has needs_relabel=1")
	}

	if err := dm.FlagCopyForRelabel(copyID); err != nil {
		t.Fatalf("FlagCopyForRelabel: %v", err)
	}
	c, err := dm.GetCopyByID(copyID)
	if err != nil {
		t.Fatalf("GetCopyByID: %v", err)
	}
	if !c.NeedsRelabel {
		t.Error("NeedsRelabel still false after FlagCopyForRelabel")
	}
}

func TestFlagCopyForRelabelMissingIDReturnsNotFound(t *testing.T) {
	dm := setupTestDB(t)
	err := dm.FlagCopyForRelabel(99999)
	if !errors.Is(err, ErrCopyNotFound) {
		t.Errorf("err = %v, want ErrCopyNotFound", err)
	}
}
