package imagefy

import (
	"context"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestScreenImage_RejectsKnownPlaceholder proves ScreenImage's placeholder
// half fires on the SAME embedded blocklist rejectedByHash uses — reusing
// testdata/placeholders/*, the exact fixtures whose own dHash seeds the
// default blocklist (see TestPlaceholderMatcher_DefaultSeedsFireOnOwnSourceImage).
func TestScreenImage_RejectsKnownPlaceholder(t *testing.T) {
	t.Parallel()

	f, err := os.Open("testdata/placeholders/no_image_available.jpg")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	cfg := &Config{}
	reject, class, reason := cfg.ScreenImage(img, "https://example.com/placeholder.jpg")
	if !reject {
		t.Fatal("known placeholder was not rejected by ScreenImage")
	}
	if class != ClassPlaceholder {
		t.Errorf("class = %q, want %q", class, ClassPlaceholder)
	}
	if reason != reasonPhashBlocklist {
		t.Errorf("reason = %q, want %q", reason, reasonPhashBlocklist)
	}
}

// TestScreenImage_RejectsFlatImage proves ScreenImage's flat-image half
// fires — same solid-color construction and Config as
// TestValidateOne_RejectsFlatCandidate, minus the pipeline plumbing.
func TestScreenImage_RejectsFlatImage(t *testing.T) {
	t.Parallel()

	solid := makeSolidImage(1000, 700, color.RGBA{180, 180, 180, 255})

	cfg := &Config{}
	reject, class, reason := cfg.ScreenImage(solid, "https://example.com/flat.jpg")
	if !reject {
		t.Fatal("flat image was not rejected by ScreenImage")
	}
	if class != ClassPlaceholder {
		t.Errorf("class = %q, want %q", class, ClassPlaceholder)
	}
	if reason != reasonFlatImage {
		t.Errorf("reason = %q, want %q", reason, reasonFlatImage)
	}
}

// TestScreenImage_AcceptsRealPhotos proves ScreenImage doesn't false-reject
// any fixture in the FP-guard corpus (real photos, plus the deliberately
// low-entropy event_poster.jpg — see TestValidateOne_AcceptsEventPosterCandidate
// for why that one is a legitimate accept too), incl. the low-contrast
// (snow/fog/night_sky/high_key) and denoised fixtures.
func TestScreenImage_AcceptsRealPhotos(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	cfg := &Config{}

	for name, img := range corpus {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reject, class, reason := cfg.ScreenImage(img, "https://example.com/"+name)
			if reject {
				t.Errorf("real photo %s rejected by ScreenImage: class=%q reason=%q", name, class, reason)
			}
		})
	}
}

// TestScreenImage_DoesNotDedup proves ScreenImage carries no cross-call
// state: calling it twice with the SAME (byte-identical) real photo must
// accept BOTH times. This is the property that distinguishes it from the
// search pipeline's validateOne, which rejects a second perceptually
// identical candidate via dedupFilter (a NEW dedupFilter per
// validateCandidates call — ScreenImage has no such filter at all, since a
// single-image caller has no "other candidates in this search" to dedup
// against).
func TestScreenImage_DoesNotDedup(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	photo := corpus["building.jpg"]
	if photo == nil {
		t.Fatal("testdata/fp_guard/building.jpg not found in corpus")
	}

	cfg := &Config{}

	reject1, class1, reason1 := cfg.ScreenImage(photo, "https://example.com/building.jpg")
	if reject1 {
		t.Fatalf("first call rejected: class=%q reason=%q, want accepted", class1, reason1)
	}

	reject2, class2, reason2 := cfg.ScreenImage(photo, "https://example.com/building.jpg")
	if reject2 {
		t.Errorf("second call with the SAME image was rejected (ScreenImage is dedup-ing across calls): class=%q reason=%q, want accepted", class2, reason2)
	}
}

