package imagefy

import "testing"

func TestPreClassify_SafeLicense_AcceptsAsPhoto(t *testing.T) {
	cand := ImageCandidate{
		ImgURL:  "https://images.unsplash.com/photo-123.jpg",
		License: LicenseSafe,
	}

	class, skip := PreClassify(cand)

	if !skip {
		t.Fatal("expected skip=true for LicenseSafe candidate")
	}
	if class != "PHOTO" {
		t.Fatalf("expected class=%q, got %q", "PHOTO", class)
	}
}

func TestPreClassify_UnknownLicense_NoSkip(t *testing.T) {
	cand := ImageCandidate{
		ImgURL:  "https://example.com/image.jpg",
		License: LicenseUnknown,
	}

	class, skip := PreClassify(cand)

	if skip {
		t.Fatal("expected skip=false for LicenseUnknown candidate")
	}
	if class != "" {
		t.Fatalf("expected class=%q, got %q", "", class)
	}
}

func TestPreClassify_BlockedLicense_NoSkip(t *testing.T) {
	cand := ImageCandidate{
		ImgURL:  "https://shutterstock.com/photo-456.jpg",
		License: LicenseBlocked,
	}

	class, skip := PreClassify(cand)

	if skip {
		t.Fatal("expected skip=false for LicenseBlocked candidate")
	}
	if class != "" {
		t.Fatalf("expected class=%q, got %q", "", class)
	}
}
