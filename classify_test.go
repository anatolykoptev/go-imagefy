package imagefy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseVisionResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		resp string
		want string
	}{
		// Exact matches.
		{name: "PHOTO exact", resp: "PHOTO", want: "PHOTO"},
		{name: "STOCK exact", resp: "STOCK", want: "STOCK"},
		{name: "REJECT exact", resp: "REJECT", want: "REJECT"},

		// Case variations (LLM may return mixed case).
		{name: "photo lowercase", resp: "photo", want: "PHOTO"},
		{name: "Stock mixed case", resp: "Stock", want: "STOCK"},
		{name: "Reject mixed case", resp: "Reject", want: "REJECT"},

		// With trailing explanation (LLM sometimes adds text).
		{name: "PHOTO with explanation", resp: "PHOTO - real photograph", want: "PHOTO"},
		{name: "STOCK with explanation", resp: "STOCK - shutterstock watermark visible", want: "STOCK"},
		{name: "REJECT with explanation", resp: "REJECT - promotional banner", want: "REJECT"},

		// With whitespace.
		{name: "PHOTO with leading space", resp: "  PHOTO", want: "PHOTO"},
		{name: "STOCK with trailing newline", resp: "STOCK\n", want: "STOCK"},
		{name: "REJECT with surrounding whitespace", resp: "\n REJECT \n", want: "REJECT"},

		// Empty / garbage → graceful degradation.
		{name: "empty string", resp: "", want: ""},
		{name: "whitespace only", resp: "   ", want: ""},
		{name: "unexpected response", resp: "I cannot classify this image", want: ""},
		{name: "partial match YES", resp: "YES", want: ""},
		{name: "partial match OK", resp: "OK", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ParseVisionResponse(tc.resp)
			if got != tc.want {
				t.Errorf("ParseVisionResponse(%q) = %q, want %q", tc.resp, got, tc.want)
			}
		})
	}
}


// mockClassifier is a test double for the Classifier interface.
type mockClassifier struct {
	response string
	err      error
	calls    int
}

func (m *mockClassifier) Classify(_ context.Context, _ string, _ []ImageInput) (string, error) {
	m.calls++
	return m.response, m.err
}

// promptCapturingClassifier records the prompt passed to Classify.
type promptCapturingClassifier struct {
	response      string
	capturedPrompt string
}

func (p *promptCapturingClassifier) Classify(_ context.Context, prompt string, _ []ImageInput) (string, error) {
	p.capturedPrompt = prompt
	return p.response, nil
}

// mockCache is a test double for the Cache interface.
// It stores values as any to support both string and ClassificationResult.
type mockCache struct {
	store map[string]any
}

func (m *mockCache) Key(prefix, value string) string { return prefix + ":" + value }
func (m *mockCache) Get(_ context.Context, key string, dest any) bool {
	v, ok := m.store[key]
	if !ok {
		return false
	}
	switch p := dest.(type) {
	case *string:
		if s, ok := v.(string); ok {
			*p = s
			return true
		}
		return false
	case *ClassificationResult:
		if r, ok := v.(ClassificationResult); ok {
			*p = r
			return true
		}
		return false
	}
	return false
}
func (m *mockCache) Set(_ context.Context, key string, value any) {
	m.store[key] = value
}

func TestClassifyImageWithMock(t *testing.T) {
	t.Parallel()

	// Serve a minimal valid JPEG (enough bytes for the download).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		// 100-byte payload — enough to pass MinBytes=0 check.
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	mc := &mockClassifier{response: "STOCK"}
	cfg := &Config{
		Classifier: mc,
		HTTPClient: srv.Client(),
	}

	got := cfg.ClassifyImage(context.Background(), srv.URL+"/test.jpg")
	if got != "STOCK" {
		t.Errorf("ClassifyImage = %q, want %q", got, "STOCK")
	}
	if mc.calls != 1 {
		t.Errorf("Classifier.Classify called %d times, want 1", mc.calls)
	}
}

func TestClassifyImageNilClassifier(t *testing.T) {
	t.Parallel()

	cfg := &Config{} // Classifier is nil
	got := cfg.ClassifyImage(context.Background(), "https://example.com/image.jpg")
	if got != "" {
		t.Errorf("ClassifyImage with nil classifier = %q, want %q", got, "")
	}
}

func TestClassifyImageCaching(t *testing.T) {
	t.Parallel()

	// Serve a minimal valid JPEG.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	mc := &mockClassifier{response: "PHOTO"}
	cache := &mockCache{store: make(map[string]any)}
	cfg := &Config{
		Classifier: mc,
		Cache:      cache,
		HTTPClient: srv.Client(),
	}

	imageURL := srv.URL + "/test.jpg"

	// First call — should invoke the classifier.
	got1 := cfg.ClassifyImage(context.Background(), imageURL)
	if got1 != "PHOTO" {
		t.Errorf("first call = %q, want %q", got1, "PHOTO")
	}
	if mc.calls != 1 {
		t.Errorf("after first call: classifier called %d times, want 1", mc.calls)
	}

	// Second call — should hit the cache, not the classifier.
	got2 := cfg.ClassifyImage(context.Background(), imageURL)
	if got2 != "PHOTO" {
		t.Errorf("second call = %q, want %q", got2, "PHOTO")
	}
	if mc.calls != 1 {
		t.Errorf("after second call: classifier called %d times, want 1 (cache hit expected)", mc.calls)
	}
}

