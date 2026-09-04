package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
)

const (
	brandingMetadataCache = "private, no-cache, must-revalidate"
	brandingAssetCache    = "public, max-age=31536000, immutable"
	brandingDevCache      = "no-cache"
)

func (server *Server) brandingMetadata(writer http.ResponseWriter, request *http.Request) {
	if server.branding == nil {
		http.Error(writer, "branding unavailable", http.StatusInternalServerError)
		return
	}
	payload, err := json.Marshal(server.branding.Metadata())
	if err != nil {
		http.Error(writer, "branding unavailable", http.StatusInternalServerError)
		return
	}
	digest := sha256.Sum256(payload)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	writer.Header().Set("Cache-Control", brandingMetadataCache)
	writer.Header().Set("ETag", etag)
	writer.Header().Set("Content-Type", "application/json")
	if etagMatches(request.Header.Get("If-None-Match"), etag) {
		writer.WriteHeader(http.StatusNotModified)
		return
	}
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(append(payload, '\n'))
}

func (server *Server) brandingAsset(writer http.ResponseWriter, request *http.Request) {
	if server.branding == nil {
		http.NotFound(writer, request)
		return
	}
	name := chi.URLParam(request, "asset")
	path, ok := server.branding.Path(name)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(writer, request)
		return
	}
	if server.branding.Immutable() {
		writer.Header().Set("Cache-Control", brandingAssetCache)
	} else {
		writer.Header().Set("Cache-Control", brandingDevCache)
	}
	http.ServeContent(writer, request, name, info.ModTime(), file)
}

func etagMatches(header, expected string) bool {
	for _, candidate := range splitETags(header) {
		if candidate == "*" {
			return true
		}
		if len(candidate) > 2 && candidate[:2] == "W/" {
			candidate = candidate[2:]
		}
		if candidate == expected {
			return true
		}
	}
	return false
}

func splitETags(header string) []string {
	if header == "" {
		return nil
	}
	values := make([]string, 0, 1)
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			values = append(values, candidate)
		}
	}
	return values
}
