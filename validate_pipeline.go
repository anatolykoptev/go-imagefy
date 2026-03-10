package imagefy

import (
	"context"
	"log/slog"
	"sync"
)

const validationSemaphore = 3

func (cfg *Config) validateCandidates(ctx context.Context, toValidate []ImageCandidate, maxResults int) []ImageCandidate {
	sem := make(chan struct{}, validationSemaphore)
	var mu sync.Mutex
	var validated []ImageCandidate
	dedup := &dedupFilter{}

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

			cfg.validateOne(ctx, cand, maxResults, &mu, &validated, dedup)
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
//  4. Perceptual dedup — reject visual duplicates (dHash)
//  5. ExtractImageMetadata + AssessLicense — domain + metadata signals
//  6. LLM Vision classification — fallback for unknown license
func (cfg *Config) validateOne(ctx context.Context, cand ImageCandidate, maxResults int, mu *sync.Mutex, validated *[]ImageCandidate, dedup *dedupFilter) {
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

	if img != nil && dedup.isDuplicate(img) {
		slog.Debug("imagefy: dedup rejected", "url", cand.ImgURL)
		return
	}

	accepted, done := cfg.assessAndAccept(ctx, cand, data, maxResults, mu, validated)
	if done {
		return
	}
	if accepted {
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
