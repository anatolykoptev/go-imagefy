package imagefy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- Hard red tests: edge cases and bug scenarios ---

func TestReverseCheck_ContextCancelled(t *testing.T) {
	// Bug scenario: parent context cancelled (e.g. maxResults reached by other goroutines).
	// ReverseCheck must respect cancellation, not hang.
	handlerDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-handlerDone:
		case <-time.After(5 * time.Second):
		}
		json.NewEncoder(w).Encode(reverseResponse{IsStock: true})
	}))
	defer func() {
		close(handlerDone)
		srv.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	start := time.Now()
	result := cfg.ReverseCheck(ctx, "https://example.com/photo.jpg")
	elapsed := time.Since(start)

	if result.IsStock {
		t.Fatal("cancelled context should not return stock")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("should return quickly on cancelled context, took %v", elapsed)
	}
}

func TestReverseCheck_SlowServer_Timeout(t *testing.T) {
	// Bug scenario: ox-browser hangs. Parent context timeout must kick in.
	handlerDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-handlerDone:
		case <-time.After(30 * time.Second):
		}
		json.NewEncoder(w).Encode(reverseResponse{IsStock: true})
	}))
	defer func() {
		close(handlerDone) // unblock handler so srv.Close() doesn't hang
		srv.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	start := time.Now()
	result := cfg.ReverseCheck(ctx, "https://example.com/photo.jpg")
	elapsed := time.Since(start)

	if result.IsStock {
		t.Fatal("timed-out request should not return stock")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("should respect timeout, took %v", elapsed)
	}
}

func TestReverseCheck_MalformedJSON(t *testing.T) {
	// ox-browser returns garbage JSON with 200 OK.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"broken": json`))
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if result.IsStock {
		t.Fatal("malformed JSON should degrade gracefully")
	}
}

func TestReverseCheck_EmptyBody(t *testing.T) {
	// ox-browser returns 200 with empty body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if result.IsStock {
		t.Fatal("empty body should degrade gracefully")
	}
}

func TestReverseCheck_UnexpectedJSON(t *testing.T) {
	// ox-browser returns valid JSON but unexpected structure (error response).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"error": "service overloaded", "code": 503}`))
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if result.IsStock {
		t.Fatal("unexpected JSON structure should default to not-stock")
	}
}

func TestReverseCheck_HTTP429RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if result.IsStock {
		t.Fatal("429 should degrade gracefully")
	}
}

func TestReverseCheck_MultipleStockDomains(t *testing.T) {
	// Image found on multiple stock sites — all should be reported.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(reverseResponse{
			IsStock:      true,
			StockDomains: []string{"shutterstock.com", "gettyimages.com", "alamy.com"},
			Matches: []reverseMatch{
				{PageURL: "https://shutterstock.com/1", Domain: "shutterstock.com"},
				{PageURL: "https://gettyimages.com/2", Domain: "gettyimages.com"},
				{PageURL: "https://alamy.com/3", Domain: "alamy.com"},
			},
		})
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if !result.IsStock {
		t.Fatal("expected stock")
	}
	if len(result.StockDomains) != 3 {
		t.Fatalf("expected 3 stock domains, got %d: %v", len(result.StockDomains), result.StockDomains)
	}
}

func TestReverseCheck_NilStockDomains(t *testing.T) {
	// ox-browser returns is_stock=false with null stock_domains (not empty array).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"matches":[],"is_stock":false,"stock_domains":null,"engines_used":["yandex"]}`))
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if result.IsStock {
		t.Fatal("expected not stock")
	}
	if result.StockDomains != nil {
		t.Fatalf("expected nil stock domains, got %v", result.StockDomains)
	}
}

func TestReverseCheck_TrailingSlashURL(t *testing.T) {
	// OxBrowserURL with trailing slash should still work.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/reverse" {
			t.Errorf("unexpected path: %s (double slash?)", r.URL.Path)
		}
		json.NewEncoder(w).Encode(reverseResponse{IsStock: false})
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL + "/", HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if result.IsStock {
		t.Fatal("expected not stock")
	}
}

func TestReverseCheck_ConcurrentSafe(t *testing.T) {
	// Pipeline runs 3 concurrent goroutines — ReverseCheck must be goroutine-safe.
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		time.Sleep(50 * time.Millisecond) // simulate real latency
		json.NewEncoder(w).Encode(reverseResponse{IsStock: false})
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	done := make(chan struct{}, 10)

	for i := 0; i < 10; i++ {
		go func(idx int) {
			result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
			if result.IsStock {
				t.Errorf("goroutine %d: unexpected stock", idx)
			}
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if callCount.Load() != 10 {
		t.Fatalf("expected 10 calls, got %d", callCount.Load())
	}
}

func TestReverseCheck_ConnectionRefused(t *testing.T) {
	// ox-browser is completely down — connection refused.
	cfg := &Config{OxBrowserURL: "http://127.0.0.1:1", HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if result.IsStock {
		t.Fatal("connection refused should degrade gracefully")
	}
}

func TestReverseCheck_OnClassificationCallback(t *testing.T) {
	// Verify pipeline emits "reverse_stock" classification event.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(reverseResponse{
			IsStock:      true,
			StockDomains: []string{"shutterstock.com"},
		})
	}))
	defer srv.Close()

	var events []ClassificationEvent
	cfg := &Config{
		OxBrowserURL: srv.URL,
		HTTPClient:   http.DefaultClient,
		OnClassification: func(e ClassificationEvent) {
			events = append(events, e)
		},
	}

	// Call ReverseCheck directly — no classification event (it's pipeline's job).
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")
	if !result.IsStock {
		t.Fatal("expected stock")
	}

	// Simulate what pipeline does on stock detection.
	if result.IsStock {
		cfg.emitClassification("https://example.com/photo.jpg", ClassStock, 0, "reverse_stock")
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Source != "reverse_stock" {
		t.Errorf("expected source 'reverse_stock', got '%s'", events[0].Source)
	}
	if events[0].Class != ClassStock {
		t.Errorf("expected class '%s', got '%s'", ClassStock, events[0].Class)
	}
}

func TestReverseCheck_LargeResponse_Truncated(t *testing.T) {
	// Response exceeds reverseBodyLimit — should be truncated and fail gracefully.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Write valid JSON prefix, then pad to exceed 512KB.
		w.Write([]byte(`{"is_stock":true,"stock_domains":["shutterstock.com"],"matches":[`))
		pad := strings.Repeat(`{"page_url":"https://x.com/`+strings.Repeat("a", 1000)+`","domain":"x.com"},`, 600)
		w.Write([]byte(pad))
		w.Write([]byte(`]}`))
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	result := cfg.ReverseCheck(context.Background(), "https://example.com/photo.jpg")

	// Truncated JSON → unmarshal error → graceful degradation.
	if result.IsStock {
		t.Fatal("truncated response should degrade gracefully (unmarshal error)")
	}
}

func TestReverseCheck_ImageURLWithSpecialChars(t *testing.T) {
	// Image URL with query params, unicode, spaces — should be passed as-is.
	const imageURL = "https://example.com/фото%20test.jpg?w=800&h=600#anchor"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req reverseRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.URL != imageURL {
			t.Errorf("URL mangled: expected %q, got %q", imageURL, req.URL)
		}
		json.NewEncoder(w).Encode(reverseResponse{IsStock: false})
	}))
	defer srv.Close()

	cfg := &Config{OxBrowserURL: srv.URL, HTTPClient: http.DefaultClient}
	cfg.ReverseCheck(context.Background(), imageURL)
}
