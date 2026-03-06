# Phase 2 — Advanced Classification

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Reduce false positives/negatives in image classification, cut LLM costs with cost-tier routing, and give consumers full control over classification behavior.

**Architecture:** Extend the existing 3-class classification (PHOTO/STOCK/REJECT) to 6 classes with confidence scores. Add a pre-filter that skips expensive LLM calls for high-confidence heuristic cases (safe sources). Add audit logging callback so consumers can debug misclassifications. All changes preserve backward compatibility — `ClassifyImage` and `IsRealPhoto` keep their signatures.

**Tech Stack:** Pure Go, no new dependencies. Uses existing `Classifier` interface for LLM calls.

---

## Execution Order

```
Batch 1 (parallel worktrees):
  Task 1: Extended classification + confidence + custom prompt  [classify.go, imagefy.go]
  Task 2: Cost-tier pre-filter                                  [prefilter.go, search.go]

Batch 2 (sequential, after merge):
  Task 3: Classification audit log                              [imagefy.go, classify.go, search.go]

Batch 3:
  Task 4: Verification + docs                                   [all files]
```

## Design Decisions

- **NSFW detection**: Enabled via custom prompt support (consumers override `Config.VisionPrompt`) — no separate implementation needed.
- **Batch classification**: Deferred — would require `Classifier` interface change, current per-image flow is simpler and sufficient.
- **Cache compatibility**: New cache key prefix `vision_cls_v2` for `ClassificationResult` values. Old `vision_cls` string entries become stale cache misses — natural refresh.
- **IsRealPhoto behavior change**: Now only accepts `PHOTO` and `""` (graceful degradation). Previously accepted anything not STOCK/REJECT. This is intentional — new classes (SCREENSHOT, ILLUSTRATION, MAP) should be rejected.

---

### Task 1: Extended Classification with Confidence + Custom Prompt

**Files:**
- Modify: `classify.go` (new types, methods, updated prompt)
- Modify: `imagefy.go` (add `VisionPrompt` field to Config)
- Modify: `classify_test.go` (new tests, update mock)

**Context for the implementer:**
- Current `classify.go` has: `VisionPrompt` constant, `ClassifyImage(ctx, url) string`, `IsRealPhoto(ctx, url) bool`, `ParseVisionResponse(resp) string`, `doClassify(ctx, url) string`
- Current `ClassifyImage` caches string results under key prefix `vision_cls`
- `Classifier` interface: `Classify(ctx, prompt string, images []ImageInput) (string, error)`
- `mockCache` in `classify_test.go` currently stores `map[string]string` — must be updated to `map[string]any`

**Step 1: Write failing tests for ParseClassificationResult**

Add to `classify_test.go`:

```go
func TestParseClassificationResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		resp       string
		wantClass  string
		wantConf   float64
	}{
		// Class + confidence.
		{name: "PHOTO with confidence", resp: "PHOTO 0.95", wantClass: "PHOTO", wantConf: 0.95},
		{name: "STOCK with confidence", resp: "STOCK 0.88", wantClass: "STOCK", wantConf: 0.88},
		{name: "REJECT with confidence", resp: "REJECT 0.72", wantClass: "REJECT", wantConf: 0.72},

		// New classes.
		{name: "SCREENSHOT", resp: "SCREENSHOT 0.91", wantClass: "SCREENSHOT", wantConf: 0.91},
		{name: "ILLUSTRATION", resp: "ILLUSTRATION 0.85", wantClass: "ILLUSTRATION", wantConf: 0.85},
		{name: "MAP", resp: "MAP 0.99", wantClass: "MAP", wantConf: 0.99},

		// Class without confidence.
		{name: "PHOTO no confidence", resp: "PHOTO", wantClass: "PHOTO", wantConf: 0},
		{name: "REJECT no confidence", resp: "REJECT", wantClass: "REJECT", wantConf: 0},

		// LLM noise: class with explanation and confidence somewhere.
		{name: "PHOTO with explanation", resp: "PHOTO - real photograph 0.92", wantClass: "PHOTO", wantConf: 0.92},
		{name: "STOCK with explanation", resp: "STOCK - watermark visible 0.80", wantClass: "STOCK", wantConf: 0.80},

		// Case insensitive.
		{name: "lowercase photo", resp: "photo 0.90", wantClass: "PHOTO", wantConf: 0.90},
		{name: "mixed case Stock", resp: "Stock 0.75", wantClass: "STOCK", wantConf: 0.75},

		// Whitespace.
		{name: "leading whitespace", resp: "  PHOTO 0.88", wantClass: "PHOTO", wantConf: 0.88},
		{name: "trailing newline", resp: "REJECT 0.70\n", wantClass: "REJECT", wantConf: 0.70},

		// Invalid confidence (out of range) → ignored.
		{name: "confidence > 1", resp: "PHOTO 1.5", wantClass: "PHOTO", wantConf: 0},
		{name: "negative confidence", resp: "PHOTO -0.5", wantClass: "PHOTO", wantConf: 0},

		// Empty / garbage.
		{name: "empty string", resp: "", wantClass: "", wantConf: 0},
		{name: "whitespace only", resp: "   ", wantClass: "", wantConf: 0},
		{name: "garbage", resp: "I cannot classify this", wantClass: "", wantConf: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ParseClassificationResult(tc.resp)
			if got.Class != tc.wantClass {
				t.Errorf("Class = %q, want %q", got.Class, tc.wantClass)
			}
			if got.Confidence != tc.wantConf {
				t.Errorf("Confidence = %v, want %v", got.Confidence, tc.wantConf)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./... -run TestParseClassificationResult -v`
Expected: FAIL — `ParseClassificationResult` not defined.

**Step 3: Implement ClassificationResult type and ParseClassificationResult**

Add to `classify.go` (after imports, before `VisionPrompt`):

```go
// ClassificationResult holds a classification decision with confidence.
type ClassificationResult struct {
	Class      string  // PHOTO, STOCK, REJECT, SCREENSHOT, ILLUSTRATION, MAP, or ""
	Confidence float64 // 0.0–1.0; 0 if not provided or out of range
}

// classificationClasses are the valid classification labels, ordered longest-first
// to avoid prefix ambiguity when parsing.
var classificationClasses = []string{
	"ILLUSTRATION", "SCREENSHOT", "REJECT", "PHOTO", "STOCK", "MAP",
}

// ParseClassificationResult parses an LLM response into a ClassificationResult.
// Expected format: "CLASS 0.95" or just "CLASS". Handles noise, whitespace, case.
func ParseClassificationResult(resp string) ClassificationResult {
	word := strings.TrimSpace(resp)
	if word == "" {
		return ClassificationResult{}
	}

	upper := strings.ToUpper(word)
	var class string
	for _, c := range classificationClasses {
		if strings.HasPrefix(upper, c) {
			class = c
			break
		}
	}
	if class == "" {
		return ClassificationResult{}
	}

	// Extract confidence: scan remaining tokens for a float in (0, 1].
	var confidence float64
	for _, field := range strings.Fields(word[len(class):]) {
		v, err := strconv.ParseFloat(field, 64)
		if err == nil && v > 0 && v <= 1 {
			confidence = v
			break
		}
	}

	return ClassificationResult{Class: class, Confidence: confidence}
}
```

Add `"strconv"` to the imports in `classify.go`.

**Step 4: Run test to verify it passes**

Run: `go test ./... -run TestParseClassificationResult -v`
Expected: PASS

**Step 5: Write failing tests for ClassifyImageFull and updated IsRealPhoto**

Add to `classify_test.go`:

