package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AI-DB/frontend"
)

// TestSPAHandler covers the same-origin static hosting wired into the server mux:
// the built index.html at /, an SPA fallback for unknown client routes, real asset
// files served directly, and a 404 (not the HTML shell) for unmatched /api paths.
func TestSPAHandler(t *testing.T) {
	dist, err := frontend.Dist()
	if err != nil {
		t.Fatalf("frontend.Dist: %v", err)
	}
	h := spaHandler(dist)

	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}

	// Root serves the built shell.
	root := get("/")
	if root.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", root.Code)
	}
	if !strings.Contains(root.Body.String(), `<div id="root">`) {
		t.Errorf("GET / body is not the built index.html: %.120s", root.Body.String())
	}

	// Unknown client-side route falls back to index.html (the SPA owns routing).
	fb := get("/some/client/route")
	if fb.Code != http.StatusOK || !strings.Contains(fb.Body.String(), `<div id="root">`) {
		t.Errorf("SPA fallback: status = %d, want 200 index.html", fb.Code)
	}

	// A real bundled asset is served directly, not the HTML fallback. Discover one
	// from the embedded tree so the test does not hardcode a hashed filename.
	entries, err := fs.ReadDir(dist, "assets")
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected embedded assets, err=%v n=%d", err, len(entries))
	}
	assetPath := "/assets/" + entries[0].Name()
	asset := get(assetPath)
	if asset.Code != http.StatusOK {
		t.Errorf("GET %s status = %d, want 200", assetPath, asset.Code)
	}
	if strings.Contains(asset.Body.String(), `<div id="root">`) {
		t.Errorf("GET %s returned the HTML shell instead of the asset", assetPath)
	}

	// Unmatched /api path 404s rather than being masked by a 200 index page.
	if api := get("/api/nope"); api.Code != http.StatusNotFound {
		t.Errorf("GET /api/nope status = %d, want 404", api.Code)
	}
}