// TestScreenImage_NilImageFailsOpen proves the graceful-degradation contract:
// a nil image (e.g. an upstream decode failure) never rejects.
func TestScreenImage_NilImageFailsOpen(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	reject, class, reason := cfg.ScreenImage(nil, "https://example.com/broken.jpg")
	if reject {
		t.Errorf("nil image was rejected: class=%q reason=%q, want reject=false", class, reason)
	}
	if class != "" || reason != "" {
		t.Errorf("class/reason = %q/%q, want empty on nil img", class, reason)
	}
}

// TestScreenImage_DisableFlatImageDetectionKillSwitch proves the operator
// kill switch (Config.DisableFlatImageDetection) reaches ScreenImage too —
// same knob, same effect as on the search pipeline.
func TestScreenImage_DisableFlatImageDetectionKillSwitch(t *testing.T) {
	t.Parallel()

	solid := makeSolidImage(1000, 700, color.RGBA{180, 180, 180, 255})
	cfg := &Config{DisableFlatImageDetection: true}

	reject, _, _ := cfg.ScreenImage(solid, "https://example.com/flat.jpg")
	if reject {
		t.Error("DisableFlatImageDetection=true did not disable ScreenImage's flat gate")
	}
}

// TestScreenImageURL_RejectsPlaceholderEndToEnd exercises the full
// download-decode-screen path against an httptest server serving a known
// placeholder image, mirroring TestValidateOne_RejectsPlaceholderCandidate's
// server setup.
func TestScreenImageURL_RejectsPlaceholderEndToEnd(t *testing.T) {
	t.Parallel()

	banner := makeBannerImage(1000, 700)
	srv := newImageServer(t, "image/jpeg", encodeJPEG(t, banner))

	cfg := &Config{
		HTTPClient:        srv.Client(),
		PlaceholderHashes: []uint64{hashOf(t, banner)},
	}

	reject, class, reason, err := cfg.ScreenImageURL(context.Background(), srv.URL+"/placeholder.jpg")
	if err != nil {
		t.Fatalf("ScreenImageURL returned error: %v", err)
	}
	if !reject {
		t.Fatal("placeholder URL was not rejected by ScreenImageURL")
	}
	if class != ClassPlaceholder || reason != reasonPhashBlocklist {
		t.Errorf("class/reason = %q/%q, want %q/%q", class, reason, ClassPlaceholder, reasonPhashBlocklist)
	}
}

// TestScreenImageURL_AcceptsRealPhotoEndToEnd is the accept-side pairing —
// a real photo served over HTTP must clear ScreenImageURL cleanly.
func TestScreenImageURL_AcceptsRealPhotoEndToEnd(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	photo := corpus["building.jpg"]
	if photo == nil {
		t.Fatal("testdata/fp_guard/building.jpg not found in corpus")
	}
	srv := newImageServer(t, "image/jpeg", encodeJPEG(t, photo))

	cfg := &Config{HTTPClient: srv.Client()}

	reject, class, reason, err := cfg.ScreenImageURL(context.Background(), srv.URL+"/photo.jpg")
	if err != nil {
		t.Fatalf("ScreenImageURL returned error: %v", err)
	}
	if reject {
		t.Errorf("real photo rejected by ScreenImageURL: class=%q reason=%q", class, reason)
	}
}

// TestScreenImageURL_DownloadFailureFailsOpen proves the fail-open contract
// on a download failure (server always 404s — no HTTPClient/StealthClient
// fallback can succeed): reject=false, with a non-nil error for the caller
// to log.
func TestScreenImageURL_DownloadFailureFailsOpen(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{HTTPClient: srv.Client()}

	reject, class, reason, err := cfg.ScreenImageURL(context.Background(), srv.URL+"/missing.jpg")
	if err == nil {
		t.Fatal("expected a non-nil error on download failure, got nil")
	}
	if reject {
		t.Errorf("download failure should fail OPEN (reject=false), got reject=true class=%q reason=%q", class, reason)
	}
}
