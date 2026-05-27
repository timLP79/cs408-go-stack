// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// labelSource constants name the four ways the librarian can pick
// which copies to print labels for on /inventory/print-labels.
const (
	labelSourceAll          = "all"
	labelSourceNeedsRelabel = "needs_relabel"
	labelSourceBook         = "book"
	labelSourceBarcode      = "barcode"
)

// LabelDisplay is the per-label view-model passed to print_labels_render.
// LabelData is hydrated from the DB; SVG is the inline-renderable
// barcode and AuthorPrefix is the computed spine prefix.
type LabelDisplay struct {
	LabelData
	SVG          template.HTML
	AuthorPrefix string
}

// HandlePrintLabelsForm renders the picker page on /inventory/print-labels
// (staff + admin). It populates the form's preset selector from
// label_settings so the default reflects the current calibration; the
// librarian can override per print run.
func HandlePrintLabelsForm(c *gin.Context) {
	dm := getDB(c)

	settings, err := dm.GetLabelSettings()
	if err != nil {
		log.Printf("HandlePrintLabelsForm: GetLabelSettings: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	renderTemplate(c, "print_labels_form", gin.H{
		"Title":   "Print Labels",
		"Presets": allLabelPresetsForUI(),
		"Default": settings.Preset,
		"Error":   readAndClearFlash(c, flashKindError),
		"Success": readAndClearFlash(c, flashKindSuccess),
	})
}

