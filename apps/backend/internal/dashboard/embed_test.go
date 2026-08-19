package dashboard

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverlayDirUsesEnvironment(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("overlay-index"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TAKO_DASHBOARD_DIR", dir)
	if OverlayDir() != dir {
		t.Fatalf("OverlayDir()=%q want %q", OverlayDir(), dir)
	}
	file, err := Files().Open("index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "overlay-index" {
		t.Fatalf("overlay content=%q", body)
	}
}

func TestEmptyDashboardDirDisablesOverlay(t *testing.T) {
	t.Setenv("TAKO_DASHBOARD_DIR", "")
	if OverlayDir() != "" {
		t.Fatalf("overlay still active: %q", OverlayDir())
	}
	file, err := Files().Open("index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Tako") {
		t.Fatalf("embed fallback missing: %q", body)
	}
}
