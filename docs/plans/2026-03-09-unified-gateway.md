# go-imagefy: Unified Image Gateway

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make go-imagefy the single entry point for all image acquisition in go-wp — every image (search, OG, external) passes through the same filter pipeline.

**Architecture:** Add `OGImageProvider` (fetches HTML, extracts og:image), export `ValidateCandidates` for external URLs, and add `FindImages` as the unified entry point. go-wp removes direct OG fetching and pipes WP media results through go-imagefy filters.

**Tech Stack:** Go, go-imagefy (library), go-wp (consumer), httptest for tests.

---

## Task 1: Export `validateCandidates` as `ValidateCandidates`

Currently `validateCandidates` in `search.go` is private. Export it so go-wp can pass external candidates (WP media) through the full filter pipeline.

**Files:**
- Modify: `search.go` (add exported wrapper)
- Create: `validate_candidates_test.go`

**Step 1: Write the failing test**

```go
// validate_candidates_test.go
package imagefy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateCandidates_FiltersBlocked(t *testing.T) {
	t.Parallel()

	// Serve a valid JPEG for the "safe" candidate.
	srv := newJPEGServer(t, 1000, 800)
	defer srv.Close()

	cfg := &Config{
		MinImageWidth: 100,
	}

	candidates := []ImageCandidate{
		{ImgURL: srv.URL + "/photo.jpg", Source: "https://example.com", License: LicenseUnknown},
		{ImgURL: "https://shutterstock.com/pic.jpg", Source: "https://shutterstock.com", License: LicenseBlocked},
	}

	result := cfg.ValidateCandidates(context.Background(), candidates, 5)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].ImgURL != srv.URL+"/photo.jpg" {
		t.Errorf("unexpected URL: %s", result[0].ImgURL)
	}
}

func TestValidateCandidates_RespectsMaxResults(t *testing.T) {
	t.Parallel()

	srv := newJPEGServer(t, 1000, 800)
	defer srv.Close()

	cfg := &Config{
		MinImageWidth: 100,
	}

	candidates := []ImageCandidate{
		{ImgURL: srv.URL + "/a.jpg", License: LicenseUnknown},
		{ImgURL: srv.URL + "/b.jpg", License: LicenseUnknown},
		{ImgURL: srv.URL + "/c.jpg", License: LicenseUnknown},
	}

	result := cfg.ValidateCandidates(context.Background(), candidates, 1)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
}

func TestValidateCandidates_EmptyInput(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	result := cfg.ValidateCandidates(context.Background(), nil, 5)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}
```

Note: `newJPEGServer` already exists in `search_test.go`. If it's not accessible, extract it to a shared test helper.

**Step 2: Run test to verify it fails**

Run: `go test -run TestValidateCandidates -v -count=1`
Expected: FAIL — `cfg.ValidateCandidates undefined`

**Step 3: Write minimal implementation**

Add to `search.go` after the existing `validateCandidates`:

```go
// ValidateCandidates runs external image candidates through the full filter
// pipeline: URL validation, license check, dedup, metadata assessment, and
// LLM vision classification. Use this to validate images from sources outside
// the built-in search providers (e.g. WP media library, user-supplied URLs).
func (cfg *Config) ValidateCandidates(ctx context.Context, candidates []ImageCandidate, maxResults int) []ImageCandidate {
	if len(candidates) == 0 {
		return nil
	}
	cfg.defaults()
	return cfg.validateCandidates(ctx, candidates, maxResults)
}
```

**Step 4: Run tests**

Run: `go test -run TestValidateCandidates -v -count=1`
Expected: PASS

**Step 5: Run full suite**

Run: `go test ./... -count=1`
Expected: all 144+ tests PASS

**Step 6: Commit**

```bash
git add search.go validate_candidates_test.go
git commit -m "feat: export ValidateCandidates for external image filtering"
```

---

## Task 2: Add `OGImageProvider`

A new `SearchProvider` that fetches a page, extracts `og:image`, and returns it as a candidate. Reuses existing `ExtractOGImageURL` from `helpers.go`.

