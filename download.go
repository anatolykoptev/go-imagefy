package imagefy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// DownloadOpts configures an image download.
type DownloadOpts struct {
	MaxBytes  int64         // max response body size (default: 200KB)
	MinBytes  int           // reject if smaller (default: 0)
	Timeout   time.Duration // per-request timeout (default: 10s)
	UserAgent string        // override config user agent
}

const (
	defaultMaxBytes = 200 * 1024       // 200KB
	defaultTimeout  = 10 * time.Second
)

// DownloadResult holds downloaded image data.
type DownloadResult struct {
	Data     []byte
	MIMEType string
}

// Download fetches an image from url. Tries cfg.StealthClient first (if set),
// falls back to cfg.HTTPClient.
// Returns nil result (not error) on recoverable failures (404, non-image, etc.)
// for graceful degradation.
func (cfg *Config) Download(ctx context.Context, url string, opts DownloadOpts) (*DownloadResult, error) {
	cfg.defaults()

	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultMaxBytes
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = cfg.UserAgent
	}

	// Try stealth client first.
	if cfg.StealthClient != nil {
		if r := fetchImageData(ctx, cfg.StealthClient, url, ua, opts); r != nil {
			return r, nil
		}
	}

	// Fallback to regular client.
	r := fetchImageData(ctx, cfg.HTTPClient, url, ua, opts)
	return r, nil
}

func fetchImageData(ctx context.Context, client *http.Client, imageURL, ua string, opts DownloadOpts) *DownloadResult {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", ua)

	resp, err := client.Do(req) //nolint:gosec // G704: URL is caller-supplied by design — SSRF is caller's responsibility
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	ct := resp.Header.Get("Content-Type")
	// Strip MIME parameters: "image/jpeg; charset=utf-8" → "image/jpeg"
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	if !strings.HasPrefix(ct, "image/") {
		return nil
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, opts.MaxBytes))
	if err != nil || len(data) < opts.MinBytes {
		return nil
	}

	return &DownloadResult{Data: data, MIMEType: ct}
}