// HandlePrintLabelsRender resolves the source filter into a copy-id set,
// hydrates label data, renders each barcode SVG, and serves the
// printable HTML page (no chrome). The page is layout-less so the
// browser's Print dialog sees only label cells and @page rules.
//
// Source params (query string):
//   - source=all              -> every available copy
//   - source=needs_relabel    -> copies with needs_relabel = 1
//   - source=book&book_id=N   -> all copies of a single title
//   - source=barcode&barcode=...  -> a single copy by exact barcode
//
// Preset override:  preset=avery-5160 (defaults to label_settings.preset)
func HandlePrintLabelsRender(c *gin.Context) {
	dm := getDB(c)

	settings, err := dm.GetLabelSettings()
	if err != nil {
		log.Printf("HandlePrintLabelsRender: GetLabelSettings: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	presetSlug := strings.TrimSpace(c.Query("preset"))
	if presetSlug == "" {
		presetSlug = settings.Preset
	}
	preset, ok := LookupLabelPreset(presetSlug)
	if !ok {
		setFlash(c, flashKindError, "label_preset_invalid")
		c.Redirect(http.StatusFound, "/inventory/print-labels")
		return
	}

	ids, err := resolveLabelSource(c, dm)
	if err != nil {
		setFlash(c, flashKindError, err.Error())
		c.Redirect(http.StatusFound, "/inventory/print-labels")
		return
	}
	if len(ids) == 0 {
		setFlash(c, flashKindError, "label_no_copies_match")
		c.Redirect(http.StatusFound, "/inventory/print-labels")
		return
	}

	rows, err := dm.GetLabelDataByCopyIDs(ids)
	if err != nil {
		log.Printf("HandlePrintLabelsRender: GetLabelDataByCopyIDs: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	labels := make([]LabelDisplay, 0, len(rows))
	idsForRelabel := make([]int, 0, len(rows))
	for _, ld := range rows {
		svg, err := RenderBarcodeSVG(ld.Barcode, ld.BarcodeFormat)
		if err != nil {
			log.Printf("HandlePrintLabelsRender: RenderBarcodeSVG(copy %d, %s, %s): %v",
				ld.CopyID, ld.Barcode, ld.BarcodeFormat, err)
			continue
		}
		labels = append(labels, LabelDisplay{
			LabelData:    ld,
			SVG:          template.HTML(svg),
			AuthorPrefix: authorPrefix3(ld.Authors),
		})
		if ld.NeedsRelabel {
			idsForRelabel = append(idsForRelabel, ld.CopyID)
		}
	}

	renderPage(c, "print_labels_render", gin.H{
		"Title":           "Print Labels",
		"Labels":          labels,
		"Preset":          preset,
		"OffsetTopMm":     settings.OffsetTopMm,
		"OffsetLeftMm":    settings.OffsetLeftMm,
		"NeedsRelabelIDs": idsForRelabel,
		"CSRFToken":       mustCSRF(c),
	})
}

// HandleMarkRelabeled clears needs_relabel for the comma-separated copy
// ids posted from the print page's confirmation form. Staff + admin.
//
// markRelabeledMaxIDs is a defense-in-depth cap: a real print run never
// exceeds a few hundred labels, but the form payload is comma-separated
// and otherwise unbounded. Capping here keeps a malformed or hostile
// POST from triggering huge IN-clause queries that could pin SQLite.
const markRelabeledMaxIDs = 1000

func HandleMarkRelabeled(c *gin.Context) {
	dm := getDB(c)

	raw := strings.TrimSpace(c.PostForm("copy_ids"))
	if raw == "" {
		setFlash(c, flashKindError, "label_no_copies_to_mark")
		c.Redirect(http.StatusFound, "/inventory/print-labels")
		return
	}
	parts := strings.Split(raw, ",")
	if len(parts) > markRelabeledMaxIDs {
		setFlash(c, flashKindError, "label_too_many_copies")
		c.Redirect(http.StatusFound, "/inventory/print-labels")
		return
	}
	ids := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.Atoi(p)
		if err != nil {
			setFlash(c, flashKindError, "label_invalid_copy_id")
			c.Redirect(http.StatusFound, "/inventory/print-labels")
			return
		}
		ids = append(ids, id)
	}
	if err := dm.MarkCopiesRelabeled(ids); err != nil {
		log.Printf("HandleMarkRelabeled: MarkCopiesRelabeled(%v): %v", ids, err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	setFlash(c, flashKindSuccess, "label_marked_relabeled")
	setFlashDetail(c, fmt.Sprintf("%d", len(ids)))
	c.Redirect(http.StatusFound, "/inventory/print-labels")
}

// HandleLabelSettings renders the admin page for default preset and
// calibration offsets.
func HandleLabelSettings(c *gin.Context) {
	dm := getDB(c)

	settings, err := dm.GetLabelSettings()
	if err != nil {
		log.Printf("HandleLabelSettings: GetLabelSettings: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	renderTemplate(c, "label_settings", gin.H{
		"Title":    "Label Settings",
		"Settings": settings,
		"Presets":  allLabelPresetsForUI(),
		"Error":    readAndClearFlash(c, flashKindError),
		"Success":  readAndClearFlash(c, flashKindSuccess),
	})
}

// HandleLabelSettingsPost validates + persists the admin form. Bad
// inputs flash an error and redirect back to the form; success flashes
// "settings_saved" and stays on the page so the librarian can click
// Print test page right after.
func HandleLabelSettingsPost(c *gin.Context) {
	dm := getDB(c)

	preset := strings.TrimSpace(c.PostForm("preset"))
	top, terr := strconv.ParseFloat(strings.TrimSpace(c.PostForm("offset_top_mm")), 64)
	left, lerr := strconv.ParseFloat(strings.TrimSpace(c.PostForm("offset_left_mm")), 64)
	if terr != nil || lerr != nil {
		setFlash(c, flashKindError, "label_offset_not_numeric")
		c.Redirect(http.StatusFound, "/admin/inventory/label-settings")
		return
	}
	switch err := dm.UpdateLabelSettings(preset, top, left); {
	case err == nil:
		setFlash(c, flashKindSuccess, "settings_saved")
	case errors.Is(err, ErrLabelPresetInvalid):
		setFlash(c, flashKindError, "label_preset_invalid")
	case errors.Is(err, ErrLabelOffsetOutOfRange):
		setFlash(c, flashKindError, "label_offset_out_of_range")
	default:
		log.Printf("HandleLabelSettingsPost: UpdateLabelSettings: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory/label-settings")
}

// HandleLabelCalibration renders a printable crosshair sheet for the
// currently-saved preset so the librarian can measure drift against
// actual stock and dial in the offsets. Admin-only.
func HandleLabelCalibration(c *gin.Context) {
	dm := getDB(c)
	settings, err := dm.GetLabelSettings()
	if err != nil {
		log.Printf("HandleLabelCalibration: GetLabelSettings: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	preset, ok := LookupLabelPreset(settings.Preset)
	if !ok {
		log.Printf("HandleLabelCalibration: stored preset %q not registered", settings.Preset)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	// Build a flat list of cell origins (row, col) so the template can
	// iterate once and drop a crosshair group at each corner.
	type cell struct{ Row, Col int }
	cells := make([]cell, 0, preset.LabelsPerSheet())
	for r := range preset.Rows {
		for cidx := range preset.Cols {
			cells = append(cells, cell{Row: r, Col: cidx})
		}
	}
	renderPage(c, "label_calibration", gin.H{
		"Title":        "Calibration Test Page",
		"Preset":       preset,
		"OffsetTopMm":  settings.OffsetTopMm,
		"OffsetLeftMm": settings.OffsetLeftMm,
		"Cells":        cells,
	})
}

// resolveLabelSource translates the source query parameters into a
// concrete list of copy ids. Returns a flash-code error string so the
// caller can redirect with setFlash without log noise on input errors.
func resolveLabelSource(c *gin.Context, dm *DatabaseManager) ([]int, error) {
	source := strings.TrimSpace(c.Query("source"))
	switch source {
	case labelSourceAll, "":
		copies, err := dm.GetAllCopiesWithFilters("", false)
		if err != nil {
			log.Printf("resolveLabelSource(all): %v", err)
			return nil, errors.New("internal_error")
		}
		return copyIDsFromDetail(copies), nil
	case labelSourceNeedsRelabel:
		copies, err := dm.GetAllCopiesWithFilters("", true)
		if err != nil {
			log.Printf("resolveLabelSource(needs_relabel): %v", err)
			return nil, errors.New("internal_error")
		}
		return copyIDsFromDetail(copies), nil
	case labelSourceBook:
		bookID, err := strconv.Atoi(strings.TrimSpace(c.Query("book_id")))
		if err != nil {
			return nil, errors.New("label_book_id_required")
		}
		copies, err := dm.GetCopiesByBookID(bookID)
		if err != nil {
			log.Printf("resolveLabelSource(book %d): %v", bookID, err)
			return nil, errors.New("internal_error")
		}
		ids := make([]int, len(copies))
		for i, cp := range copies {
			ids[i] = cp.ID
		}
		return ids, nil
	case labelSourceBarcode:
		barcode := strings.TrimSpace(c.Query("barcode"))
		if barcode == "" {
			return nil, errors.New("label_barcode_required")
		}
		cp, err := dm.GetCopyByBarcode(barcode)
		if errors.Is(err, ErrCopyNotFound) {
			return nil, errors.New("label_barcode_unknown")
		}
		if err != nil {
			log.Printf("resolveLabelSource(barcode %q): %v", barcode, err)
			return nil, errors.New("internal_error")
		}
		return []int{cp.ID}, nil
	}
	return nil, errors.New("label_source_invalid")
}

func copyIDsFromDetail(rows []CopyDetail) []int {
	ids := make([]int, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

// PresetForUI is a small view-model: the preset's slug + a human label
// for the dropdown. The template doesn't need the millimeter fields
// for the selector itself.
type PresetForUI struct {
	Slug  string
	Label string
}

func allLabelPresetsForUI() []PresetForUI {
	labels := map[string]string{
		"avery-5160":  "Avery 5160 (US letter, 30/sheet, 1\" x 2 5/8\")",
		"avery-5161":  "Avery 5161 (US letter, 20/sheet, 1\" x 4\")",
		"avery-l7160": "Avery L7160 (A4, 21/sheet, 1\" x 2.625\")",
		"roll-1x2.5":  "Continuous roll (1\" x 2.5\", Brother/Dymo/Zebra)",
	}
	slugs := AllLabelPresetSlugs()
	out := make([]PresetForUI, len(slugs))
	for i, s := range slugs {
		out[i] = PresetForUI{Slug: s, Label: labels[s]}
	}
	return out
}

// mustCSRF returns the request's CSRF token. Render handlers use this
// to wire the "Mark as relabeled" POST form.
func mustCSRF(c *gin.Context) string {
	if v, ok := c.Get("csrfToken"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
