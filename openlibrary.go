// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// openLibraryBaseURL, openLibraryHost, and openLibraryCoversHost are
// vars (not const) so tests can swap them at the httptest.Server URL
// via t.Cleanup(...) without spinning up a real network proxy.
// Production callers never mutate them.
//
//	openLibraryBaseURL    Books API endpoint (/api/books).
//	openLibraryHost       Host root used for /works/X.json fetches.
//	openLibraryCoversHost The covers.openlibrary.org host. We HEAD-probe
//	                      /b/isbn/<isbn>-L.jpg?default=false as the
//	                      final cover fallback, because that endpoint
//	                      resolves a cover whenever OL has ANY image
//	                      indexed under the ISBN -- regardless of
//	                      whether the edition's specific work record
//	                      is the canonical one (OL frequently has
//	                      duplicate work records for the same book,
//	                      and a sparse edition can point at a
//	                      coverless work even when a sibling work has
//	                      covers).
var (
	openLibraryBaseURL    = "https://openlibrary.org/api/books"
	openLibraryHost       = "https://openlibrary.org"
	openLibraryCoversHost = "https://covers.openlibrary.org"
)

const (
	openLibraryUserAgent = "LibreShelf/0.1 (+https://github.com/timLP79/LibreShelf)"
	openLibraryTimeout   = 10 * time.Second
)

var ErrOpenLibraryNotFound = errors.New("open library: isbn not found")

// Distinguishes OL-miss + GB-error (retryable) from OL-miss + GB-miss
// (which uses ErrOpenLibraryNotFound).
var ErrExternalSourcesUnavailable = errors.New("open library: external sources unavailable")

// BookPrefill is the merged-source prefill payload returned by
// HandleOpenLibraryLookup. Fields originate from the Open Library chain
// (jscmd=details, work record, jscmd=data) and from the Google Books
// volumes API when GOOGLE_BOOKS_API_KEY is configured. The merge rule is
// "OL wins, GB fills gaps": each field uses the OL value if non-empty
// after strings.TrimSpace, otherwise the GB value. DescriptionSource and
// CoverSource carry the per-field origin so the admin Lookup JS can show
// a "via Google Books" hint on the staged banner. GoogleBooksError is
// true when GB was attempted (key set, gap detected) but failed; the JS
// uses this to render a small "Google Books unavailable" note.
type BookPrefill struct {
	Title             string   `json:"title,omitempty"`
	Authors           []string `json:"authors,omitempty"`
	PublishYear       int      `json:"publish_year,omitempty"`
	Publisher         string   `json:"publisher,omitempty"`
	Dewey             string   `json:"dewey,omitempty"`
	CoverURL          string   `json:"cover_url,omitempty"`
	Description       string   `json:"description,omitempty"`
	DescriptionSource string   `json:"description_source,omitempty"`
	CoverSource       string   `json:"cover_source,omitempty"`
	GoogleBooksError  bool     `json:"google_books_error,omitempty"`
}

// olResponse mirrors the jscmd=details envelope returned by
// https://openlibrary.org/api/books?jscmd=details. Switched from
// jscmd=data because data does not include descriptions; details does
// (under .details.description) and additionally returns cover IDs we
// can format into our preferred -L.jpg URL ourselves.
type olResponse map[string]olEntry

type olEntry struct {
	Details olBook `json:"details"`
}

// olBook is the shape of the `.details` sub-object on the jscmd=details
// response. Several fields differ from the jscmd=data shape: publishers
// is []string not []{name string}, the covers field is []int (cover
// IDs we format into URLs ourselves), and description appears here
// at all (data does not return descriptions).
//
// OL is inconsistent about how it credits authors. Some records have
// `authors`: an array of {key, name} objects in "First Last" form.
// Others have `author`: a flat string array in catalog-card form like
// "Austen, Jane, 1775-1817." Many sparse edition records (e.g. Dell
// paperbacks) have NEITHER -- the author refs live only on the work
// record. We read both edition shapes and fall back to a second
// jscmd=data call when the edition is sparse.
type olBook struct {
	Title       string        `json:"title"`
	Authors     []olAuthor    `json:"authors"`
	Author      []string      `json:"author"`
	Publishers  []string      `json:"publishers"`
	PublishDate string        `json:"publish_date"`
	Covers      []int         `json:"covers"`
	Description olDescription `json:"description"`
	Works       []olWorkRef   `json:"works"`
	// DeweyDecimalClass is OL's classification array; present on some
	// edition records, absent on many. We consume the first non-empty
	// value opportunistically (cs408-go-stack-8vi). GB never supplies
	// Dewey, so there is no source-label tracking for this field.
	DeweyDecimalClass []string `json:"dewey_decimal_class"`
}

