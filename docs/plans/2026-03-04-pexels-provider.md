# Pexels Provider Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a PexelsProvider to go-imagefy that searches Pexels photos using the official API (primary) with scraped internal API fallback when the official key is unavailable or rate-limited.

**Architecture:** Two-tier API access — official Pexels API (`api.pexels.com/v1`, key via env) as primary, scraped internal API (`pexels.com/en-us/api/v3/search/photos`, secret-key extracted from JS bundles) as fallback. The provider implements the existing `SearchProvider` interface. All Pexels images are CC0 → `LicenseSafe`.

**Tech Stack:** Go 1.26, `net/http`, `encoding/json`, `regexp`, `github.com/anatolykoptev/go-imagefy` interfaces. No new dependencies.

---

### Task 1: PexelsProvider struct and Name()

**Files:**
- Create: `pexels.go`
- Create: `pexels_test.go`

**Step 1: Write the failing test**

```go
// pexels_test.go
package imagefy

import "testing"

func TestPexelsProviderName(t *testing.T) {
	t.Parallel()
	p := &PexelsProvider{}
	if p.Name() != "pexels" {
		t.Errorf("Name() = %q, want %q", p.Name(), "pexels")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd ~/src/go-imagefy && go test -run TestPexelsProviderName -v`
Expected: FAIL — `PexelsProvider` not defined.

**Step 3: Write minimal implementation**

```go
// pexels.go
package imagefy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	pexelsOfficialURL = "https://api.pexels.com/v1"
	pexelsInternalURL = "https://www.pexels.com/en-us/api/v3/search/photos"
	pexelsBodyLimit   = 2 * 1024 * 1024 // 2MB
	pexelsPerPage     = 40
)

// PexelsProvider searches free-to-use photographs on Pexels.
// Uses official API (APIKey) as primary; falls back to internal API (SecretKey
// scraped from JS bundles) when APIKey is empty or rate-limited.
// All Pexels photos are licensed under CC0 → LicenseSafe.
type PexelsProvider struct {
	APIKey     string       // official Pexels API key (primary)
	SecretKey  string       // scraped internal API key (fallback)
	HTTPClient *http.Client // optional (nil = http.DefaultClient)
	UserAgent  string       // optional
}

// Name returns the provider name.
func (p *PexelsProvider) Name() string { return "pexels" }
```

**Step 4: Run test to verify it passes**

Run: `cd ~/src/go-imagefy && go test -run TestPexelsProviderName -v`
Expected: PASS

**Step 5: Commit**

```bash
cd ~/src/go-imagefy
git add pexels.go pexels_test.go
git commit -m "feat: add PexelsProvider struct and Name()"
```

---

### Task 2: Official API search (primary path)

**Files:**
- Modify: `pexels.go`
- Modify: `pexels_test.go`

**Step 1: Write the failing test**

```go
// pexels_test.go — add to existing file

func buildPexelsOfficialJSON(photos []pexelsOfficialPhoto) []byte {
	body, _ := json.Marshal(map[string]any{"photos": photos})
	return body
}

func TestPexelsProviderSearch_OfficialAPI(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsOfficialJSON([]pexelsOfficialPhoto{
			{
				ID:           12345,
				Width:        4000,
				Height:       3000,
				URL:          "https://www.pexels.com/photo/mountain-12345/",
				Photographer: "Jane Doe",
				Alt:          "Mountain landscape",
				Src: pexelsSrc{
					Original: "https://images.pexels.com/photos/12345/original.jpeg",
					Large:    "https://images.pexels.com/photos/12345/large.jpeg",
					Medium:   "https://images.pexels.com/photos/12345/medium.jpeg",
					Small:    "https://images.pexels.com/photos/12345/small.jpeg",
				},
			},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{APIKey: "test-key", HTTPClient: srv.Client()}
	// Override base URL for test
	candidates, err := p.searchOfficial(context.Background(), srv.URL, "mountains", SearchOpts{})

	if err != nil {
		t.Fatalf("searchOfficial returned error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least 1 candidate")
	}

	got := candidates[0]
	if got.ImgURL != "https://images.pexels.com/photos/12345/large.jpeg" {
		t.Errorf("ImgURL = %q, want large src", got.ImgURL)
	}
	if got.Thumbnail != "https://images.pexels.com/photos/12345/small.jpeg" {
		t.Errorf("Thumbnail = %q, want small src", got.Thumbnail)
	}
	if got.Source != "https://www.pexels.com/photo/mountain-12345/" {
		t.Errorf("Source = %q, want page URL", got.Source)
	}
	if got.Title != "Mountain landscape" {
		t.Errorf("Title = %q, want alt text", got.Title)
	}
	if got.License != LicenseSafe {
		t.Errorf("License = %v, want LicenseSafe", got.License)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd ~/src/go-imagefy && go test -run TestPexelsProviderSearch_OfficialAPI -v`
