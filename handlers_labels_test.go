// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// helper: drive a GET request through the test router as a logged-in
// user. Returns the response recorder so the caller can assert.
func authGet(t *testing.T, router http.Handler, path string, sess *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// helper: drive a POST request with form-encoded body + CSRF token.
func authPost(t *testing.T, router http.Handler, path string, sess *http.Cookie, csrf string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf_token", csrf)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sess)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func TestPrintLabelsFormRendersAsStaff(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_print", "staff")

	rr := authGet(t, router, "/inventory/print-labels", sess)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	wantSnippets := []string{
		"Print Labels",
		`name="source"`,
		`value="avery-5160"`,
		`value="avery-l7160"`,
		`value="roll-1x2.5"`,
		"/inventory/print-labels/render",
	}
	for _, s := range wantSnippets {
		if !strings.Contains(body, s) {
			t.Errorf("body missing %q", s)
		}
	}
}

func TestPrintLabelsRenderProducesOneSVGPerCopy(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_render", "staff")

	bookID, err := dm.CreateBook(&Book{Title: "Render Test"}, []string{"Test Author"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	for range 3 {
		if _, _, err := dm.AddLibraryCopy(bookID); err != nil {
			t.Fatalf("AddLibraryCopy: %v", err)
		}
	}

	// Filter by the specific book so the seeded fixture catalog
	// doesn't inflate the SVG count.
	url := "/inventory/print-labels/render?source=book&book_id=" + intToString(bookID)
	rr := authGet(t, router, url, sess)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	svgCount := strings.Count(body, "<svg")
	if svgCount != 3 {
		t.Errorf("got %d <svg occurrences, want 3", svgCount)
	}
	if !strings.Contains(body, "preset-avery-5160") {
		t.Errorf("body missing default preset class")
	}
}

func TestPrintLabelsRenderSingleBarcode(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_one", "staff")

	bookID, err := dm.CreateBook(&Book{Title: "Single Copy Book"}, []string{"A Author"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	_, barcode, err := dm.AddLibraryCopy(bookID)
	if err != nil {
		t.Fatalf("AddLibraryCopy: %v", err)
	}

	rr := authGet(t, router, "/inventory/print-labels/render?source=barcode&barcode="+barcode, sess)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if strings.Count(body, "<svg") != 1 {
		t.Errorf("got %d <svg, want 1", strings.Count(body, "<svg"))
	}
	if !strings.Contains(body, barcode) {
		t.Errorf("body missing barcode %q", barcode)
	}
}

func TestPrintLabelsRenderUnknownBarcodeRedirects(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_404", "staff")

	rr := authGet(t, router, "/inventory/print-labels/render?source=barcode&barcode=LSF99999998", sess)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 redirect", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Location"), "/inventory/print-labels") {
		t.Errorf("Location = %q, want /inventory/print-labels", rr.Header().Get("Location"))
	}
}

func TestPrintLabelsRenderInvalidPresetRedirects(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "staff_badpreset", "staff")

	rr := authGet(t, router, "/inventory/print-labels/render?source=all&preset=not-a-preset", sess)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rr.Code)
	}
}

func TestMarkRelabeledClearsFlagAndRedirects(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "staff_relabel", "staff")

	bookID, err := dm.CreateBook(&Book{Title: "Relabel"}, []string{"A"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	copyID, _, err := dm.AddLibraryCopy(bookID)
	if err != nil {
		t.Fatalf("AddLibraryCopy: %v", err)
	}
	if _, err := dm.db.Exec(`UPDATE copies SET needs_relabel = 1 WHERE id = ?`, copyID); err != nil {
		t.Fatalf("seed needs_relabel: %v", err)
	}

	form := url.Values{"copy_ids": {string(rune('0' + (copyID % 10)))}}
	// build a real comma-free single-id string regardless of magnitude
	form.Set("copy_ids", intToString(copyID))
	rr := authPost(t, router, "/inventory/print-labels/mark-relabeled", sess, csrf, form)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", rr.Code, rr.Body.String())
	}

	c, err := dm.GetCopyByID(copyID)
	if err != nil {
		t.Fatalf("GetCopyByID: %v", err)
	}
	if c.NeedsRelabel {
		t.Error("needs_relabel still true after MarkRelabeled POST")
	}
}