type olAuthor struct {
	Name string `json:"name"`
}

// olWorkRef points at the abstract "work" record for an edition.
// Edition records carry one entry: works: [{"key": "/works/OL...W"}].
// We follow this key when the edition itself is missing a description.
type olWorkRef struct {
	Key string `json:"key"`
}

// olDescription unmarshals OL's description field, which is either a
// bare string (older records) or a typed-text object of the form
// {"type": "/type/text", "value": "..."} (newer records). Both yield
// a plain string for the caller.
type olDescription struct {
	Value string
}

func (d *olDescription) UnmarshalJSON(data []byte) error {
	// Try the typed-text object shape first.
	var obj struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &obj); err == nil && obj.Value != "" {
		d.Value = obj.Value
		return nil
	}
	// Fall through to the bare-string shape.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		d.Value = s
		return nil
	}
	// Anything else (null, number, etc.) -> leave empty, do not error.
	// OL records are heterogeneous; refusing to parse one stray record
	// would block the whole lookup.
	d.Value = ""
	return nil
}

// olCoverURLTemplate builds the large cover URL for a given OL cover
// ID. The covers endpoint takes the raw ID and a size suffix
// (S/M/L). We always request L so the cover-storage pipeline gets
// the highest available resolution.
const olCoverURLTemplate = "https://covers.openlibrary.org/b/id/%d-L.jpg"

var yearRegex = regexp.MustCompile(`\b(1[5-9]\d{2}|20\d{2}|2100)\b`)

func parsePublishYear(s string) int {
	match := yearRegex.FindString(s)
	if match == "" {
		return 0
	}
	n, err := strconv.Atoi(match)
	if err != nil {
		return 0
	}
	return n
}

func stripISBNFormatting(s string) string {
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

func normalizeOpenLibraryBook(b olBook) *BookPrefill {
	out := &BookPrefill{
		Title:       b.Title,
		PublishYear: parsePublishYear(b.PublishDate),
		Description: strings.TrimSpace(b.Description.Value),
	}
	// Prefer the structured `authors` array when present. Fall back to
	// `author` (singular) for records that only carry the catalog-card
	// form like "Austen, Jane, 1775-1817." -- those need flipping to
	// "First Last" before they're useful in our form.
	for _, a := range b.Authors {
		if a.Name != "" {
			out.Authors = append(out.Authors, a.Name)
		}
	}
	if len(out.Authors) == 0 {
		for _, raw := range b.Author {
			if name := normalizeOLAuthorString(raw); name != "" {
				out.Authors = append(out.Authors, name)
			}
		}
	}
	if len(b.Publishers) > 0 {
		out.Publisher = b.Publishers[0]
	}
	if len(b.Covers) > 0 && b.Covers[0] > 0 {
		out.CoverURL = fmt.Sprintf(olCoverURLTemplate, b.Covers[0])
	}
	// Dewey: take the first non-empty, trimmed classification entry. OL
	// records that lack the field leave Dewey empty; manual entry on the
	// book form always wins downstream.
	for _, d := range b.DeweyDecimalClass {
		if v := strings.TrimSpace(d); v != "" {
			out.Dewey = v
			break
		}
	}
	return out
}

// normalizeOLAuthorString flips OL's catalog-card author form
// ("Austen, Jane, 1775-1817.") into "First Last" and drops the trailing
// date range. Algorithm:
//  1. Strip trailing period.
//  2. Split on the first comma.
//  3. The portion after the first comma may itself contain a date or
//     a role marker, separated by another comma. Take everything up to
//     that next comma as the first-name segment.
//  4. If the first-name segment looks like a date range (begins with a
//     digit), there is no useful first name; return the last-name
//     segment as-is.
//  5. Otherwise return "<first> <last>".
//
// Records with no comma (e.g. "Anonymous") pass through unchanged.
func normalizeOLAuthorString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Drop a trailing period only when the char before it is a digit,
	// i.e. it terminates a date range. A period after an initial
	// (e.g. "Tolkien, J. R. R.") must survive.
	if len(s) >= 2 && s[len(s)-1] == '.' && s[len(s)-2] >= '0' && s[len(s)-2] <= '9' {
		s = strings.TrimSpace(s[:len(s)-1])
	}

	commaIdx := strings.Index(s, ",")
	if commaIdx == -1 {
		return s
	}

	last := strings.TrimSpace(s[:commaIdx])
	rest := strings.TrimSpace(s[commaIdx+1:])
	if rest == "" {
		return last
	}

	// Take the rest up to the next comma so a trailing date or role
	// suffix is dropped.
	if next := strings.Index(rest, ","); next >= 0 {
		rest = strings.TrimSpace(rest[:next])
	}

	if rest == "" {
		return last
	}
	if rest[0] >= '0' && rest[0] <= '9' {
		// The "first name" slot is actually a date range. Author has
		// only a last name in the record.
		return last
	}
	return rest + " " + last
}

