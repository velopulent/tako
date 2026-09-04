package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/velopulent/tako/internal/branding"
)

func TestBrandingMetadataIsPublicAndSupportsETag(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()

	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/branding", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("branding returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != brandingMetadataCache {
		t.Fatalf("metadata cache header = %q", recorder.Header().Get("Cache-Control"))
	}
	etag := recorder.Header().Get("ETag")
	if etag == "" {
		t.Fatal("metadata omitted ETag")
	}
	var metadata branding.Metadata
	if err := json.Unmarshal(recorder.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Hostname == "" {
		t.Fatal("metadata omitted hostname")
	}

	conditional := httptest.NewRequest(http.MethodGet, "/api/v1/branding", nil)
	conditional.Header.Set("If-None-Match", etag)
	conditionalRecorder := httptest.NewRecorder()
	server.routes().ServeHTTP(conditionalRecorder, conditional)
	if conditionalRecorder.Code != http.StatusNotModified || conditionalRecorder.Body.Len() != 0 {
		t.Fatalf("conditional branding returned %d with body %q", conditionalRecorder.Code, conditionalRecorder.Body.String())
	}
}

func TestBrandingAssetRouteIsAllowlistedAndCacheable(t *testing.T) {
	directory := t.TempDir()
	assetPath := filepath.Join(directory, "ubuntu.png")
	if err := os.WriteFile(assetPath, []byte("png-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	server.branding = branding.New(directory, "1.2.3", false)

	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/branding/ubuntu.png?v=1.2.3", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "png-data" {
		t.Fatalf("asset returned %d with body %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != brandingAssetCache {
		t.Fatalf("asset cache header = %q", recorder.Header().Get("Cache-Control"))
	}

	for _, path := range []string{"/branding/not-allowed.png", "/branding/%2e%2e%2fubuntu.png"} {
		recorder = httptest.NewRecorder()
		server.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("asset %s returned %d, want 404", path, recorder.Code)
		}
	}
}