func TestLabelSettingsAdminRendersStaffRejected(t *testing.T) {
	router, dm := setupTestRouter(t)
	adminSess, _ := loginAs(t, dm, "admin_ls", "admin")
	staffSess, _ := loginAs(t, dm, "staff_ls", "staff")

	rrAdmin := authGet(t, router, "/admin/inventory/label-settings", adminSess)
	if rrAdmin.Code != http.StatusOK {
		t.Errorf("admin status = %d, want 200", rrAdmin.Code)
	}
	if !strings.Contains(rrAdmin.Body.String(), "Label Settings") {
		t.Errorf("admin body missing 'Label Settings'")
	}

	rrStaff := authGet(t, router, "/admin/inventory/label-settings", staffSess)
	if rrStaff.Code == http.StatusOK {
		t.Errorf("staff status = %d, expected non-200 (forbidden or redirect)", rrStaff.Code)
	}
}

func TestLabelSettingsPostHappyPath(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin_lsp", "admin")

	form := url.Values{
		"preset":         {"avery-l7160"},
		"offset_top_mm":  {"1.5"},
		"offset_left_mm": {"-0.5"},
	}
	rr := authPost(t, router, "/admin/inventory/label-settings", sess, csrf, form)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", rr.Code, rr.Body.String())
	}

	s, err := dm.GetLabelSettings()
	if err != nil {
		t.Fatalf("GetLabelSettings: %v", err)
	}
	if s.Preset != "avery-l7160" || s.OffsetTopMm != 1.5 || s.OffsetLeftMm != -0.5 {
		t.Errorf("settings = %+v, want preset=avery-l7160 offsets=(1.5, -0.5)", s)
	}
}

func TestLabelSettingsPostInvalidPresetRejected(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin_badp", "admin")

	form := url.Values{
		"preset":         {"nonsense"},
		"offset_top_mm":  {"0"},
		"offset_left_mm": {"0"},
	}
	rr := authPost(t, router, "/admin/inventory/label-settings", sess, csrf, form)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (with flash)", rr.Code)
	}
	// DB should still hold the default since the update rejected.
	s, err := dm.GetLabelSettings()
	if err != nil {
		t.Fatalf("GetLabelSettings: %v", err)
	}
	if s.Preset != "avery-5160" {
		t.Errorf("preset = %q, want default avery-5160 (rejected post should not mutate)", s.Preset)
	}
}

func TestLabelSettingsPostOutOfRangeOffsetRejected(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin_oor", "admin")

	form := url.Values{
		"preset":         {"avery-5160"},
		"offset_top_mm":  {"99"},
		"offset_left_mm": {"0"},
	}
	rr := authPost(t, router, "/admin/inventory/label-settings", sess, csrf, form)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rr.Code)
	}
	s, _ := dm.GetLabelSettings()
	if s.OffsetTopMm != 0 {
		t.Errorf("OffsetTopMm = %v, want 0 (rejected post should not mutate)", s.OffsetTopMm)
	}
}

func TestLabelCalibrationCrosshairCount(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, _ := loginAs(t, dm, "admin_cal", "admin")

	// Default preset is avery-5160: 3 x 10 = 30 cells, so 30 crosshair groups.
	rr := authGet(t, router, "/admin/inventory/label-settings/calibration", sess)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	count := strings.Count(body, `class="crosshair"`)
	if count != 30 {
		t.Errorf("crosshair count = %d, want 30 (default avery-5160 3x10)", count)
	}
}

func TestLabelCalibrationCrosshairCountL7160(t *testing.T) {
	router, dm := setupTestRouter(t)
	sess, csrf := loginAs(t, dm, "admin_cal2", "admin")

	// Switch the default preset to A4 L7160 (3 x 7 = 21).
	form := url.Values{
		"preset":         {"avery-l7160"},
		"offset_top_mm":  {"0"},
		"offset_left_mm": {"0"},
	}
	postRR := authPost(t, router, "/admin/inventory/label-settings", sess, csrf, form)
	if postRR.Code != http.StatusFound {
		t.Fatalf("settings post status = %d, want 302", postRR.Code)
	}

	rr := authGet(t, router, "/admin/inventory/label-settings/calibration", sess)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	count := strings.Count(rr.Body.String(), `class="crosshair"`)
	if count != 21 {
		t.Errorf("crosshair count = %d, want 21 (avery-l7160 3x7)", count)
	}
}

// intToString is a tiny helper to render an int into the form value;
// avoids importing strconv for one-off test fixtures.
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
