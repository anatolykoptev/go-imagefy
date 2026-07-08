package imagefy

import (
	"context"
	"image"
	"image/color"
	"sync"
	"testing"
)

// makeSolidImage returns an image filled entirely with one color.
func makeSolidImage(width, height int, c color.RGBA) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, c)
		}
	}
	return img
}

// makeNearSolidImage returns an image that is ~95% background color with a
// single contrasting rectangular "text" block covering ~5% of the area —
// mimicking a blank/error placeholder with a short centered message. This is
// the realistic near-solid shape (NOT scattered rainbow noise, which would
// itself spread across many color buckets and dodge the unique-buckets /
// entropy signals).
func makeNearSolidImage(width, height int, bg, text color.RGBA) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	// Text block: centered, sized to ~5% of total area.
	blockW, blockH := width/4, height/5
	x0, y0 := (width-blockW)/2, (height-blockH)/2
	for y := range height {
		for x := range width {
			c := bg
			if x >= x0 && x < x0+blockW && y >= y0 && y < y0+blockH {
				c = text
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func defaultFlatThresholds() flatThresholds {
	return flatThresholds{
		dominantFraction: DefaultFlatImageDominantFraction,
		maxUniqueBuckets: DefaultFlatImageMaxUniqueBuckets,
		maxEntropyBits:   DefaultFlatImageMaxEntropyBits,
	}
}

// TestIsNonPhotographic_RejectsFlatImages is the REJECT half of the gate:
// solid and near-solid synthetic images must all be flagged.
func TestIsNonPhotographic_RejectsFlatImages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		img  image.Image
	}{
		{"solid_white", makeSolidImage(800, 600, color.RGBA{255, 255, 255, 255})},
		{"solid_black", makeSolidImage(800, 600, color.RGBA{0, 0, 0, 255})},
		{
			"near_solid_95pct_with_text_block",
			makeNearSolidImage(800, 600, color.RGBA{200, 200, 200, 255}, color.RGBA{20, 20, 20, 255}),
		},
	}

	th := defaultFlatThresholds()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rejected, reason := isNonPhotographic(tc.img, th)
			if !rejected {
				t.Errorf("%s: expected reject, got accept", tc.name)
			}
			if reason == "" {
				t.Error("expected a non-empty reason string on reject")
			}
		})
	}
}

// TestIsNonPhotographic_FPGuardCorpus is the critical false-positive guard:
// the fp_guard corpus (4 real photos + event_poster.jpg, the closest
// legitimate-content risk class — a real, low-dominant-fraction, high-color
// promotional graphic) must NEVER be rejected. If this fails, the thresholds
// are too aggressive and must be loosened (favor false-negatives).
func TestIsNonPhotographic_FPGuardCorpus(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t) // reuses placeholder_test.go's loader
	th := defaultFlatThresholds()

	for name, img := range corpus {
		rejected, reason := isNonPhotographic(img, th)
		if rejected {
			t.Errorf("FALSE POSITIVE: %q flagged as flat/non-photographic (reason=%q) — thresholds are too aggressive", name, reason)
		}
	}
}

// TestIsNonPhotographic_FPGuardEventPosterMargin is the dedicated margin
// assertion for the production risk class that motivated this gate:
// text-on-solid-background promotional graphics. Logs the actual metrics so
// a future threshold change has this as a concrete regression guard.
func TestIsNonPhotographic_FPGuardEventPosterMargin(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	img := corpus["event_poster.jpg"]
	if img == nil {
		t.Fatal("testdata/fp_guard/event_poster.jpg not found in corpus")
	}

	bounds := img.Bounds()
	var hist [flatBucketCount]int
	total := 0
	stepX, stepY := flatSampleStep(bounds.Dx(), bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r, g, b, _ := img.At(x, y).RGBA()
			hist[flatBucket(r, g, b)]++
			total++
		}
	}
	dominant, unique, entropy := flatHistStats(hist[:], total)
	dominantFraction := float64(dominant) / float64(total)

	t.Logf("event_poster.jpg: dominantFraction=%.4f (threshold %.2f) unique=%d (threshold %d) entropy=%.4f (threshold %.2f)",
		dominantFraction, DefaultFlatImageDominantFraction,
		unique, DefaultFlatImageMaxUniqueBuckets,
		entropy, DefaultFlatImageMaxEntropyBits)

	if dominantFraction >= DefaultFlatImageDominantFraction {
		t.Errorf("event_poster.jpg dominantFraction=%.4f is within the reject threshold %.2f — tighten before shipping",
			dominantFraction, DefaultFlatImageDominantFraction)
	}

	rejected, reason := isNonPhotographic(img, defaultFlatThresholds())
	if rejected {
		t.Errorf("event_poster.jpg was rejected (reason=%q) — the FP-guard event poster must never be flagged", reason)
	}
}

// TestIsNonPhotographic_GracefulDegradation covers the "never false-reject on
// a degenerate input" contract: nil image and a zero-area image must both
// return (false, "") — same contract as rejectedByHash.
func TestIsNonPhotographic_GracefulDegradation(t *testing.T) {
	t.Parallel()

	th := defaultFlatThresholds()

	cases := []struct {
		name string
		img  image.Image
	}{
		{"nil_image", nil},
		{"zero_area_image", image.NewNRGBA(image.Rect(0, 0, 0, 0))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rejected, reason := isNonPhotographic(tc.img, th)
			if rejected {
				t.Errorf("%s: expected graceful accept, got reject (reason=%q)", tc.name, reason)
			}
			if reason != "" {
				t.Errorf("%s: reason = %q, want empty on accept", tc.name, reason)
			}
		})
	}
}