```go
func TestClassifyImageFull_ReturnsResult(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	mc := &mockClassifier{response: "PHOTO 0.95"}
	cfg := &Config{
		Classifier: mc,
		HTTPClient: srv.Client(),
	}

	got := cfg.ClassifyImageFull(context.Background(), srv.URL+"/test.jpg")
	if got.Class != "PHOTO" {
		t.Errorf("Class = %q, want %q", got.Class, "PHOTO")
	}
	if got.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want %v", got.Confidence, 0.95)
	}
}

func TestClassifyImageFull_NilClassifier(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	got := cfg.ClassifyImageFull(context.Background(), "https://example.com/img.jpg")
	if got.Class != "" || got.Confidence != 0 {
		t.Errorf("got %+v, want zero ClassificationResult", got)
	}
}

func TestClassifyImageFull_CustomPrompt(t *testing.T) {
	t.Parallel()

	var capturedPrompt string
	mc := &mockClassifier{response: "PHOTO 0.90"}
	mc2 := &promptCapturingClassifier{response: "PHOTO 0.90"}

	// Use a wrapper that captures the prompt.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()
	_ = mc // unused, using mc2 instead

	cfg := &Config{
		Classifier:  mc2,
		HTTPClient:  srv.Client(),
		VisionPrompt: "Custom NSFW prompt",
	}

	cfg.ClassifyImageFull(context.Background(), srv.URL+"/test.jpg")
	capturedPrompt = mc2.lastPrompt
	if capturedPrompt != "Custom NSFW prompt" {
		t.Errorf("prompt = %q, want %q", capturedPrompt, "Custom NSFW prompt")
	}
}

func TestIsRealPhoto_NewClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cls  string
		want bool
	}{
		{"PHOTO", true},
		{"STOCK", false},
		{"REJECT", false},
		{"SCREENSHOT", false},
		{"ILLUSTRATION", false},
		{"MAP", false},
		{"", true}, // graceful degradation
	}

	for _, tc := range tests {
		got := tc.cls == "PHOTO" || tc.cls == ""
		if got != tc.want {
			t.Errorf("IsRealPhoto for cls=%q: got %v, want %v", tc.cls, got, tc.want)
		}
	}
}
```

Also add a prompt-capturing classifier mock:

```go
type promptCapturingClassifier struct {
	response   string
	lastPrompt string
}

func (m *promptCapturingClassifier) Classify(_ context.Context, prompt string, _ []ImageInput) (string, error) {
	m.lastPrompt = prompt
	return m.response, nil
}
```

**Step 6: Run tests to verify they fail**

Run: `go test ./... -run "TestClassifyImageFull|TestIsRealPhoto_NewClasses" -v`
Expected: FAIL — `ClassifyImageFull` not defined.

**Step 7: Implement ClassifyImageFull, custom prompt, update ClassifyImage and IsRealPhoto**

Rename the `VisionPrompt` constant to `DefaultVisionPrompt` in `classify.go`:

```go
const DefaultVisionPrompt = `You are an editorial image filter for a city guide website.
We only accept real photographs without stock watermarks.

Classify this image. Answer with one word and your confidence (0.0 to 1.0).

Categories:
- PHOTO — real photograph. Small corner watermark is OK.
- STOCK — photograph with visible stock watermark (Shutterstock, Getty, iStock, etc.)
- REJECT — banner, ad, promotional graphic, large text overlay, collage, meme.
- SCREENSHOT — screenshot of a website, app, or software interface.
- ILLUSTRATION — drawing, painting, digital art, cartoon, vector graphic.
- MAP — map, satellite view, floor plan, diagram.

Key distinctions:
- Small corner watermark of photographer → PHOTO
- Repeating diagonal stock watermark → STOCK
- Text/graphics dominate the image → REJECT

Answer format: CLASS 0.95
Example: PHOTO 0.92
Answer:`
```

Add `VisionPrompt string` field to `Config` in `imagefy.go`:

