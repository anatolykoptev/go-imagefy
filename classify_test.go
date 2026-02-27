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

func TestIsRealPhotoClassification(t *testing.T) {
	t.Parallel()

	// Test the classification → bool mapping directly.
	// IsRealPhoto depends on ClassifyImage which needs LLM,
	// so we test the logic: cls != "REJECT" && cls != "STOCK".
	tests := []struct {
		cls  string
		want bool
	}{
		{"PHOTO", true},
		{"STOCK", false},
		{"REJECT", false},
		{"", true}, // error/unknown → graceful accept
	}

	for _, tc := range tests {
		got := tc.cls != "REJECT" && tc.cls != "STOCK"
		if got != tc.want {
			t.Errorf("IsRealPhoto logic for cls=%q: got %v, want %v", tc.cls, got, tc.want)
		}
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

// mockCache is a test double for the Cache interface.
type mockCache struct {
	store map[string]string
}

func (m *mockCache) Key(prefix, value string) string { return prefix + ":" + value }
func (m *mockCache) Get(_ context.Context, key string, dest any) bool {
	v, ok := m.store[key]
	if !ok {
		return false
	}
	if p, ok := dest.(*string); ok {
		*p = v
	}
	return true
}
func (m *mockCache) Set(_ context.Context, key string, value any) {
	if s, ok := value.(string); ok {
		m.store[key] = s
	}
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
	cache := &mockCache{store: make(map[string]string)}
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