**Files:**
- Create: `provider_og.go`
- Create: `provider_og_test.go`

**Step 1: Write the failing test**

```go
// provider_og_test.go
package imagefy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOGImageProvider_Name(t *testing.T) {
	t.Parallel()
	p := &OGImageProvider{}
	if p.Name() != "og" {
		t.Errorf("expected 'og', got %q", p.Name())
	}
}

func TestOGImageProvider_Search_ExtractsOGImage(t *testing.T) {
	t.Parallel()

	html := `<html><head>
		<meta property="og:image" content="https://example.com/photo.jpg">
	</head><body></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer srv.Close()

	p := &OGImageProvider{HTTPClient: srv.Client()}

	candidates, err := p.Search(context.Background(), "", SearchOpts{PageURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].ImgURL != "https://example.com/photo.jpg" {
		t.Errorf("unexpected URL: %s", candidates[0].ImgURL)
	}
	if candidates[0].Source != srv.URL {
		t.Errorf("unexpected source: %s", candidates[0].Source)
	}
}

func TestOGImageProvider_Search_NoPageURL(t *testing.T) {
	t.Parallel()
	p := &OGImageProvider{}
	candidates, err := p.Search(context.Background(), "", SearchOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected empty, got %d", len(candidates))
	}
}

func TestOGImageProvider_Search_NoOGTag(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><head><title>No OG</title></head></html>"))
	}))
	defer srv.Close()

	p := &OGImageProvider{HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "", SearchOpts{PageURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected empty, got %d", len(candidates))
	}
}

func TestOGImageProvider_Search_BlockedDomain(t *testing.T) {
	t.Parallel()

	html := `<html><head>
		<meta property="og:image" content="https://shutterstock.com/stock.jpg">
	</head></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer srv.Close()

	p := &OGImageProvider{HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "", SearchOpts{PageURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected blocked image filtered out, got %d", len(candidates))
	}
}

func TestOGImageProvider_Search_LogoBannerFiltered(t *testing.T) {
	t.Parallel()

	html := `<html><head>
		<meta property="og:image" content="https://example.com/logo.png">
	</head></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer srv.Close()

	p := &OGImageProvider{HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "", SearchOpts{PageURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected logo filtered out, got %d", len(candidates))
	}
}

func TestOGImageProvider_Search_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := &OGImageProvider{HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "", SearchOpts{PageURL: srv.URL})
	// Graceful: no error, just empty results.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected empty, got %d", len(candidates))
	}
}
```

**Step 2: Add `PageURL` field to `SearchOpts`**

In `imagefy.go`, add to `SearchOpts`:

```go
type SearchOpts struct {
	PageNumber int           // SearXNG page number (default: 1)
	Engines    []string      // SearXNG engines to use (default: all)
	Timeout    time.Duration // search timeout (default: 15s)
	PageURL    string        // page URL for OG image extraction (used by OGImageProvider)
}
```

**Step 3: Run test to verify it fails**

Run: `go test -run TestOGImageProvider -v -count=1`
Expected: FAIL — `OGImageProvider undefined`

**Step 4: Write implementation**

```go
// provider_og.go
package imagefy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	ogFetchTimeout = 10 * time.Second
	ogBodyLimit    = 2 * 1024 * 1024 // 2MB
)

// OGImageProvider extracts og:image from a page URL and returns it as a candidate.
// Uses ExtractOGImageURL internally. Filters out blocked domains and logo/banner URLs.
// The page URL is passed via SearchOpts.PageURL; the query parameter is ignored.
type OGImageProvider struct {
	HTTPClient *http.Client // optional (nil = http.DefaultClient)
}

// Name returns the provider name.
func (p *OGImageProvider) Name() string { return "og" }

