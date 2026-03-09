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
	if got := p.Name(); got != "og" {
		t.Errorf("Name() = %q, want %q", got, "og")
	}
}

func TestOGImageProvider_Search_ExtractsOGImage(t *testing.T) {
	t.Parallel()

	const html = `<html><head>
		<meta property="og:image" content="https://example.com/photo.jpg">
	</head><body></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	p := &OGImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "ignored", SearchOpts{PageURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].ImgURL != "https://example.com/photo.jpg" {
		t.Errorf("ImgURL = %q, want %q", results[0].ImgURL, "https://example.com/photo.jpg")
	}
	if results[0].Source != srv.URL {
		t.Errorf("Source = %q, want %q", results[0].Source, srv.URL)
	}
	if results[0].Title != "og:image" {
		t.Errorf("Title = %q, want %q", results[0].Title, "og:image")
	}
}

func TestOGImageProvider_Search_NoPageURL(t *testing.T) {
	t.Parallel()

	p := &OGImageProvider{}
	results, err := p.Search(context.Background(), "query", SearchOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("got %v, want nil", results)
	}
}

func TestOGImageProvider_Search_NoOGTag(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>No OG</title></head><body></body></html>`))
	}))
	defer srv.Close()

	p := &OGImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "", SearchOpts{PageURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestOGImageProvider_Search_BlockedDomain(t *testing.T) {
	t.Parallel()

	const html = `<html><head>
		<meta property="og:image" content="https://www.shutterstock.com/image-123.jpg">
	</head><body></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	p := &OGImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "", SearchOpts{PageURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0 (blocked domain)", len(results))
	}
}

func TestOGImageProvider_Search_LogoBannerFiltered(t *testing.T) {
	t.Parallel()

	const html = `<html><head>
		<meta property="og:image" content="https://example.com/site-logo.png">
	</head><body></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	p := &OGImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "", SearchOpts{PageURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0 (logo filtered)", len(results))
	}
}

func TestOGImageProvider_Search_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := &OGImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "", SearchOpts{PageURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0 (HTTP error)", len(results))
	}
}
