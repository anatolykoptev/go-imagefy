package imagefy

import (
	"image"
	"image/color"
	"sync"
	"testing"
)

// makeGradientImage creates an image with a horizontal gradient from black to white.
// dHash (difference hash) measures adjacent pixel differences, so a gradient produces
// a unique, spatially-rich hash — unlike solid-color images which all hash identically.
func makeGradientImage(width, height int, baseGray uint8) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			// Mix a horizontal gradient with a base brightness offset so that
			// two images with different baseGray values produce distinct hashes.
			v := uint8(int(baseGray) + x*int(255-baseGray)/width)
			img.Set(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

// makeCheckerImage creates an image with a black-and-white checkerboard pattern.
// Its dHash is maximally distinct from a gradient image.
func makeCheckerImage(width, height, tileSize int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			var v uint8
			if (x/tileSize+y/tileSize)%2 == 0 {
				v = 255
			}
			img.Set(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

func TestDedupFilter_IdenticalImagesAreDeduplicated(t *testing.T) {
	t.Parallel()

	d := &dedupFilter{}
	img := makeGradientImage(100, 100, 0)

	// First call: unique image — must be accepted.
	if d.isDuplicate(img) {
		t.Fatal("first image should not be a duplicate")
	}

	// Second call with identical image: must be rejected as duplicate.
	if !d.isDuplicate(img) {
		t.Fatal("identical image should be detected as duplicate")
	}
}

func TestDedupFilter_DifferentImagesNotDeduplicated(t *testing.T) {
	t.Parallel()

	d := &dedupFilter{}

	// Two structurally different images: one gradient, one checkerboard.
	// Their dHashes should differ by more than dedupThreshold.
	grad := makeGradientImage(100, 100, 0)
	checker := makeCheckerImage(100, 100, 10)

	if d.isDuplicate(grad) {
		t.Fatal("first image (gradient) should not be a duplicate")
	}
	if d.isDuplicate(checker) {
		t.Fatal("second image (checkerboard) should not be flagged as duplicate of gradient")
	}
}

func TestDedupFilter_ThreeDistinctImagesAllAccepted(t *testing.T) {
	t.Parallel()

	d := &dedupFilter{}

	// Three structurally distinct images.
	imgs := []image.Image{
		makeGradientImage(100, 100, 0),    // black→white gradient
		makeCheckerImage(100, 100, 10),    // 10px checker
		makeCheckerImage(100, 100, 2),     // 2px checker (different frequency)
	}

	for i, img := range imgs {
		if d.isDuplicate(img) {
			t.Errorf("image %d should not be a duplicate", i)
		}
	}
	// All three hashes stored.
	if len(d.hashes) != len(imgs) {
		t.Errorf("expected %d stored hashes, got %d", len(imgs), len(d.hashes))
	}
}

func TestDedupFilter_GracefulDegradationZeroSizeImage(t *testing.T) {
	t.Parallel()

	// goimagehash returns an error for zero-size images.
	// isDuplicate must accept (not panic, not return true) via graceful degradation.
	d := &dedupFilter{}
	emptyImg := image.NewNRGBA(image.Rect(0, 0, 0, 0))

	result := d.isDuplicate(emptyImg)
	// Graceful degradation: hash failure → accept image (return false).
	if result {
		t.Error("zero-size image should be accepted (graceful degradation), not flagged as duplicate")
	}
}

func TestDedupFilter_ConcurrentCallsAreSafe(t *testing.T) {
	t.Parallel()

	d := &dedupFilter{}
	img := makeGradientImage(100, 100, 0)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			// Each goroutine calls isDuplicate — no panic, no data race expected.
			d.isDuplicate(img)
		}()
	}

	wg.Wait()

	// The hash must be stored exactly once (first goroutine wins; rest see it as duplicate).
	d.mu.Lock()
	hashCount := len(d.hashes)
	d.mu.Unlock()

	if hashCount != 1 {
		t.Errorf("expected exactly 1 stored hash after concurrent inserts, got %d", hashCount)
	}
}

func TestDedupFilter_FreshFilterPerSearch(t *testing.T) {
	t.Parallel()

	img := makeGradientImage(100, 100, 0)

	// First filter instance: image accepted and stored.
	d1 := &dedupFilter{}
	if d1.isDuplicate(img) {
		t.Fatal("d1: first call should not be a duplicate")
	}

	// Fresh filter for next search — same image must be accepted again (no shared state).
	d2 := &dedupFilter{}
	if d2.isDuplicate(img) {
		t.Fatal("d2: fresh filter should not inherit previous filter's hashes")
	}
}