// Search fetches the page at opts.PageURL, extracts og:image, and returns it as
// a filtered candidate. Returns empty results (not error) on any failure.
func (p *OGImageProvider) Search(ctx context.Context, _ string, opts SearchOpts) ([]ImageCandidate, error) {
	if opts.PageURL == "" {
		return nil, nil
	}

	imgURL := p.fetchOG(ctx, opts.PageURL)
	if imgURL == "" {
		return nil, nil
	}

	lower := strings.ToLower(imgURL)
	if IsLogoOrBanner(lower) {
		return nil, nil
	}

	license := CheckLicense(imgURL, opts.PageURL)
	if license == LicenseBlocked {
		return nil, nil
	}

	return []ImageCandidate{{
		ImgURL:  imgURL,
		Source:   opts.PageURL,
		Title:   "og:image",
		License: license,
	}}, nil
}

func (p *OGImageProvider) fetchOG(ctx context.Context, pageURL string) string {
	ctx, cancel := context.WithTimeout(ctx, ogFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; go-imagefy/1.0)")

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req) //nolint:gosec // G107: URL is caller-supplied
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, ogBodyLimit))
	if err != nil {
		return ""
	}

	return ExtractOGImageURL(string(body))
}
```

**Step 5: Run tests**

Run: `go test -run TestOGImageProvider -v -count=1`
Expected: PASS (6 tests)

**Step 6: Run full suite**

Run: `go test ./... -count=1`
Expected: all tests PASS

**Step 7: Commit**

```bash
git add imagefy.go provider_og.go provider_og_test.go
git commit -m "feat: add OGImageProvider — extracts og:image with full filtering"
```

---

## Task 3: Add `FindImages` unified entry point

Single method that combines search providers + OG + external candidates, runs everything through the filter pipeline.

**Files:**
- Create: `find.go`
- Create: `find_test.go`

**Step 1: Write the failing test**

```go
// find_test.go
package imagefy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindImages_SearchOnly(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t, 1000, 800)
	defer imgSrv.Close()

	cfg := &Config{
		MinImageWidth: 100,
		Providers: []SearchProvider{
			&mockProvider{results: []ImageCandidate{
				{ImgURL: imgSrv.URL + "/a.jpg", License: LicenseUnknown},
			}},
		},
	}

	result := cfg.FindImages(context.Background(), FindOpts{
		Query:      "test",
		MaxResults: 5,
	})

	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
}

func TestFindImages_ExternalCandidates(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t, 1000, 800)
	defer imgSrv.Close()

	cfg := &Config{
		MinImageWidth: 100,
	}

	result := cfg.FindImages(context.Background(), FindOpts{
		External: []ImageCandidate{
			{ImgURL: imgSrv.URL + "/wp-media.jpg", Source: "wp-media", License: LicenseUnknown},
		},
		MaxResults: 5,
	})

	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Source != "wp-media" {
		t.Errorf("unexpected source: %s", result[0].Source)
	}
}

func TestFindImages_ExternalBlockedFiltered(t *testing.T) {
	t.Parallel()

	cfg := &Config{MinImageWidth: 100}

	result := cfg.FindImages(context.Background(), FindOpts{
		External: []ImageCandidate{
			{ImgURL: "https://shutterstock.com/stock.jpg", License: LicenseBlocked},
		},
		MaxResults: 5,
	})

	if len(result) != 0 {
		t.Errorf("expected blocked filtered, got %d", len(result))
	}
}

func TestFindImages_CombinesSearchAndExternal(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t, 1000, 800)
	defer imgSrv.Close()

	cfg := &Config{
		MinImageWidth: 100,
		Providers: []SearchProvider{
			&mockProvider{results: []ImageCandidate{
				{ImgURL: imgSrv.URL + "/search.jpg", License: LicenseSafe},
			}},
		},
	}

	result := cfg.FindImages(context.Background(), FindOpts{
		Query: "test",
		External: []ImageCandidate{
			{ImgURL: imgSrv.URL + "/external.jpg", License: LicenseUnknown},
		},
		MaxResults: 5,
	})

	if len(result) < 1 {
		t.Fatal("expected at least 1 result")
	}
	// Safe-licensed search results should come before unknown external.
	if result[0].License != LicenseSafe {
		t.Errorf("expected safe first, got %v", result[0].License)
	}
}

