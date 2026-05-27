// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"fmt"
	"strings"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/code39"
	"github.com/boombuler/barcode/ean"
)

// LabelPreset describes a physical sheet/roll of label stock. All
// distances are in millimeters; the print CSS converts mm to the
// browser's actual print units via @page rules.
type LabelPreset struct {
	Slug         string
	Paper        string // "letter", "A4", "roll"
	Cols         int
	Rows         int
	CellWmm      float64
	CellHmm      float64
	PageWmm      float64
	PageHmm      float64
	MarginTopmm  float64
	MarginLeftmm float64
	ColGapmm     float64
	RowGapmm     float64
}

// LabelsPerSheet returns the number of label cells on one sheet.
// For continuous-roll presets the value is 1 (one label per "page").
func (p LabelPreset) LabelsPerSheet() int { return p.Cols * p.Rows }

// Avery dimensions sourced from the manufacturer specs; A4 L7160 from
// Avery UK. Margins are nominal — the calibration offsets on
// label_settings let the librarian dial in printer drift per machine.
var labelPresets = map[string]LabelPreset{
	"avery-5160": {
		Slug: "avery-5160", Paper: "letter", Cols: 3, Rows: 10,
		CellWmm: 66.675, CellHmm: 25.4,
		PageWmm: 215.9, PageHmm: 279.4,
		MarginTopmm: 12.7, MarginLeftmm: 4.7625,
		ColGapmm: 3.175, RowGapmm: 0.0,
	},
	"avery-5161": {
		Slug: "avery-5161", Paper: "letter", Cols: 2, Rows: 10,
		CellWmm: 101.6, CellHmm: 25.4,
		PageWmm: 215.9, PageHmm: 279.4,
		MarginTopmm: 12.7, MarginLeftmm: 4.7625,
		ColGapmm: 4.7625, RowGapmm: 0.0,
	},
	"avery-l7160": {
		Slug: "avery-l7160", Paper: "A4", Cols: 3, Rows: 7,
		CellWmm: 63.5, CellHmm: 38.1,
		PageWmm: 210.0, PageHmm: 297.0,
		MarginTopmm: 15.15, MarginLeftmm: 7.0,
		ColGapmm: 2.5, RowGapmm: 0.0,
	},
	"roll-1x2.5": {
		Slug: "roll-1x2.5", Paper: "roll", Cols: 1, Rows: 1,
		CellWmm: 63.5, CellHmm: 25.4,
		PageWmm: 63.5, PageHmm: 25.4,
		MarginTopmm: 0.0, MarginLeftmm: 0.0,
		ColGapmm: 0.0, RowGapmm: 0.0,
	},
}

// presetOrder is the canonical display order for the four CP8 presets.
// AllLabelPresetSlugs returns this slice so menu rendering is stable.
var presetOrder = []string{"avery-5160", "avery-5161", "avery-l7160", "roll-1x2.5"}

// LookupLabelPreset returns the preset for slug. Ok is false when slug
// is not a registered preset.
func LookupLabelPreset(slug string) (LabelPreset, bool) {
	p, ok := labelPresets[slug]
	return p, ok
}

// AllLabelPresetSlugs returns the registered preset slugs in display
// order. Callers must not mutate the returned slice.
func AllLabelPresetSlugs() []string {
	out := make([]string, len(presetOrder))
	copy(out, presetOrder)
	return out
}

// authorPrefix3 returns the 3-character uppercase prefix of the last
// whitespace-separated token of author. Empty author or whitespace-only
// input returns empty string. Tokens shorter than 3 characters pass
// through at their natural length (still uppercased).
//
// Last-token rule example: "Ursula K. Le Guin" -> "Guin" -> "GUI".
// This is imperfect for compound surnames like "Le Guin" or
// "van der Berg" but is the conventional library spine-label rule.
func authorPrefix3(author string) string {
	fields := strings.Fields(author)
	if len(fields) == 0 {
		return ""
	}
	// Slice by runes, not bytes, so multi-byte UTF-8 surnames
	// (e.g. "Ñoño") don't get truncated mid-codepoint into a
	// malformed string that breaks ToUpper or the template.
	runes := []rune(fields[len(fields)-1])
	if len(runes) > 3 {
		runes = runes[:3]
	}
	return strings.ToUpper(string(runes))
}

// RenderBarcodeSVG returns a self-contained SVG string for value
// rendered in format. The SVG uses viewBox "0 0 <bar-count> 1" with
// preserveAspectRatio="none", so the consumer's CSS controls final
// display size. Returns ErrBarcodeFailsValidation or
// ErrBarcodeFormatInvalid for inputs that fail the same per-format
// rules as the Add-Copy validators.
func RenderBarcodeSVG(value, format string) (string, error) {
	if err := ValidateBarcode(value, format); err != nil {
		return "", err
	}
	var bc barcode.Barcode
	var err error
	switch format {
	case BarcodeFormatCode128:
		bc, err = code128.Encode(value)
	case BarcodeFormatCode39:
		bc, err = code39.Encode(value, false, false)
	case BarcodeFormatEAN13:
		bc, err = ean.Encode(value)
	case BarcodeFormatUPCA:
		// UPC-A is structurally EAN-13 with a leading zero; the bar
		// pattern is identical so we encode via the EAN package.
		bc, err = ean.Encode("0" + value)
	default:
		return "", ErrBarcodeFormatInvalid
	}
	if err != nil {
		return "", fmt.Errorf("encode %s: %w", format, err)
	}
	return svgFromBarcode(bc), nil
}

// svgFromBarcode walks the boombuler barcode's top row, coalescing
// runs of black columns into <rect> elements. boombuler renders 1D
// barcodes with uniform vertical extent, so reading only y=Min.Y
// captures the full bar pattern. 2D barcodes (QR, DataMatrix, etc.)
// would break this assumption; we restrict the format dispatch above
// to 1D-only.
func svgFromBarcode(bc barcode.Barcode) string {
	b := bc.Bounds()
	width := b.Dx()
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d 1" preserveAspectRatio="none" shape-rendering="crispEdges">`, width)
	runStart := -1
	flush := func(end int) {
		if runStart < 0 {
			return
		}
		fmt.Fprintf(&sb, `<rect x="%d" y="0" width="%d" height="1" fill="#000"/>`, runStart, end-runStart)
		runStart = -1
	}
	for x := range width {
		// boombuler's 1D output is fully opaque monochrome, so alpha
		// can be discarded; pure black (0,0,0) is a bar, anything else
		// (i.e. pure white) is a space. A future 2D format would need
		// a different sampling strategy, but RenderBarcodeSVG above
		// hard-gates the dispatch to 1D formats only.
		r, g, blue, _ := bc.At(x+b.Min.X, b.Min.Y).RGBA()
		isBar := r == 0 && g == 0 && blue == 0
		if isBar {
			if runStart < 0 {
				runStart = x
			}
		} else {
			flush(x)
		}
	}
	flush(width)
	sb.WriteString("</svg>")
	return sb.String()
}
