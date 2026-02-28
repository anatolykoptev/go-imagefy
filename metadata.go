package imagefy

import "strings"

// ImageMetadata holds EXIF, IPTC, XMP, and Dublin Core fields extracted from
// image binary data. Used for stock-photo detection and CC license detection.
type ImageMetadata struct {
	EXIFCopyright   string
	EXIFArtist      string
	IPTCCopyright   string
	IPTCCredit      string
	IPTCSource      string
	IPTCByline      string
	XMPLicense      string
	XMPWebStatement string
	XMPUsageTerms   string
	DCRights        string
	DCCreator       string
}

// stockMetadataKeywords are substrings that indicate a stock-photo agency when
// found (case-insensitive) in any metadata field.
var stockMetadataKeywords = []string{
	"shutterstock",
	"gettyimages",
	"getty images",
	"istockphoto",
	"istock",
	"alamy",
	"depositphotos",
	"dreamstime",
	"123rf",
	"adobestock",
	"adobe stock",
	"bigstockphoto",
	"stocksy",
	"pond5",
	"masterfile",
	"superstock",
	"agefotostock",
	"age fotostock",
	"colourbox",
	"yayimages",
	"vectorstock",
	"freepik",
	"canstockphoto",
}

// IsStockByMetadata reports whether the image metadata contains fingerprints
// of a known stock-photo agency (case-insensitive substring match).
func IsStockByMetadata(meta *ImageMetadata) bool {
	if meta == nil {
		return false
	}
	fields := []string{
		meta.EXIFCopyright,
		meta.EXIFArtist,
		meta.IPTCCopyright,
		meta.IPTCCredit,
		meta.IPTCSource,
		meta.IPTCByline,
		meta.DCRights,
		meta.DCCreator,
	}
	for _, f := range fields {
		if f == "" {
			continue
		}
		lower := strings.ToLower(f)
		for _, kw := range stockMetadataKeywords {
			if strings.Contains(lower, kw) {
				return true
			}
		}
	}
	return false
}

// IsCCByMetadata reports whether the image metadata contains a Creative
// Commons license URL in any XMP/DC license field.
func IsCCByMetadata(meta *ImageMetadata) bool {
	if meta == nil {
		return false
	}
	for _, f := range []string{
		meta.XMPLicense,
		meta.XMPWebStatement,
		meta.XMPUsageTerms,
		meta.DCRights,
	} {
		if IsCCLicenseURL(f) {
			return true
		}
		// Also check if a CC URL appears as a substring (e.g. in free-text fields).
		lower := strings.ToLower(f)
		for _, seg := range ccLicensePathSegments {
			if strings.Contains(lower, seg) {
				return true
			}
		}
	}
	return false
}

// ExtractImageMetadata parses EXIF/IPTC/XMP metadata from raw image bytes.
// Returns nil if the data is nil, empty, or cannot be parsed.
func ExtractImageMetadata(data []byte) *ImageMetadata {
	if len(data) == 0 {
		return nil
	}
	// TODO: implement actual EXIF/IPTC/XMP parsing.
	return nil
}
