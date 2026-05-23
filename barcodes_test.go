// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import "testing"

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