Expected: FAIL — types/methods not defined.

**Step 3: Write implementation**

Add to `pexels.go`:

```go
// pexelsOfficialPhoto is the JSON shape from Pexels official API v1.
type pexelsOfficialPhoto struct {
	ID           int       `json:"id"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	URL          string    `json:"url"`
	Photographer string    `json:"photographer"`
	Alt          string    `json:"alt"`
	Src          pexelsSrc `json:"src"`
}

type pexelsSrc struct {
	Original string `json:"original"`
	Large    string `json:"large"`
	Medium   string `json:"medium"`
	Small    string `json:"small"`
}

// searchOfficial queries the official Pexels API v1.
func (p *PexelsProvider) searchOfficial(ctx context.Context, baseURL, query string, opts SearchOpts) ([]ImageCandidate, error) {
	page := opts.PageNumber
	if page < 1 {
		page = 1
	}

	searchURL := fmt.Sprintf("%s/search?query=%s&page=%d&per_page=%d",
		strings.TrimRight(baseURL, "/"),
		url.QueryEscape(query), page, pexelsPerPage)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", p.APIKey)
	req.Header.Set("Accept", "application/json")
	if p.UserAgent != "" {
		req.Header.Set("User-Agent", p.UserAgent)
	}

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req) //nolint:gosec // G107: URL is cfg-supplied
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pexels: official API status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, pexelsBodyLimit))
	if err != nil {
		return nil, err
	}

	var searchResp struct {
		Photos []pexelsOfficialPhoto `json:"photos"`
	}
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, err
	}

	return filterOfficialResults(searchResp.Photos), nil
}

func filterOfficialResults(photos []pexelsOfficialPhoto) []ImageCandidate {
	candidates := make([]ImageCandidate, 0, len(photos))
	for _, p := range photos {
		imgURL := p.Src.Large
		if imgURL == "" {
			imgURL = p.Src.Original
		}
		if imgURL == "" {
			continue
		}
		if IsLogoOrBanner(strings.ToLower(imgURL)) {
			continue
		}

		candidates = append(candidates, ImageCandidate{
			ImgURL:    imgURL,
			Thumbnail: p.Src.Small,
			Source:    p.URL,
			Title:     p.Alt,
			License:   LicenseSafe,
		})
	}
	return candidates
}
```

**Step 4: Run test to verify it passes**

Run: `cd ~/src/go-imagefy && go test -run TestPexelsProviderSearch_OfficialAPI -v`
Expected: PASS

**Step 5: Commit**

```bash
cd ~/src/go-imagefy
git add pexels.go pexels_test.go
git commit -m "feat: add official Pexels API search"
```

---

### Task 3: Internal API search (fallback path)

**Files:**
- Modify: `pexels.go`
- Modify: `pexels_test.go`

**Step 1: Write the failing test**

The internal API uses a different JSON format: `{data: [{attributes: {id, slug, title, description, width, height, image: {small, download_link}, user: {username}}}]}` with a `secret-key` header.

```go
func buildPexelsInternalJSON(items []pexelsInternalItem) []byte {
	body, _ := json.Marshal(map[string]any{"data": items})
	return body
}

