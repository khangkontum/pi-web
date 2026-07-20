package piweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestUIHandlerServesBuiltIndex(t *testing.T) {
	dist := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><title>built</title>")},
	}
	rec := httptest.NewRecorder()
	uiHandlerFS(dist).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "built") {
		t.Fatalf("body did not serve built index: %q", rec.Body.String())
	}
}

func TestUIHandlerFallbackWhenNotBuilt(t *testing.T) {
	// Only the placeholder is present — mirrors a bare `go build`.
	dist := fstest.MapFS{".gitkeep": {Data: []byte("")}}
	rec := httptest.NewRecorder()
	uiHandlerFS(dist).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "UI not built") {
		t.Fatalf("fallback body missing: %q", rec.Body.String())
	}
}
