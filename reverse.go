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
	"time"
)

const (
	reverseMaxResults = 10
	reverseBodyLimit  = 512 * 1024 // 512KB
	reverseTimeout    = 10         // seconds
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

	// Enforce timeout to prevent blocking pipeline goroutines when ox-browser is slow/down.
	ctx, cancel := context.WithTimeout(ctx, reverseTimeout*time.Second)
	defer cancel()

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
