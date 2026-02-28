package imagefy

// PreClassify applies cheap heuristics to classify an image candidate without
// calling the LLM. Returns the predicted class and skip=true if the heuristic
// is conclusive. Returns ("", false) if the LLM should be consulted.
//
// Superseded in the search pipeline by [Config.AssessLicense], which combines
// domain classification, extended domain checks, and metadata signals into a
// single transparent verdict. PreClassify is still available as a standalone
// helper for callers that don't need the full assessment pipeline.
//
// Current heuristics:
//   - LicenseSafe sources (Openverse, Unsplash, Pixabay) → auto-accept as PHOTO.
//     These are curated CC/public-domain collections with negligible false-positive risk.
func PreClassify(cand ImageCandidate) (class string, skip bool) {
	if cand.License == LicenseSafe {
		return ClassPhoto, true
	}
	return "", false
}