func TestFindImages_EmptyOpts(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	result := cfg.FindImages(context.Background(), FindOpts{})
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

// mockProvider is a test helper that returns preset results.
type mockProvider struct {
	results []ImageCandidate
	err     error
}

func (m *mockProvider) Search(_ context.Context, _ string, _ SearchOpts) ([]ImageCandidate, error) {
	return m.results, m.err
}

func (m *mockProvider) Name() string { return "mock" }
```

Note: `mockProvider` may already exist in `provider_test.go` — check and reuse if so.

**Step 2: Run test to verify it fails**

Run: `go test -run TestFindImages -v -count=1`
Expected: FAIL — `FindOpts undefined`, `cfg.FindImages undefined`

**Step 3: Write implementation**

```go
// find.go
package imagefy

import (
	"context"
	"sort"
)

// FindOpts configures a unified image search across all sources.
type FindOpts struct {
	Query      string           // search query for providers
	PageURL    string           // page URL for OG image extraction
	External   []ImageCandidate // external candidates (WP media, user-supplied URLs)
	MaxResults int              // max results to return (default: 3)
	SearchOpts SearchOpts       // additional search options (timeout, engines, etc.)
}

const findDefaultMaxResults = 3

// FindImages is the unified entry point for image acquisition. It:
//  1. Queries configured search providers (if Query is set)
//  2. Extracts og:image (if PageURL is set and an OGImageProvider is configured)
//  3. Accepts external candidates (e.g. WP media library)
//  4. Merges all candidates, sorts by license (safe first)
//  5. Runs the full filter pipeline (validate, dedup, metadata, LLM vision)
//
// Returns only validated, high-quality photo candidates.
func (cfg *Config) FindImages(ctx context.Context, opts FindOpts) []ImageCandidate {
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = findDefaultMaxResults
	}

	cfg.defaults()

	var candidates []ImageCandidate

	// 1. Search providers (if query is set).
	if opts.Query != "" {
		searchOpts := opts.SearchOpts
		if searchOpts.PageURL == "" {
			searchOpts.PageURL = opts.PageURL
		}
		providers := cfg.resolveProviders()
		candidates = append(candidates, cfg.gatherCandidates(ctx, providers, opts.Query, searchOpts)...)
	}

	// 2. OG image (if PageURL is set but no query, or query didn't trigger OG provider).
	// Only if no OGImageProvider is already in Providers list.
	if opts.PageURL != "" && !cfg.hasOGProvider() {
		ogP := &OGImageProvider{HTTPClient: cfg.HTTPClient}
		ogCandidates, _ := ogP.Search(ctx, "", SearchOpts{PageURL: opts.PageURL})
		candidates = append(candidates, ogCandidates...)
	}

	// 3. External candidates.
	candidates = append(candidates, opts.External...)

	if len(candidates) == 0 {
		return nil
	}

	// Sort: safe first.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].License < candidates[j].License
	})

	return cfg.validateCandidates(ctx, candidates, maxResults)
}