// --- New tests for extended classification ---

func TestParseClassificationResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		resp       string
		wantClass  string
		wantConf   float64
	}{
		// All 6 classes with confidence.
		{name: "PHOTO with confidence", resp: "PHOTO 0.95", wantClass: "PHOTO", wantConf: 0.95},
		{name: "STOCK with confidence", resp: "STOCK 0.80", wantClass: "STOCK", wantConf: 0.80},
		{name: "REJECT with confidence", resp: "REJECT 0.70", wantClass: "REJECT", wantConf: 0.70},
		{name: "SCREENSHOT with confidence", resp: "SCREENSHOT 0.99", wantClass: "SCREENSHOT", wantConf: 0.99},
		{name: "ILLUSTRATION with confidence", resp: "ILLUSTRATION 0.60", wantClass: "ILLUSTRATION", wantConf: 0.60},
		{name: "MAP with confidence", resp: "MAP 0.85", wantClass: "MAP", wantConf: 0.85},

		// Without confidence (just class).
		{name: "PHOTO no confidence", resp: "PHOTO", wantClass: "PHOTO", wantConf: 0},
		{name: "STOCK no confidence", resp: "STOCK", wantClass: "STOCK", wantConf: 0},
		{name: "ILLUSTRATION no confidence", resp: "ILLUSTRATION", wantClass: "ILLUSTRATION", wantConf: 0},

		// Case insensitivity.
		{name: "photo lowercase", resp: "photo 0.9", wantClass: "PHOTO", wantConf: 0.9},
		{name: "Stock mixed", resp: "Stock 0.75", wantClass: "STOCK", wantConf: 0.75},
		{name: "illustration lowercase", resp: "illustration 0.5", wantClass: "ILLUSTRATION", wantConf: 0.5},

		// Whitespace handling.
		{name: "leading whitespace", resp: "  PHOTO 0.9", wantClass: "PHOTO", wantConf: 0.9},
		{name: "trailing whitespace", resp: "PHOTO 0.9  ", wantClass: "PHOTO", wantConf: 0.9},
		{name: "extra spaces between", resp: "PHOTO  0.9", wantClass: "PHOTO", wantConf: 0.9},

		// LLM noise after valid response.
		{name: "trailing LLM text", resp: "PHOTO 0.92 - real photograph", wantClass: "PHOTO", wantConf: 0.92},

		// Out-of-range confidence → 0.
		{name: "confidence zero", resp: "PHOTO 0.0", wantClass: "PHOTO", wantConf: 0},
		{name: "confidence above 1", resp: "PHOTO 1.5", wantClass: "PHOTO", wantConf: 0},
		{name: "confidence negative", resp: "PHOTO -0.5", wantClass: "PHOTO", wantConf: 0},

		// Confidence exactly 1.0 is valid.
		{name: "confidence exactly 1", resp: "PHOTO 1.0", wantClass: "PHOTO", wantConf: 1.0},

		// Empty / garbage.
		{name: "empty string", resp: "", wantClass: "", wantConf: 0},
		{name: "whitespace only", resp: "   ", wantClass: "", wantConf: 0},
		{name: "garbage", resp: "I cannot classify this image", wantClass: "", wantConf: 0},
		{name: "unknown class", resp: "UNKNOWN 0.9", wantClass: "", wantConf: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ParseClassificationResult(tc.resp)
			if got.Class != tc.wantClass {
				t.Errorf("ParseClassificationResult(%q).Class = %q, want %q", tc.resp, got.Class, tc.wantClass)
			}
			// Use a tolerance for float comparison.
			const eps = 1e-9
			diff := got.Confidence - tc.wantConf
			if diff < -eps || diff > eps {
				t.Errorf("ParseClassificationResult(%q).Confidence = %v, want %v", tc.resp, got.Confidence, tc.wantConf)
			}
		})
	}
}

func TestClassifyImageFull_ReturnsResult(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	mc := &mockClassifier{response: "PHOTO 0.92"}
	cfg := &Config{
		Classifier: mc,
		HTTPClient: srv.Client(),
	}

	got := cfg.ClassifyImageFull(context.Background(), srv.URL+"/test.jpg")
	if got.Class != "PHOTO" {
		t.Errorf("ClassifyImageFull.Class = %q, want %q", got.Class, "PHOTO")
	}
	const wantConf = 0.92
	const eps = 1e-9
	diff := got.Confidence - wantConf
	if diff < -eps || diff > eps {
		t.Errorf("ClassifyImageFull.Confidence = %v, want %v", got.Confidence, wantConf)
	}
}

