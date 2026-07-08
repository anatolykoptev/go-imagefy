package imagefy

import (
	"fmt"
	"image"
	"math"
)

// Flat/non-photographic image reject gate.
//
// Insight: this catches UNKNOWN placeholders generically, where the phash
// blocklist (placeholder.go) only catches KNOWN, seeded ones. It looks at the
// pixel PALETTE, not the content: a real photograph can never be
// near-monochrome, and a rich, modern AI-generated poster always spans many
// colors — so a near-flat / solid-color / tiny-palette image is almost
// certainly a placeholder or blank, not a legitimate photo or poster. No OCR,
// no vision-LLM: pure Go over the already-decoded image.
//
// The three palette signals below (dominant fraction, unique buckets,
// entropy) are NOT independent: for a scene confined to a narrow tonal band —
// overcast snow, fog, night sky, a high-key white-backdrop studio shot — once
// dominantFraction collapses past the threshold, the remaining mass
// necessarily concentrates in a few buckets, so uniqueBuckets/entropy follow
// automatically. Palette alone can't tell a genuinely flat placeholder apart
// from a legitimately low-contrast REAL photo. A fourth, palette-independent
// signal closes that gap: gradientEnergy, the mean pixel-to-pixel luma
// difference between truly adjacent pixels. A real photograph — even a
// foggy snowscape — carries sensor noise and subtle tonal micro-gradients,
// so adjacent pixels differ slightly; a solid/placeholder fill is dead-flat
// pixel-to-pixel. Reject requires the palette collapse AND near-zero
// gradient energy — see isNonPhotographic for the full verdict.
const (
	// DefaultFlatImageDominantFraction is the minimum share of sampled pixels
	// that must fall in a single coarse color bucket for the dominant-fraction
	// signal to fire. No real photograph has a single color at this share.
	DefaultFlatImageDominantFraction = 0.85

	// DefaultFlatImageMaxUniqueBuckets is the maximum number of distinct
	// coarse color buckets (out of flatBucketCount) allowed for the
	// unique-buckets signal to fire. A rich AI-generated poster spans far
	// more buckets than this.
	DefaultFlatImageMaxUniqueBuckets = 32

	// DefaultFlatImageMaxEntropyBits is the maximum Shannon entropy (bits) of
	// the sampled color-bucket histogram allowed for the entropy signal to
	// fire.
	DefaultFlatImageMaxEntropyBits = 1.5

	// DefaultFlatImageMaxGradientEnergy is the maximum mean pixel-to-pixel
	// luma difference (8-bit-equivalent units, adjacent sampled pixels)
	// allowed for the gradient-energy signal to fire. Tuned against real
	// low-contrast photo fixtures (testdata/fp_guard/{snow,night_sky,fog,
	// high_key}.jpg) — see flatimage_test.go for measured margins. Synthetic
	// solid/near-solid images (even JPEG-recompressed) sit at 0.0-0.0025;
	// the flattest real photo found (night_sky.jpg, a long-exposure dark-sky
	// shot) sits at 0.72 — clean separation with margin on both sides.
	DefaultFlatImageMaxGradientEnergy = 0.5

	// flatSampleCap bounds the number of pixels sampled per image, keeping
	// the detector's cost roughly constant regardless of image resolution.
	flatSampleCap = 4096

	// flatBitsPerChannel quantizes each color channel down to this many
	// most-significant bits before bucketing (3 bits/channel = 8 levels per
	// channel = 512 total buckets).
	flatBitsPerChannel = 3
	flatBucketCount    = 1 << (3 * flatBitsPerChannel) // 512

	// Rec.601 luma weights + 16-bit-to-8-bit-equivalent scale, used by
	// flatLuma for the gradient-energy signal.
	lumaRWeight   = 0.299
	lumaGWeight   = 0.587
	lumaBWeight   = 0.114
	eightBitScale = 257.0 // RGBA() returns 16-bit (0-65535); 65535/255 = 257
)

