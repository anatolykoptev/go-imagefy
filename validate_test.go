package imagefy

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"
)

// makeJPEG returns a minimal valid JPEG of the given dimensions.
func makeJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Fill with a solid color so the encoder produces a valid JPEG.
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 100, G: 149, B: 237, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic("makeJPEG: " + err.Error())
	}
	return buf.Bytes()
}

func newImageServer(t *testing.T, contentType string, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestValidateImageURL_WideImagePasses(t *testing.T) {
	body := makeJPEG(1000, 600)
	srv := newImageServer(t, "image/jpeg", body)

	cfg := &Config{
		HTTPClient:    srv.Client(),
		MinImageWidth: 880,
	}
	// ValidateImageURL creates its own http.Client, so we need to serve on a
	// real address that the stdlib client can reach — httptest.NewServer does that.
	if !cfg.ValidateImageURL(context.Background(), srv.URL+"/photo.jpg") {
		t.Error("expected wide image (1000px) to pass validation")
	}
}

func TestValidateImageURL_NarrowImageFails(t *testing.T) {
	body := makeJPEG(400, 300)
	srv := newImageServer(t, "image/jpeg", body)

	cfg := &Config{
		HTTPClient:    srv.Client(),
		MinImageWidth: 880,
	}
	if cfg.ValidateImageURL(context.Background(), srv.URL+"/thumb.jpg") {
		t.Error("expected narrow image (400px) to fail validation")
	}
}

func TestValidateImageURL_LogoURLRejected(t *testing.T) {
	// No server needed — URL pattern check happens before any HTTP call.
	cfg := &Config{}
	if cfg.ValidateImageURL(context.Background(), "https://example.com/assets/logo.png") {
		t.Error("expected logo URL to be rejected")
	}
}

func TestValidateImageURL_NonImageContentTypeRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not an image</html>"))
	}))
	defer srv.Close()

	cfg := &Config{}
	if cfg.ValidateImageURL(context.Background(), srv.URL+"/page.html") {
		t.Error("expected non-image content type to fail validation")
	}
}

func TestValidateImageURL_DefaultMinImageWidth(t *testing.T) {
	// Config with zero MinImageWidth — defaults() should apply DefaultMinImageWidth (880).
	// A narrow image (400px) must be rejected using the default.
	body := makeJPEG(400, 300)
	srv := newImageServer(t, "image/jpeg", body)

	cfg := &Config{} // MinImageWidth intentionally left as zero value
	if cfg.ValidateImageURL(context.Background(), srv.URL+"/narrow.jpg") {
		t.Error("expected narrow image to fail with default MinImageWidth (880)")
	}
	if cfg.MinImageWidth != DefaultMinImageWidth {
		t.Errorf("MinImageWidth after defaults() = %d, want %d", cfg.MinImageWidth, DefaultMinImageWidth)
	}
}
