// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"strings"
	"testing"
)

func TestAuthorPrefix3(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single-name surname", "Jane Austen", "AUS"},
		{"initials before surname", "F. Scott Fitzgerald", "FIT"},
		{"compound surname last-token rule", "Ursula K. Le Guin", "GUI"},
		{"single name", "Madonna", "MAD"},
		{"short surname under 3 chars", "Bo Bo", "BO"},
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
		{"trailing whitespace", "Mark Twain   ", "TWA"},
		{"lowercase input", "george orwell", "ORW"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := authorPrefix3(tc.in)
			if got != tc.want {
				t.Errorf("authorPrefix3(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLabelPresetLookup(t *testing.T) {
	cases := []struct {
		slug  string
		ok    bool
		paper string
		cols  int
		rows  int
	}{
		{"avery-5160", true, "letter", 3, 10},
		{"avery-5161", true, "letter", 2, 10},
		{"avery-l7160", true, "A4", 3, 7},
		{"roll-1x2.5", true, "roll", 1, 1},
		{"unknown", false, "", 0, 0},
		{"", false, "", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			p, ok := LookupLabelPreset(tc.slug)
			if ok != tc.ok {
				t.Fatalf("LookupLabelPreset(%q) ok=%v, want %v", tc.slug, ok, tc.ok)
			}
			if !ok {
				return
			}
			if p.Paper != tc.paper {
				t.Errorf("paper = %q, want %q", p.Paper, tc.paper)
			}
			if p.Cols != tc.cols || p.Rows != tc.rows {
				t.Errorf("grid = %dx%d, want %dx%d", p.Cols, p.Rows, tc.cols, tc.rows)
			}
			if p.LabelsPerSheet() != tc.cols*tc.rows {
				t.Errorf("LabelsPerSheet() = %d, want %d", p.LabelsPerSheet(), tc.cols*tc.rows)
			}
		})
	}
}

func TestAllLabelPresetsRegistered(t *testing.T) {
	want := []string{"avery-5160", "avery-5161", "avery-l7160", "roll-1x2.5"}
	got := AllLabelPresetSlugs()
	if len(got) != len(want) {
		t.Fatalf("AllLabelPresetSlugs len = %d, want %d (%v)", len(got), len(want), got)
	}
	seen := map[string]bool{}
	for _, s := range got {
		seen[s] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("missing preset %q in AllLabelPresetSlugs", w)
		}
	}
}

func TestRenderBarcodeSVG(t *testing.T) {
	cases := []struct {
		name   string
		value  string
		format string
		ok     bool
	}{
		{"code128 LSF library label", "LSF00000018", BarcodeFormatCode128, true},
		{"code39 uppercase alnum", "ABC123", BarcodeFormatCode39, true},
		{"ean13 valid check digit", "5901234123457", BarcodeFormatEAN13, true},
		{"upca valid check digit", "036000291452", BarcodeFormatUPCA, true},
		{"invalid format", "anything", "qrcode", false},
		{"empty value code128", "", BarcodeFormatCode128, false},
		{"bad ean13 check digit", "5901234123450", BarcodeFormatEAN13, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svg, err := RenderBarcodeSVG(tc.value, tc.format)
			if tc.ok {
				if err != nil {
					t.Fatalf("RenderBarcodeSVG(%q,%q) err = %v, want nil", tc.value, tc.format, err)
				}
				if !strings.HasPrefix(svg, "<svg") {
					t.Errorf("svg does not start with <svg: %q", svg[:min(50, len(svg))])
				}
				if !strings.Contains(svg, "<rect") {
					t.Error("svg contains no <rect> bars")
				}
				if !strings.HasSuffix(strings.TrimSpace(svg), "</svg>") {
					t.Error("svg does not end with </svg>")
				}
				if !strings.Contains(svg, `xmlns="http://www.w3.org/2000/svg"`) {
					t.Error("svg missing xmlns")
				}
				if !strings.Contains(svg, `viewBox=`) {
					t.Error("svg missing viewBox")
				}
			} else {
				if err == nil {
					t.Errorf("RenderBarcodeSVG(%q,%q) err = nil, want non-nil", tc.value, tc.format)
				}
			}
		})
	}
}
