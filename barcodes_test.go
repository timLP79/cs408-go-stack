// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"strings"
	"testing"
)

func TestLuhnCheckDigit(t *testing.T) {
	// Reference cases. Hand-computed using the standard mod-10
	// algorithm: starting from the rightmost payload digit, double
	// every other digit (subtracting 9 if the result > 9), sum, then
	// check = (10 - sum%10) % 10.
	cases := []struct {
		payload string
		want    byte
	}{
		{"7992739871", '3'}, // canonical Wikipedia Luhn example
		{"0", '0'},
		{"1", '8'},
		{"9", '1'},
		{"0000001", '8'},
		{"0000017", '4'},
		{"9999999", '7'},
	}
	for _, tc := range cases {
		got := luhnCheckDigit(tc.payload)
		if got != tc.want {
			t.Errorf("luhnCheckDigit(%q) = %q, want %q", tc.payload, got, tc.want)
		}
	}
}

func TestMakeLSFBarcode(t *testing.T) {
	cases := []struct {
		seq     int
		want    string
		wantErr bool
	}{
		{1, "LSF00000018", false},
		{17, "LSF00000174", false},
		{9999999, "LSF99999997", false},
		{0, "", true},
		{-1, "", true},
		{10000000, "", true},
	}
	for _, tc := range cases {
		got, err := MakeLSFBarcode(tc.seq)
		if tc.wantErr {
			if err == nil {
				t.Errorf("MakeLSFBarcode(%d): want error, got %q", tc.seq, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("MakeLSFBarcode(%d): unexpected error: %v", tc.seq, err)
			continue
		}
		if got != tc.want {
			t.Errorf("MakeLSFBarcode(%d) = %q, want %q", tc.seq, got, tc.want)
		}
		if len(got) != 11 {
			t.Errorf("MakeLSFBarcode(%d) = %q has length %d, want 11", tc.seq, got, len(got))
		}
	}
}

func TestMakeLSFBarcodeUniqueness(t *testing.T) {
	// All barcodes for sequence 1..1000 should be unique. Guards
	// against a regression that drops the sequence portion.
	seen := make(map[string]int, 1000)
	for seq := 1; seq <= 1000; seq++ {
		bc, err := MakeLSFBarcode(seq)
		if err != nil {
			t.Fatalf("MakeLSFBarcode(%d): %v", seq, err)
		}
		if prev, ok := seen[bc]; ok {
			t.Errorf("barcode %q produced for both sequence %d and %d", bc, prev, seq)
		}
		seen[bc] = seq
	}
}

// ---------- GTIN check digit + EAN-13 + UPC-A ----------

func TestGTINCheckDigit(t *testing.T) {
	// Reference cases hand-verified against published numbers.
	cases := []struct {
		payload string
		want    byte
		label   string
	}{
		{"03600029145", '2', "UPC-A 036000291452 (canonical)"},
		{"590123412345", '7', "EAN-13 5901234123457 (canonical)"},
		{"00000000000", '0', "UPC-A all zeros payload"},
		{"000000000000", '0', "EAN-13 all zeros payload"},
	}
	for _, tc := range cases {
		if got := gtinCheckDigit(tc.payload); got != tc.want {
			t.Errorf("%s: gtinCheckDigit(%q) = %q, want %q", tc.label, tc.payload, got, tc.want)
		}
	}
}

func TestValidateEAN13(t *testing.T) {
	cases := []struct {
		s       string
		wantErr bool
		label   string
	}{
		{"5901234123457", false, "valid"},
		{"9780141439518", false, "real ISBN (Pride and Prejudice)"},
		{"9780062316097", false, "real ISBN (Behave)"},
		{"5901234123456", true, "valid digits but wrong check"},
		{"123456789012", true, "too short (12 digits)"},
		{"12345678901234", true, "too long (14 digits)"},
		{"5901234A23457", true, "letter inside digits"},
		{"", true, "empty"},
		{"             ", true, "spaces"},
	}
	for _, tc := range cases {
		err := ValidateEAN13(tc.s)
		if tc.wantErr && err == nil {
			t.Errorf("%s: ValidateEAN13(%q) = nil, want error", tc.label, tc.s)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: ValidateEAN13(%q) = %v, want nil", tc.label, tc.s, err)
		}
	}
}

func TestValidateUPCA(t *testing.T) {
	cases := []struct {
		s       string
		wantErr bool
		label   string
	}{
		{"036000291452", false, "valid (canonical)"},
		{"012345678905", false, "valid"},
		{"036000291451", true, "wrong check digit"},
		{"03600029145", true, "too short (11 digits)"},
		{"0360002914520", true, "too long (13 digits)"},
		{"03600029145A", true, "letter at check position"},
		{"", true, "empty"},
	}
	for _, tc := range cases {
		err := ValidateUPCA(tc.s)
		if tc.wantErr && err == nil {
			t.Errorf("%s: ValidateUPCA(%q) = nil, want error", tc.label, tc.s)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: ValidateUPCA(%q) = %v, want nil", tc.label, tc.s, err)
		}
	}
}

// ---------- Code 128 + Code 39 ----------

func TestValidateCode128(t *testing.T) {
	cases := []struct {
		s       string
		wantErr bool
		label   string
	}{
		{"LSF00000018", false, "library-format code"},
		{"abc123!@#", false, "printable ASCII"},
		{"A", false, "single char (minimum)"},
		{"", true, "empty (below min)"},
		{"hello\tworld", true, "contains tab (control)"},
		{"hello\nworld", true, "contains newline"},
		{"café", true, "non-ASCII (é)"},
	}
	for _, tc := range cases {
		err := ValidateCode128(tc.s)
		if tc.wantErr && err == nil {
			t.Errorf("%s: ValidateCode128(%q) = nil, want error", tc.label, tc.s)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: ValidateCode128(%q) = %v, want nil", tc.label, tc.s, err)
		}
	}

	// Length cap: at-max accepted, one-over-max rejected.
	atMax := strings.Repeat("A", Code128MaxLen)
	if err := ValidateCode128(atMax); err != nil {
		t.Errorf("Code128 at max length should pass, got %v", err)
	}
	if err := ValidateCode128(atMax + "A"); err == nil {
		t.Errorf("Code128 one over max length should fail")
	}
}

func TestValidateCode39(t *testing.T) {
	cases := []struct {
		s       string
		wantErr bool
		label   string
	}{
		{"HELLO", false, "uppercase"},
		{"HELLO 123", false, "uppercase + digits + space"},
		{"BOOK-001", false, "with hyphen"},
		{"A.B$C/D+E%", false, "all six punctuation"},
		{"hello", true, "lowercase rejected"},
		{"BOOK_001", true, "underscore not in charset"},
		{"BOOK*001", true, "star (start/stop) not in data charset"},
		{"BOOK@001", true, "at-sign rejected"},
		{"", true, "empty"},
	}
	for _, tc := range cases {
		err := ValidateCode39(tc.s)
		if tc.wantErr && err == nil {
			t.Errorf("%s: ValidateCode39(%q) = nil, want error", tc.label, tc.s)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: ValidateCode39(%q) = %v, want nil", tc.label, tc.s, err)
		}
	}
}

func TestValidateBarcodeDispatch(t *testing.T) {
	cases := []struct {
		barcode string
		format  string
		wantErr error
		label   string
	}{
		{"LSF00000018", "code128", nil, "code128 dispatches to ValidateCode128"},
		{"5901234123457", "ean13", nil, "ean13 dispatches"},
		{"036000291452", "upca", nil, "upca dispatches"},
		{"BOOK-001", "code39", nil, "code39 dispatches"},
		{"5901234123457", "garbage_format", ErrBarcodeFormatInvalid, "unknown format -> ErrBarcodeFormatInvalid"},
		{"", "", ErrBarcodeFormatInvalid, "empty format -> ErrBarcodeFormatInvalid"},
		{"5901234123456", "ean13", ErrBarcodeFailsValidation, "ean13 wrong check -> ErrBarcodeFailsValidation"},
	}
	for _, tc := range cases {
		err := ValidateBarcode(tc.barcode, tc.format)
		if tc.wantErr == nil {
			if err != nil {
				t.Errorf("%s: got %v, want nil", tc.label, err)
			}
			continue
		}
		if err != tc.wantErr {
			t.Errorf("%s: got %v, want %v", tc.label, err, tc.wantErr)
		}
	}
}
