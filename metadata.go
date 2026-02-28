package imagefy

import (
	"bytes"
	"strings"

	"github.com/bep/imagemeta"
)

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
	XMPMarked       bool // xmpRights:Marked
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

// wantedTags maps (source, tag-name) → true for every tag we care about.
var wantedTags = map[imagemeta.Source]map[string]bool{
	imagemeta.IPTC: {
		"CopyrightNotice": true,
		"Credit":          true,
		"Byline":          true,
		"Source":          true,
	},
	imagemeta.EXIF: {
		"Copyright": true,
		"Artist":    true,
	},
	imagemeta.XMP: {
		"WebStatement": true,
		"UsageTerms":   true,
		"License":      true,
		"Marked":       true,
		"Rights":       true,
		"Creator":      true,
	},
}

// ExtractImageMetadata parses EXIF/IPTC/XMP metadata from raw image bytes.
// Returns nil if the data is nil, empty, or cannot be parsed.
// Graceful degradation: never returns an error.
func ExtractImageMetadata(data []byte) *ImageMetadata {
	if len(data) == 0 {
		return nil
	}

	meta := &ImageMetadata{}
	found := false

	_, err := imagemeta.Decode(imagemeta.Options{
		R:       bytes.NewReader(data),
		Sources: imagemeta.EXIF | imagemeta.IPTC | imagemeta.XMP,
		ShouldHandleTag: func(ti imagemeta.TagInfo) bool {
			if tags, ok := wantedTags[ti.Source]; ok {
				return tags[ti.Tag]
			}
			return false
		},
		HandleTag: func(ti imagemeta.TagInfo) error {
			switch ti.Source {
			case imagemeta.IPTC:
				handleIPTCTag(meta, ti, &found)
			case imagemeta.EXIF:
				handleEXIFTag(meta, ti, &found)
			case imagemeta.XMP:
				handleXMPTag(meta, ti, &found)
			}
			return nil
		},
	})

	if err != nil || !found {
		return nil
	}

	return meta
}

// handleIPTCTag sets the appropriate ImageMetadata field for an IPTC tag.
func handleIPTCTag(meta *ImageMetadata, ti imagemeta.TagInfo, found *bool) {
	s := tagValueString(ti.Value)
	if s == "" {
		return
	}

	switch ti.Tag {
	case "CopyrightNotice":
		meta.IPTCCopyright = s
	case "Credit":
		meta.IPTCCredit = s
	case "Byline":
		meta.IPTCByline = s
	case "Source":
		meta.IPTCSource = s
	default:
		return
	}

	*found = true
}

// handleEXIFTag sets the appropriate ImageMetadata field for an EXIF tag.
func handleEXIFTag(meta *ImageMetadata, ti imagemeta.TagInfo, found *bool) {
	s := tagValueString(ti.Value)
	if s == "" {
		return
	}

	switch ti.Tag {
	case "Copyright":
		meta.EXIFCopyright = s
	case "Artist":
		meta.EXIFArtist = s
	default:
		return
	}

	*found = true
}

// handleXMPTag sets the appropriate ImageMetadata field for an XMP tag.
func handleXMPTag(meta *ImageMetadata, ti imagemeta.TagInfo, found *bool) {
	switch ti.Tag {
	case "Marked":
		if b, ok := ti.Value.(bool); ok {
			meta.XMPMarked = b
			*found = true
		}
	case "WebStatement":
		if s := tagValueString(ti.Value); s != "" {
			meta.XMPWebStatement = s
			*found = true
		}
	case "UsageTerms":
		if s := tagValueString(ti.Value); s != "" {
			meta.XMPUsageTerms = s
			*found = true
		}
	case "License":
		if s := tagValueString(ti.Value); s != "" {
			meta.XMPLicense = s
			*found = true
		}
	case "Rights":
		if s := tagValueString(ti.Value); s != "" {
			meta.DCRights = s
			*found = true
		}
	case "Creator":
		if s := tagValueString(ti.Value); s != "" {
			meta.DCCreator = s
			*found = true
		}
	}
}

// tagValueString extracts a string from a tag value.
// XMP values may be string or []string (from altList/seqList).
func tagValueString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []string:
		if len(val) > 0 {
			return val[0]
		}
		return ""
	case []any:
		if len(val) > 0 {
			if s, ok := val[0].(string); ok {
				return s
			}
		}
		return ""
	default:
		return ""
	}
}