// FetchOpenLibraryBookGated is the offline-aware entry point for the
// admin Lookup path and any future caller that should respect the
// operator's offline-mode declaration. Returns ErrExternalDisabled
// without making any HTTP attempt when external calls are blocked.
//
// Tests that need to drive the OL chain against httptest.NewServer
// should keep calling FetchOpenLibraryBook directly.
func FetchOpenLibraryBookGated(ctx context.Context, dm *DatabaseManager, isbn string) (*BookPrefill, error) {
	if !IsExternalAllowed(dm) {
		return nil, ErrExternalDisabled
	}
	return FetchOpenLibraryBook(ctx, isbn)
}

func FetchOpenLibraryBook(ctx context.Context, isbn string) (*BookPrefill, error) {
	bibkey := "ISBN:" + stripISBNFormatting(isbn)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openLibraryBaseURL, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("bibkeys", bibkey)
	q.Set("format", "json")
	q.Set("jscmd", "details")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("User-Agent", openLibraryUserAgent)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: openLibraryTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open library request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open library returned status %d", resp.StatusCode)
	}

	var payload olResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("open library decode: %w", err)
	}

	entry, ok := payload[bibkey]
	if !ok {
		// OL has no record for this ISBN. Fall back to Google Books if
		// configured -- this is the "total-miss fallback" case from
		// DEC-035. The merge function's gbOnly path stamps source labels
		// so the JS banner can show "Some fields via Google Books."
		if IsGoogleBooksConfigured() {
			gbResult, gbErr := FetchByISBN(ctx, isbn)
			switch {
			case gbErr == nil && gbResult != nil:
				return mergePrefill(nil, gbResult), nil
			case errors.Is(gbErr, ErrGoogleBooksNotFound), errors.Is(gbErr, ErrGoogleBooksDisabled):
				// Neither source has it; fall through to NotFound.
			default:
				log.Printf("openlibrary: google books fetch for %s (OL miss): %v", isbn, gbErr)
				return nil, ErrExternalSourcesUnavailable
			}
		}
		return nil, ErrOpenLibraryNotFound
	}

	book := normalizeOpenLibraryBook(entry.Details)

	// Fallbacks 1 + 2 (work fetch for description/cover, jscmd=data
	// for authors) are both triggered by gaps in the edition response
	// but are independent of each other. Fire them in parallel via
	// goroutines so the worst-case Lookup latency (sparse Dell-style
	// edition) drops from ~1100ms sequential to ~700ms.
	//
	// The ISBN-cover HEAD probe (the third potential fallback)
	// depends on the work fetch's result (only fires when the work
	// also has no covers), so it stays sequential after these two
	// concurrent fetches complete.
	needWork := (book.Description == "" || book.CoverURL == "") && len(entry.Details.Works) > 0
	needDataAuthors := len(book.Authors) == 0

	var (
		wg     sync.WaitGroup
		workMu sync.Mutex // guards workResult / workErr
		// Closure-captured results from the two concurrent fetches.
		// Read after wg.Wait() so no further synchronization needed
		// then.
		workResult  *olWorkRecord
		workErr     error
		authorNames []string
		authorErr   error
	)
	if needWork {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := fetchOLWork(ctx, entry.Details.Works[0].Key)
			workMu.Lock()
			workResult, workErr = r, err
			workMu.Unlock()
		}()
	}
	if needDataAuthors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			names, err := fetchOLDataAuthors(ctx, bibkey)
			// authorNames + authorErr are only written by this one
			// goroutine and only read after Wait, so no mutex needed.
			authorNames, authorErr = names, err
		}()
	}

	// GB fans out alongside the OL work/jscmd=data fallbacks. Only
	// fires when (1) the API key is configured AND (2) the OL response
	// has at least one prefill gap (any field empty). The gap check
	// happens here against the *current* OL state (after the edition
	// parse but before the work/data fallbacks merge in), which is
	// conservative -- the GB call may end up unused if work/data later
	// fill all the gaps. Acceptable trade for the latency win of the
	// parallel fan-out.
	var (
		gbResult *BookPrefill
		gbErr    error
	)
	needGB := IsGoogleBooksConfigured() && hasAnyGap(book)
	if needGB {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gbResult, gbErr = FetchByISBN(ctx, isbn)
		}()
	}

	wg.Wait()

	if needWork {
		if workErr != nil {
			log.Printf("openlibrary: work fetch for %s: %v", entry.Details.Works[0].Key, workErr)
		} else if workResult != nil {
			if book.Description == "" && workResult.Description != "" {
				book.Description = workResult.Description
			}
			if book.CoverURL == "" && len(workResult.Covers) > 0 && workResult.Covers[0] > 0 {
				book.CoverURL = fmt.Sprintf(olCoverURLTemplate, workResult.Covers[0])
			}
		}
	}
	if needDataAuthors {
		if authorErr != nil {
			log.Printf("openlibrary: data-authors fetch for %s: %v", bibkey, authorErr)
		} else {
			book.Authors = authorNames
		}
	}

	// Apply GB merge after OL fallbacks have had their chance to fill
	// gaps. This means GB only contributes to fields still empty after
	// the full OL chain ran, which respects DEC-035's "OL wins" rule.
	if needGB {
		switch {
		case errors.Is(gbErr, ErrGoogleBooksNotFound):
			// Normal outcome for ISBNs GB does not catalog. No error
			// flag; OL data flows through unchanged.
		case errors.Is(gbErr, ErrGoogleBooksDisabled):
			// Can only happen if the key was unset between the
			// IsGoogleBooksConfigured check and the FetchByISBN call.
			// Treat the same as not-found.
		case gbErr != nil:
			// Real GB failure (network, 5xx, decode). Surface via the
			// flag so the JS can render a banner note; OL data still
			// flows through.
			log.Printf("openlibrary: google books fetch for %s: %v", isbn, gbErr)
			book.GoogleBooksError = true
		case gbResult != nil:
			merged := mergePrefill(book, gbResult)
			// Preserve the GoogleBooksError flag on the merge output;
			// merge does not carry it through itself.
			merged.GoogleBooksError = book.GoogleBooksError
			book = merged
		}
	}

	// Fallback 3: when neither the edition nor the work surfaced a
	// cover ID, HEAD-probe OL's ISBN-based covers endpoint. OL
	// frequently has duplicate work records for the same book; an
	// edition can link to a coverless work even when a sibling work
	// has plenty of covers. The /b/isbn/<isbn>-L.jpg endpoint
	// resolves a cover whenever OL has ANY image indexed under the
	// ISBN, regardless of which work record holds it. ?default=false
	// makes the endpoint return 404 (rather than a 1x1 placeholder)
	// when nothing is indexed -- without it we'd save placeholders
	// as if they were real covers. The HEAD probe also keeps broken
	// images out of the staff form preview by checking resolvability
	// before we commit the URL into the prefill payload.
	if book.CoverURL == "" {
		cleaned := stripISBNFormatting(isbn)
		candidate := fmt.Sprintf("%s/b/isbn/%s-L.jpg?default=false", openLibraryCoversHost, cleaned)
		if probeCoverURL(ctx, candidate) {
			book.CoverURL = candidate
		}
	}

	return book, nil
}