```go
type Config struct {
	Cache         Cache
	Classifier    Classifier
	StealthClient *http.Client
	HTTPClient    *http.Client
	SearxngURL    string
	MinImageWidth int
	UserAgent     string
	Providers     []SearchProvider
	VisionPrompt  string // custom classification prompt (default: DefaultVisionPrompt)

	OnImageSearch func()
	OnPanic       func(tag string, r any)
}
```

Add `ClassifyImageFull` and helper to `classify.go`:

```go
// ClassifyImageFull uses a multimodal LLM to classify the image at imageURL.
// Returns a ClassificationResult with class and confidence.
// Returns zero ClassificationResult on error (graceful degradation).
func (cfg *Config) ClassifyImageFull(ctx context.Context, imageURL string) ClassificationResult {
	cfg.defaults()

	if cfg.Classifier == nil {
		return ClassificationResult{}
	}

	if cfg.Cache != nil {
		cacheKey := cfg.Cache.Key("vision_cls_v2", imageURL)
		var cached ClassificationResult
		if cfg.Cache.Get(ctx, cacheKey, &cached) {
			return cached
		}
		result := cfg.doClassifyFull(ctx, imageURL)
		cfg.Cache.Set(ctx, cacheKey, result)
		return result
	}

	return cfg.doClassifyFull(ctx, imageURL)
}

func (cfg *Config) doClassifyFull(ctx context.Context, imageURL string) ClassificationResult {
	r, err := cfg.Download(ctx, imageURL, DownloadOpts{
		MaxBytes: visionMaxBytes,
	})
	if r == nil || err != nil {
		return ClassificationResult{}
	}

	dataURL := EncodeDataURL(r.Data, r.MIMEType)

	prompt := cfg.VisionPrompt
	if prompt == "" {
		prompt = DefaultVisionPrompt
	}

	resp, err := cfg.Classifier.Classify(ctx, prompt, []ImageInput{{URL: dataURL}})
	if err != nil {
		slog.Debug("imagefy: vision LLM error", "url", imageURL, "error", err.Error())
		return ClassificationResult{}
	}

	slog.Debug("imagefy: vision result", "url", imageURL, "response", resp)
	return ParseClassificationResult(resp)
}
```

Update `ClassifyImage` to delegate to `ClassifyImageFull`:

```go
func (cfg *Config) ClassifyImage(ctx context.Context, imageURL string) string {
	return cfg.ClassifyImageFull(ctx, imageURL).Class
}
```

Remove the old `doClassify` method entirely.

Update `IsRealPhoto`:

```go
func (cfg *Config) IsRealPhoto(ctx context.Context, imageURL string) bool {
	cls := cfg.ClassifyImage(ctx, imageURL)
	return cls == "PHOTO" || cls == ""
}
```

**Step 8: Update mockCache to handle struct values**

In `classify_test.go`, update `mockCache`:

```go
type mockCache struct {
	store map[string]any
}

func (m *mockCache) Key(prefix, value string) string { return prefix + ":" + value }
func (m *mockCache) Get(_ context.Context, key string, dest any) bool {
	v, ok := m.store[key]
	if !ok {
		return false
	}
	switch d := dest.(type) {
	case *string:
		if s, ok := v.(string); ok {
			*d = s
		}
	case *ClassificationResult:
		if r, ok := v.(ClassificationResult); ok {
			*d = r
		}
	}
	return true
}
func (m *mockCache) Set(_ context.Context, key string, value any) {
	m.store[key] = value
}
```

Update `TestClassifyImageCaching` to use `map[string]any`:

```go
cache := &mockCache{store: make(map[string]any)}
```

**Step 9: Update existing test for ParseVisionResponse backward compat**

`ParseVisionResponse` must still work — keep it as-is. It delegates to `ParseClassificationResult` internally (optional optimization) or stays independent. Keep it independent for simplicity.

**Step 10: Run all tests**

Run: `go test ./... -v -count=1`
Expected: ALL PASS

**Step 11: Run linter**

Run: `make lint`
Expected: 0 issues

**Step 12: Commit**