func TestPexelsProviderSearch_InternalAPI(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Secret-Key") != "scraped-key" {
			t.Errorf("Secret-Key header = %q, want %q", r.Header.Get("Secret-Key"), "scraped-key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsInternalJSON([]pexelsInternalItem{
			{
				Attributes: pexelsInternalAttrs{
					ID:          99999,
					Slug:        "sunset-beach",
					Title:       "Sunset at the Beach",
					Description: "Beautiful sunset",
					Width:       5000,
					Height:      3333,
					Image: pexelsInternalImage{
						Small:        "https://images.pexels.com/99999/small.jpeg",
						DownloadLink: "https://images.pexels.com/99999/download.jpeg",
					},
					User: pexelsInternalUser{Username: "photographer1"},
				},
			},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{SecretKey: "scraped-key", HTTPClient: srv.Client()}
	candidates, err := p.searchInternal(context.Background(), srv.URL, "sunset", SearchOpts{})

	if err != nil {
		t.Fatalf("searchInternal returned error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least 1 candidate")
	}

	got := candidates[0]
	if got.ImgURL != "https://images.pexels.com/99999/download.jpeg" {
		t.Errorf("ImgURL = %q, want download_link", got.ImgURL)
	}
	if got.Thumbnail != "https://images.pexels.com/99999/small.jpeg" {
		t.Errorf("Thumbnail = %q, want small", got.Thumbnail)
	}
	if got.Title != "Sunset at the Beach" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.License != LicenseSafe {
		t.Errorf("License = %v, want LicenseSafe", got.License)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd ~/src/go-imagefy && go test -run TestPexelsProviderSearch_InternalAPI -v`
Expected: FAIL — types not defined.

**Step 3: Write implementation**

Add to `pexels.go`:

```go
// --- Internal API types (v3, scraped from pexels.com frontend) ---

type pexelsInternalItem struct {
	Attributes pexelsInternalAttrs `json:"attributes"`
}

type pexelsInternalAttrs struct {
	ID          int                  `json:"id"`
	Slug        string               `json:"slug"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Width       int                  `json:"width"`
	Height      int                  `json:"height"`
	Image       pexelsInternalImage  `json:"image"`
	User        pexelsInternalUser   `json:"user"`
}

type pexelsInternalImage struct {
	Small        string `json:"small"`
	DownloadLink string `json:"download_link"`
}

type pexelsInternalUser struct {
	Username string `json:"username"`
}

// searchInternal queries the Pexels internal API v3 using a scraped secret-key.
func (p *PexelsProvider) searchInternal(ctx context.Context, baseURL, query string, opts SearchOpts) ([]ImageCandidate, error) {
	page := opts.PageNumber
	if page < 1 {
		page = 1
	}

	searchURL := fmt.Sprintf("%s?query=%s&page=%d&per_page=%d",
		strings.TrimRight(baseURL, "/"),
		url.QueryEscape(query), page, pexelsPerPage)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Secret-Key", p.SecretKey)
	req.Header.Set("Accept", "application/json")
	if p.UserAgent != "" {
		req.Header.Set("User-Agent", p.UserAgent)
	}

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req) //nolint:gosec // G107: URL is cfg-supplied
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pexels: internal API status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, pexelsBodyLimit))
	if err != nil {
		return nil, err
	}

	var searchResp struct {
		Data []pexelsInternalItem `json:"data"`
	}
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, err
	}

	return filterInternalResults(searchResp.Data), nil
}

func filterInternalResults(items []pexelsInternalItem) []ImageCandidate {
	candidates := make([]ImageCandidate, 0, len(items))
	for _, item := range items {
		a := item.Attributes
		imgURL := a.Image.DownloadLink
		if imgURL == "" {
			continue
		}
		if IsLogoOrBanner(strings.ToLower(imgURL)) {
			continue
		}

		source := fmt.Sprintf("https://www.pexels.com/photo/%s-%d/", a.Slug, a.ID)

		candidates = append(candidates, ImageCandidate{
			ImgURL:    imgURL,
			Thumbnail: a.Image.Small,
			Source:    source,
			Title:     a.Title,
			License:   LicenseSafe,
		})
	}
	return candidates
}
```

**Step 4: Run test to verify it passes**

Run: `cd ~/src/go-imagefy && go test -run TestPexelsProviderSearch_InternalAPI -v`
Expected: PASS

**Step 5: Commit**

```bash
cd ~/src/go-imagefy
git add pexels.go pexels_test.go
git commit -m "feat: add internal Pexels API search (fallback)"
```

---

### Task 4: Search() method with fallback logic

**Files:**
- Modify: `pexels.go`
- Modify: `pexels_test.go`

**Step 1: Write the failing tests**

```go
func TestPexelsProviderSearch_PrefersOfficialAPI(t *testing.T) {
	t.Parallel()

	var hitOfficial bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			hitOfficial = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsOfficialJSON([]pexelsOfficialPhoto{
			{ID: 1, URL: "https://pexels.com/photo/1", Alt: "Photo", Src: pexelsSrc{Large: "https://img.pexels.com/1.jpg", Small: "https://img.pexels.com/1s.jpg"}},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{
		APIKey:       "official-key",
		SecretKey:    "scraped-key",
		HTTPClient:   srv.Client(),
		officialBase: srv.URL,
	}
	candidates, err := p.Search(context.Background(), "test", SearchOpts{})

	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if !hitOfficial {
		t.Error("expected official API to be called first")
	}
	if len(candidates) == 0 {
		t.Fatal("expected candidates")
	}
}

func TestPexelsProviderSearch_FallsBackToInternal(t *testing.T) {
	t.Parallel()

	officialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests) // 429 — rate limited
	}))
	t.Cleanup(officialSrv.Close)

	internalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsInternalJSON([]pexelsInternalItem{
			{Attributes: pexelsInternalAttrs{
				ID: 2, Slug: "fallback", Title: "Fallback",
				Image: pexelsInternalImage{Small: "https://img.pexels.com/2s.jpg", DownloadLink: "https://img.pexels.com/2.jpg"},
				User:  pexelsInternalUser{Username: "user"},
			}},
		}))
	}))
	t.Cleanup(internalSrv.Close)

	p := &PexelsProvider{
		APIKey:       "official-key",
		SecretKey:    "scraped-key",
		HTTPClient:   http.DefaultClient,
		officialBase: officialSrv.URL,
		internalBase: internalSrv.URL,
	}
	candidates, err := p.Search(context.Background(), "test", SearchOpts{})

	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected fallback candidates")
	}
	if candidates[0].Title != "Fallback" {
		t.Errorf("Title = %q, want Fallback (from internal API)", candidates[0].Title)
	}
}

func TestPexelsProviderSearch_InternalOnlyWhenNoAPIKey(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Secret-Key") == "" {
			t.Error("expected Secret-Key header for internal API")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsInternalJSON([]pexelsInternalItem{
			{Attributes: pexelsInternalAttrs{
				ID: 3, Slug: "internal-only", Title: "Internal",
				Image: pexelsInternalImage{Small: "https://img.pexels.com/3s.jpg", DownloadLink: "https://img.pexels.com/3.jpg"},
				User:  pexelsInternalUser{Username: "user"},
			}},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{
		SecretKey:    "scraped-key",
		HTTPClient:   srv.Client(),
		internalBase: srv.URL,
	}
	candidates, err := p.Search(context.Background(), "test", SearchOpts{})

	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected candidates from internal API")
	}
}

func TestPexelsProviderSearch_NoBothKeys(t *testing.T) {
	t.Parallel()

	p := &PexelsProvider{} // no keys at all
	_, err := p.Search(context.Background(), "test", SearchOpts{})

	if err == nil {
		t.Error("expected error when both APIKey and SecretKey are empty")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd ~/src/go-imagefy && go test -run "TestPexelsProviderSearch_(PrefersOfficial|FallsBack|InternalOnly|NoBothKeys)" -v`
Expected: FAIL — `Search` method and `officialBase`/`internalBase` fields not defined.

**Step 3: Write implementation**

Update `PexelsProvider` struct and add `Search()`:

```go
// Add fields to PexelsProvider struct:
type PexelsProvider struct {
	APIKey     string       // official Pexels API key (primary)
	SecretKey  string       // scraped internal API key (fallback)
	HTTPClient *http.Client // optional (nil = http.DefaultClient)
	UserAgent  string       // optional

	// Test overrides (unexported). Empty = production URLs.
	officialBase string
	internalBase string
}

// Search queries Pexels: official API first, internal API as fallback.
func (p *PexelsProvider) Search(ctx context.Context, query string, opts SearchOpts) ([]ImageCandidate, error) {
	// Try official API first if key is available.
	if p.APIKey != "" {
		base := p.officialBase
		if base == "" {
			base = pexelsOfficialURL
		}
		candidates, err := p.searchOfficial(ctx, base, query, opts)
		if err == nil {
			return candidates, nil
		}
		// Official failed — fall through to internal if available.
	}

	// Try internal API.
	if p.SecretKey != "" {
		base := p.internalBase
		if base == "" {
			base = pexelsInternalURL
		}
		return p.searchInternal(ctx, base, query, opts)
	}

	if p.APIKey != "" {
		// Had API key but official failed and no fallback.
		base := p.officialBase
		if base == "" {
			base = pexelsOfficialURL
		}
		return p.searchOfficial(ctx, base, query, opts)
	}

	return nil, fmt.Errorf("pexels: no API key or secret key configured")
}
```

**Step 4: Run tests to verify they pass**

Run: `cd ~/src/go-imagefy && go test -run "TestPexelsProviderSearch_(PrefersOfficial|FallsBack|InternalOnly|NoBothKeys)" -v`
Expected: PASS

**Step 5: Commit**

```bash
cd ~/src/go-imagefy
git add pexels.go pexels_test.go
git commit -m "feat: add Search() with official-first, internal-fallback"
```

---

### Task 5: Edge case tests

**Files:**
- Modify: `pexels_test.go`

**Step 1: Write edge case tests**

```go
func TestPexelsProviderSearch_EmptyResults(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsOfficialJSON(nil))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{APIKey: "key", HTTPClient: srv.Client(), officialBase: srv.URL}
	candidates, err := p.Search(context.Background(), "nothing", SearchOpts{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("got %d candidates, want 0", len(candidates))
	}
}

func TestPexelsProviderSearch_Pagination(t *testing.T) {
	t.Parallel()

	var capturedRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsOfficialJSON(nil))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{APIKey: "key", HTTPClient: srv.Client(), officialBase: srv.URL}
	_, _ = p.Search(context.Background(), "sky", SearchOpts{PageNumber: 5})

	q, _ := url.ParseQuery(capturedRawQuery)
	if q.Get("page") != "5" {
		t.Errorf("page = %q, want %q", q.Get("page"), "5")
	}
}

func TestPexelsProviderSearch_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{APIKey: "key", HTTPClient: srv.Client(), officialBase: srv.URL}
	_, err := p.Search(context.Background(), "test", SearchOpts{})

	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestPexelsProviderSearch_ConnectionError(t *testing.T) {
	t.Parallel()

	p := &PexelsProvider{APIKey: "key", officialBase: "http://127.0.0.1:1"}
	_, err := p.Search(context.Background(), "test", SearchOpts{})

	if err == nil {
		t.Error("expected connection error")
	}
}

func TestPexelsProviderSearch_EmptyDownloadLinkSkipped(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsInternalJSON([]pexelsInternalItem{
			{Attributes: pexelsInternalAttrs{
				ID: 1, Slug: "empty", Title: "Empty",
				Image: pexelsInternalImage{Small: "https://img.pexels.com/s.jpg", DownloadLink: ""},
				User:  pexelsInternalUser{Username: "u"},
			}},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{SecretKey: "key", HTTPClient: srv.Client(), internalBase: srv.URL}
	candidates, _ := p.Search(context.Background(), "test", SearchOpts{})

	if len(candidates) != 0 {
		t.Errorf("got %d candidates, want 0 (empty download_link should be skipped)", len(candidates))
	}
}

func TestPexelsProviderSearch_AllResultsAreLicenseSafe(t *testing.T) {
	t.Parallel()

	photos := []pexelsOfficialPhoto{
		{ID: 1, URL: "https://pexels.com/1", Alt: "A", Src: pexelsSrc{Large: "https://img.pexels.com/1.jpg", Small: "https://img.pexels.com/1s.jpg"}},
		{ID: 2, URL: "https://pexels.com/2", Alt: "B", Src: pexelsSrc{Large: "https://img.pexels.com/2.jpg", Small: "https://img.pexels.com/2s.jpg"}},
		{ID: 3, URL: "https://pexels.com/3", Alt: "C", Src: pexelsSrc{Large: "https://img.pexels.com/3.jpg", Small: "https://img.pexels.com/3s.jpg"}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsOfficialJSON(photos))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{APIKey: "key", HTTPClient: srv.Client(), officialBase: srv.URL}
	candidates, err := p.Search(context.Background(), "nature", SearchOpts{})

	if err != nil {
		t.Fatalf("error: %v", err)
	}
	for i, c := range candidates {
		if c.License != LicenseSafe {
			t.Errorf("candidates[%d].License = %v, want LicenseSafe", i, c.License)
		}
	}
}
```

**Step 2: Run all Pexels tests**

Run: `cd ~/src/go-imagefy && go test -run TestPexels -v`
Expected: ALL PASS

**Step 3: Commit**

```bash
cd ~/src/go-imagefy
git add pexels_test.go
git commit -m "test: add edge case tests for PexelsProvider"
```

---

### Task 6: Run full test suite and lint

**Step 1: Run all tests**

Run: `cd ~/src/go-imagefy && go test ./... -v -count=1`
Expected: ALL PASS (existing + new tests)

**Step 2: Run linter**

Run: `cd ~/src/go-imagefy && golangci-lint run`
Expected: No new warnings

**Step 3: Fix any issues found**

Address lint warnings or test failures.

**Step 4: Commit if fixes were needed**

```bash
cd ~/src/go-imagefy
git add -A
git commit -m "fix: address lint warnings in pexels provider"
```

---

## Notes for ox-browser integration (future)

The `SecretKey` scraping from Pexels JS bundles is a separate concern, to be implemented later as an ox-browser utility:

1. `Browser.page("https://www.pexels.com")` → get page HTML
2. `page.select("script[src]")` → collect all external script URLs
3. For each script: fetch content, regex `"secret-key":\s*"([^"]+)"`
4. Cache the extracted key (Redis or file, TTL ~24h)

This would be a standalone Rust binary or ox-browser CLI command that go-imagefy calls via `exec.Command` or reads from a shared cache. Not part of this plan — implement when the official API 200 req/month limit becomes a bottleneck.