// TestIsNonPhotographic_ConfigOverrideChangesVerdict proves the thresholds
// parameter is actually consulted, not hardcoded: the same image flips
// verdict under a loosened vs. tightened threshold set.
func TestIsNonPhotographic_ConfigOverrideChangesVerdict(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	photo := corpus["building.jpg"] // a real photo — never rejected at default thresholds
	if photo == nil {
		t.Fatal("testdata/fp_guard/building.jpg not found in corpus")
	}

	if rejected, _ := isNonPhotographic(photo, defaultFlatThresholds()); rejected {
		t.Fatal("precondition failed: building.jpg is rejected at default thresholds")
	}

	// Loosened thresholds (any nonzero dominant-fraction/entropy passes,
	// generous bucket count) must flip a real photo to rejected.
	loose := flatThresholds{dominantFraction: 0.01, maxUniqueBuckets: 1000, maxEntropyBits: 10}
	if rejected, reason := isNonPhotographic(photo, loose); !rejected {
		t.Error("expected loosened thresholds to reject building.jpg, got accept — Config override not consulted")
	} else if reason == "" {
		t.Error("expected a reason string on reject")
	}

	// A near-solid synthetic image, rejected at default thresholds, must
	// flip to accepted under a threshold set no real placeholder can meet.
	nearSolid := makeNearSolidImage(800, 600, color.RGBA{200, 200, 200, 255}, color.RGBA{20, 20, 20, 255})
	if rejected, _ := isNonPhotographic(nearSolid, defaultFlatThresholds()); !rejected {
		t.Fatal("precondition failed: near-solid image is accepted at default thresholds")
	}
	strict := flatThresholds{dominantFraction: 1.01, maxUniqueBuckets: 0, maxEntropyBits: 0}
	if rejected, _ := isNonPhotographic(nearSolid, strict); rejected {
		t.Error("expected impossible-to-meet thresholds to accept the near-solid image, got reject — Config override not consulted")
	}
}

// --- Pipeline integration (validateOne wiring) ---
//
// These are the falsification-load-bearing tests: they drive the reject
// through the REAL validateCandidates -> validateOne -> rejectedByFlatness
// path (not a direct call to isNonPhotographic), so deleting the
// rejectedByFlatness call in validate_pipeline.go flips them red — a solid
// synthetic image has no domain/license signal and no Classifier configured,
// so without the flatness gate it falls through assessAndAccept (Unknown
// license) and classifyPredownloaded (nil Classifier -> accept) straight
// into validated.

func TestValidateOne_RejectsFlatCandidate(t *testing.T) {
	t.Parallel()

	solid := makeSolidImage(1000, 700, color.RGBA{180, 180, 180, 255}) // above DefaultMinImageWidth (880)
	srv := newImageServer(t, "image/jpeg", encodeJPEG(t, solid))

	var events []ClassificationEvent
	var eventsMu sync.Mutex

	cfg := &Config{
		HTTPClient: srv.Client(),
		OnClassification: func(e ClassificationEvent) {
			eventsMu.Lock()
			events = append(events, e)
			eventsMu.Unlock()
		},
	}

	cand := ImageCandidate{
		ImgURL: srv.URL + "/flat.jpg",
		Source: srv.URL + "/page",
		Title:  "Flat placeholder",
	}

	results := cfg.validateCandidates(context.Background(), []ImageCandidate{cand}, 5)
	if len(results) != 0 {
		t.Errorf("flat candidate was accepted, want rejected: %+v", results)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	found := false
	for _, e := range events {
		if e.Class == ClassPlaceholder && e.Source == "flat_image" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an OnClassification event with Class=%q Source=%q, got %+v", ClassPlaceholder, "flat_image", events)
	}
}

// TestValidateOne_AcceptsEventPosterCandidate proves the pipeline-level
// wiring doesn't regress the FP-guard case: a real, structured, low-entropy
// promotional poster must clear the flatness gate and be accepted.
func TestValidateOne_AcceptsEventPosterCandidate(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	poster := corpus["event_poster.jpg"]
	if poster == nil {
		t.Fatal("testdata/fp_guard/event_poster.jpg not found in corpus")
	}
	srv := newImageServer(t, "image/jpeg", encodeJPEG(t, poster))

	var events []ClassificationEvent
	var eventsMu sync.Mutex

	cfg := &Config{
		HTTPClient: srv.Client(),
		// event_poster.jpg is 500x747 — below DefaultMinImageWidth (880);
		// lower the floor so ValidateImageURL doesn't reject it before the
		// flatness gate even runs (irrelevant to what this test verifies).
		MinImageWidth: 300,
		OnClassification: func(e ClassificationEvent) {
			eventsMu.Lock()
			events = append(events, e)
			eventsMu.Unlock()
		},
	}

	cand := ImageCandidate{
		ImgURL: srv.URL + "/poster.jpg",
		Source: srv.URL + "/page",
		Title:  "Event poster",
	}

	results := cfg.validateCandidates(context.Background(), []ImageCandidate{cand}, 5)
	if len(results) != 1 {
		t.Errorf("event poster candidate was rejected, want accepted: results=%+v", results)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	for _, e := range events {
		if e.Class == ClassPlaceholder && e.Source == "flat_image" {
			t.Errorf("event poster was rejected by the flatness gate: %+v", e)
		}
	}
}
