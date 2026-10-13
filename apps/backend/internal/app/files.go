package app

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

const (
	previewGrantTTL   = 15 * time.Minute
	maxPreviewGrants  = 4096
	maxFileSearchHits = 1000
)

type previewGrant struct {
	sessionID   string
	path        string
	fingerprint string
	expires     time.Time
}

// previewGrants are short-lived capabilities for media previews. An <img> or
// <video> element cannot send the CSRF header, so the listing issues an opaque
// token bound to the session, path, and file fingerprint.
type previewGrants struct {
	mu     sync.Mutex
	grants map[string]previewGrant
}

func newPreviewGrants() *previewGrants {
	return &previewGrants{grants: make(map[string]previewGrant)}
}

func (store *previewGrants) issue(sessionID string, directory *platform.FileDirectory) {
	if sessionID == "" || directory == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	now := time.Now()
	for token, grant := range store.grants {
		if !now.Before(grant.expires) {
			delete(store.grants, token)
		}
	}
	for index := range directory.Entries {
		entry := &directory.Entries[index]
		if entry.Kind != "file" || entry.Fingerprint == "" || len(store.grants) >= maxPreviewGrants {
			continue
		}
		var random [24]byte
		if _, err := rand.Read(random[:]); err != nil {
			return
		}
		token := base64.RawURLEncoding.EncodeToString(random[:])
		store.grants[token] = previewGrant{sessionID: sessionID, path: entry.Path, fingerprint: entry.Fingerprint, expires: now.Add(previewGrantTTL)}
		entry.PreviewToken = token
	}
}

func (store *previewGrants) fingerprint(sessionID, token, path string) (string, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	grant, ok := store.grants[token]
	if !ok || grant.sessionID != sessionID || grant.path != path || !time.Now().Before(grant.expires) {
		return "", false
	}
	return grant.fingerprint, true
}

// readOnlyFileActions are excluded from the audit log.
var readOnlyFileActions = map[string]bool{"list": true, "stat": true, "read": true, "search": true}

// fileCall runs one file operation in the scope the request names: "system"
// uses the administrative lane rooted at /, otherwise the user's own bridge
// rooted at their home directory.
func (server *Server) fileCall(writer http.ResponseWriter, request *http.Request, operation platform.FileOperation) (platform.FileResult, bool) {
	current := currentSession(request)
	if operation.Scope == "" {
		operation.Scope = request.URL.Query().Get("scope")
	}
	var credentials auth.Credentials
	switch operation.Scope {
	case "system":
		if !requireAdmin(writer, current) {
			return platform.FileResult{}, false
		}
		credentials = adminCredentials(current)
	case "", "home":
		if current.Identity.BridgeToken == "" {
			reject(writer, "user-bridge-unavailable")
			return platform.FileResult{}, false
		}
		credentials = userCredentials(current)
	default:
		reject(writer, "invalid-file-operation")
		return platform.FileResult{}, false
	}
	var result platform.FileResult
	err := server.broker.Call(request.Context(), "files.apply", credentials, operation, &result)
	if !readOnlyFileActions[operation.Action] {
		server.audit(request, "file/"+operation.Action, err, operation.Scope == "system")
	}
	if err != nil {
		fail(writer, err, "file-operation-failed")
		return platform.FileResult{}, false
	}
	if operation.Scope == "system" {
		absoluteFilePaths(&result)
	}
	return result, true
}

// absoluteFilePaths turns root-relative paths from the system lane into
// absolute paths for display.
func absoluteFilePaths(result *platform.FileResult) {
	absolute := func(path string) string {
		if path == "." {
			return "/"
		}
		return "/" + strings.TrimPrefix(path, "/")
	}
	if result.Entry != nil {
		result.Entry.Path = absolute(result.Entry.Path)
	}
	if result.Directory != nil {
		result.Directory.Path = absolute(result.Directory.Path)
		result.Directory.Parent = absolute(result.Directory.Parent)
		for index := range result.Directory.Entries {
			result.Directory.Entries[index].Path = absolute(result.Directory.Entries[index].Path)
		}
	}
	if result.Search != nil {
		for index := range result.Search.Entries {
			result.Search.Entries[index].Path = absolute(result.Search.Entries[index].Path)
		}
	}
}

func queryInt(request *http.Request, name string, fallback int64) (int64, bool) {
	raw := request.URL.Query().Get(name)
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil && value >= 0
}