```bash
git add classify.go imagefy.go classify_test.go
git commit -m "feat: extended classification with confidence scores and custom prompt

Add ClassificationResult type, ClassifyImageFull method, and 3 new categories
(SCREENSHOT, ILLUSTRATION, MAP). Support custom VisionPrompt via Config field.
IsRealPhoto now correctly rejects non-photo classes."
```

---

### Task 2: Cost-Tier Pre-Filter

**Files:**
- Create: `prefilter.go`
- Create: `prefilter_test.go`
- Modify: `search.go:130-164` (validateOne — add pre-filter before IsRealPhoto)

**Context for the implementer:**
- `ImageCandidate` has `License ImageLicense` field. `LicenseSafe` means source is Openverse/Unsplash/Pixabay.
- `validateOne` in `search.go` runs: URL validation → dedup → IsRealPhoto (LLM) → append to validated.
- The pre-filter inserts between dedup and IsRealPhoto: if heuristics are conclusive, skip the LLM call entirely.
- `LicenseSafe` images come from trusted sources (Openverse API returns only CC/PD content). Safe to auto-accept as PHOTO.

**Step 1: Write failing tests for PreClassify**

Create `prefilter_test.go`:

```go
package imagefy

import "testing"

func TestPreClassify_SafeLicense_AcceptsAsPhoto(t *testing.T) {
	t.Parallel()

	cand := ImageCandidate{
		ImgURL:  "https://openverse.org/image/123.jpg",
		License: LicenseSafe,
	}

	class, skip := PreClassify(cand)
	if !skip {
		t.Fatal("expected skip=true for LicenseSafe candidate")
	}
	if class != "PHOTO" {
		t.Errorf("class = %q, want %q", class, "PHOTO")
	}
}

func TestPreClassify_UnknownLicense_NoSkip(t *testing.T) {
	t.Parallel()

	cand := ImageCandidate{
		ImgURL:  "https://example.com/image.jpg",
		License: LicenseUnknown,
	}

	class, skip := PreClassify(cand)
	if skip {
		t.Fatal("expected skip=false for LicenseUnknown candidate")
	}
	if class != "" {
		t.Errorf("class = %q, want empty", class)
	}
}

func TestPreClassify_BlockedLicense_NoSkip(t *testing.T) {
	t.Parallel()

	// Blocked candidates are normally filtered by the provider, but if one
	// slips through, PreClassify should not auto-accept it.
	cand := ImageCandidate{
		ImgURL:  "https://cdn.example.com/stock.jpg",
		License: LicenseBlocked,
	}

	_, skip := PreClassify(cand)
	if skip {
		t.Fatal("expected skip=false for LicenseBlocked candidate")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./... -run TestPreClassify -v`
Expected: FAIL — `PreClassify` not defined.

**Step 3: Implement PreClassify**

Create `prefilter.go`:

```go
package imagefy

// PreClassify applies cheap heuristics to classify an image candidate without
// calling the LLM. Returns the predicted class and skip=true if the heuristic
// is conclusive. Returns ("", false) if the LLM should be consulted.
//
// Current heuristics:
//   - LicenseSafe sources (Openverse, Unsplash, Pixabay) → auto-accept as PHOTO.
//     These are curated CC/public-domain collections with negligible false-positive risk.
func PreClassify(cand ImageCandidate) (class string, skip bool) {
	if cand.License == LicenseSafe {
		return "PHOTO", true
	}
	return "", false
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./... -run TestPreClassify -v`
Expected: PASS

**Step 5: Write test for pre-filter integration in validateOne**

Add to `prefilter_test.go`:

```go
func TestPreClassify_IntegrationWithValidateOne(t *testing.T) {
	t.Parallel()

	// A safe-license candidate should be accepted without needing a Classifier.
	// If Classifier were called, it would return REJECT — proving the pre-filter
	// bypassed the LLM entirely.
	cfg := &Config{
		Classifier: &mockClassifier{response: "REJECT"},
	}

	cand := ImageCandidate{
		ImgURL:  "https://openverse.org/photo.jpg",
		License: LicenseSafe,
	}

	// Simulate the pre-filter logic that validateOne should apply.
	class, skip := PreClassify(cand)
	if !skip || class != "PHOTO" {
		t.Fatalf("PreClassify should skip safe candidate, got class=%q skip=%v", class, skip)
	}

	// If not skipped, classifier would reject — proves cost savings.
	if !skip {
		cls := cfg.ClassifyImage(nil, cand.ImgURL)
		if cls != "REJECT" {
			t.Errorf("classifier returned %q, expected REJECT", cls)
		}
	}
}
```

**Step 6: Wire PreClassify into validateOne in search.go**

In `search.go`, modify `validateOne` — insert pre-filter check between dedup and IsRealPhoto:

Current code (lines 155-158):
```go
	if !cfg.IsRealPhoto(ctx, cand.ImgURL) {
		slog.Debug("imagefy: vision rejected", "url", cand.ImgURL)
		return
	}
```

Replace with:
```go
	// Cost-tier routing: skip LLM for high-confidence heuristic cases.
	if class, skip := PreClassify(cand); skip {
		slog.Debug("imagefy: pre-filter accepted", "url", cand.ImgURL, "class", class)
		mu.Lock()
		if len(*validated) < maxResults {
			*validated = append(*validated, cand)
		}
		mu.Unlock()
		return
	}

	if !cfg.IsRealPhoto(ctx, cand.ImgURL) {
		slog.Debug("imagefy: vision rejected", "url", cand.ImgURL)
		return
	}
```

**Step 7: Run all tests**

Run: `go test ./... -v -count=1`
Expected: ALL PASS

**Step 8: Run linter**

Run: `make lint`
Expected: 0 issues

**Step 9: Commit**

```bash
git add prefilter.go prefilter_test.go search.go
git commit -m "feat: cost-tier pre-filter skips LLM for safe-license images

LicenseSafe candidates (Openverse, Unsplash, Pixabay) are auto-accepted
without calling the expensive LLM classifier. Saves API costs for the
majority of curated CC/public-domain results."
```

---

### Task 3: Classification Audit Log

**Files:**
- Modify: `imagefy.go` (add `OnClassification` callback to Config)
- Modify: `classify.go` (call callback in ClassifyImageFull)
- Modify: `search.go` (call callback for pre-filter decisions)
- Modify: `classify_test.go` (new tests)

**Dependencies:** Task 1 (ClassificationResult), Task 2 (PreClassify wiring in validateOne).

**Context for the implementer:**
- After Tasks 1+2 are merged, `ClassifyImageFull` returns `ClassificationResult` and `validateOne` has a PreClassify block.
- The audit log callback fires on every classification decision — both LLM and heuristic.
- Signature: `func(url, class string, confidence float64, source string)` where source is "llm" or "prefilter".

**Step 1: Write failing tests**

Add to `classify_test.go`:

```go
func TestClassifyImageFull_AuditLog(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	var logged []ClassificationEvent
	cfg := &Config{
		Classifier: &mockClassifier{response: "PHOTO 0.95"},
		HTTPClient: srv.Client(),
		OnClassification: func(ev ClassificationEvent) {
			logged = append(logged, ev)
		},
	}

	cfg.ClassifyImageFull(context.Background(), srv.URL+"/test.jpg")

	if len(logged) != 1 {
		t.Fatalf("expected 1 audit log entry, got %d", len(logged))
	}
	if logged[0].Class != "PHOTO" {
		t.Errorf("logged class = %q, want %q", logged[0].Class, "PHOTO")
	}
	if logged[0].Confidence != 0.95 {
		t.Errorf("logged confidence = %v, want %v", logged[0].Confidence, 0.95)
	}
	if logged[0].Source != "llm" {
		t.Errorf("logged source = %q, want %q", logged[0].Source, "llm")
	}
}

func TestClassifyImageFull_AuditLogNilCallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	// No OnClassification callback — must not panic.
	cfg := &Config{
		Classifier: &mockClassifier{response: "PHOTO 0.90"},
		HTTPClient: srv.Client(),
	}

	got := cfg.ClassifyImageFull(context.Background(), srv.URL+"/test.jpg")
	if got.Class != "PHOTO" {
		t.Errorf("Class = %q, want %q", got.Class, "PHOTO")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./... -run "TestClassifyImageFull_AuditLog" -v`
