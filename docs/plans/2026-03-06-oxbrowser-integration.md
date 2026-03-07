# Phase 2.5: ox-browser Integration + Parallel Search

**Date:** 2026-03-06
**Status:** Planned
**Depends on:** ox-browser Phase 4.6

## New files

### `provider_ox.go` (~60 LOC)

```go
type OxBrowserProvider struct {
    BaseURL    string   // http://ox-browser:8901 (Docker) or http://127.0.0.1:8901
    Engines    []string // ["bing", "ddg", "yandex", "brave"]
    MaxResults int      // default: 10
    Client     *http.Client
}

func (p *OxBrowserProvider) Search(ctx context.Context, query string, opts SearchOpts) ([]ImageCandidate, error)
func (p *OxBrowserProvider) Name() string // "ox-browser"
```

POST to `{BaseURL}/images/search` with JSON body:
```json
{"query": "кот на крыше", "engines": ["bing", "ddg"], "max_results": 10}
```

Parse response, convert `ImageResult` → `ImageCandidate`, apply `CheckLicense` + `IsLogoOrBanner`.

### `orchestrator.go` (~50 LOC)

```go
type FallbackProvider struct {
    Providers []SearchProvider
}

func (f *FallbackProvider) Search(ctx, query, opts) ([]ImageCandidate, error)
// Tries providers in order, returns first successful result
```

### Modified: `search.go`

Replace sequential `gatherCandidates` with parallel:

```go
func (cfg *Config) gatherCandidates(ctx, providers, query, opts) []ImageCandidate {
    var mu sync.Mutex
    var all []ImageCandidate
    var wg sync.WaitGroup
    for _, p := range providers {
        wg.Add(1)
        go func(p SearchProvider) {
            defer wg.Done()
            results, err := p.Search(ctx, query, opts)
            if err != nil { slog.Warn(...); return }
            mu.Lock()
            all = append(all, results...)
            mu.Unlock()
        }(p)
    }
    wg.Wait()
    return all
}
```

## Consumer wiring (go-wp imageadapter)

```go
oxProv := &imagefy.OxBrowserProvider{
    BaseURL: os.Getenv("OX_BROWSER_URL"), // http://ox-browser:8901
    Engines: []string{"bing", "ddg", "yandex"},
    Client:  httpClient,
}
ddgFallback := &imagefy.DDGImageProvider{HTTPClient: stealthClient}
openverse := &imagefy.OpenverseProvider{HTTPClient: httpClient}
pexels := &imagefy.PexelsProvider{APIKey: os.Getenv("PEXELS_API_KEY"), HTTPClient: httpClient}

providers := []imagefy.SearchProvider{oxProv, openverse}
if pexels.APIKey != "" {
    providers = append(providers, pexels)
}
// DDG as last-resort fallback (if ox-browser unavailable)
providers = append(providers, ddgFallback)
```

## Bug fix: upload.go

Replace `http.DefaultClient` with proxied stealth client passed via imageadapter.
