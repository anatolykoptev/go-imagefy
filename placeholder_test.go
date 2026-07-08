package imagefy

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/corona10/goimagehash"
)

// makeBannerImage creates a deterministic "placeholder-style" image: a mostly
// flat background with a centered high-contrast block, mimicking the coarse
// structure of a text-on-solid-background hotlink-protection graphic. Used as
// a stand-in blocklist target — we inject its hash via Config.PlaceholderHashes
// rather than depending on any embedded default (which requires no committed
// image bytes; see placeholder.go).
func makeBannerImage(width, height int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	bg := color.RGBA{R: 235, G: 235, B: 235, A: 255}
	block := color.RGBA{R: 20, G: 20, B: 20, A: 255}
	for y := range height {
		for x := range width {
			c := bg
			if x > width/4 && x < 3*width/4 && y > height/3 && y < 2*height/3 {
				c = block
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// jpegRoundTrip encodes img to JPEG at the given quality and decodes it back —
// simulating the lossy re-encoding an image undergoes across CDN fetches/edges.
func jpegRoundTrip(t *testing.T, img image.Image, quality int) image.Image {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	decoded, _, err := image.Decode(&buf)
	if err != nil {
		t.Fatalf("image.Decode: %v", err)
	}
	return decoded
}

func hashOf(t *testing.T, img image.Image) uint64 {
	t.Helper()
	h, err := goimagehash.DifferenceHash(img)
	if err != nil {
		t.Fatalf("DifferenceHash: %v", err)
	}
	return h.GetHash()
}

// loadFPGuardCorpus decodes every real photo under testdata/fp_guard.
func loadFPGuardCorpus(t *testing.T) map[string]image.Image {
	t.Helper()
	entries, err := os.ReadDir("testdata/fp_guard")
	if err != nil {
		t.Fatalf("ReadDir testdata/fp_guard: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("testdata/fp_guard is empty — FP-guard corpus missing")
	}

	corpus := make(map[string]image.Image, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join("testdata/fp_guard", e.Name())
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		corpus[e.Name()] = img
	}
	return corpus
}

func TestPlaceholderMatcher_DefaultBlocklistIsSeeded(t *testing.T) {
	t.Parallel()

	m := newPlaceholderMatcher(nil)
	if len(m.entries) == 0 {
		t.Fatal("default blocklist is empty — placeholder.go must seed at least one known placeholder hash")
	}
	if len(m.entries) != len(defaultPlaceholderHashes) {
		t.Errorf("matcher has %d entries, want %d (defaultPlaceholderHashes)", len(m.entries), len(defaultPlaceholderHashes))
	}
	for i, e := range m.entries {
		if e.source == "" {
			t.Errorf("defaultPlaceholderHashes[%d] has no source label — every embedded hash must be auditable", i)
		}
	}
}

// TestPlaceholderMatcher_MatchesInjectedHash covers the "Config extension" case:
// an operator-supplied hash (Config.PlaceholderHashes) rejects a matching image
// without needing a go-imagefy release.
func TestPlaceholderMatcher_MatchesInjectedHash(t *testing.T) {
	t.Parallel()

	banner := makeBannerImage(120, 90)
	m := newPlaceholderMatcher([]uint64{hashOf(t, banner)})

	matched, source := m.matches(banner)
	if !matched {
		t.Fatal("expected injected hash to match its own source image")
	}
	if source != "config" {
		t.Errorf("source = %q, want %q", source, "config")
	}
}

// TestPlaceholderMatcher_MatchesNearIdenticalReencodedImage proves the matcher
// does perceptual matching, not exact-byte comparison: the same source image,
// JPEG-recompressed at a different quality (as happens across CDN fetches),
// still matches the hash computed from the original.
func TestPlaceholderMatcher_MatchesNearIdenticalReencodedImage(t *testing.T) {
	t.Parallel()

	original := makeBannerImage(160, 120)
	reencoded := jpegRoundTrip(t, original, 55) // lossy re-encode, simulating a different CDN edge/fetch

	m := newPlaceholderMatcher([]uint64{hashOf(t, original)})

	matched, _ := m.matches(reencoded)
	if !matched {
		t.Fatal("re-encoded (lossy JPEG round-trip) copy of a blocklisted image should still match within placeholderThreshold")
	}
}

// TestPlaceholderMatcher_DistinctImageNotMatched is the negative baseline:
// a structurally different image must not match an unrelated blocklist entry.
func TestPlaceholderMatcher_DistinctImageNotMatched(t *testing.T) {
	t.Parallel()

	banner := makeBannerImage(120, 90)
	distinct := makeGradientImage(120, 90, 0) // reuses dedup_test.go's generator

	m := newPlaceholderMatcher([]uint64{hashOf(t, banner)})

	if matched, source := m.matches(distinct); matched {
		t.Errorf("gradient image should not match banner blocklist entry, got match with source %q", source)
	}
}

// TestPlaceholderMatcher_FPGuardRealPhotos is the critical false-positive guard:
// real, high-entropy photos must NEVER be flagged by the default blocklist. If
// this test fails, placeholderThreshold is too loose and must be tightened.
//
// The corpus includes testdata/fp_guard/event_poster.jpg — a real, low-entropy,
// text-on-solid-background promotional poster — which is the actual production
// risk class (event cards, not generic stock photos): structurally the closest
// LEGITIMATE image type to the "image unavailable" placeholders this gate
// blocks. See TestPlaceholderMatcher_FPGuardLowEntropyEventPoster for the
// dedicated distance-to-nearest-seed assertion on that fixture.
func TestPlaceholderMatcher_FPGuardRealPhotos(t *testing.T) {
	t.Parallel()

	m := newPlaceholderMatcher(nil)
	corpus := loadFPGuardCorpus(t)

	for name, img := range corpus {
		if matched, source := m.matches(img); matched {
			t.Errorf("FALSE POSITIVE: real photo %q matched placeholder blocklist (source=%q) — placeholderThreshold=%d is too loose",
				name, source, placeholderThreshold)
		}
	}
}

// TestPlaceholderMatcher_FPGuardLowEntropyEventPoster is the dedicated FP-guard
// for the production risk class that motivated this gate: piter.now event
// cards are text-on-solid-background promotional graphics — structurally the
// closest LEGITIMATE image type to a hotlink-protection "image unavailable"
// placeholder (also flat background + centered text). The generic photo
// corpus (building/food/nature/street) doesn't represent this risk; this test
// does, using a real low-entropy poster (Wikimedia Commons "Keep Calm and
// Carry On" scan — solid-color background, centered text block).
//
// Asserts (and logs) the Hamming distance to every default seed so a future
// threshold change has this as a concrete regression guard, not just a
// pass/fail.
func TestPlaceholderMatcher_FPGuardLowEntropyEventPoster(t *testing.T) {
	t.Parallel()

	const fixture = "testdata/fp_guard/event_poster.jpg"
	f, err := os.Open(fixture)
	if err != nil {
		t.Fatalf("open %s: %v", fixture, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", fixture, err)
	}

	hash, err := goimagehash.DifferenceHash(img)
	if err != nil {
		t.Fatalf("DifferenceHash: %v", err)
	}

	m := newPlaceholderMatcher(nil)
	minDist := -1
	for i, h := range m.hashes {
		dist, err := hash.Distance(h)
		if err != nil {
			continue
		}
		t.Logf("event_poster.jpg vs default[%d] (%s): Hamming distance=%d", i, m.entries[i].source, dist)
		if minDist == -1 || dist < minDist {
			minDist = dist
		}
	}

	if minDist <= placeholderThreshold {
		t.Errorf("FALSE POSITIVE RISK: low-entropy event-poster fixture is within placeholderThreshold=%d of a default seed (nearest distance=%d) — this is exactly the production risk class (text-on-solid-background promotional graphics); tighten placeholderThreshold or reconsider the seed before shipping",
			placeholderThreshold, minDist)
	}

	if matched, source := m.matches(img); matched {
		t.Errorf("event-poster candidate matched placeholder blocklist (source=%q) — real event cards must never be rejected", source)
	}
}

// TestPlaceholderMatcher_DefaultSeedsFireOnOwnSourceImage is the golden test
// proving the embedded default hash VALUES are correct — not just that the
// slice is non-empty (TestPlaceholderMatcher_DefaultBlocklistIsSeeded only
// covers that). If a seed were recorded with the wrong hash function
// (PerceptionHash/AverageHash instead of DifferenceHash), a different resize,
// or transposed bytes, this test catches it: the recorded hash must actually
// fire on (a small copy of) its own source image.
func TestPlaceholderMatcher_DefaultSeedsFireOnOwnSourceImage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fixture        string
		expectedSource string
	}{
		{
			fixture:        "testdata/placeholders/no_image_available.jpg",
			expectedSource: "Wikimedia Commons: File:No_Image_Available.jpg (generic image-unavailable placeholder, 547x547 JPEG)",
		},
		{
			fixture:        "testdata/placeholders/no_image_available_svg.jpg",
			expectedSource: "Wikimedia Commons: File:No_image_available.svg (rendered as 960px PNG; generic gray no-image box)",
		},
	}

	m := newPlaceholderMatcher(nil)

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()

			f, err := os.Open(tc.fixture)
			if err != nil {
				t.Fatalf("open %s: %v", tc.fixture, err)
			}
			defer f.Close()
			img, _, err := image.Decode(f)
			if err != nil {
				t.Fatalf("decode %s: %v", tc.fixture, err)
			}

			matched, source := m.matches(img)
			if !matched {
				t.Fatalf("default blocklist did NOT fire on its own source image %s — the recorded hash is dead (wrong hash function, wrong resize, or transposed bytes)", tc.fixture)
			}
			if source != tc.expectedSource {
				t.Errorf("matched source = %q, want %q", source, tc.expectedSource)
			}
		})
	}
}

// TestPlaceholderMatcher_MatchesHashEquivalentToMatches proves matchesHash
// (the precomputed-hash entry point validateOne uses to share a single dHash
// computation with the dedup check) behaves identically to matches.
func TestPlaceholderMatcher_MatchesHashEquivalentToMatches(t *testing.T) {
	t.Parallel()

	banner := makeBannerImage(120, 90)
	m := newPlaceholderMatcher([]uint64{hashOf(t, banner)})

	hash, err := goimagehash.DifferenceHash(banner)
	if err != nil {
		t.Fatalf("DifferenceHash: %v", err)
	}

	wantMatched, wantSource := m.matches(banner)
	gotMatched, gotSource := m.matchesHash(hash)
	if gotMatched != wantMatched || gotSource != wantSource {
		t.Errorf("matchesHash(precomputed) = (%v, %q), want matches(img) = (%v, %q)", gotMatched, gotSource, wantMatched, wantSource)
	}
}

// TestPlaceholderMatcher_ConfigDuplicateOfDefaultIsSkipped covers the
// exact-duplicate dedup in newPlaceholderMatcher: injecting a hash that
// already equals a default entry must not grow the matcher's entry count.
func TestPlaceholderMatcher_ConfigDuplicateOfDefaultIsSkipped(t *testing.T) {
	t.Parallel()

	dup := defaultPlaceholderHashes[0].hash
	m := newPlaceholderMatcher([]uint64{dup})

	if len(m.entries) != len(defaultPlaceholderHashes) {
		t.Errorf("entries = %d, want %d (config hash duplicating a default should be skipped)", len(m.entries), len(defaultPlaceholderHashes))
	}
}

// TestPlaceholderMatcher_GracefulDegradationZeroSizeImage mirrors
// dedup_test.go's graceful-degradation case: a hashing failure must accept
// (never false-reject on error).
func TestPlaceholderMatcher_GracefulDegradationZeroSizeImage(t *testing.T) {
	t.Parallel()

	m := newPlaceholderMatcher([]uint64{hashOf(t, makeBannerImage(120, 90))})
	emptyImg := image.NewNRGBA(image.Rect(0, 0, 0, 0))

	matched, source := m.matches(emptyImg)
	if matched {
		t.Error("zero-size image should be accepted (graceful degradation), not flagged as placeholder")
	}
	if source != "" {
		t.Errorf("source = %q, want empty on graceful degradation", source)
	}
}

// --- Pipeline integration (validateOne wiring) ---

// encodeJPEG is a small helper to feed a generated image.Image into the
// existing newImageServer(t, contentType, body) helper (validate_test.go).
func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestValidateOne_RejectsPlaceholderCandidate(t *testing.T) {
	t.Parallel()

	banner := makeBannerImage(1000, 700) // above DefaultMinImageWidth (880)
	srv := newImageServer(t, "image/jpeg", encodeJPEG(t, banner))

	var events []ClassificationEvent
	var eventsMu sync.Mutex

	cfg := &Config{
		HTTPClient:        srv.Client(),
		PlaceholderHashes: []uint64{hashOf(t, banner)},
		OnClassification: func(e ClassificationEvent) {
			eventsMu.Lock()
			events = append(events, e)
			eventsMu.Unlock()
		},
	}

	cand := ImageCandidate{
		ImgURL: srv.URL + "/placeholder.jpg",
		Source: srv.URL + "/page",
		Title:  "Placeholder",
	}

	results := cfg.validateCandidates(context.Background(), []ImageCandidate{cand}, 5)
	if len(results) != 0 {
		t.Errorf("placeholder candidate was accepted, want rejected: %+v", results)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	found := false
	for _, e := range events {
		if e.Class == ClassPlaceholder && e.Source == "phash_blocklist" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an OnClassification event with Class=%q Source=%q, got %+v", ClassPlaceholder, "phash_blocklist", events)
	}
}

func TestValidateOne_AcceptsRealPhotoCandidate(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	realPhoto := corpus["building.jpg"]
	if realPhoto == nil {
		t.Fatal("testdata/fp_guard/building.jpg not found in corpus")
	}
	srv := newImageServer(t, "image/jpeg", encodeJPEG(t, realPhoto))

	var events []ClassificationEvent
	var eventsMu sync.Mutex

	cfg := &Config{
		HTTPClient: srv.Client(),
		// testdata/fp_guard photos are 400-500px wide; lower the width floor so
		// ValidateImageURL's dimension check doesn't reject them before the
		// placeholder gate even runs (irrelevant to what this test verifies).
		MinImageWidth: 300,
		// No PlaceholderHashes injected — only the embedded defaults apply.
		OnClassification: func(e ClassificationEvent) {
			eventsMu.Lock()
			events = append(events, e)
			eventsMu.Unlock()
		},
	}

	cand := ImageCandidate{
		ImgURL: srv.URL + "/photo.jpg",
		Source: srv.URL + "/page",
		Title:  "Real Photo",
	}

	results := cfg.validateCandidates(context.Background(), []ImageCandidate{cand}, 5)
	if len(results) != 1 {
		t.Errorf("real photo candidate was rejected, want accepted: results=%+v", results)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	for _, e := range events {
		if e.Class == ClassPlaceholder && e.Source == "phash_blocklist" {
			t.Errorf("real photo was rejected by the placeholder gate: %+v", e)
		}
	}
}