func TestClassifyImageFull_NilClassifier(t *testing.T) {
	t.Parallel()

	cfg := &Config{} // Classifier is nil
	got := cfg.ClassifyImageFull(context.Background(), "https://example.com/image.jpg")
	if got.Class != "" {
		t.Errorf("ClassifyImageFull with nil classifier: Class = %q, want %q", got.Class, "")
	}
	if got.Confidence != 0 {
		t.Errorf("ClassifyImageFull with nil classifier: Confidence = %v, want 0", got.Confidence)
	}
}

func TestClassifyImageFull_CustomPrompt(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	const customPrompt = "MY CUSTOM PROMPT"
	pc := &promptCapturingClassifier{response: "PHOTO 0.8"}
	cfg := &Config{
		Classifier:   pc,
		HTTPClient:   srv.Client(),
		VisionPrompt: customPrompt,
	}

	_ = cfg.ClassifyImageFull(context.Background(), srv.URL+"/test.jpg")
	if pc.capturedPrompt != customPrompt {
		t.Errorf("ClassifyImageFull used prompt %q, want %q", pc.capturedPrompt, customPrompt)
	}
}

func TestClassifyImageFull_DefaultPrompt(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	pc := &promptCapturingClassifier{response: "PHOTO 0.8"}
	cfg := &Config{
		Classifier: pc,
		HTTPClient: srv.Client(),
		// VisionPrompt not set → should use DefaultVisionPrompt
	}

	_ = cfg.ClassifyImageFull(context.Background(), srv.URL+"/test.jpg")
	if pc.capturedPrompt != DefaultVisionPrompt {
		t.Errorf("ClassifyImageFull used prompt %q, want DefaultVisionPrompt", pc.capturedPrompt)
	}
}

func TestClassifyImageFull_AuditLog(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	mc := &mockClassifier{response: "PHOTO 0.92"}

	var capturedEvent ClassificationEvent
	cfg := &Config{
		Classifier: mc,
		HTTPClient: srv.Client(),
		OnClassification: func(e ClassificationEvent) {
			capturedEvent = e
		},
	}

	imageURL := srv.URL + "/test.jpg"
	_ = cfg.ClassifyImageFull(context.Background(), imageURL)

	if capturedEvent.URL != imageURL {
		t.Errorf("audit log URL = %q, want %q", capturedEvent.URL, imageURL)
	}
	if capturedEvent.Class != "PHOTO" {
		t.Errorf("audit log Class = %q, want %q", capturedEvent.Class, "PHOTO")
	}
	const wantConf = 0.92
	const eps = 1e-9
	diff := capturedEvent.Confidence - wantConf
	if diff < -eps || diff > eps {
		t.Errorf("audit log Confidence = %v, want %v", capturedEvent.Confidence, wantConf)
	}
	if capturedEvent.Source != "llm" {
		t.Errorf("audit log Source = %q, want %q", capturedEvent.Source, "llm")
	}
}

func TestClassifyImageFull_AuditLogNilCallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	mc := &mockClassifier{response: "PHOTO 0.92"}
	cfg := &Config{
		Classifier:       mc,
		HTTPClient:       srv.Client(),
		OnClassification: nil, // explicitly nil — must not panic
	}

	// Should not panic.
	got := cfg.ClassifyImageFull(context.Background(), srv.URL+"/test.jpg")
	if got.Class != "PHOTO" {
		t.Errorf("ClassifyImageFull.Class = %q, want %q", got.Class, "PHOTO")
	}
}

func TestIsRealPhoto_NewClasses(t *testing.T) {
	t.Parallel()

	// IsRealPhoto should accept PHOTO and "" (graceful), reject all others.
	tests := []struct {
		cls  string
		want bool
	}{
		{"PHOTO", true},
		{"", true},        // error/unknown → graceful accept
		{"STOCK", false},
		{"REJECT", false},
		{"SCREENSHOT", false},
		{"ILLUSTRATION", false},
		{"MAP", false},
	}

	for _, tc := range tests {
		t.Run("cls="+tc.cls, func(t *testing.T) {
			t.Parallel()
			// Each subtest gets its own server to avoid closing races.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write(make([]byte, 100))
			}))
			defer srv.Close()

			mc := &mockClassifier{response: tc.cls}
			cfg := &Config{
				Classifier: mc,
				HTTPClient: srv.Client(),
			}
			got := cfg.IsRealPhoto(context.Background(), srv.URL+"/test.jpg")
			if got != tc.want {
				t.Errorf("IsRealPhoto for cls=%q = %v, want %v", tc.cls, got, tc.want)
			}
		})
	}
}
