package imagefy_test

import (
	"context"
	"io"
	"testing"

	imagefy "github.com/anatolykoptev/go-imagefy"
	"github.com/anatolykoptev/go-stealth/imagesearch"
)

type fakeNativeDoer struct{}

func (f *fakeNativeDoer) Do(_, _ string, _ map[string]string, _ io.Reader) ([]byte, map[string]string, int, error) {
	return nil, nil, 200, nil
}

type fakeNativeEngine struct {
	results []imagesearch.ImageResult
}

func (f *fakeNativeEngine) Name() string { return "fake" }
func (f *fakeNativeEngine) Search(_ context.Context, _ imagesearch.BrowserDoer, _ string, _ int) ([]imagesearch.ImageResult, error) {
	return f.results, nil
}

func TestNativeImageProvider_converts(t *testing.T) {
	ms := &imagesearch.MultiSearch{
		Engines: []imagesearch.ImageEngine{
			&fakeNativeEngine{results: []imagesearch.ImageResult{
				{URL: "https://img.com/photo.jpg", Thumbnail: "https://th.com/t.jpg", Source: "https://page.com", Title: "Photo", Width: 1200, Height: 800, Engine: "bing"},
			}},
		},
		Doer: &fakeNativeDoer{},
	}
	p := imagefy.NewNativeImageProvider(ms)
	if p.Name() != "native" {
		t.Errorf("name = %q", p.Name())
	}
	candidates, err := p.Search(context.Background(), "test", imagefy.SearchOpts{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d, want 1", len(candidates))
	}
	c := candidates[0]
	if c.ImgURL != "https://img.com/photo.jpg" {
		t.Errorf("ImgURL = %q", c.ImgURL)
	}
	if c.Width != 1200 {
		t.Errorf("Width = %d", c.Width)
	}
	if c.Engine != "bing" {
		t.Errorf("Engine = %q", c.Engine)
	}
}

func TestNativeImageProvider_filtersBlocked(t *testing.T) {
	ms := &imagesearch.MultiSearch{
		Engines: []imagesearch.ImageEngine{
			&fakeNativeEngine{results: []imagesearch.ImageResult{
				{URL: "https://shutterstock.com/photo.jpg", Source: "https://shutterstock.com/page"},
				{URL: "https://example.com/logo.png", Source: "https://example.com"},
				{URL: "https://safe.com/photo.jpg", Source: "https://safe.com"},
			}},
		},
		Doer: &fakeNativeDoer{},
	}
	p := imagefy.NewNativeImageProvider(ms)
	candidates, err := p.Search(context.Background(), "test", imagefy.SearchOpts{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d, want 1 (shutterstock blocked, logo filtered)", len(candidates))
	}
	if candidates[0].ImgURL != "https://safe.com/photo.jpg" {
		t.Errorf("expected safe.com, got %q", candidates[0].ImgURL)
	}
}