// hasOGProvider checks if an OGImageProvider is already in the Providers list.
func (cfg *Config) hasOGProvider() bool {
	for _, p := range cfg.Providers {
		if p.Name() == "og" {
			return true
		}
	}
	return false
}
```

**Step 4: Run tests**

Run: `go test -run TestFindImages -v -count=1`
Expected: PASS (5 tests)

**Step 5: Run full suite**

Run: `go test ./... -count=1`
Expected: all tests PASS

**Step 6: Commit**

```bash
git add find.go find_test.go
git commit -m "feat: add FindImages — unified entry point for all image sources"
```

---

## Task 4: Update go-wp imageadapter to use `FindImages`

Wire `OGImageProvider` into the provider list, update adapter to expose `FindImages`.

**Files:**
- Modify: `internal/imageadapter/adapter.go`

**Step 1: Add OGImageProvider to Init**

In `adapter.go`, after the DDG provider, add:

```go
// OG image: extracts og:image from article source pages.
providers = append(providers, &imagefy.OGImageProvider{
	HTTPClient: httpClient,
})
```

**Step 2: Run go-wp imageadapter tests**

Run: `cd ~/src/go-wp && GOWORK=off go test ./internal/imageadapter/... -v -count=1`
Expected: PASS (existing tests shouldn't break)

**Step 3: Commit**

```bash
git add internal/imageadapter/adapter.go
git commit -m "feat: add OGImageProvider to imageadapter provider chain"
```

---

## Task 5: Refactor `media/resolve.go` to use `FindImages`

Replace direct `SearchImages` + WP media fallback with single `FindImages` call.

**Files:**
- Modify: `internal/wptools/media/resolve.go`

**Step 1: Refactor**

```go
func handleImageResolve(ctx context.Context, input ImageInput) (*shared.DispatchOutput, error) {
	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = resolveImageDefaultMax
	}

	// Fast path: validate caller-supplied candidate.
	if input.CandidateURL != "" &&
		imagefy.CheckLicense(input.CandidateURL, "") != imagefy.LicenseBlocked &&
		imageadapter.Cfg().ValidateImageURL(ctx, input.CandidateURL) &&
		imageadapter.Cfg().IsRealPhoto(ctx, input.CandidateURL) {
		return &shared.DispatchOutput{Action: "resolve", Result: &ResolveImageOutput{
			Images: []ResolvedImage{{URL: input.CandidateURL}},
		}}, nil
	}

	// Collect WP media as external candidate.
	var external []imagefy.ImageCandidate
	if _, mediaURL := wordpress.SearchMediaLibrary(ctx, ssh, input.Query); mediaURL != "" {
		external = append(external, imagefy.ImageCandidate{
			ImgURL:  mediaURL,
			Source:   "wp-media",
			License: imagefy.LicenseUnknown,
		})
	}

	// Unified search through go-imagefy.
	found := imageadapter.Cfg().FindImages(ctx, imagefy.FindOpts{
		Query:      input.Query,
		External:   external,
		MaxResults: maxResults,
	})

	results := make([]ResolvedImage, 0, len(found))
	for _, c := range found {
		results = append(results, ResolvedImage{
			URL:     c.ImgURL,
			Source:  c.Source,
			Title:   c.Title,
			License: c.License.String(),
		})
	}

	if len(results) == 0 {
		results = []ResolvedImage{}
	}

	return &shared.DispatchOutput{Action: "resolve", Result: &ResolveImageOutput{Images: results}}, nil
}
```

**Step 2: Update vendor & verify**

Run: `cd ~/src/go-wp && GOWORK=off go mod tidy && go mod vendor`
Run: `GOWORK=off go build ./...`
Expected: builds successfully

**Step 3: Commit**

```bash
git add internal/wptools/media/resolve.go vendor/
git commit -m "refactor: resolve uses FindImages — all images pass through go-imagefy filters"
```

---

## Task 6: Refactor `media/prepare_select.go` to use `FindImages`

Replace the 3-strategy approach (OG, search, media) with unified `FindImages`.

**Files:**
- Modify: `internal/wptools/media/prepare_select.go`

**Step 1: Refactor `selectImageForArticle`**

```go
func selectImageForArticle(
	ctx context.Context,
	_ *wordpress.Client,
	sshRunner *wordpress.SSHRunner,
	art PrepareImageArticle,
	strategy string,
	maxCandidates int,
) PrepareImageResult {
	opts := imagefy.FindOpts{
		MaxResults: maxCandidates,
	}

	useOG := strategy == strategyAuto || strategy == "og"
	useSearch := strategy == strategyAuto || strategy == "search"
	useMedia := strategy == strategyAuto || strategy == "media"

	if useSearch {
		opts.Query = art.Title
	}

	if useOG {
		pageURL := art.SourceURL
		if pageURL == "" {
			pageURL = art.URL
		}
		opts.PageURL = pageURL
	}

	if useMedia {
		if _, mediaURL := wordpress.SearchMediaLibrary(ctx, sshRunner, art.Title); mediaURL != "" {
			opts.External = append(opts.External, imagefy.ImageCandidate{
				ImgURL:  mediaURL,
				Source:   "wp-media",
				License: imagefy.LicenseUnknown,
			})
		}
	}

	found := imageadapter.Cfg().FindImages(ctx, opts)

	if len(found) == 0 {
		return PrepareImageResult{Title: art.Title, StrategyUsed: "none"}
	}

	if len(found) == 1 {
		c := found[0]
		return PrepareImageResult{
			Title:        art.Title,
			ImageURL:     c.ImgURL,
			ImageSource:  c.Source,
			StrategyUsed: strategySource(c),
			ThumbnailURL: c.Thumbnail,
		}
	}

	// Multiple candidates: LLM multimodal picks the best.
	candidates := make([]imageCandidate, len(found))
	for i, c := range found {
		candidates[i] = imageCandidate{
			imgURL:    c.ImgURL,
			thumbnail: c.Thumbnail,
			source:    c.Source,
			strategy:  strategySource(c),
		}
	}

	chosen := pickBestImageLLM(ctx, art.Title, candidates)
	return PrepareImageResult{
		Title:        art.Title,
		ImageURL:     chosen.imgURL,
		ImageSource:  chosen.source,
		StrategyUsed: chosen.strategy,
		ThumbnailURL: chosen.thumbnail,
	}
}