// olWorkRecord is the subset of the OL work-record JSON we use as a
// fallback when the edition is sparse. Both fields are best-effort;
// callers check each for non-empty before using.
type olWorkRecord struct {
	Description string
	Covers      []int
}

// fetchOLWork pulls description and cover IDs off an OL work record.
// workKey is the path-style form OL returns from the edition, e.g.
// "/works/OL8990536W"; we fetch "<host>/<workKey>.json".
//
// Returns a zero-value record without error if the work has nothing
// useful for us, or if the fetch fails in a way we want to silently
// swallow (4xx, 5xx, network error). The edition data still gets
// returned to the client either way; this is best-effort enrichment.
func fetchOLWork(ctx context.Context, workKey string) (*olWorkRecord, error) {
	if workKey == "" {
		return &olWorkRecord{}, nil
	}
	endpoint := openLibraryHost + workKey + ".json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", openLibraryUserAgent)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: openLibraryTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open library work request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open library work returned status %d", resp.StatusCode)
	}

	// Work records use the same string-or-{type,value} shape for
	// description; reuse our olDescription unmarshaler. Covers is
	// the same []int as on editions.
	var work struct {
		Description olDescription `json:"description"`
		Covers      []int         `json:"covers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&work); err != nil {
		return nil, fmt.Errorf("open library work decode: %w", err)
	}
	return &olWorkRecord{
		Description: strings.TrimSpace(work.Description.Value),
		Covers:      work.Covers,
	}, nil
}

// probeCoverURL HEAD-requests the given URL and reports whether it
// resolves to a real resource (200 OK after any redirects). Used as
// a presence check for /b/isbn/<isbn>-L.jpg before we commit that URL
// into the prefill payload. Failures (network error, non-2xx) return
// false rather than propagating -- the caller treats "no cover" the
// same as "OL has no cover", which is the common case anyway.
func probeCoverURL(ctx context.Context, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", openLibraryUserAgent)

	client := &http.Client{Timeout: openLibraryTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// hasAnyGap returns true when at least one BookPrefill field is empty
// after TrimSpace. The merge step uses this to decide whether to fan
// out to GB; an entirely-populated OL response skips the GB call.
func hasAnyGap(b *BookPrefill) bool {
	if b == nil {
		return true
	}
	if strings.TrimSpace(b.Title) == "" {
		return true
	}
	if len(b.Authors) == 0 {
		return true
	}
	if b.PublishYear == 0 {
		return true
	}
	if strings.TrimSpace(b.Publisher) == "" {
		return true
	}
	if strings.TrimSpace(b.CoverURL) == "" {
		return true
	}
	if strings.TrimSpace(b.Description) == "" {
		return true
	}
	return false
}

// fetchOLDataAuthors makes a second call to the OL Books API with
// jscmd=data and returns the resolved author names. jscmd=data
// resolves work-record author refs into {name, url} pairs, which is
// exactly what we need when jscmd=details has no authors. Most
// editions only need one call; this fallback fires when details is
// sparse.
func fetchOLDataAuthors(ctx context.Context, bibkey string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openLibraryBaseURL, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("bibkeys", bibkey)
	q.Set("format", "json")
	q.Set("jscmd", "data")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("User-Agent", openLibraryUserAgent)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: openLibraryTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open library data request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open library data returned status %d", resp.StatusCode)
	}

	// jscmd=data uses [{"name": "..."}] shape inline.
	var payload map[string]struct {
		Authors []olAuthor `json:"authors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("open library data decode: %w", err)
	}
	entry, ok := payload[bibkey]
	if !ok {
		return nil, nil
	}
	out := make([]string, 0, len(entry.Authors))
	for _, a := range entry.Authors {
		if a.Name != "" {
			out = append(out, a.Name)
		}
	}
	return out, nil
}
