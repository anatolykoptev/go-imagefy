package imagefy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownload_Success(t *testing.T) {
	const body = "FAKEIMAGEDATA_1KB_PADDING_XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	cfg := &Config{HTTPClient: srv.Client()}
	res, err := cfg.Download(context.Background(), srv.URL+"/image.jpg", DownloadOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected result, got nil")
	}
	if res.MIMEType != "image/jpeg" {
		t.Errorf("MIMEType = %q, want image/jpeg", res.MIMEType)
	}
	if len(res.Data) == 0 {
		t.Error("Data is empty")
	}
}

func TestDownload_NonImageContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()

	cfg := &Config{HTTPClient: srv.Client()}
	res, err := cfg.Download(context.Background(), srv.URL+"/page.html", DownloadOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil {
		t.Errorf("expected nil result for non-image content type, got %v", res)
	}
}

func TestDownload_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()

	cfg := &Config{HTTPClient: srv.Client()}
	res, err := cfg.Download(context.Background(), srv.URL+"/missing.jpg", DownloadOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil {
		t.Errorf("expected nil result for 404, got %v", res)
	}
}

func TestDownload_MinBytesEnforcement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("tiny"))
	}))
	defer srv.Close()

	cfg := &Config{HTTPClient: srv.Client()}
	res, err := cfg.Download(context.Background(), srv.URL+"/small.jpg", DownloadOpts{MinBytes: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil {
		t.Errorf("expected nil result when body smaller than MinBytes, got %v", res)
	}
}

func TestDownload_MaxBytesEnforcement(t *testing.T) {
	const maxBytes = 10
	body := strings.Repeat("X", 100)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	cfg := &Config{HTTPClient: srv.Client()}
	res, err := cfg.Download(context.Background(), srv.URL+"/big.png", DownloadOpts{MaxBytes: maxBytes})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected result, got nil")
	}
	if int64(len(res.Data)) > maxBytes {
		t.Errorf("Data len = %d, want <= %d", len(res.Data), maxBytes)
	}
}

func TestDownload_StealthClientFallback(t *testing.T) {
	// srv is the real server that the fallback HTTPClient will reach.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		_, _ = w.Write([]byte("GIF89a_FAKE_IMAGE_DATA_PADDING_XXXXXXXXXXXX"))
	}))
	defer srv.Close()

	// stealthSrv always returns 403 to simulate a failed stealth attempt.
	stealthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer stealthSrv.Close()

	// Point StealthClient at stealthSrv (fails), HTTPClient at real srv.
	stealthClient := stealthSrv.Client()
	stealthClient.Transport = redirectTransport(stealthSrv.URL)

	regularClient := srv.Client()
	regularClient.Transport = redirectTransport(srv.URL)

	cfg := &Config{
		StealthClient: stealthClient,
		HTTPClient:    regularClient,
	}

	// The URL itself doesn't matter; transports redirect to their respective test servers.
	res, err := cfg.Download(context.Background(), "http://example.com/image.gif", DownloadOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected fallback result, got nil")
	}
	if res.MIMEType != "image/gif" {
		t.Errorf("MIMEType = %q, want image/gif", res.MIMEType)
	}
}

// redirectTransport returns a RoundTripper that rewrites all requests to target.
type redirectTransport string

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = strings.TrimPrefix(string(rt), "http://")
	return http.DefaultTransport.RoundTrip(req2)
}

func TestDownload_MIMEParameterStripping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg; charset=utf-8")
		_, _ = w.Write([]byte("FAKEIMAGEDATA"))
	}))
	defer srv.Close()

	cfg := &Config{HTTPClient: srv.Client()}
	res, err := cfg.Download(context.Background(), srv.URL+"/photo.jpg", DownloadOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected result, got nil")
	}
	if res.MIMEType != "image/jpeg" {
		t.Errorf("MIMEType = %q after stripping, want image/jpeg", res.MIMEType)
	}
}