// strategySource maps a candidate's source to a strategy name for reporting.
func strategySource(c imagefy.ImageCandidate) string {
	if c.Source == "wp-media" {
		return "media"
	}
	if c.Title == "og:image" {
		return "og"
	}
	return "search"
}
```

**Step 2: Remove `imageValidatorAdapter` struct** — no longer needed (OG validation now in go-imagefy).

**Step 3: Update vendor & verify**

Run: `cd ~/src/go-wp && GOWORK=off go mod tidy && go mod vendor && go build ./...`
Expected: builds successfully

**Step 4: Commit**

```bash
git add internal/wptools/media/prepare_select.go vendor/
git commit -m "refactor: prepare uses FindImages — OG and media go through go-imagefy filters"
```

---

## Task 7: Clean up deprecated code in go-wp

Remove `FetchOGImage` from `wordpress/image_helpers.go` (now handled by go-imagefy `OGImageProvider`).

**Files:**
- Modify: `internal/wordpress/image_helpers.go`

**Step 1: Check for other callers**

Run: `grep -r 'FetchOGImage\|fetchOGImage' ~/src/go-wp/internal/ --include='*.go' | grep -v vendor | grep -v _test.go`

If only `prepare_select.go` used it (now removed) — delete `fetchOGImage`, `FetchOGImage`, and `ImageValidator` interface.

Keep `SearchMediaLibrary` and `searchMediaLibrary` — still needed.

**Step 2: Remove dead code**

Remove from `image_helpers.go`:
- `ImageValidator` interface
- `fetchOGImage` function
- `FetchOGImage` function

Keep:
- `searchMediaLibrary` / `SearchMediaLibrary`
- `parseMediaResult`

**Step 3: Verify build**

Run: `cd ~/src/go-wp && GOWORK=off go build ./...`
Expected: builds successfully

**Step 4: Run all tests**

Run: `GOWORK=off go test ./... -count=1 -short`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/wordpress/image_helpers.go
git commit -m "cleanup: remove FetchOGImage — now handled by go-imagefy OGImageProvider"
```

---

## Task 8: Vendor update & deploy verification

**Step 1: Update go-imagefy vendor in go-wp**

```bash
cd ~/src/go-wp
GOWORK=off go mod tidy
go mod vendor
```

**Step 2: Run full test suite**

```bash
GOWORK=off go test ./... -count=1
```
Expected: all tests PASS

**Step 3: Build and deploy**

```bash
cd ~/deploy/krolik-server
docker compose build --no-cache go-wp
docker compose up -d --no-deps --force-recreate go-wp
```

**Step 4: Smoke test — verify image search works**

```bash
curl -s -X POST http://127.0.0.1:8894/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wp_image","arguments":{"action":"resolve","query":"Эрмитаж Санкт-Петербург"}}}' | jq .
```
Expected: non-empty `images` array with validated URLs

**Step 5: Commit final state**

```bash
git add -A
git commit -m "chore: vendor go-imagefy with unified gateway"
```