// flatThresholds bundles the four-signal verdict thresholds for
// isNonPhotographic: three palette signals (AND) plus the palette-independent
// gradient-energy signal. Built from Config's FlatImage* fields (see
// imagefy.go) via Config.flatThresholds — self-defaulting, so it's correct
// even if cfg.defaults() hasn't run (see flatThresholds doc).
type flatThresholds struct {
	dominantFraction  float64
	maxUniqueBuckets  int
	maxEntropyBits    float64
	maxGradientEnergy float64
}

// flatThresholds builds the isNonPhotographic threshold bundle from this
// Config's FlatImage* fields, falling back to the Default* constants for any
// field left at its zero value. Self-defaulting rather than relying on a
// prior cfg.defaults() call: validateCandidates (the entry point that
// actually calls isNonPhotographic) never calls cfg.defaults() itself, so a
// caller that reaches it directly — as this package's own tests do — would
// otherwise get zero-valued thresholds and the gate would silently fail-open
// (reject nothing, since dominantFraction>=0 and 0<=0 trivially hold for a
// truly empty threshold... worse, an all-zero maxEntropyBits/maxUniqueBuckets
// would reject far too aggressively). Self-defaulting makes this robust
// regardless of caller.
func (cfg *Config) flatThresholds() flatThresholds {
	return flatThresholds{
		dominantFraction:  flatFloatOrDefault(cfg.FlatImageDominantFraction, DefaultFlatImageDominantFraction),
		maxUniqueBuckets:  flatIntOrDefault(cfg.FlatImageMaxUniqueBuckets, DefaultFlatImageMaxUniqueBuckets),
		maxEntropyBits:    flatFloatOrDefault(cfg.FlatImageMaxEntropyBits, DefaultFlatImageMaxEntropyBits),
		maxGradientEnergy: flatFloatOrDefault(cfg.FlatImageMaxGradientEnergy, DefaultFlatImageMaxGradientEnergy),
	}
}

func flatFloatOrDefault(v, def float64) float64 {
	if v <= 0 {
		return def
	}
	return v
}

func flatIntOrDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// isNonPhotographic decides whether img is a flat / near-solid-color / blank
// placeholder, computed over a bounded sample of its pixels:
//
//   - dominantFraction — largest coarse-color-bucket count / total samples.
//   - uniqueBuckets    — count of non-empty coarse color buckets.
//   - entropy          — Shannon entropy (bits) of the bucket histogram.
//   - gradientEnergy   — mean luma difference between adjacent sampled
//     pixels and their immediate right/down neighbor in the ORIGINAL image
//     (not two strided samples — those can be far apart and would always
//     read as high-gradient noise).
//
// Verdict: reject only if ALL THREE palette signals agree (dominantFraction
// >= threshold AND uniqueBuckets <= threshold AND entropy <= threshold) —
// this is the "is the palette collapsed" gate — AND gradientEnergy is at or
// below its threshold — the "is there real micro-texture" gate. The palette
// signals alone are NOT sufficient: they are correlated (a narrow tonal band
// collapses all three together), so a legitimately low-contrast photograph
// (overcast snow, fog, night sky, high-key studio) can satisfy all three
// palette signals while still being a real photo. gradientEnergy is what
// tells them apart — a placeholder fill is dead-flat pixel-to-pixel; a real
// photo, even a flat-looking one, carries sensor noise/tonal micro-gradients.
// Preserve this two-stage (palette-collapse AND dead-flat) structure on any
// retune — favor false-negatives (a missed blank is cheap) over
// false-positives (a rejected legitimate photo/poster is not).
//
// Returns (true, reason) on reject with a human-readable metrics summary for
// logging; (false, "") otherwise. A nil or zero-area img is graceful
// degradation — accept, same contract as rejectedByHash.
func isNonPhotographic(img image.Image, t flatThresholds) (bool, string) {
	if img == nil {
		return false, ""
	}

	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return false, ""
	}

	var hist [flatBucketCount]int
	total := 0
	var gradSum float64
	var gradCount int

	stepX, stepY := flatSampleStep(width, height)
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r, g, b, _ := img.At(x, y).RGBA()
			hist[flatBucket(r, g, b)]++
			total++

			l0 := flatLuma(r, g, b)
			// Gradient energy MUST use the actual neighbor pixel (x+1, y)
			// and (x, y+1) in the original image, never the next STRIDED
			// sample — a stride-4096 "next sample" can be dozens of pixels
			// away and would always read as high-gradient noise, defeating
			// the signal for every image regardless of flatness.
			if x+1 < bounds.Max.X {
				r1, g1, b1, _ := img.At(x+1, y).RGBA()
				gradSum += math.Abs(l0 - flatLuma(r1, g1, b1))
				gradCount++
			}
			if y+1 < bounds.Max.Y {
				r2, g2, b2, _ := img.At(x, y+1).RGBA()
				gradSum += math.Abs(l0 - flatLuma(r2, g2, b2))
				gradCount++
			}
		}
	}
	if total == 0 {
		return false, ""
	}

	dominant, unique, entropy := flatHistStats(hist[:], total)
	dominantFraction := float64(dominant) / float64(total)

	paletteCollapsed := dominantFraction >= t.dominantFraction && unique <= t.maxUniqueBuckets && entropy <= t.maxEntropyBits
	if !paletteCollapsed {
		return false, ""
	}

	gradientEnergy := 0.0
	if gradCount > 0 {
		gradientEnergy = gradSum / float64(gradCount)
	}
	if gradientEnergy > t.maxGradientEnergy {
		// Palette looks collapsed, but real pixel-to-pixel micro-texture is
		// present — a legitimate low-contrast photo, not a dead-flat
		// placeholder. Accept.
		return false, ""
	}

	reason := fmt.Sprintf("dominant_fraction=%.3f unique_buckets=%d entropy_bits=%.3f gradient_energy=%.3f samples=%d",
		dominantFraction, unique, entropy, gradientEnergy, total)
	return true, reason
}

// flatHistStats reduces a color-bucket histogram to the three palette
// verdict signals: the largest single-bucket count, the number of non-empty
// buckets, and the histogram's Shannon entropy in bits.
func flatHistStats(hist []int, total int) (dominant, unique int, entropyBits float64) {
	for _, count := range hist {
		if count == 0 {
			continue
		}
		unique++
		if count > dominant {
			dominant = count
		}
		p := float64(count) / float64(total)
		entropyBits -= p * math.Log2(p)
	}
	return dominant, unique, entropyBits
}

// flatSampleStep picks an (x, y) pixel stride so the number of samples taken
// across the image stays near flatSampleCap regardless of resolution — O(1)
// detector cost instead of O(width*height).
func flatSampleStep(width, height int) (stepX, stepY int) {
	total := width * height
	if total <= flatSampleCap {
		return 1, 1
	}
	step := int(math.Sqrt(float64(total) / float64(flatSampleCap)))
	if step < 1 {
		step = 1
	}
	return step, step
}

// flatBucket quantizes an RGBA() triple (16-bit, alpha-premultiplied per the
// image.Color contract) down to a flatBitsPerChannel-per-channel coarse
// bucket index in [0, flatBucketCount).
func flatBucket(r, g, b uint32) int {
	const shift = 16 - flatBitsPerChannel
	ri := int(r >> shift)
	gi := int(g >> shift)
	bi := int(b >> shift)
	return (ri << (2 * flatBitsPerChannel)) | (gi << flatBitsPerChannel) | bi
}

// flatLuma converts an RGBA() triple (16-bit, alpha-premultiplied) to an
// 8-bit-equivalent luma value (Rec.601 weights), for the gradient-energy
// signal.
func flatLuma(r, g, b uint32) float64 {
	return (lumaRWeight*float64(r) + lumaGWeight*float64(g) + lumaBWeight*float64(b)) / eightBitScale
}
