// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"errors"
	"fmt"
	"strings"
)

// Barcode validation error sentinels. Handlers map these to flash
// slugs; the DB layer surfaces ErrBarcodeFormatInvalid /
// ErrBarcodeFailsValidation when CreateCopyWithBarcode is called
// with a value that doesn't match the chosen format.
var (
	ErrBarcodeFormatInvalid   = errors.New("barcodes: format must be one of code128, code39, ean13, upca")
	ErrBarcodeFailsValidation = errors.New("barcodes: value does not match the chosen format")
	ErrBarcodeAlreadyExists   = errors.New("barcodes: a copy with that barcode already exists")
)

// Code 128 and Code 39 length caps. The standards themselves have no
// fixed maximum (Code 128 is variable-length; Code 39 is limited by
// printable label width). These caps are practical limits for library
// inventory: long enough to handle real-world scanned IDs, short
// enough to keep the printed label readable.
const (
	Code128MinLen = 1
	Code128MaxLen = 80
	Code39MinLen  = 1
	Code39MaxLen  = 43
)

// code39Charset is the standard Code 39 character set: uppercase
// letters, digits, space, and six punctuation characters. Code 39
// data does not include the * start/stop characters; those are added
// by the encoder when rendering.
const code39Charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 -.$/+%"

// Library barcode format (LSF): "LSF" + 7-digit zero-padded sequence +
// 1 Luhn check digit, total 11 characters. Rendered as Code 128 by the
// print pipeline. See DEC-037.
const (
	LSFPrefix      = "LSF"
	LSFSequenceLen = 7
	LSFMaxSequence = 9999999
)

// MakeLSFBarcode formats a sequence number as a library-format barcode.
// Sequence must be in 1..LSFMaxSequence; out-of-range input returns an
// error rather than producing a malformed barcode.
func MakeLSFBarcode(sequence int) (string, error) {
	if sequence < 1 || sequence > LSFMaxSequence {
		return "", fmt.Errorf("LSF sequence %d out of range [1..%d]", sequence, LSFMaxSequence)
	}
	digits := fmt.Sprintf("%07d", sequence)
	return LSFPrefix + digits + string(luhnCheckDigit(digits)), nil
}

// luhnCheckDigit computes the Luhn check digit for a string of decimal
// digits using the standard mod-10 algorithm. Appending the returned
// digit to the input produces a string that passes Luhn validation.
func luhnCheckDigit(digits string) byte {
	sum := 0
	double := true
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return byte((10-sum%10)%10) + '0'
}

// gtinCheckDigit computes the check digit for a GTIN-family payload
// (UPC-A 11-digit payload, EAN-13 12-digit payload). Algorithm:
// starting from the digit immediately left of the check position,
// multiply by alternating weights 3, 1, 3, 1, ... and sum. Check
// digit is (10 - sum%10) % 10. The same algorithm produces the
// correct check for both formats; the only difference is payload
// length.
//
// Verified by hand: UPC 036000291452 -> payload "03600029145" yields
// check '2'; EAN-13 5901234123457 -> payload "590123412345" yields
// check '7'.
func gtinCheckDigit(payload string) byte {
	sum := 0
	weight := 3
	for i := len(payload) - 1; i >= 0; i-- {
		d := int(payload[i] - '0')
		sum += d * weight
		weight = 4 - weight
	}
	return byte((10-sum%10)%10) + '0'
}

// isAllDigits reports whether s is non-empty and every byte is in '0'..'9'.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ValidateEAN13 reports whether s is exactly 13 digits with a correct
// check digit. Returns nil on success, ErrBarcodeFailsValidation on
// any length / charset / check-digit mismatch.
func ValidateEAN13(s string) error {
	if len(s) != 13 || !isAllDigits(s) {
		return ErrBarcodeFailsValidation
	}
	if gtinCheckDigit(s[:12]) != s[12] {
		return ErrBarcodeFailsValidation
	}
	return nil
}

// ValidateUPCA reports whether s is exactly 12 digits with a correct
// check digit.
func ValidateUPCA(s string) error {
	if len(s) != 12 || !isAllDigits(s) {
		return ErrBarcodeFailsValidation
	}
	if gtinCheckDigit(s[:11]) != s[11] {
		return ErrBarcodeFailsValidation
	}
	return nil
}

// ValidateCode128 reports whether s is printable ASCII (0x20..0x7E)
// within the configured length bounds. Code 128 itself supports the
// full ASCII set including control characters, but for library
// barcodes we restrict to the printable subset so the human-readable
// fallback under the barcode actually reads.
func ValidateCode128(s string) error {
	if len(s) < Code128MinLen || len(s) > Code128MaxLen {
		return ErrBarcodeFailsValidation
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7E {
			return ErrBarcodeFailsValidation
		}
	}
	return nil
}

// ValidateCode39 reports whether s contains only characters in the
// Code 39 data charset (uppercase A-Z, digits, space, and six
// punctuation marks) within the configured length bounds. The *
// start/stop character is not part of the data and is rejected.
func ValidateCode39(s string) error {
	if len(s) < Code39MinLen || len(s) > Code39MaxLen {
		return ErrBarcodeFailsValidation
	}
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune(code39Charset, rune(s[i])) {
			return ErrBarcodeFailsValidation
		}
	}
	return nil
}

// ValidateBarcode dispatches to the per-format validator. Returns
// ErrBarcodeFormatInvalid when format is not one of the four
// constants, ErrBarcodeFailsValidation when the format is valid but
// the value does not match it.
func ValidateBarcode(barcode, format string) error {
	switch format {
	case BarcodeFormatCode128:
		return ValidateCode128(barcode)
	case BarcodeFormatCode39:
		return ValidateCode39(barcode)
	case BarcodeFormatEAN13:
		return ValidateEAN13(barcode)
	case BarcodeFormatUPCA:
		return ValidateUPCA(barcode)
	}
	return ErrBarcodeFormatInvalid
}
