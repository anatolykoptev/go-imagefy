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
		dominantFraction:  DefaultFlatImageDominantFraction,
		maxUniqueBuckets:  DefaultFlatImageMaxUniqueBuckets,
		maxEntropyBits:    DefaultFlatImageMaxEntropyBits,
		maxGradientEnergy: DefaultFlatImageMaxGradientEnergy,
	}
}

// flatMetrics mirrors isNonPhotographic's internal computation, exposing the
// raw per-signal values (rather than just the final verdict) for margin
// assertions and t.Logf diagnostics in the tests below.
type flatMetrics struct {
	dominantFraction float64
	uniqueBuckets    int
	entropyBits      float64
	gradientEnergy   float64
}

func computeFlatMetrics(img image.Image) flatMetrics {
	bounds := img.Bounds()
	var hist [flatBucketCount]int
	total := 0
	var gradSum float64
	var gradCount int

	stepX, stepY := flatSampleStep(bounds.Dx(), bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r, g, b, _ := img.At(x, y).RGBA()
			hist[flatBucket(r, g, b)]++
			total++

			l0 := flatLuma(r, g, b)
			if x+1 < bounds.Max.X {
				r1, g1, b1, _ := img.At(x+1, y).RGBA()
				gradSum += absFloat64(l0 - flatLuma(r1, g1, b1))
				gradCount++
			}
			if y+1 < bounds.Max.Y {
				r2, g2, b2, _ := img.At(x, y+1).RGBA()
				gradSum += absFloat64(l0 - flatLuma(r2, g2, b2))
				gradCount++
			}
		}
	}

	dominant, unique, entropy := flatHistStats(hist[:], total)
	ge := 0.0
	if gradCount > 0 {
		ge = gradSum / float64(gradCount)
	}
	return flatMetrics{
		dominantFraction: float64(dominant) / float64(total),
		uniqueBuckets:    unique,
		entropyBits:      entropy,
		gradientEnergy:   ge,
	}
}

func absFloat64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// TestIsNonPhotographic_RejectsFlatImages is the REJECT half of the gate:
// solid and near-solid synthetic images must all be flagged. These have
// gradientEnergy ~0 (dead-flat, no real pixel-to-pixel texture) so the
// gradient signal doesn't rescue them, same as before it existed.
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

// TestIsNonPhotographic_RejectsFlatImages_JPEGRoundTrip re-runs the same
// reject cases after a real JPEG encode/decode round trip (quality 85) — the
// form these images actually arrive in through the pipeline. Confirms JPEG's
// DCT compression of a solid-color region doesn't smuggle in enough
// pixel-to-pixel noise to push gradientEnergy past the epsilon and dodge the
// gate.
func TestIsNonPhotographic_RejectsFlatImages_JPEGRoundTrip(t *testing.T) {
	t.Parallel()

	white := jpegRoundTrip(t, makeSolidImage(800, 600, color.RGBA{255, 255, 255, 255}), 85)
	near := jpegRoundTrip(t, makeNearSolidImage(800, 600, color.RGBA{200, 200, 200, 255}, color.RGBA{20, 20, 20, 255}), 85)

	th := defaultFlatThresholds()
	for name, img := range map[string]image.Image{"solid_white_jpeg85": white, "near_solid_jpeg85": near} {
		rejected, reason := isNonPhotographic(img, th)
		m := computeFlatMetrics(img)
		if !rejected {
			t.Errorf("%s: expected reject after JPEG round-trip, got accept (gradientEnergy=%.4f, epsilon=%.2f)",
				name, m.gradientEnergy, th.maxGradientEnergy)
		}
		if m.gradientEnergy >= th.maxGradientEnergy {
			t.Errorf("%s: gradientEnergy=%.4f is not comfortably below epsilon=%.2f — JPEG compression noise is eating the margin",
				name, m.gradientEnergy, th.maxGradientEnergy)
		}
		if reason == "" {
			t.Error("expected a non-empty reason string on reject")
		}
	}
}