func (server *Server) filesList(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	operation := platform.FileOperation{Action: "list", Path: query.Get("path"), ShowHidden: query.Get("hidden") == "true", ExpectedFingerprint: query.Get("fingerprint")}
	if operation.Path == "" {
		operation.Path = "."
	}
	var offsetOK, limitOK bool
	operation.Offset, offsetOK = queryInt(request, "offset", 0)
	operation.Limit, limitOK = queryInt(request, "limit", 0)
	if !offsetOK || !limitOK {
		problem(writer, http.StatusBadRequest, "invalid-file-range", "Directory page is invalid")
		return
	}
	result, ok := server.fileCall(writer, request, operation)
	if !ok {
		return
	}
	server.previews.issue(currentSession(request).ID, result.Directory)
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) filesSearch(writer http.ResponseWriter, request *http.Request) {
	path := request.URL.Query().Get("path")
	if path == "" {
		path = "."
	}
	maxEntries, ok := queryInt(request, "maxEntries", maxFileSearchHits)
	if !ok || maxEntries < 1 || maxEntries > maxFileSearchHits {
		problem(writer, http.StatusBadRequest, "invalid-file-search", "Search limit is invalid")
		return
	}
	result, ok := server.fileCall(writer, request, platform.FileOperation{Action: "search", Path: path, Query: request.URL.Query().Get("query"), MaxEntries: int(maxEntries)})
	if ok {
		writeJSON(writer, http.StatusOK, result)
	}
}

func (server *Server) fileOperation(writer http.ResponseWriter, request *http.Request) {
	operation, ok := decodeBody[platform.FileOperation](writer, request, 8<<20, "invalid-file-operation")
	if !ok {
		return
	}
	if result, ok := server.fileCall(writer, request, operation); ok {
		writeJSON(writer, http.StatusOK, result)
	}
}

func (server *Server) fileUpload(writer http.ResponseWriter, request *http.Request) {
	path := request.URL.Query().Get("path")
	if path == "" || len(path) > platform.MaxFilePath {
		reject(writer, "invalid-file-operation")
		return
	}
	offset, offsetOK := queryInt(request, "offset", 0)
	total, totalOK := queryInt(request, "total", 0)
	if !offsetOK || !totalOK || total <= 0 || total > 1<<40 || offset > total {
		problem(writer, http.StatusBadRequest, "invalid-file-range", "The resumable upload range is invalid")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, int64(platform.MaxFileChunk)+1)
	content, err := io.ReadAll(request.Body)
	if err != nil || len(content) > platform.MaxFileChunk {
		problem(writer, http.StatusRequestEntityTooLarge, "file-chunk-too-large", "Upload chunks are limited to 4 MiB")
		return
	}
	if len(content) == 0 || int64(len(content)) > total-offset {
		problem(writer, http.StatusBadRequest, "invalid-file-range", "The chunk exceeds the declared upload size")
		return
	}
	operation := platform.FileOperation{Action: "write-chunk", Path: path, Offset: offset, TotalSize: total, Content: content, ContentSHA256: request.Header.Get("X-Content-SHA256"), UploadID: request.URL.Query().Get("uploadId"), ExpectedFingerprint: request.Header.Get("X-File-Fingerprint")}
	if result, ok := server.fileCall(writer, request, operation); ok {
		writer.Header().Set("Upload-Offset", strconv.FormatInt(result.Offset, 10))
		writeJSON(writer, http.StatusOK, result)
	}
}

