package dashboard

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

const defaultOverlayDir = "/run/tako/dashboard"

//go:embed dist/*
var assets embed.FS

func Files() fs.FS {
	if dir := OverlayDir(); dir != "" {
		return os.DirFS(dir)
	}
	return embedded()
}

func embedded() fs.FS {
	files, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return files
}

// OverlayDir returns a filesystem dashboard root when host-dev overlay is
// active. An empty TAKO_DASHBOARD_DIR disables overlay (used by tests).
func OverlayDir() string {
	if dir, ok := os.LookupEnv("TAKO_DASHBOARD_DIR"); ok {
		if dir == "" {
			return ""
		}
		if overlayReady(dir) {
			return dir
		}
		return ""
	}
	if overlayReady(defaultOverlayDir) {
		return defaultOverlayDir
	}
	return ""
}

func overlayReady(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "index.html"))
	return err == nil && !info.IsDir()
}
