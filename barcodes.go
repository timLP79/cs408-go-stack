// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import "fmt"

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