// TestIsNonPhotographic_FPGuardCorpus is the critical false-positive guard:
// the fp_guard corpus — 4 high-entropy real photos, event_poster.jpg (the
// closest legitimate-content risk class for the palette signals), and
// snow.jpg/night_sky.jpg/fog.jpg/high_key.jpg (real, low-contrast photos —
// the risk class the gradient-energy signal exists for) — must NEVER be
// rejected. If this fails, thresholds are too aggressive and must be
// loosened (favor false-negatives).
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
// assertion for the production risk class that motivated the palette
// signals: text-on-solid-background promotional graphics. Logs the actual
// metrics so a future threshold change has this as a concrete regression
// guard.
func TestIsNonPhotographic_FPGuardEventPosterMargin(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	img := corpus["event_poster.jpg"]
	if img == nil {
		t.Fatal("testdata/fp_guard/event_poster.jpg not found in corpus")
	}

	m := computeFlatMetrics(img)
	t.Logf("event_poster.jpg: dominantFraction=%.4f (threshold %.2f) unique=%d (threshold %d) entropy=%.4f (threshold %.2f)",
		m.dominantFraction, DefaultFlatImageDominantFraction,
		m.uniqueBuckets, DefaultFlatImageMaxUniqueBuckets,
		m.entropyBits, DefaultFlatImageMaxEntropyBits)

	if m.dominantFraction >= DefaultFlatImageDominantFraction {
		t.Errorf("event_poster.jpg dominantFraction=%.4f is within the reject threshold %.2f — tighten before shipping",
			m.dominantFraction, DefaultFlatImageDominantFraction)
	}

	rejected, reason := isNonPhotographic(img, defaultFlatThresholds())
	if rejected {
		t.Errorf("event_poster.jpg was rejected (reason=%q) — the FP-guard event poster must never be flagged", reason)
	}
}

// TestIsNonPhotographic_FPGuardLowContrastPhotoMargins is the dedicated
// margin assertion for legitimately low-contrast REAL photos — overcast
// snow, dark night sky, fog, a high-key white-backdrop studio shot. At the
// SHIPPED default palette thresholds none of these un-doctored photos
// actually collapses the palette (closest: high_key.jpg at ~0.81 vs. the
// 0.85 reject threshold) — the palette gate alone already accepts every one
// of them. This test is the regression guard for that: confirms none is
// ever rejected, and logs the gradientEnergy margin above epsilon as
// supplementary insurance data (see TestIsNonPhotographic_
// GradientSignalIsLoadBearing for the fixture that DOES genuinely collapse
// the palette and where gradientEnergy is what saves it).
//
// Fixtures (testdata/fp_guard/{snow,night_sky,fog,high_key}.jpg) are real
// Wikimedia Commons photographs, not synthetic gradients — synthetic flat
// images lack real sensor noise and would falsely "prove" the gate works.
func TestIsNonPhotographic_FPGuardLowContrastPhotoMargins(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	names := []string{"snow.jpg", "night_sky.jpg", "fog.jpg", "high_key.jpg"}
	th := defaultFlatThresholds()

	for _, name := range names {
		img := corpus[name]
		if img == nil {
			t.Fatalf("testdata/fp_guard/%s not found in corpus", name)
		}

		m := computeFlatMetrics(img)
		t.Logf("%-14s dominantFraction=%.4f(<%.2f?) unique=%3d(<=%d?) entropy=%.4f(<=%.2f?) gradientEnergy=%.4f (epsilon=%.3f, margin=%.4f)",
			name, m.dominantFraction, DefaultFlatImageDominantFraction,
			m.uniqueBuckets, DefaultFlatImageMaxUniqueBuckets,
			m.entropyBits, DefaultFlatImageMaxEntropyBits,
			m.gradientEnergy, th.maxGradientEnergy, m.gradientEnergy-th.maxGradientEnergy)

		if m.gradientEnergy <= th.maxGradientEnergy {
			t.Errorf("%s: gradientEnergy=%.4f does not clear epsilon=%.3f — this real low-contrast photo is no longer distinguishable from a dead-flat placeholder",
				name, m.gradientEnergy, th.maxGradientEnergy)
		}

		rejected, reason := isNonPhotographic(img, th)
		if rejected {
			t.Errorf("FALSE POSITIVE: %s was rejected (reason=%q) — a legitimate low-contrast photo must never be flagged", name, reason)
		}
	}
}

