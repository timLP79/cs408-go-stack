// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"errors"
	"fmt"
	"strings"
)

// LabelSettings is the single-row /label_settings configuration:
// default sheet preset and printer-drift calibration offsets.
type LabelSettings struct {
	Preset       string
	OffsetTopMm  float64
	OffsetLeftMm float64
}

// LabelData is the per-copy bundle the print template needs to render
// one label. The handler hydrates this struct from
// GetLabelDataByCopyIDs, then renders the barcode SVG inline via
// RenderBarcodeSVG and the spine fields directly from the struct.
type LabelData struct {
	CopyID        int
	Barcode       string
	BarcodeFormat string
	BookTitle     string
	Authors       string
	Dewey         string
	NeedsRelabel  bool
}

// Label-settings sentinel errors. UpdateLabelSettings surfaces these
// from handler validation; the page template maps them to flashes.
var (
	ErrLabelPresetInvalid    = errors.New("labels: preset is not one of the registered slugs")
	ErrLabelOffsetOutOfRange = errors.New("labels: calibration offset must be within -10..+10 mm")
)

// Calibration offset clamp. The Avery / roll stock used in CP8 has at
// most a few mm of physical drift between machines; anything beyond
// 10mm indicates the wrong preset was selected. Hard-limit at the
// validator layer so the print page can't push a label off the sheet
// entirely.
const labelOffsetMaxAbsMm = 10.0

// GetLabelSettings returns the current single-row label_settings
// record. The row is seeded by createSchema via INSERT OR IGNORE, so a
// missing row here is unexpected; we surface the bare ErrNoRows in
// that case rather than guessing a default at the handler boundary.
func (dm *DatabaseManager) GetLabelSettings() (LabelSettings, error) {
	var s LabelSettings
	err := dm.db.QueryRow(`SELECT preset, offset_top_mm, offset_left_mm FROM label_settings WHERE id = 1`).
		Scan(&s.Preset, &s.OffsetTopMm, &s.OffsetLeftMm)
	if err != nil {
		return LabelSettings{}, fmt.Errorf("read label_settings: %w", err)
	}
	return s, nil
}

// UpdateLabelSettings writes preset + offsets to the id=1 row.
// Validates preset against the registered slugs and clamps offsets
// to the supported range; surfaces ErrLabelPresetInvalid /
// ErrLabelOffsetOutOfRange on rejection rather than corrupting the row.
func (dm *DatabaseManager) UpdateLabelSettings(preset string, offsetTopMm, offsetLeftMm float64) error {
	if _, ok := LookupLabelPreset(preset); !ok {
		return ErrLabelPresetInvalid
	}
	if offsetTopMm < -labelOffsetMaxAbsMm || offsetTopMm > labelOffsetMaxAbsMm {
		return ErrLabelOffsetOutOfRange
	}
	if offsetLeftMm < -labelOffsetMaxAbsMm || offsetLeftMm > labelOffsetMaxAbsMm {
		return ErrLabelOffsetOutOfRange
	}
	_, err := dm.db.Exec(
		`UPDATE label_settings SET preset = ?, offset_top_mm = ?, offset_left_mm = ? WHERE id = 1`,
		preset, offsetTopMm, offsetLeftMm,
	)
	if err != nil {
		return fmt.Errorf("update label_settings: %w", err)
	}
	return nil
}

// GetLabelDataByCopyIDs hydrates the per-label render bundle for the
// given copy ids. Missing ids are silently skipped (the IN clause
// matches what exists). Returns rows in the order SQLite hands them
// back; the handler is responsible for any final ordering it wants on
// the printed page.
func (dm *DatabaseManager) GetLabelDataByCopyIDs(ids []int) ([]LabelData, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	query := `
		SELECT c.id, c.barcode, c.barcode_format, c.needs_relabel,
		       b.title,
		       COALESCE(GROUP_CONCAT(a.name, ', '), '') AS authors,
		       COALESCE(b.dewey, '') AS dewey
		FROM copies c
		JOIN books b ON c.book_id = b.id
		LEFT JOIN book_authors ba ON b.id = ba.book_id
		LEFT JOIN authors a ON ba.author_id = a.id
		WHERE c.id IN (` + placeholders + `)
		GROUP BY c.id
		ORDER BY b.title, c.id`
	rows, err := dm.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query label data: %w", err)
	}
	defer rows.Close()

	var out []LabelData
	for rows.Next() {
		var ld LabelData
		if err := rows.Scan(&ld.CopyID, &ld.Barcode, &ld.BarcodeFormat, &ld.NeedsRelabel,
			&ld.BookTitle, &ld.Authors, &ld.Dewey); err != nil {
			return nil, fmt.Errorf("scan label data: %w", err)
		}
		out = append(out, ld)
	}
	return out, rows.Err()
}

// FlagCopyForRelabel sets needs_relabel = 1 on a single copy. Returns
// ErrCopyNotFound when the id does not exist; the print-labels page
// later clears the flag automatically after a successful print run via
// MarkCopiesRelabeled.
func (dm *DatabaseManager) FlagCopyForRelabel(copyID int) error {
	res, err := dm.db.Exec(`UPDATE copies SET needs_relabel = 1 WHERE id = ?`, copyID)
	if err != nil {
		return fmt.Errorf("flag copy %d for relabel: %w", copyID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrCopyNotFound
	}
	return nil
}

// MarkCopiesRelabeled clears needs_relabel on the given copy ids.
// Missing ids are tolerated (UPDATE just affects zero rows). Empty
// input is a no-op.
func (dm *DatabaseManager) MarkCopiesRelabeled(ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := dm.db.Exec(`UPDATE copies SET needs_relabel = 0 WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return fmt.Errorf("mark copies relabeled: %w", err)
	}
	return nil
}
