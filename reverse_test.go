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