// TestIsNonPhotographic_GradientSignalIsLoadBearing reproduces the exact
// intersection risk a code-quality review reported (MAJOR): a real photo
// that is BOTH near-monochrome AND denoised/heavily-recompressed — denoising
// destroys the sensor-noise micro-texture the palette signals can't see, so
// palette collapse alone could misclassify it as a placeholder.
//
// testdata/fp_guard/high_key_denoised.jpg is that exact fixture: a REAL
// photo (high_key.jpg) cropped to its near-monochrome background region,
// then denoised (blur + quality-45 JPEG recompression — a realistic "went
// through a lossy CDN/thumbnail pipeline" scenario, not a synthetic
// gradient). Unlike the un-doctored fixtures above, this one genuinely
// collapses the palette at the SHIPPED DEFAULT dominantFraction threshold —
// no artificial threshold tightening needed to reproduce the reported risk.
// gradientEnergy is what keeps it from being rejected. The test also proves
// the check is load-bearing, not dead code: raising maxGradientEnergy past
// the photo's own gradient energy (simulating the gradient signal being
// absent/misconfigured) flips it back to rejected.
func TestIsNonPhotographic_GradientSignalIsLoadBearing(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	img := corpus["high_key_denoised.jpg"]
	if img == nil {
		t.Fatal("testdata/fp_guard/high_key_denoised.jpg not found in corpus")
	}

	m := computeFlatMetrics(img)
	t.Logf("high_key_denoised.jpg: dominantFraction=%.4f unique=%d entropy=%.4f gradientEnergy=%.4f",
		m.dominantFraction, m.uniqueBuckets, m.entropyBits, m.gradientEnergy)

	// Precondition: this fixture must genuinely collapse the palette at the
	// real shipped defaults — that's the whole point (no tightening hack).
	if m.dominantFraction < DefaultFlatImageDominantFraction ||
		m.uniqueBuckets > DefaultFlatImageMaxUniqueBuckets ||
		m.entropyBits > DefaultFlatImageMaxEntropyBits {
		t.Fatalf("precondition failed: high_key_denoised.jpg does not collapse the palette at default thresholds (dominantFraction=%.4f unique=%d entropy=%.4f) — fixture no longer reproduces the intersection risk",
			m.dominantFraction, m.uniqueBuckets, m.entropyBits)
	}

	// At the real production defaults, gradientEnergy must rescue the photo.
	if rejected, reason := isNonPhotographic(img, defaultFlatThresholds()); rejected {
		t.Errorf("gradient signal did not rescue a real, denoised, near-monochrome photo from a genuinely collapsed palette (reason=%q) — this is the exact intersection risk a code-quality review flagged",
			reason)
	}

	// Same real palette collapse, but maxGradientEnergy raised above the
	// photo's own gradient energy — simulating the gradient check being
	// absent or misconfigured. This MUST reject: if it doesn't, the
	// gradient parameter isn't actually being consulted (dead code / no-op).
	noRescue := defaultFlatThresholds()
	noRescue.maxGradientEnergy = m.gradientEnergy + 0.05 // comfortably above the photo's actual energy
	if rejected, _ := isNonPhotographic(img, noRescue); !rejected {
		t.Error("expected a misconfigured (too-loose) gradient threshold to reject high_key_denoised.jpg — gradientEnergy parameter is not being consulted")
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
	// generous bucket count, and any gradient energy counts as "dead flat")
	// must flip a real photo to rejected.
	loose := flatThresholds{dominantFraction: 0.01, maxUniqueBuckets: 1000, maxEntropyBits: 10, maxGradientEnergy: 1000}
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
	strict := flatThresholds{dominantFraction: 1.01, maxUniqueBuckets: 0, maxEntropyBits: 0, maxGradientEnergy: 1000}
	if rejected, _ := isNonPhotographic(nearSolid, strict); rejected {
		t.Error("expected impossible-to-meet palette thresholds to accept the near-solid image, got reject — Config override not consulted")
	}
}

// TestFlatThresholds_SelfDefaultsWithoutPriorDefaultsCall covers the
// silent-fail-open robustness fix a code-quality review flagged (MINOR):
// Config.flatThresholds() must return the Default* constants for any
// zero-valued FlatImage* field, regardless of whether cfg.defaults() has
// run. In the real validateOne pipeline, cfg.defaults() always runs first
// (via ValidateImageURL) before rejectedByFlatness is reached — but a caller
// that constructs a bare Config and calls flatThresholds() (or
// rejectedByFlatness) directly, bypassing that chain, must not silently get
// zero-valued thresholds instead of the documented defaults.
func TestFlatThresholds_SelfDefaultsWithoutPriorDefaultsCall(t *testing.T) {
	t.Parallel()

	cfg := &Config{} // deliberately NOT calling cfg.defaults() first
	th := cfg.flatThresholds()

	if th.dominantFraction != DefaultFlatImageDominantFraction {
		t.Errorf("dominantFraction = %v, want default %v", th.dominantFraction, DefaultFlatImageDominantFraction)
	}
	if th.maxUniqueBuckets != DefaultFlatImageMaxUniqueBuckets {
		t.Errorf("maxUniqueBuckets = %v, want default %v", th.maxUniqueBuckets, DefaultFlatImageMaxUniqueBuckets)
	}
	if th.maxEntropyBits != DefaultFlatImageMaxEntropyBits {
		t.Errorf("maxEntropyBits = %v, want default %v", th.maxEntropyBits, DefaultFlatImageMaxEntropyBits)
	}
	if th.maxGradientEnergy != DefaultFlatImageMaxGradientEnergy {
		t.Errorf("maxGradientEnergy = %v, want default %v", th.maxGradientEnergy, DefaultFlatImageMaxGradientEnergy)
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

// TestValidateOne_DisableFlatImageDetectionKillSwitch is the falsification
// pairing for the operator kill switch: the EXACT same solid candidate that
// TestValidateOne_RejectsFlatCandidate proves gets rejected must instead be
// ACCEPTED when Config.DisableFlatImageDetection is set — an explicit
// production off-switch that doesn't require a redeploy or an
// impossible-to-meet threshold.
func TestValidateOne_DisableFlatImageDetectionKillSwitch(t *testing.T) {
	t.Parallel()

	solid := makeSolidImage(1000, 700, color.RGBA{180, 180, 180, 255})
	srv := newImageServer(t, "image/jpeg", encodeJPEG(t, solid))

	cfg := &Config{
		HTTPClient:                srv.Client(),
		DisableFlatImageDetection: true,
	}

	cand := ImageCandidate{
		ImgURL: srv.URL + "/flat.jpg",
		Source: srv.URL + "/page",
		Title:  "Flat placeholder",
	}

	results := cfg.validateCandidates(context.Background(), []ImageCandidate{cand}, 5)
	if len(results) != 1 {
		t.Errorf("DisableFlatImageDetection=true did not disable the gate: results=%+v", results)
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

// TestValidateOne_AcceptsLowContrastPhotoCandidates is the pipeline-level
// FP-guard for the reported false-positive class: real low-contrast photos
// (snow, night sky, fog, high-key studio) must clear the full validateOne
// chain and be accepted, exercising rejectedByFlatness -> isNonPhotographic
// -> the gradient-energy rescue exactly as production traffic would.
func TestValidateOne_AcceptsLowContrastPhotoCandidates(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	names := []string{"snow.jpg", "night_sky.jpg", "fog.jpg", "high_key.jpg"}

	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			img := corpus[name]
			if img == nil {
				t.Fatalf("testdata/fp_guard/%s not found in corpus", name)
			}
			srv := newImageServer(t, "image/jpeg", encodeJPEG(t, img))

			var events []ClassificationEvent
			var eventsMu sync.Mutex

			cfg := &Config{
				HTTPClient:    srv.Client(),
				MinImageWidth: 300, // fixtures are smaller than DefaultMinImageWidth; irrelevant to this test
				OnClassification: func(e ClassificationEvent) {
					eventsMu.Lock()
					events = append(events, e)
					eventsMu.Unlock()
				},
			}

			cand := ImageCandidate{
				ImgURL: srv.URL + "/photo.jpg",
				Source: srv.URL + "/page",
				Title:  name,
			}

			results := cfg.validateCandidates(context.Background(), []ImageCandidate{cand}, 5)
			if len(results) != 1 {
				t.Errorf("%s was rejected by the pipeline, want accepted: results=%+v", name, results)
			}

			eventsMu.Lock()
			defer eventsMu.Unlock()
			for _, e := range events {
				if e.Class == ClassPlaceholder && e.Source == "flat_image" {
					t.Errorf("%s was rejected by the flatness gate: %+v", name, e)
				}
			}
		})
	}
}

// TestValidateOne_AcceptsDenoisedNearMonochromeCandidate is the
// pipeline-level counterpart to TestIsNonPhotographic_
// GradientSignalIsLoadBearing: drives the real, denoised, near-monochrome
// photo fixture (which genuinely collapses the palette at default
// thresholds) through the full validateOne chain and confirms gradientEnergy
// rescues it in production, not just at the isNonPhotographic unit level.
func TestValidateOne_AcceptsDenoisedNearMonochromeCandidate(t *testing.T) {
	t.Parallel()

	corpus := loadFPGuardCorpus(t)
	img := corpus["high_key_denoised.jpg"]
	if img == nil {
		t.Fatal("testdata/fp_guard/high_key_denoised.jpg not found in corpus")
	}
	srv := newImageServer(t, "image/jpeg", encodeJPEG(t, img))

	var events []ClassificationEvent
	var eventsMu sync.Mutex

	cfg := &Config{
		HTTPClient: srv.Client(),
		// high_key_denoised.jpg is cropped to 100px wide; lower the floor so
		// ValidateImageURL doesn't reject it before the flatness gate even
		// runs (irrelevant to what this test verifies).
		MinImageWidth: 50,
		OnClassification: func(e ClassificationEvent) {
			eventsMu.Lock()
			events = append(events, e)
			eventsMu.Unlock()
		},
	}

	cand := ImageCandidate{
		ImgURL: srv.URL + "/photo.jpg",
		Source: srv.URL + "/page",
		Title:  "Denoised near-monochrome background",
	}

	results := cfg.validateCandidates(context.Background(), []ImageCandidate{cand}, 5)
	if len(results) != 1 {
		t.Errorf("denoised near-monochrome candidate was rejected by the pipeline, want accepted: results=%+v", results)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	for _, e := range events {
		if e.Class == ClassPlaceholder && e.Source == "flat_image" {
			t.Errorf("denoised near-monochrome candidate was rejected by the flatness gate: %+v", e)
		}
	}
}
