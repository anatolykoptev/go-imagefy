# Fix Remaining Issues in go-imagefy

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix ValidateImageURL to use injected HTTP client (proxy support), remove DDG duplication with ox-browser, increase search timeout.

**Architecture:** ValidateImageURL currently creates a bare `http.Client{}` — change it to use `cfg.HTTPClient` (with redirect-limit wrapper). Remove DDGImageProvider from go-wp's adapter since ox-browser already covers DDG. Bump search timeout from 15s to 30s.

**Tech Stack:** Go, go-imagefy (library), go-wp (consumer)

---

## Task 1: Fix ValidateImageURL to use cfg.HTTPClient

The core bug: `validate.go:38` creates `&http.Client{}` ignoring `cfg.HTTPClient` and `cfg.StealthClient`. This means validation requests bypass proxy. Fix: use `cfg.HTTPClient` (already injected), wrap with redirect limit.

**Files:**
- Modify: `validate.go`
- Modify: `validate_test.go` (add test for client usage)

**Step 1: Write the failing test**

Add to `validate_test.go`:

```go
func TestValidateImageURL_UsesConfigHTTPClient(t *testing.T) {
	t.Parallel()

	called := false
	customTransport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		// Return a valid JPEG response.
		img := createTestJPEG(1000, 800)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
			Body:       io.NopCloser(bytes.NewReader(img)),
		}, nil
	})

	cfg := &Config{
		HTTPClient:    &http.Client{Transport: customTransport},
		MinImageWidth: 100,
	}

	result := cfg.ValidateImageURL(context.Background(), "https://example.com/photo.jpg")

	if !called {
		t.Error("expected cfg.HTTPClient to be used, but custom transport was not called")
	}
	if !result {
		t.Error("expected valid image to pass")
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
```

Note: `createTestJPEG` may need to be extracted from existing test helpers or created. Check `search_test.go` for `newJPEGServer` — it generates JPEG data that can be reused.

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-imagefy && go test -run TestValidateImageURL_UsesConfigHTTPClient -v -count=1`
Expected: FAIL — custom transport not called (bare http.Client used instead)

**Step 3: Implement the fix**

Replace the client creation in `validate.go` (lines 38-47):

```go
// Before (broken):
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

// After (fixed):
client := cfg.validationClient()
```

Add a new method to `validate.go`:

```go
// validationClient returns an HTTP client for image validation.
// Uses cfg.StealthClient if available, otherwise cfg.HTTPClient.
// Wraps with redirect limit to prevent infinite loops.
func (cfg *Config) validationClient() *http.Client {
	base := cfg.HTTPClient
	if cfg.StealthClient != nil {
		base = cfg.StealthClient
	}

	return &http.Client{
		Transport: base.Transport,
		Timeout:   defaultTimeout,
		Jar:       base.Jar,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			const maxRedirects = 3
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}
```

**Step 4: Run test**

Run: `go test -run TestValidateImageURL -v -count=1`
Expected: PASS

**Step 5: Run full suite**

Run: `go test ./... -count=1`
Expected: all tests PASS

**Step 6: Commit**

```bash
git add validate.go validate_test.go
git commit -m "fix: ValidateImageURL uses cfg.HTTPClient instead of bare client"
```

---

## Task 2: Remove DDG duplication in go-wp

Ox-browser already searches DDG (`engines: ["bing", "ddg", "yandex"]`). The separate `DDGImageProvider` duplicates DDG requests. Remove it from go-wp's imageadapter.

**Files:**
- Modify: `/home/krolik/src/go-wp/internal/imageadapter/adapter.go`

**Step 1: Remove DDGImageProvider**

In `adapter.go`, delete lines 79-82:

```go
// DELETE these lines:
// Last-resort: direct DDG via stealth proxy (avoids ox-browser dependency).
providers = append(providers, &imagefy.DDGImageProvider{
    HTTPClient: ddgClient,
})
```

Also remove the `ddgClient` variable (lines 41-44) since it's no longer used:

```go
// DELETE these lines:
ddgClient := stealthClient
if ddgClient == nil {
    ddgClient = httpClient
}
```

**Step 2: Verify build**

Run: `cd /home/krolik/src/go-wp && GOWORK=off go build ./...`
Expected: builds successfully

**Step 3: Run tests**

Run: `GOWORK=off go test ./internal/imageadapter/... -v -count=1`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/imageadapter/adapter.go
git commit -m "fix: remove DDG provider — ox-browser already searches DDG"
```

---

## Task 3: Increase search timeout from 15s to 30s

The `searxngTimeout = 15s` is the total budget for all providers + validation. With 4+ providers running in parallel, 15s is tight. Bump to 30s.

**Files:**
- Modify: `search.go` (go-imagefy)

**Step 1: Change the constant**

In `search.go:12`, change:

```go
// Before:
searxngTimeout = 15 * time.Second

// After:
searchTimeout = 30 * time.Second
```

Also rename references from `searxngTimeout` to `searchTimeout` throughout `search.go` (there should be one reference in `SearchImagesWithOpts`).

**Step 2: Run full suite**

Run: `cd /home/krolik/src/go-imagefy && go test ./... -count=1`
Expected: all tests PASS

**Step 3: Commit**

```bash
git add search.go
git commit -m "fix: increase search timeout from 15s to 30s, rename to searchTimeout"
```

---

## Task 4: Vendor update, deploy, smoke test

**Step 1: Update vendor in go-wp**

```bash
cd /home/krolik/src/go-wp
GOWORK=off go mod tidy && GOWORK=off go mod vendor
```

**Step 2: Build and test**

```bash
GOWORK=off go build ./...
GOWORK=off go test ./... -count=1 -short
```
Expected: all tests PASS

**Step 3: Commit vendor**

```bash
git add vendor/ go.mod go.sum
git commit -m "chore: vendor go-imagefy with proxy fix and DDG dedup removal"
```

**Step 4: Deploy**

```bash
cd /home/krolik/deploy/krolik-server
docker compose build --no-cache go-wp
docker compose up -d --no-deps --force-recreate go-wp
```

**Step 5: Verify startup**

```bash
sleep 3 && docker logs go-wp --tail 5
```
Expected: `listening service=go-wp addr=:8894`

**Step 6: Smoke test**

Call `wp_image resolve` with query "Эрмитаж" via MCP and verify non-empty results.
