package imagefy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	pexelsOfficialURL = "https://api.pexels.com/v1"
	pexelsInternalURL = "https://www.pexels.com/en-us/api/v3/search/photos"
	pexelsBodyLimit   = 2 * 1024 * 1024
	pexelsPerPage     = 40
)

// PexelsProvider searches images via the Pexels API.
// Supports both official API (with APIKey) and internal API (with SecretKey).
// When both keys are set, official API is tried first with fallback to internal.
type PexelsProvider struct {
	APIKey     string       // official API key (Authorization header)
	SecretKey  string       // internal API key (Secret-Key header)
	HTTPClient *http.Client // optional (nil = http.DefaultClient)
	UserAgent  string       // optional

	officialBase string // test override
	internalBase string // test override
}

// Name returns the provider name.
func (p *PexelsProvider) Name() string { return "pexels" }

// Suppress unused import warnings during incremental TDD.
var (
	_ = context.Background
	_ = json.Marshal
	_ = fmt.Sprintf
	_ = io.LimitReader
	_ = (*http.Client)(nil)
	_ = url.QueryEscape
	_ = strings.TrimRight
)
