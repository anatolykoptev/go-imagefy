package imagefy

import "github.com/bep/imagemeta"

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
