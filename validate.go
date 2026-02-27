package imagefy

import (
	"context"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"strings"

	_ "golang.org/x/image/webp"
)

// ValidateImageURL fetches image headers and checks:
//   - HTTP 200 + image/* content type
//   - Width >= cfg.MinImageWidth
//   - Not a logo/banner (URL pattern check)
func (cfg *Config) ValidateImageURL(ctx context.Context, rawURL string) bool {
	cfg.defaults()

	if IsLogoOrBanner(strings.ToLower(rawURL)) {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", cfg.UserAgent)

	client := &http.Client{
		Timeout: defaultTimeout,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			const maxRedirects = 3
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	resp, err := client.Do(req) //nolint:gosec // G704: URL is caller-supplied by design — SSRF is caller's responsibility
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return false
	}

	const decodeLimit = 256 * 1024
	imgCfg, _, err := image.DecodeConfig(io.LimitReader(resp.Body, decodeLimit))
	if err != nil {
		// Can't decode dimensions — accept (passed content-type check).
		return true
	}

	if imgCfg.Width < cfg.MinImageWidth {
		slog.Debug("imagefy: too narrow", "url", rawURL, "width", imgCfg.Width, "min", cfg.MinImageWidth)
		return false
	}

	return true
}
