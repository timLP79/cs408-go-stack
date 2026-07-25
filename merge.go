// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import "strings"

// mergePrefill applies the "OL wins, GB fills gaps" rule documented in
// DEC-035. For each field, the OL value is kept if non-empty after
// strings.TrimSpace; otherwise the GB value is used. When GB fills the
// gap for Description or CoverURL, the corresponding source label
// (DescriptionSource, CoverSource) is stamped "googlebooks". Other
// source labels (e.g. "openlibrary") are the caller's responsibility;
// merge preserves whatever the OL input arrived with.
//
// Dewey is the one exception to the gap-fill rule: it is OL-only, and GB
// never supplies it on any branch (see gbOnly).
//
// Either argument may be nil. The function never returns nil.
func mergePrefill(ol, gb *BookPrefill) *BookPrefill {
	switch {
	case ol == nil && gb == nil:
		return &BookPrefill{}
	case ol == nil:
		return gbOnly(gb)
	case gb == nil:
		return olCopy(ol)
	}

	out := *ol // shallow copy; slices share backing array (not mutated below)

	if strings.TrimSpace(out.Title) == "" && gb.Title != "" {
		out.Title = gb.Title
	}
	if len(out.Authors) == 0 && len(gb.Authors) > 0 {
		out.Authors = gb.Authors
	}
	if out.PublishYear == 0 && gb.PublishYear != 0 {
		out.PublishYear = gb.PublishYear
	}
	if strings.TrimSpace(out.Publisher) == "" && gb.Publisher != "" {
		out.Publisher = gb.Publisher
	}
	if strings.TrimSpace(out.Description) == "" && gb.Description != "" {
		out.Description = gb.Description
		out.DescriptionSource = "googlebooks"
	}
	if strings.TrimSpace(out.CoverURL) == "" && gb.CoverURL != "" {
		out.CoverURL = gb.CoverURL
		out.CoverSource = "googlebooks"
	}
	return &out
}

func gbOnly(gb *BookPrefill) *BookPrefill {
	out := *gb
	// Dewey is OL-only (cs408-go-stack-8vi). normalizeGoogleBook does not
	// populate the field today, but the whole-struct copy above would
	// carry it through if that ever changed, so the invariant is enforced
	// here rather than relying on googlebooks.go leaving it zero.
	out.Dewey = ""
	if strings.TrimSpace(out.Description) != "" {
		out.DescriptionSource = "googlebooks"
	}
	if strings.TrimSpace(out.CoverURL) != "" {
		out.CoverSource = "googlebooks"
	}
	return &out
}

func olCopy(ol *BookPrefill) *BookPrefill {
	out := *ol
	return &out
}
