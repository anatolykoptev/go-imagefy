package imagefy

import "fmt"

// LicenseSignal represents a single evidence point about an image's license status.
type LicenseSignal struct {
	Source  string       // signal source: "domain", "extra_domain", "metadata_stock", "metadata_cc", "url_pattern"
	Detail  string       // human-readable detail
	License ImageLicense // what this signal indicates
}

// LicenseAssessment combines multiple signals into a final license verdict.
type LicenseAssessment struct {
	License ImageLicense    // final verdict: Blocked > Safe > Unknown
	Signals []LicenseSignal // contributing evidence (never nil, may be empty)
}

// AssessLicense combines domain classification, extended domain checks, and
// metadata signals (stock detection, CC detection) into a single transparent
// license verdict. Blocked signals always take precedence over Safe.
func (cfg *Config) AssessLicense(cand ImageCandidate, meta *ImageMetadata) LicenseAssessment {
	signals := make([]LicenseSignal, 0, 4) //nolint:mnd // pre-allocate for up to 4 signal types

	// Signal 1: search-time domain classification (already set by provider).
	switch cand.License {
	case LicenseBlocked:
		signals = append(signals, LicenseSignal{
			Source:  "domain",
			Detail:  fmt.Sprintf("blocked by search-time domain check: %s", cand.Source),
			License: LicenseBlocked,
		})
	case LicenseSafe:
		signals = append(signals, LicenseSignal{
			Source:  "domain",
			Detail:  fmt.Sprintf("safe by search-time domain check: %s", cand.Source),
			License: LicenseSafe,
		})
	}

	// Signal 2: extended domain check — only when extra lists are configured.
	if len(cfg.ExtraBlockedDomains) > 0 || len(cfg.ExtraSafeDomains) > 0 {
		extLicense := CheckLicenseWith(cand.ImgURL, cand.Source, cfg.ExtraBlockedDomains, cfg.ExtraSafeDomains)
		// Only add a signal if it changes the classification from the search-time check.
		if extLicense != cand.License && extLicense != LicenseUnknown {
			signals = append(signals, LicenseSignal{
				Source:  "extra_domain",
				Detail:  fmt.Sprintf("reclassified by extended domain check: %s", extLicense),
				License: extLicense,
			})
		}
	}

	// Signal 3: metadata stock detection.
	if IsStockByMetadata(meta) {
		signals = append(signals, LicenseSignal{
			Source:  "metadata_stock",
			Detail:  fmt.Sprintf("stock agency detected in metadata: %s", metadataStockDetail(meta)),
			License: LicenseBlocked,
		})
	}

	// Signal 4: metadata CC detection.
	if IsCCByMetadata(meta) {
		signals = append(signals, LicenseSignal{
			Source:  "metadata_cc",
			Detail:  fmt.Sprintf("Creative Commons license in metadata: %s", metadataCCDetail(meta)),
			License: LicenseSafe,
		})
	}

	// Resolution: Blocked > Safe > Unknown.
	final := LicenseUnknown
	for _, sig := range signals {
		if sig.License == LicenseBlocked {
			final = LicenseBlocked
			break
		}
		if sig.License == LicenseSafe {
			final = LicenseSafe
		}
	}

	return LicenseAssessment{
		License: final,
		Signals: signals,
	}
}

// metadataStockDetail returns the first non-empty rights field for context
// in a stock-detection signal.
func metadataStockDetail(meta *ImageMetadata) string {
	if meta == nil {
		return ""
	}
	for _, f := range []string{
		meta.EXIFCopyright,
		meta.EXIFArtist,
		meta.IPTCCopyright,
		meta.IPTCCredit,
		meta.IPTCSource,
		meta.IPTCByline,
		meta.DCRights,
		meta.DCCreator,
	} {
		if f != "" {
			return f
		}
	}
	return ""
}

// metadataCCDetail returns the first non-empty CC license field for context
// in a CC-detection signal.
func metadataCCDetail(meta *ImageMetadata) string {
	if meta == nil {
		return ""
	}
	for _, f := range []string{
		meta.XMPLicense,
		meta.XMPWebStatement,
		meta.XMPUsageTerms,
		meta.DCRights,
	} {
		if f != "" {
			return f
		}
	}
	return ""
}
