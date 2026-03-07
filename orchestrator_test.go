package imagefy

import (
	"context"
	"errors"
	"testing"
)

// fallbackMock is a test double for SearchProvider used in FallbackProvider tests.
type fallbackMock struct {
	name       string
	candidates []ImageCandidate
	err        error
	called     bool
}

func (m *fallbackMock) Name() string { return m.name }

func (m *fallbackMock) Search(_ context.Context, _ string, _ SearchOpts) ([]ImageCandidate, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	return m.candidates, nil
}

func TestFallbackProvider_FirstSucceeds(t *testing.T) {
	want := []ImageCandidate{{ImgURL: "https://example.com/a.jpg"}}
	first := &fallbackMock{name: "first", candidates: want}
	second := &fallbackMock{name: "second", candidates: []ImageCandidate{{ImgURL: "https://example.com/b.jpg"}}}

	fp := &FallbackProvider{Providers: []SearchProvider{first, second}}
	got, err := fp.Search(context.Background(), "cat", SearchOpts{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ImgURL != want[0].ImgURL {
		t.Errorf("got %v, want %v", got, want)
	}
	if second.called {
		t.Error("second provider should not have been called")
	}
}

func TestFallbackProvider_FirstFails_SecondSucceeds(t *testing.T) {
	want := []ImageCandidate{{ImgURL: "https://example.com/b.jpg"}}
	first := &fallbackMock{name: "first", err: errors.New("timeout")}
	second := &fallbackMock{name: "second", candidates: want}

	fp := &FallbackProvider{Providers: []SearchProvider{first, second}}
	got, err := fp.Search(context.Background(), "dog", SearchOpts{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ImgURL != want[0].ImgURL {
		t.Errorf("got %v, want %v", got, want)
	}
	if !first.called {
		t.Error("first provider should have been called")
	}
	if !second.called {
		t.Error("second provider should have been called")
	}
}

func TestFallbackProvider_AllFail(t *testing.T) {
	errA := errors.New("provider A down")
	errB := errors.New("provider B down")
	first := &fallbackMock{name: "a", err: errA}
	second := &fallbackMock{name: "b", err: errB}

	fp := &FallbackProvider{Providers: []SearchProvider{first, second}}
	got, err := fp.Search(context.Background(), "bird", SearchOpts{})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errB) {
		t.Errorf("expected last error %v, got %v", errB, err)
	}
	if got != nil {
		t.Errorf("expected nil results, got %v", got)
	}
}

func TestFallbackProvider_FirstEmpty_SecondHasResults(t *testing.T) {
	want := []ImageCandidate{{ImgURL: "https://example.com/c.jpg"}}
	first := &fallbackMock{name: "first", candidates: []ImageCandidate{}} // empty, no error
	second := &fallbackMock{name: "second", candidates: want}

	fp := &FallbackProvider{Providers: []SearchProvider{first, second}}
	got, err := fp.Search(context.Background(), "flower", SearchOpts{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ImgURL != want[0].ImgURL {
		t.Errorf("got %v, want %v", got, want)
	}
	if !first.called {
		t.Error("first provider should have been called")
	}
	if !second.called {
		t.Error("second provider should have been called")
	}
}

func TestFallbackProvider_Name_Default(t *testing.T) {
	fp := &FallbackProvider{}
	if fp.Name() != "fallback" {
		t.Errorf("expected 'fallback', got %q", fp.Name())
	}
}

func TestFallbackProvider_Name_Custom(t *testing.T) {
	fp := &FallbackProvider{FallbackName: "my-chain"}
	if fp.Name() != "my-chain" {
		t.Errorf("expected 'my-chain', got %q", fp.Name())
	}
}
