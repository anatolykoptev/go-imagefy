package imagefy

import (
	"context"
	"image"
	"log/slog"
	"sync"

	"github.com/corona10/goimagehash"
)

const validationSemaphore = 3

func (cfg *Config) validateCandidates(ctx context.Context, toValidate []ImageCandidate, maxResults int) []ImageCandidate {
	sem := make(chan struct{}, validationSemaphore)
	var mu sync.Mutex
	var validated []ImageCandidate
	dedup := &dedupFilter{}
	placeholders := newPlaceholderMatcher(cfg.PlaceholderHashes)

	var wg sync.WaitGroup
	for _, c := range toValidate {
		mu.Lock()
		enough := len(validated) >= maxResults
		mu.Unlock()
		if enough {
			break
		}

		wg.Add(1)
		go func(cand ImageCandidate) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			cfg.validateOne(ctx, cand, maxResults, &mu, &validated, dedup, placeholders)
		}(c)
	}
	wg.Wait()

	return validated
}

// validateOne validates a single candidate and appends it to validated if it passes all checks.
// Recovers from panics to protect the goroutine pool.
//
// Pipeline stages:
//  1. ValidateImageURL — HTTP probe (dimensions, content-type, logo/banner check)
//  2. Extra domain pre-check — skip download for known-blocked domains
//  3. downloadForValidation — single download for dedup + metadata + LLM
//  4. rejectedByHash — perceptual dedup, then the placeholder-blocklist +
//     flat/non-photographic gates (screenDecodedImage, shared with the public
//     ScreenImage — see screen.go), all off one shared dHash
//  5. ExtractImageMetadata + AssessLicense — domain + metadata signals
//  6. ReverseCheck — reverse image search for laundered stock (opt-in)
//  7. LLM Vision classification — fallback for unknown license
func (cfg *Config) validateOne(ctx context.Context, cand ImageCandidate, maxResults int, mu *sync.Mutex, validated *[]ImageCandidate, dedup *dedupFilter, placeholders *placeholderMatcher) {
	defer func() {
		if r := recover(); r != nil {
			if cfg.OnPanic != nil {
				cfg.OnPanic("imageValidation", r)
			}
		}
	}()

	if !cfg.ValidateImageURL(ctx, cand.ImgURL) {
		return
	}

	if cfg.isBlockedByExtraDomains(cand) {
		return
	}

	data, mimeType, img := cfg.downloadForValidation(ctx, cand.ImgURL)

	if cfg.rejectedByHash(img, cand.ImgURL, dedup, placeholders) {
		return
	}

	accepted, done := cfg.assessAndAccept(ctx, cand, data, maxResults, mu, validated)
	if done {
		return
	}
	if accepted {
		return
	}

	// Step 6: Reverse image search — detect laundered stock photos.
	reverseResult := cfg.ReverseCheck(ctx, cand.ImgURL)
	if reverseResult.IsStock {
		slog.Debug("imagefy: blocked by reverse stock check",
			"url", cand.ImgURL,
			"stock_domains", reverseResult.StockDomains,
		)
		cfg.emitClassification(cand.ImgURL, ClassStock, 0, "reverse_stock")
		return
	}

	// Unknown license — classify using pre-downloaded data.
	result := cfg.classifyPredownloaded(ctx, cand.ImgURL, data, mimeType)
	if result.Class != ClassPhoto && result.Class != "" {
		slog.Debug("imagefy: vision rejected", "url", cand.ImgURL, "class", result.Class)
		return
	}
	appendValidated(mu, validated, cand, maxResults)
}

// rejectedByHash runs perceptual dedup, then the placeholder-blocklist +
// flat/non-photographic reject gates (screenDecodedImage, shared with the
// public ScreenImage — see screen.go) against img, computing the dHash once
// and sharing it across all three (avoids hashing the same decoded image
// twice).
//
// Graceful degradation: a nil img (decode failed) or a hashing failure skips
// dedup and falls through to screenDecodedImage's own graceful degradation
// (see there) — never false-reject on error, let downstream checks (license
// assessment, reverse-stock, vision classification) decide instead.
func (cfg *Config) rejectedByHash(img image.Image, url string, dedup *dedupFilter, placeholders *placeholderMatcher) bool {
	if img == nil {
		return false
	}
	hash, err := goimagehash.DifferenceHash(img)
	if err != nil {
		return false
	}

	if dedup.isDuplicateHash(hash) {
		slog.Debug("imagefy: dedup rejected", "url", url)
		return true
	}

	reject, class, reason := cfg.screenDecodedImage(img, hash, url, placeholders)
	if !reject {
		return false
	}
	cfg.emitClassification(url, class, 1.0, reason)
	return true
}

// isBlockedByExtraDomains checks extra blocked domains before downloading.
func (cfg *Config) isBlockedByExtraDomains(cand ImageCandidate) bool {
	if len(cfg.ExtraBlockedDomains) == 0 {
		return false
	}
	if CheckLicenseWith(cand.ImgURL, cand.Source, cfg.ExtraBlockedDomains, nil) != LicenseBlocked {
		return false
	}
	slog.Debug("imagefy: blocked by extra domain pre-check", "url", cand.ImgURL)
	cfg.emitClassification(cand.ImgURL, ClassStock, 0, "license_assessment")
	return true
}

// assessAndAccept runs metadata extraction and license assessment.
// Returns (accepted, done): accepted=true if candidate was added, done=true if pipeline should stop.
func (cfg *Config) assessAndAccept(ctx context.Context, cand ImageCandidate, data []byte, maxResults int, mu *sync.Mutex, validated *[]ImageCandidate) (bool, bool) {
	meta := ExtractImageMetadata(data)
	assessment := cfg.AssessLicense(cand, meta)

	if assessment.License == LicenseBlocked {
		slog.Debug("imagefy: blocked by license assessment", "url", cand.ImgURL, "signals", assessment.Signals)
		cfg.emitClassification(cand.ImgURL, ClassStock, 0, "license_assessment")
		return false, true
	}

	if assessment.License == LicenseSafe {
		slog.Debug("imagefy: safe by license assessment", "url", cand.ImgURL, "signals", assessment.Signals)
		cfg.emitClassification(cand.ImgURL, ClassPhoto, 1.0, "license_assessment")
		appendValidated(mu, validated, cand, maxResults)
		return true, true
	}

	return false, false
}

// emitClassification fires the OnClassification callback if configured.
func (cfg *Config) emitClassification(url, class string, confidence float64, source string) {
	if cfg.OnClassification != nil {
		cfg.OnClassification(ClassificationEvent{
			URL:        url,
			Class:      class,
			Confidence: confidence,
			Source:     source,
		})
	}
}

// appendValidated safely appends a candidate to the validated slice if capacity remains.
func appendValidated(mu *sync.Mutex, validated *[]ImageCandidate, cand ImageCandidate, maxResults int) {
	mu.Lock()
	if len(*validated) < maxResults {
		*validated = append(*validated, cand)
	}
	mu.Unlock()
}
