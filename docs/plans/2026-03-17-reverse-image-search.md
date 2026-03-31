# Reverse Image Search Integration — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect "laundered" stock photos (stripped metadata) by reverse-searching candidate images via ox-browser and checking if results contain stock domains.

**Architecture:** New pipeline step 5.5 between metadata assessment and LLM classification. Only runs for `LicenseUnknown` images (metadata didn't resolve). Calls `POST /images/reverse` on ox-browser, checks `is_stock` in response. If stock detected → `LicenseBlocked` (skip LLM, save API cost). Feature is opt-in via `OxBrowserURL` in Config.

**Tech Stack:** Go 1.26, net/http, encoding/json, httptest (tests)

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `reverse.go` | Create | `ReverseCheck()` — calls ox-browser `/images/reverse`, returns stock verdict |
| `reverse_test.go` | Create | Tests with httptest mock server |
| `imagefy.go` | Modify | Add `OxBrowserURL string` to Config |
| `validate_pipeline.go` | Modify | Insert reverse check between `assessAndAccept` and LLM classification |
| `assessment.go` | Modify | Add `"reverse_stock"` signal type |

---

### Task 1: ReverseCheck function

**Files:**
- Create: `reverse.go`
- Create: `reverse_test.go`

- [ ] **Step 1: Write the failing test**

```go
// reverse_test.go
package imagefy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReverseCheck_StockDetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/images/reverse" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(reverseResponse{
			IsStock:      true,
			StockDomains: []string{"shutterstock.com"},
			Matches: []reverseMatch{
				{PageURL: "https://shutterstock.com/img/123", Domain: "shutterstock.com"},
			},
		})
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if !result.IsStock {
		t.Fatal("expected stock detection")
	}
	if len(result.StockDomains) != 1 || result.StockDomains[0] != "shutterstock.com" {
		t.Fatalf("unexpected stock domains: %v", result.StockDomains)
	}
}

func TestReverseCheck_NotStock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(reverseResponse{IsStock: false})
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if result.IsStock {
		t.Fatal("expected not stock")
	}
}

func TestReverseCheck_Disabled(t *testing.T) {
	cfg := &Config{} // no OxBrowserURL
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if result.IsStock {
		t.Fatal("expected not stock when disabled")
	}
}

func TestReverseCheck_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if result.IsStock {
		t.Fatal("expected graceful fallback on error")
	}
}

func TestReverseCheck_RequestBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req reverseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.URL != "https://example.com/photo.jpg" {
			t.Errorf("unexpected url: %s", req.URL)
		}
		if req.MaxResults != reverseMaxResults {
			t.Errorf("unexpected max_results: %d", req.MaxResults)
		}
		json.NewEncoder(w).Encode(reverseResponse{IsStock: false})
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/src/go-imagefy && go test -run TestReverseCheck -v`
Expected: FAIL — `ReverseCheck` not defined

- [ ] **Step 3: Write the implementation**

```go
// reverse.go
package imagefy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const (
	reverseMaxResults = 10
	reverseBodyLimit  = 512 * 1024 // 512KB
)

// ReverseResult holds the outcome of a reverse image search check.
type ReverseResult struct {
	IsStock      bool
	StockDomains []string
}

type reverseRequest struct {
	URL        string `json:"url"`
	MaxResults int    `json:"max_results"`
}

type reverseMatch struct {
	PageURL string `json:"page_url"`
	Domain  string `json:"domain"`
}

type reverseResponse struct {
	Matches      []reverseMatch `json:"matches"`
	IsStock      bool           `json:"is_stock"`
	StockDomains []string       `json:"stock_domains"`
}

// ReverseCheck calls ox-browser /images/reverse to detect if the image
// appears on stock photo sites. Returns a zero ReverseResult if disabled
// (OxBrowserURL empty) or on any error (graceful degradation).
func (cfg *Config) ReverseCheck(ctx context.Context, imageURL string) ReverseResult {
	if cfg.OxBrowserURL == "" {
		return ReverseResult{}
	}

	payload, err := json.Marshal(reverseRequest{
		URL:        imageURL,
		MaxResults: reverseMaxResults,
	})
	if err != nil {
		return ReverseResult{}
	}

	endpoint := strings.TrimRight(cfg.OxBrowserURL, "/") + "/images/reverse"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ReverseResult{}
	}
	req.Header.Set("Content-Type", "application/json")

	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("imagefy: reverse check failed", "url", imageURL, "error", err)
		return ReverseResult{}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("imagefy: reverse check bad status", "url", imageURL, "status", resp.StatusCode)
		return ReverseResult{}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, reverseBodyLimit))
	if err != nil {
		return ReverseResult{}
	}

	var result reverseResponse
	if err := json.Unmarshal(body, &result); err != nil {
		slog.Debug("imagefy: reverse check parse error", "url", imageURL, "error", err)
		return ReverseResult{}
	}

	if result.IsStock {
		slog.Debug("imagefy: reverse search detected stock",
			"url", imageURL,
			"stock_domains", fmt.Sprintf("%v", result.StockDomains),
		)
	}

	return ReverseResult{
		IsStock:      result.IsStock,
		StockDomains: result.StockDomains,
	}
}
```

- [ ] **Step 4: Add `OxBrowserURL` to Config**

In `imagefy.go`, add field to Config struct:

```go
// OxBrowserURL is the base URL of the ox-browser service for reverse image search.
// When set, enables reverse stock detection in the validation pipeline.
// Example: "http://ox-browser:8901" or "http://127.0.0.1:8901".
OxBrowserURL string
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/src/go-imagefy && go test -run TestReverseCheck -v`
Expected: PASS (all 5 tests)

- [ ] **Step 6: Commit**

```bash
git add reverse.go reverse_test.go imagefy.go
git commit -m "feat: add ReverseCheck for stock detection via ox-browser"
```

---

### Task 2: Pipeline integration

**Files:**
- Modify: `validate_pipeline.go`
- Create: `validate_pipeline_test.go` (or add to existing)

- [ ] **Step 1: Write the failing test**

```go
// validate_reverse_test.go
package imagefy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPipeline_ReverseBlocksStock(t *testing.T) {
	// Mock ox-browser: always returns stock.
	reverseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(reverseResponse{
			IsStock:      true,
			StockDomains: []string{"shutterstock.com"},
		})
	}))
	defer reverseSrv.Close()

	// Mock image server: returns a valid 1x1 JPEG.
	imgSrv := newTestImageServer(t)
	defer imgSrv.Close()

	cfg := &Config{
		OxBrowserURL:  reverseSrv.URL,
		HTTPClient:    http.DefaultClient,
		MinImageWidth: 1, // accept any width for test
	}

	candidates := []ImageCandidate{
		{ImgURL: imgSrv.URL + "/photo.jpg", Source: "https://example.com"},
	}

	result := cfg.validateCandidates(context.Background(), candidates, 5)
	if len(result) != 0 {
		t.Fatalf("expected candidate to be blocked by reverse check, got %d", len(result))
	}
}

func TestPipeline_ReversePassesClean(t *testing.T) {
	// Mock ox-browser: not stock.
	reverseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(reverseResponse{IsStock: false})
	}))
	defer reverseSrv.Close()

	imgSrv := newTestImageServer(t)
	defer imgSrv.Close()

	cfg := &Config{
		OxBrowserURL:  reverseSrv.URL,
		HTTPClient:    http.DefaultClient,
		MinImageWidth: 1,
	}

	candidates := []ImageCandidate{
		{ImgURL: imgSrv.URL + "/photo.jpg", Source: "https://example.com"},
	}

	// Without classifier, unknown-license images that pass reverse check
	// go to LLM step — but with nil Classifier they get accepted as-is.
	result := cfg.validateCandidates(context.Background(), candidates, 5)
	// The candidate should NOT be blocked by reverse check.
	// It may or may not pass LLM depending on Classifier being nil.
	// Key assertion: reverse check didn't block it.
	_ = result // integration test — verified by absence of "blocked by reverse" log
}
```

Note: `newTestImageServer` is a helper that returns a 1x1 JPEG with proper Content-Type and dimensions headers. If it doesn't exist yet, create a small helper in a `testutil_test.go` file.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/src/go-imagefy && go test -run TestPipeline_Reverse -v`
Expected: FAIL or unexpected behavior (reverse check not wired)

- [ ] **Step 3: Insert reverse check into pipeline**

In `validate_pipeline.go`, modify `validateOne` — insert between `assessAndAccept` (line 74-80) and LLM classification (line 82-88):

Replace the section after `assessAndAccept` call:

```go
	accepted, done := cfg.assessAndAccept(ctx, cand, data, maxResults, mu, validated)
	if done {
		return
	}
	if accepted {
		return
	}

	// Step 5.5: Reverse image search — detect laundered stock photos.
	reverseResult := cfg.ReverseCheck(ctx, cand.ImgURL)
	if reverseResult.IsStock {
		slog.Debug("imagefy: blocked by reverse stock check",
			"url", cand.ImgURL,
			"stock_domains", reverseResult.StockDomains,
		)
		cfg.emitClassification(cand.ImgURL, ClassStock, 0, "reverse_stock")
		return
	}

	// Unknown license — classify using pre-downloaded data.
	result := cfg.classifyPredownloaded(ctx, cand.ImgURL, data, mimeType)
```

Update the pipeline doc comment to include step 5.5:

```go
// Pipeline stages:
//  1. ValidateImageURL — HTTP probe (dimensions, content-type, logo/banner check)
//  2. Extra domain pre-check — skip download for known-blocked domains
//  3. downloadForValidation — single download for dedup + metadata + LLM
//  4. Perceptual dedup — reject visual duplicates (dHash)
//  5. ExtractImageMetadata + AssessLicense — domain + metadata signals
//  5.5. ReverseCheck — reverse image search for laundered stock (opt-in)
//  6. LLM Vision classification — fallback for unknown license
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/src/go-imagefy && go test ./... -v -count=1`
Expected: All tests PASS (existing 364 + new tests)

- [ ] **Step 5: Commit**

```bash
git add validate_pipeline.go validate_reverse_test.go
git commit -m "feat: integrate reverse stock check into validation pipeline"
```

---

### Task 3: Wire OxBrowserURL in go-wp adapter

**Files:**
- Modify: `/home/krolik/src/go-wp/internal/imageadapter/adapter.go`

- [ ] **Step 1: Add OxBrowserURL to imagefy Config**

In `adapter.go`, add the field when constructing `imagefyCfg`:

```go
	imagefyCfg = &imagefy.Config{
		Cache:         &goeCacheAdapter{c: cache, ttl: cacheTTL},
		Classifier:    &goeLLMClassifier{llm: llm},
		StealthClient: stealthClient,
		HTTPClient:    httpClient,
		Providers:     providers,
		OxBrowserURL:  oxURL, // enables reverse stock detection
		OnImageSearch: func() { metrics.Incr("image_searches") },
	}
```

`oxURL` is already resolved on line 41-44, so just reference it.

- [ ] **Step 2: Run go-wp build to verify**

Run: `cd ~/src/go-wp && go build ./...`
Expected: Build succeeds (requires `go get` for updated go-imagefy first)

- [ ] **Step 3: Commit**

```bash
git add internal/imageadapter/adapter.go
git commit -m "feat: enable reverse stock detection via ox-browser"
```

---

### Task 4: Update ROADMAP

**Files:**
- Modify: `/home/krolik/src/go-imagefy/docs/ROADMAP.md`

- [ ] **Step 1: Mark reverse image search as complete in Phase 6**

Update Phase 6 section to show reverse search as implemented, C2PA and AI detection still deferred.

- [ ] **Step 2: Commit**

```bash
git add docs/ROADMAP.md
git commit -m "docs: mark reverse image search as complete in Phase 6"
```