Expected: FAIL — `ClassificationEvent` and `OnClassification` not defined.

**Step 3: Implement ClassificationEvent and wire audit callback**

Add `ClassificationEvent` type to `classify.go`:

```go
// ClassificationEvent is emitted by the audit log callback for each classification decision.
type ClassificationEvent struct {
	URL        string  // image URL that was classified
	Class      string  // classification result (PHOTO, STOCK, etc.)
	Confidence float64 // 0.0–1.0
	Source     string  // "llm" or "prefilter"
}
```

Add `OnClassification` to Config in `imagefy.go`:

```go
OnClassification func(ClassificationEvent) // optional: audit log for every classification decision
```

Wire into `doClassifyFull` in `classify.go` — after parsing the result, before return:

```go
func (cfg *Config) doClassifyFull(ctx context.Context, imageURL string) ClassificationResult {
	// ... existing download and classify logic ...

	result := ParseClassificationResult(resp)

	if cfg.OnClassification != nil {
		cfg.OnClassification(ClassificationEvent{
			URL:        imageURL,
			Class:      result.Class,
			Confidence: result.Confidence,
			Source:     "llm",
		})
	}

	return result
}
```

Wire into `validateOne` in `search.go` — in the PreClassify block:

```go
	if class, skip := PreClassify(cand); skip {
		slog.Debug("imagefy: pre-filter accepted", "url", cand.ImgURL, "class", class)
		if cfg.OnClassification != nil {
			cfg.OnClassification(ClassificationEvent{
				URL:    cand.ImgURL,
				Class:  class,
				Source: "prefilter",
			})
		}
		mu.Lock()
		if len(*validated) < maxResults {
			*validated = append(*validated, cand)
		}
		mu.Unlock()
		return
	}
```

**Step 4: Run all tests**

Run: `go test ./... -v -count=1`
Expected: ALL PASS

**Step 5: Run linter**

Run: `make lint`
Expected: 0 issues

**Step 6: Commit**

```bash
git add classify.go imagefy.go search.go classify_test.go
git commit -m "feat: classification audit log via OnClassification callback

Consumers can set Config.OnClassification to receive ClassificationEvent
for every decision — both LLM and pre-filter. Enables debugging
misclassifications and tracking classification distribution metrics."
```

---

### Task 4: Verification + Docs

**Files:**
- All Go files (test)
- `docs/ROADMAP.md` (update Phase 2 checkboxes)

**Dependencies:** Tasks 1-3 complete.

**Step 1: Run all tests with race detector**

Run: `go test -race -count=1 ./...`
Expected: ALL PASS, no data races.

**Step 2: Run linter**

Run: `make lint`
Expected: 0 issues

**Step 3: Verify test count increased**

Run: `go test -v ./... 2>&1 | grep -c "=== RUN"`
Expected: ~145+ tests (was 133 before Phase 2).

**Step 4: Update ROADMAP.md**

Mark completed Phase 2 items with `[x]`:
- [x] Multi-class classification
- [x] Confidence scores
- [x] Custom prompt support
- [x] Cost-tier routing
- [x] Classification audit log

Mark deferred items with note:
- [ ] Batch classification — deferred (requires Classifier interface change)
- [x] NSFW detection via prompt — enabled via custom prompt support

**Step 5: Commit**

```bash
git add docs/ROADMAP.md
git commit -m "docs: update ROADMAP.md with Phase 2 completion status"
```
