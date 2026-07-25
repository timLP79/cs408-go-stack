// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"testing"
)

func TestMergePrefill_OLFullGBEmpty_OLWins(t *testing.T) {
	ol := &BookPrefill{
		Title:             "OL Title",
		Authors:           []string{"OL Author"},
		PublishYear:       1965,
		Publisher:         "OL Publisher",
		CoverURL:          "https://ol.example/cover.jpg",
		Description:       "OL description.",
		DescriptionSource: "openlibrary",
	}
	gb := &BookPrefill{}

	got := mergePrefill(ol, gb)

	if got.Title != "OL Title" || got.Authors[0] != "OL Author" || got.PublishYear != 1965 {
		t.Errorf("OL bibliographic fields should win, got %+v", got)
	}
	if got.Description != "OL description." || got.DescriptionSource != "openlibrary" {
		t.Errorf("OL description should win, got desc=%q src=%q", got.Description, got.DescriptionSource)
	}
	if got.CoverURL != "https://ol.example/cover.jpg" {
		t.Errorf("OL cover should win, got %q", got.CoverURL)
	}
	if got.CoverSource != "" {
		t.Errorf("CoverSource should stay empty when GB did not contribute, got %q", got.CoverSource)
	}
}

// TestMergePrefill_OLDeweyPreserved pins that the OL Dewey value
// survives the merge unchanged (cs408-go-stack-8vi).
func TestMergePrefill_OLDeweyPreserved(t *testing.T) {
	ol := &BookPrefill{Title: "T", Dewey: "823.92"}
	gb := &BookPrefill{Title: "T"}
	got := mergePrefill(ol, gb)
	if got.Dewey != "823.92" {
		t.Errorf("Dewey = %q, want %q (OL value preserved)", got.Dewey, "823.92")
	}
}

// TestMergePrefill_GBNeverSuppliesDewey locks the OL-only Dewey policy
// on every branch that can return a GB-sourced value: the OL+GB merge
// and the OL-miss gbOnly path. Guards against a future maintainer wiring
// GB categories into Dewey without revisiting the policy in DEC-035 /
// cs408-go-stack-8vi.
func TestMergePrefill_GBNeverSuppliesDewey(t *testing.T) {
	t.Run("OL present, GB carries Dewey", func(t *testing.T) {
		ol := &BookPrefill{Title: "T"} // no Dewey
		gb := &BookPrefill{Title: "T", Dewey: "999.99"}
		got := mergePrefill(ol, gb)
		if got.Dewey != "" {
			t.Errorf("Dewey = %q, want empty (no GB->Dewey gap-fill branch)", got.Dewey)
		}
	})

	// The gbOnly path is reachable in production via the OL-total-miss
	// plus GB-fallback branch in FetchOpenLibraryBook (openlibrary.go).
	t.Run("OL nil, gbOnly path clears Dewey", func(t *testing.T) {
		gb := &BookPrefill{Title: "T", Dewey: "999.99"}
		got := mergePrefill(nil, gb)
		if got.Dewey != "" {
			t.Errorf("Dewey = %q, want empty (gbOnly must clear a GB Dewey)", got.Dewey)
		}
	})
}

func TestMergePrefill_OLPartialNoDescGBHasDesc_GBFillsDesc(t *testing.T) {
	ol := &BookPrefill{
		Title:       "Shared Title",
		Authors:     []string{"Shared Author"},
		PublishYear: 1965,
		CoverURL:    "https://ol.example/cover.jpg",
	}
	gb := &BookPrefill{
		Title:       "GB Title (ignored)",
		Description: "GB description fills the gap.",
		CoverURL:    "https://gb.example/cover.jpg",
	}

	got := mergePrefill(ol, gb)

	if got.Title != "Shared Title" {
		t.Errorf("OL title should win, got %q", got.Title)
	}
	if got.Description != "GB description fills the gap." {
		t.Errorf("GB description should fill, got %q", got.Description)
	}
	if got.DescriptionSource != "googlebooks" {
		t.Errorf("DescriptionSource should be googlebooks, got %q", got.DescriptionSource)
	}
	if got.CoverURL != "https://ol.example/cover.jpg" {
		t.Errorf("OL cover should win when present, got %q", got.CoverURL)
	}
	if got.CoverSource != "" {
		t.Errorf("CoverSource should stay empty when OL covered it, got %q", got.CoverSource)
	}
}

func TestMergePrefill_OLEmptyGBFull_GBWinsAll(t *testing.T) {
	ol := &BookPrefill{}
	gb := &BookPrefill{
		Title:       "GB Title",
		Authors:     []string{"GB Author"},
		PublishYear: 2020,
		Publisher:   "GB Publisher",
		CoverURL:    "https://gb.example/cover.jpg",
		Description: "GB description.",
	}

	got := mergePrefill(ol, gb)

	if got.Title != "GB Title" || got.Authors[0] != "GB Author" || got.PublishYear != 2020 {
		t.Errorf("GB bibliographic fields should fill, got %+v", got)
	}
	if got.Description != "GB description." || got.DescriptionSource != "googlebooks" {
		t.Errorf("GB description should fill with source label, got desc=%q src=%q", got.Description, got.DescriptionSource)
	}
	if got.CoverURL != "https://gb.example/cover.jpg" || got.CoverSource != "googlebooks" {
		t.Errorf("GB cover should fill with source label, got url=%q src=%q", got.CoverURL, got.CoverSource)
	}
}

func TestMergePrefill_BothNil_ReturnsEmpty(t *testing.T) {
	got := mergePrefill(nil, nil)
	if got == nil {
		t.Fatalf("merge should never return nil")
	}
	if got.Title != "" || len(got.Authors) != 0 || got.Description != "" {
		t.Errorf("expected empty BookPrefill, got %+v", got)
	}
	if got.GoogleBooksError {
		t.Errorf("GoogleBooksError should be false by default")
	}
}

func TestMergePrefill_OLNilGBOnly_ReturnsGBWithSourceLabels(t *testing.T) {
	gb := &BookPrefill{
		Title:       "GB Title",
		Description: "GB desc.",
		CoverURL:    "https://gb.example/cover.jpg",
	}
	got := mergePrefill(nil, gb)
	if got.Title != "GB Title" {
		t.Errorf("GB title should pass through, got %q", got.Title)
	}
	if got.DescriptionSource != "googlebooks" {
		t.Errorf("DescriptionSource should be googlebooks, got %q", got.DescriptionSource)
	}
	if got.CoverSource != "googlebooks" {
		t.Errorf("CoverSource should be googlebooks, got %q", got.CoverSource)
	}
}

func TestMergePrefill_OLOnlyGBNil_ReturnsOL(t *testing.T) {
	ol := &BookPrefill{
		Title:             "OL Title",
		Description:       "OL desc.",
		DescriptionSource: "openlibrary",
	}
	got := mergePrefill(ol, nil)
	if got.Title != "OL Title" || got.DescriptionSource != "openlibrary" {
		t.Errorf("OL fields should pass through, got %+v", got)
	}
}

func TestMergePrefill_OLDescIsWhitespace_GBFills(t *testing.T) {
	ol := &BookPrefill{Title: "X", Description: "   \n  "}
	gb := &BookPrefill{Description: "Real description."}
	got := mergePrefill(ol, gb)
	if got.Description != "Real description." {
		t.Errorf("whitespace-only OL description should not block GB, got %q", got.Description)
	}
	if got.DescriptionSource != "googlebooks" {
		t.Errorf("DescriptionSource should be googlebooks, got %q", got.DescriptionSource)
	}
}