// fileContent streams a file for preview or download. Every response is
// sandboxed: only plain media types render inline, and nothing a host file
// contains can run script in the dashboard origin.
func (server *Server) fileContent(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	path := request.URL.Query().Get("path")
	if path == "" {
		reject(writer, "invalid-file-operation")
		return
	}
	expected := ""
	if token := request.URL.Query().Get("token"); token != "" {
		fingerprint, ok := server.previews.fingerprint(current.ID, token, path)
		if !ok {
			reject(writer, "file-preview-expired")
			return
		}
		expected = fingerprint
	}
	offset, offsetOK := queryInt(request, "offset", 0)
	limit, limitOK := queryInt(request, "limit", platform.MaxFileChunk)
	if !offsetOK || !limitOK || limit < 1 || limit > platform.MaxFileChunk {
		problem(writer, http.StatusBadRequest, "invalid-file-range", "The file range is invalid")
		return
	}
	status := http.StatusOK
	if rangeValue := request.Header.Get("Range"); rangeValue != "" {
		stat, ok := server.fileCall(writer, request, platform.FileOperation{Action: "stat", Path: path, ExpectedFingerprint: expected})
		if !ok || stat.Entry == nil {
			return
		}
		ifRange := strings.Trim(request.Header.Get("If-Range"), `"`)
		if ifRange != "" && ifRange != stat.Entry.Fingerprint {
			// A stale validator turns a range request into a bounded full read.
			offset, limit = 0, platform.MaxFileChunk
		} else {
			start, end, valid := parseFileRange(rangeValue, stat.Entry.Size)
			if !valid {
				writer.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", stat.Entry.Size))
				problem(writer, http.StatusRequestedRangeNotSatisfiable, "invalid-file-range", "The requested range is outside the file")
				return
			}
			offset, limit, status = start, end-start+1, http.StatusPartialContent
		}
	}
	result, ok := server.fileCall(writer, request, platform.FileOperation{Action: "read", Path: path, Offset: offset, Limit: limit, ExpectedFingerprint: expected})
	if !ok {
		return
	}
	if result.Total > 0 && offset >= result.Total {
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", result.Total))
		problem(writer, http.StatusRequestedRangeNotSatisfiable, "invalid-file-range", "The requested range is outside the file")
		return
	}
	header := writer.Header()
	contentType := result.Mime
	if contentType == "" {
		contentType = http.DetectContentType(result.Content)
	}
	header.Set("Content-Type", contentType)
	header.Set("Accept-Ranges", "bytes")
	header.Set("Content-Disposition", fileContentDisposition(contentType, path))
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	if mediaType(contentType) == "application/pdf" {
		// Browser PDF viewers refuse sandboxed documents. A PDF cannot run
		// script in the page origin, so only framing is relaxed for previews.
		header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'")
		header.Set("X-Frame-Options", "SAMEORIGIN")
	} else {
		header.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	}
	if result.Fingerprint != "" {
		etag := `"` + result.Fingerprint + `"`
		header.Set("ETag", etag)
		header.Set("X-File-Fingerprint", result.Fingerprint)
		if status == http.StatusOK && request.Header.Get("If-None-Match") == etag {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
	}
	if status == http.StatusPartialContent {
		end := result.Offset + max(int64(len(result.Content))-1, 0)
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", result.Offset, end, result.Total))
	}
	fullDownload := status == http.StatusOK && request.URL.Query().Get("offset") == "" && request.URL.Query().Get("limit") == ""
	length := int64(len(result.Content))
	if fullDownload {
		length = result.Total
	}
	header.Set("Content-Length", strconv.FormatInt(length, 10))
	writer.WriteHeader(status)
	if _, err := writer.Write(result.Content); err != nil || !fullDownload {
		return
	}
	// Larger downloads continue chunk by chunk, pinned to the first chunk's
	// fingerprint so a concurrent change cannot splice two versions together.
	credentials := userCredentials(current)
	if request.URL.Query().Get("scope") == "system" {
		credentials = adminCredentials(current)
	}
	for next := int64(len(result.Content)); next < result.Total; {
		var chunk platform.FileResult
		operation := platform.FileOperation{Action: "read", Path: path, Offset: next, Limit: platform.MaxFileChunk, ExpectedFingerprint: result.Fingerprint}
		if err := server.broker.Call(request.Context(), "files.apply", credentials, operation, &chunk); err != nil || len(chunk.Content) == 0 {
			return
		}
		if _, err := writer.Write(chunk.Content); err != nil {
			return
		}
		next += int64(len(chunk.Content))
	}
}

// inlineFileTypes may render in place. Everything else, including HTML, SVG,
// XML, and scripts, downloads as an attachment.
var inlineFileTypes = map[string]bool{"text/plain": true, "text/csv": true, "text/markdown": true, "application/json": true, "application/pdf": true}

func mediaType(contentType string) string {
	parsed, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return parsed
}

func fileContentDisposition(contentType, path string) string {
	kind := mediaType(contentType)
	disposition := "attachment"
	if inlineFileTypes[kind] || (kind != "image/svg+xml" && (strings.HasPrefix(kind, "image/") || strings.HasPrefix(kind, "video/") || strings.HasPrefix(kind, "audio/"))) {
		disposition = "inline"
	}
	name := filepath.Base(path)
	if name == "" || name == "." || name == "/" {
		name = "download"
	}
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return mime.FormatMediaType(disposition, map[string]string{"filename": fallback}) + "; filename*=UTF-8''" + url.PathEscape(name)
}

func parseFileRange(value string, total int64) (int64, int64, bool) {
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") || total <= 0 {
		return 0, 0, false
	}
	start, end, found := strings.Cut(strings.TrimPrefix(value, "bytes="), "-")
	if !found {
		return 0, 0, false
	}
	if start == "" {
		suffix, err := strconv.ParseInt(end, 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, false
		}
		suffix = min(suffix, platform.MaxFileChunk, total)
		return total - suffix, total - 1, true
	}
	first, err := strconv.ParseInt(start, 10, 64)
	if err != nil || first < 0 || first >= total {
		return 0, 0, false
	}
	last := total - 1
	if end != "" {
		last, err = strconv.ParseInt(end, 10, 64)
		if err != nil || last < first {
			return 0, 0, false
		}
		last = min(last, total-1)
	}
	if last-first+1 > platform.MaxFileChunk {
		return 0, 0, false
	}
	return first, last, true
}
