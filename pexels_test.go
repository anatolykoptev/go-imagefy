package imagefy

import (
	"testing"
)

// TestPexelsProviderName verifies the provider name.
func TestPexelsProviderName(t *testing.T) {
	t.Parallel()

	p := &PexelsProvider{}
	if p.Name() != "pexels" {
		t.Errorf("Name() = %q, want %q", p.Name(), "pexels")
	}
}
