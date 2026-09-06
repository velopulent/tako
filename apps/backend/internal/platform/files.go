package platform

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	MaxFileEntries       = 4096
	MaxFilePath          = 4096
	MaxFileChunk         = 4 << 20
	MaxTextFile          = 5 << 20
	MaxSearchEntries     = 10000
	MaxArchiveEntries    = 10000
	MaxArchiveBytes      = 256 << 20
	MaxArchiveDepth      = 32
	MaxArchiveEntryBytes = 64 << 20
)

var (
	ErrInvalidFileOperation = errors.New("invalid file operation")
	ErrFileNotFound         = errors.New("file not found")
	ErrFilePermission       = errors.New("file permission denied")
	ErrFileConflict         = errors.New("file changed since preview")
	ErrFileTooLarge         = errors.New("file exceeds configured limit")
	ErrUnsafeArchive        = errors.New("archive entry is unsafe")
	ErrArchiveLimit         = errors.New("archive exceeds configured limit")
)

type FileEntry struct {
	Name             string    `json:"name"`
	Path             string    `json:"path"`
	Kind             string    `json:"kind"`
	Size             int64     `json:"size"`
	Mode             uint32    `json:"mode"`
	ModifiedAt       time.Time `json:"modifiedAt"`
	Fingerprint      string    `json:"fingerprint"`
	Hidden           bool      `json:"hidden"`
	Readable         bool      `json:"readable"`
	Writable         bool      `json:"writable"`
	PermissionDenied bool      `json:"permissionDenied,omitempty"`
	SymlinkTarget    string    `json:"symlinkTarget,omitempty"`
	Mime             string    `json:"mime,omitempty"`
	Reason           string    `json:"reason,omitempty"`
	PreviewToken     string    `json:"previewToken,omitempty"`
}

type FileDirectory struct {
	Parent      string      `json:"parent"`
	NextOffset  int64       `json:"nextOffset,omitempty"`
	HasMore     bool        `json:"hasMore"`
	Path        string      `json:"path"`
	Entries     []FileEntry `json:"entries"`
	ShowHidden  bool        `json:"showHidden"`
	Fingerprint string      `json:"fingerprint"`
}

type FileSearchResult struct {
	Root    string      `json:"root"`
	Query   string      `json:"query"`
	Entries []FileEntry `json:"entries"`
	Limited bool        `json:"limited"`
}

type FileResult struct {
	Directory   *FileDirectory    `json:"directory,omitempty"`
	Entry       *FileEntry        `json:"entry,omitempty"`
	Entries     []FileEntry       `json:"entries,omitempty"`
	Search      *FileSearchResult `json:"search,omitempty"`
	Content     []byte            `json:"content,omitempty"`
	Offset      int64             `json:"offset,omitempty"`
	Total       int64             `json:"total,omitempty"`
	EOF         bool              `json:"eof,omitempty"`
	Mime        string            `json:"mime,omitempty"`
	Warnings    []string          `json:"warnings,omitempty"`
	Message     string            `json:"message,omitempty"`
	Fingerprint string            `json:"fingerprint,omitempty"`
	UploadID    string            `json:"uploadId,omitempty"`
	Uploads     []UploadInfo      `json:"uploads,omitempty"`
}

type UploadInfo struct {
	UploadID  string    `json:"uploadId"`
	Path      string    `json:"path"`
	Offset    int64     `json:"offset"`
	Total     int64     `json:"total"`
	ExpiresAt time.Time `json:"expiresAt"`
	Completed bool      `json:"completed,omitempty"`
}

// FileOperation is the narrow wire contract shared by the gateway, sessiond,
// and user bridge. User-scoped paths are relative to the authenticated home;
// privileged paths may be absolute and are still checked against protected
// system locations and operation-specific limits.
type FileOperation struct {
	Action              string  `json:"action"`
	Scope               string  `json:"scope,omitempty"`
	UploadID            string  `json:"uploadId,omitempty"`
	Path                string  `json:"path,omitempty"`
	Destination         string  `json:"destination,omitempty"`
	Kind                string  `json:"kind,omitempty"`
	Content             []byte  `json:"content,omitempty"`
	ContentSHA256       string  `json:"contentSha256,omitempty"`
	Offset              int64   `json:"offset,omitempty"`
	Limit               int64   `json:"limit,omitempty"`
	TotalSize           int64   `json:"totalSize,omitempty"`
	LineOffset          int     `json:"lineOffset,omitempty"`
	LineLimit           int     `json:"lineLimit,omitempty"`
	ExpectedFingerprint string  `json:"expectedFingerprint,omitempty"`
	ShowHidden          bool    `json:"showHidden,omitempty"`
	Recursive           bool    `json:"recursive,omitempty"`
	Permanent           bool    `json:"permanent,omitempty"`
	Overwrite           bool    `json:"overwrite,omitempty"`
	Confirmation        string  `json:"confirmation,omitempty"`
	Query               string  `json:"query,omitempty"`
	MaxEntries          int     `json:"maxEntries,omitempty"`
	ArchivePath         string  `json:"archivePath,omitempty"`
	Mode                *uint32 `json:"mode,omitempty"`
	Owner               string  `json:"owner,omitempty"`
	Group               string  `json:"group,omitempty"`
}

func ValidateFileOperation(operation FileOperation) error {
	validActions := map[string]bool{"list": true, "stat": true, "read": true, "read-window": true, "write": true, "write-text": true, "write-chunk": true, "upload-status": true, "create": true, "rename": true, "move": true, "copy": true, "trash": true, "delete": true, "search": true, "archive": true, "extract": true, "metadata": true, "cancel-upload": true, "restore": true}
	if !validActions[operation.Action] || len(operation.Path) > MaxFilePath || len(operation.Destination) > MaxFilePath || len(operation.ArchivePath) > MaxFilePath || len(operation.Owner) > 256 || len(operation.Group) > 256 || strings.ContainsAny(operation.Path+operation.Destination+operation.ArchivePath+operation.Owner+operation.Group, "\x00\r\n") {
		return ErrInvalidFileOperation
	}
	if operation.Path == "" && operation.Action != "archive" {
		return ErrInvalidFileOperation
	}
	contentLimit := MaxFileChunk
	if operation.Action == "write-text" {
		contentLimit = MaxTextFile
	}
	if operation.Scope != "" && operation.Scope != "home" && operation.Scope != "system" {
		return ErrInvalidFileOperation
	}
	if operation.UploadID != "" && (len(operation.UploadID) != 32 || !isHex(operation.UploadID)) {
		return ErrInvalidFileOperation
	}
	if operation.Action == "write-chunk" && (operation.TotalSize <= 0 || operation.Offset > operation.TotalSize || int64(len(operation.Content)) > operation.TotalSize-operation.Offset || len(operation.Content) == 0 || operation.ContentSHA256 == "") {
		return ErrInvalidFileOperation
	}
	if operation.Action == "list" && operation.Limit > MaxFileEntries {
		return ErrInvalidFileOperation
	}
	if operation.Offset < 0 || operation.Limit < 0 || operation.Limit > MaxFileChunk || len(operation.Content) > contentLimit {
		return ErrInvalidFileOperation
	}
	if operation.TotalSize < 0 || operation.TotalSize > 1<<40 {
		return ErrInvalidFileOperation
	}
	if operation.Action == "read-window" && (operation.LineOffset < 0 || operation.LineLimit < 1 || operation.LineLimit > 1000) {
		return ErrInvalidFileOperation
	}
	if operation.MaxEntries < 0 || operation.MaxEntries > MaxSearchEntries {
		return ErrInvalidFileOperation
	}
	if operation.ExpectedFingerprint != "" && (len(operation.ExpectedFingerprint) != sha256.Size*2 || !isHex(operation.ExpectedFingerprint)) {
		return ErrInvalidFileOperation
	}
	if operation.Mode != nil && *operation.Mode > 0o7777 {
		return ErrInvalidFileOperation
	}
	if operation.ContentSHA256 != "" && (len(operation.ContentSHA256) != sha256.Size*2 || !isHex(operation.ContentSHA256)) {
		return ErrInvalidFileOperation
	}
	if len(operation.Confirmation) > 128 || strings.ContainsAny(operation.Confirmation, "\x00\r\n") {
		return ErrInvalidFileOperation
	}
	if operation.Overwrite {
		if operation.Action != "rename" && operation.Action != "move" && operation.Action != "copy" {
			return ErrInvalidFileOperation
		}
		if operation.Confirmation != "CONFIRM FILE OVERWRITE" {
			return ErrInvalidFileOperation
		}
	}
	if operation.Permanent || operation.Recursive || operation.Action == "metadata" {
		if operation.Confirmation != "CONFIRM FILE OPERATION" {
			return ErrInvalidFileOperation
		}
	}
	if operation.Action == "archive" || operation.Action == "extract" {
		if operation.Confirmation != "CONFIRM FILE OPERATION" {
			return ErrInvalidFileOperation
		}
	}
	if (operation.Action == "write" || operation.Action == "write-text") && operation.Offset != 0 {
		return ErrInvalidFileOperation
	}
	if operation.Action == "rename" || operation.Action == "move" || operation.Action == "copy" {
		if operation.Destination == "" {
			return ErrInvalidFileOperation
		}
	}
	if operation.Action == "search" && operation.Query == "" {
		return ErrInvalidFileOperation
	}
	if operation.Action == "archive" && operation.ArchivePath == "" {
		return ErrInvalidFileOperation
	}
	if operation.Action == "extract" && operation.ArchivePath == "" {
		return ErrInvalidFileOperation
	}
	return nil
}

func isHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func ApplyUserFileOperation(ctx context.Context, operation FileOperation) (FileResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return FileResult{}, err
	}
	return applyFileOperation(ctx, operation, home, false)
}

func ApplyPrivilegedFileOperation(ctx context.Context, operation FileOperation) (FileResult, error) {
	return applyFileOperation(ctx, operation, "/", true)
}

func applyFileOperation(ctx context.Context, operation FileOperation, root string, privileged bool) (FileResult, error) {
	if err := ValidateFileOperation(operation); err != nil {
		return FileResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return FileResult{}, err
	}
	tree, err := openFileRoot(root)
	if err != nil {
		return FileResult{}, err
	}
	defer tree.Close()
	f := fileTree{root: tree}
	resolve := func(value string) (string, error) {
		if value == "" {
			value = "."
		}
		if filepath.IsAbs(value) {
			if !privileged {
				return "", ErrFilePermission
			}
			value = strings.TrimPrefix(filepath.Clean(value), "/")
			if value == "" {
				value = "."
			}
		}
		value = filepath.Clean(value)
		if !filepath.IsLocal(value) {
			return "", ErrFilePermission
		}
		if privileged && (value == "proc" || strings.HasPrefix(value, "proc/") || value == "sys" || strings.HasPrefix(value, "sys/") || value == "dev" || strings.HasPrefix(value, "dev/")) {
			return "", ErrFilePermission
		}
		return value, nil
	}
	path, err := resolve(operation.Path)
	if err != nil {
		return FileResult{}, err
	}
	if path == "." && operation.Action != "list" && operation.Action != "stat" && operation.Action != "search" && operation.Action != "archive" {
		return FileResult{}, ErrFilePermission
	}

	if operation.ExpectedFingerprint != "" && (operation.Action == "read" || operation.Action == "read-window" || operation.Action == "stat") {
		info, statErr := f.root.Stat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			return FileResult{}, ErrFileNotFound
		}
		if statErr != nil {
			return FileResult{}, statErr
		}
		if statFingerprint(info) != operation.ExpectedFingerprint {
			return FileResult{}, ErrFileConflict
		}
	}
	switch operation.Action {
	case "list":
		return f.listDirectory(path, operation)
	case "stat":
		entry, err := f.entry(path, operation.Path)
		return FileResult{Entry: &entry}, err
	case "read":
		return f.readFile(path, operation.Offset, operation.Limit, operation.ExpectedFingerprint)
	case "read-window":
		return f.readTextWindow(path, operation.LineOffset, operation.LineLimit)
	case "write":
		return f.writeFile(path, operation)
	case "write-text":
		if len(operation.Content) > MaxTextFile {
			return FileResult{}, ErrFileTooLarge
		}
		return f.writeFile(path, operation)
	case "write-chunk":
		return f.writeChunk(path, operation)
	case "upload-status":
		return f.uploadStatus(path, operation)
	case "cancel-upload":
		return f.cancelUpload(path, operation)
	case "create":
		return f.createFile(path, operation)
	case "rename", "move", "copy":
		destination, resolveErr := resolve(operation.Destination)
		if resolveErr != nil {
			return FileResult{}, resolveErr
		}
		return f.transferFile(operation.Action, path, destination, operation)
	case "trash":
		trashRoot := "."
		if privileged {
			trashRoot = "root"
		}
		return f.trashFile(path, trashRoot, operation)
	case "restore":
		return f.restoreFile(path, operation, privileged)
	case "delete":
		return f.deleteFile(path, operation)
	case "search":
		return f.searchFiles(ctx, path, operation)
	case "archive":
		archive, resolveErr := resolve(operation.ArchivePath)
		if resolveErr != nil {
			return FileResult{}, resolveErr
		}
		return f.createArchive(ctx, path, archive, operation)
	case "extract":
		archive, resolveErr := resolve(operation.ArchivePath)
		if resolveErr != nil {
			return FileResult{}, resolveErr
		}
		return f.extractArchive(ctx, archive, path, operation)
	case "metadata":
		return f.updateMetadata(path, operation, privileged)
	default:
		return FileResult{}, ErrInvalidFileOperation
	}
}

func resolveFilePath(root, value string, privileged bool) (string, error) {
	if value == "" {
		value = "."
	}
	if !privileged && filepath.IsAbs(value) {
		return "", ErrFilePermission
	}
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	if !privileged {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", ErrFilePermission
		}
		check := path
		if _, statErr := os.Lstat(check); errors.Is(statErr, os.ErrNotExist) {
			check = filepath.Dir(check)
		}
		realPath, evalErr := filepath.EvalSymlinks(check)
		if evalErr != nil {
			if !errors.Is(evalErr, os.ErrNotExist) {
				return "", ErrFilePermission
			}
		} else {
			realRoot, rootErr := filepath.EvalSymlinks(root)
			if rootErr != nil || !pathWithin(realRoot, realPath) {
				return "", ErrFilePermission
			}
		}
	}
	return path, nil
}

func (f fileTree) listDirectory(path string, operation FileOperation) (FileResult, error) {
	directory, err := f.root.Open(path)
	if err != nil {
		return FileResult{}, err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return FileResult{}, err
	}
	if operation.ExpectedFingerprint != "" && statFingerprint(info) != operation.ExpectedFingerprint {
		return FileResult{}, ErrFileConflict
	}
	limit := int(operation.Limit)
	if limit == 0 {
		limit = 200
	}
	result := FileDirectory{Path: path, Parent: filepath.Dir(path), Entries: []FileEntry{}, ShowHidden: operation.ShowHidden, Fingerprint: statFingerprint(info)}
	var offset int64
	for offset < operation.Offset {
		n := min(int64(256), operation.Offset-offset)
		entries, err := directory.ReadDir(int(n))
		offset += int64(len(entries))
		if err != nil {
			if errors.Is(err, io.EOF) {
				return FileResult{Directory: &result}, nil
			}
			return FileResult{}, err
		}
	}
	for scanned := 0; scanned < 100000; scanned++ {
		entries, readErr := directory.ReadDir(1)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return FileResult{}, readErr
		}
		item := entries[0]
		offset++
		if scanned == 99999 {
			result.HasMore = true
			result.NextOffset = offset
		}
		if !operation.ShowHidden && strings.HasPrefix(item.Name(), ".") {
			continue
		}
		if len(result.Entries) == limit {
			result.HasMore = true
			result.NextOffset = offset - 1
			break
		}
		entry, entryErr := f.entry(filepath.Join(path, item.Name()), filepath.Join(path, item.Name()))
		if entryErr == nil {
			result.Entries = append(result.Entries, entry)
		}
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i], result.Entries[j]
		if (a.Kind == "directory") != (b.Kind == "directory") {
			return a.Kind == "directory"
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return FileResult{Directory: &result}, nil
}

func fileEntry(path, displayPath string) (FileEntry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return FileEntry{}, err
	}
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	} else if info.Mode()&os.ModeSymlink != 0 {
		kind = "symlink"
	}
	entry := FileEntry{Name: filepath.Base(path), Path: displayPath, Kind: kind, Size: info.Size(), Mode: uint32(info.Mode().Perm()), ModifiedAt: info.ModTime().UTC(), Fingerprint: statFingerprint(info), Hidden: strings.HasPrefix(filepath.Base(path), "."), Readable: info.Mode().Perm()&0o444 != 0, Writable: info.Mode().Perm()&0o222 != 0}
	if kind == "file" {
		entry.Mime = mime.TypeByExtension(filepath.Ext(path))
		if entry.Mime == "" {
			entry.Mime = "application/octet-stream"
		}
	}
	if kind == "symlink" {
		if target, readErr := os.Readlink(path); readErr == nil && len(target) <= MaxFilePath {
			entry.SymlinkTarget = target
		}
	}
	return entry, nil
}

func (f fileTree) entry(path, displayPath string) (FileEntry, error) {
	info, err := f.root.Lstat(path)
	if err != nil {
		return FileEntry{}, err
	}
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	} else if info.Mode()&os.ModeSymlink != 0 {
		kind = "symlink"
	}
	entry := FileEntry{Name: filepath.Base(path), Path: displayPath, Kind: kind, Size: info.Size(), Mode: uint32(info.Mode().Perm()), ModifiedAt: info.ModTime().UTC(), Fingerprint: statFingerprint(info), Hidden: strings.HasPrefix(filepath.Base(path), "."), Readable: info.Mode().Perm()&0o444 != 0, Writable: info.Mode().Perm()&0o222 != 0}
	if kind == "file" {
		entry.Mime = mime.TypeByExtension(filepath.Ext(path))
		if entry.Mime == "" {
			entry.Mime = "application/octet-stream"
		}
	}
	if kind == "symlink" {
		if target, readErr := f.root.Readlink(path); readErr == nil && len(target) <= MaxFilePath {
			entry.SymlinkTarget = target
		}
	}
	return entry, nil
}

func statFingerprint(info os.FileInfo) string {
	if info == nil {
		return ""
	}
	value := fmt.Sprintf("%s:%d:%d:%o", info.Mode().String(), info.Size(), info.ModTime().UnixNano(), info.Mode().Perm())
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		value += fmt.Sprintf(":%d:%d:%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Uid, stat.Gid, stat.Ctim.Sec, stat.Ctim.Nsec)
	}
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func (f fileTree) readFile(path string, offset, limit int64, expected string) (FileResult, error) {
	info, err := f.root.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return FileResult{}, ErrFileNotFound
	}
	if err != nil {
		return FileResult{}, err
	}
	if !info.Mode().IsRegular() {
		return FileResult{}, ErrFileTooLarge
	}
	if limit == 0 {
		limit = MaxFileChunk
	}
	file, err := f.root.Open(path)
	if err != nil {
		return FileResult{}, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return FileResult{}, err
	}
	if !info.Mode().IsRegular() {
		return FileResult{}, ErrFilePermission
	}
	if expected != "" && statFingerprint(info) != expected {
		return FileResult{}, ErrFileConflict
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return FileResult{}, err
	}
	content, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return FileResult{}, err
	}
	return FileResult{Content: content, Offset: offset, Total: info.Size(), EOF: offset+int64(len(content)) >= info.Size(), Mime: mime.TypeByExtension(filepath.Ext(path)), Fingerprint: statFingerprint(info)}, nil
}

func (f fileTree) readText(path string, offset, limit int64) (FileResult, error) {
	info, err := f.root.Stat(path)
	if err != nil {
		return FileResult{}, err
	}
	if info.Size() > MaxTextFile {
		return FileResult{}, ErrFileTooLarge
	}
	result, err := f.readFile(path, offset, limit, "")
	if err == nil {
		result.Mime = "text/plain"
	}
	return result, err
}

func (f fileTree) writeFile(path string, operation FileOperation) (FileResult, error) {
	if operation.ContentSHA256 != "" {
		digest := sha256.Sum256(operation.Content)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), operation.ContentSHA256) {
			return FileResult{}, ErrFileConflict
		}
	}
	info, err := f.root.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return FileResult{}, err
	}
	if info != nil && operation.ExpectedFingerprint != "" && statFingerprint(info) != operation.ExpectedFingerprint {
		return FileResult{}, ErrFileConflict
	}
	if operation.Offset > 0 {
		file, openErr := f.root.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
		if openErr != nil {
			return FileResult{}, openErr
		}
		defer file.Close()
		if _, openErr = file.Seek(operation.Offset, io.SeekStart); openErr == nil {
			_, openErr = file.Write(operation.Content)
		}
		if openErr != nil {
			return FileResult{}, openErr
		}
		_ = file.Sync()
	} else {
		temporary, createErr := f.temporary(filepath.Dir(path))
		if createErr != nil {
			return FileResult{}, createErr
		}
		temporaryName := filepath.Join(filepath.Dir(path), filepath.Base(temporary.Name()))
		defer f.root.Remove(temporaryName)
		if _, createErr = temporary.Write(operation.Content); createErr == nil {
			createErr = temporary.Sync()
		}
		if closeErr := temporary.Close(); createErr == nil {
			createErr = closeErr
		}
		if createErr == nil {
			if current, e := f.root.Stat(path); operation.ExpectedFingerprint != "" && (e != nil || statFingerprint(current) != operation.ExpectedFingerprint) {
				return FileResult{}, ErrFileConflict
			}
			if info != nil {
				if modeErr := f.preserveMetadata(temporaryName, info); modeErr != nil {
					return FileResult{}, modeErr
				}
			}
			createErr = f.root.Rename(temporaryName, path)
		}
		if createErr != nil {
			return FileResult{}, createErr
		}
	}
	entry, err := f.entry(path, path)
	return FileResult{Entry: &entry, Fingerprint: entry.Fingerprint}, err
}

func (f fileTree) createFile(path string, operation FileOperation) (FileResult, error) {
	if operation.Kind == "directory" {
		if err := f.root.Mkdir(path, 0o755); err != nil {
			return FileResult{}, err
		}
	} else {
		file, err := f.root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return FileResult{}, err
		}
		_ = file.Close()
	}
	entry, err := f.entry(path, path)
	return FileResult{Entry: &entry}, err
}

func (f fileTree) transferFile(action, source, destination string, operation FileOperation) (FileResult, error) {
	info, err := f.root.Lstat(source)
	if err != nil {
		return FileResult{}, err
	}
	if operation.ExpectedFingerprint != "" && statFingerprint(info) != operation.ExpectedFingerprint {
		return FileResult{}, ErrFileConflict
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return FileResult{}, errors.New("symlink transfers require explicit link handling")
	}
	if info.IsDir() && filePathWithin(source, destination) {
		return FileResult{}, ErrInvalidFileOperation
	}
	if operation.Overwrite {
		if info.IsDir() || !info.Mode().IsRegular() {
			return FileResult{}, ErrInvalidFileOperation
		}
		destinationInfo, destinationErr := f.root.Lstat(destination)
		if destinationErr == nil {
			if destinationInfo.Mode()&os.ModeSymlink != 0 || !destinationInfo.Mode().IsRegular() {
				return FileResult{}, ErrInvalidFileOperation
			}
		} else if !errors.Is(destinationErr, os.ErrNotExist) {
			return FileResult{}, destinationErr
		}
	}
	if action == "copy" {
		if info.IsDir() {
			if !operation.Recursive {
				return FileResult{}, errors.New("directory copy requires recursive confirmation")
			}
			if err := f.copyDirectory(source, destination); err != nil {
				return FileResult{}, err
			}
		} else if err := f.copyRegularFile(source, destination, info, nil, operation.Overwrite); err != nil {
			return FileResult{}, err
		}
	} else {
		if err := f.root.rename(source, destination, !operation.Overwrite); err != nil {
			if errors.Is(err, syscall.EXDEV) && action == "move" {
				if _, copyErr := f.transferFile("copy", source, destination, operation); copyErr != nil {
					return FileResult{}, copyErr
				}
				removeErr := f.root.Remove(source)
				if info.IsDir() {
					removeErr = f.root.RemoveAll(source)
				}
				if removeErr != nil {
					return FileResult{}, removeErr
				}
			} else {
				return FileResult{}, err
			}
		}
	}
	entry, err := f.entry(destination, destination)
	return FileResult{Entry: &entry}, err
}

type fileCopyBudget struct {
	entries int
	bytes   int64
}

func (f fileTree) copyDirectory(source, destination string) error {
	budget := fileCopyBudget{}
	if err := f.copyDirectoryContents(source, destination, 0, &budget); err != nil {
		_ = f.root.RemoveAll(destination)
		return err
	}
	return nil
}

func (f fileTree) copyDirectoryContents(source, destination string, depth int, budget *fileCopyBudget) error {
	if depth > MaxArchiveDepth {
		return ErrArchiveLimit
	}
	info, err := f.root.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrFilePermission
	}
	if _, err := f.root.Lstat(destination); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := f.root.Mkdir(destination, info.Mode().Perm()); err != nil {
		return err
	}
	directory, err := f.root.Open(source)
	if err != nil {
		return err
	}
	defer directory.Close()
	for {
		entries, readErr := directory.ReadDir(128)
		for _, item := range entries {
			budget.entries++
			if budget.entries > MaxArchiveEntries {
				return ErrArchiveLimit
			}
			childSource := filepath.Join(source, item.Name())
			childDestination := filepath.Join(destination, item.Name())
			childInfo, infoErr := f.root.Lstat(childSource)
			if infoErr != nil {
				return infoErr
			}
			if childInfo.Mode()&os.ModeSymlink != 0 || (!childInfo.IsDir() && !childInfo.Mode().IsRegular()) {
				return ErrFilePermission
			}
			if childInfo.IsDir() {
				if err := f.copyDirectoryContents(childSource, childDestination, depth+1, budget); err != nil {
					return fmt.Errorf("copy directory %s: %w", childSource, err)
				}
				continue
			}
			if err := f.copyRegularFile(childSource, childDestination, childInfo, budget, false); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return f.preserveMetadata(destination, info)
}

func (f fileTree) copyRegularFile(source, destination string, info os.FileInfo, budget *fileCopyBudget, overwrite bool) error {
	if info.Size() < 0 || info.Size() > MaxArchiveEntryBytes {
		return ErrFileTooLarge
	}
	if budget != nil && (budget.bytes > MaxArchiveBytes-info.Size()) {
		return ErrArchiveLimit
	}
	input, err := f.root.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	var output *os.File
	outputPath := destination
	if overwrite {
		temporary, temporaryErr := f.temporary(filepath.Dir(destination))
		if temporaryErr != nil {
			return temporaryErr
		}
		outputPath = filepath.Join(filepath.Dir(destination), filepath.Base(temporary.Name()))
		defer f.root.Remove(outputPath)
		output = temporary
	} else {
		output, err = f.root.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
	}
	written, copyErr := io.CopyN(output, input, info.Size())
	closeErr := output.Close()
	if copyErr == nil && closeErr == nil && written == info.Size() {
		if err := f.preserveMetadata(outputPath, info); err != nil {
			return err
		}
		if overwrite {
			if err := f.root.rename(outputPath, destination, false); err != nil {
				return err
			}
		}
		if budget != nil {
			budget.bytes += written
		}
		return nil
	}
	_ = f.root.Remove(outputPath)
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return io.ErrUnexpectedEOF
}

func (f fileTree) trashFile(path, root string, operation FileOperation) (FileResult, error) {
	if operation.Permanent {
		return f.deleteFile(path, operation)
	}
	info, err := f.root.Lstat(path)
	if err != nil {
		return FileResult{}, err
	}
	if operation.ExpectedFingerprint != "" && statFingerprint(info) != operation.ExpectedFingerprint {
		return FileResult{}, ErrFileConflict
	}
	trashRoot := filepath.Join(root, ".local", "share", "Trash")
	if strings.HasPrefix(path, trashRoot+"/") {
		return FileResult{}, ErrInvalidFileOperation
	}
	if err := f.root.MkdirAll(filepath.Join(trashRoot, "files"), 0700); err != nil {
		return FileResult{}, err
	}
	if err := f.root.MkdirAll(filepath.Join(trashRoot, "info"), 0700); err != nil {
		return FileResult{}, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return FileResult{}, err
	}
	name := filepath.Base(path) + "-" + hex.EncodeToString(random[:])
	destination := filepath.Join(trashRoot, "files", name)
	recordPath := filepath.Join(trashRoot, "info", name+".trashinfo")
	record, err := f.root.OpenFile(recordPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return FileResult{}, err
	}
	original := path
	if root == "." {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			original = filepath.Join(home, path)
		}
	} else {
		original = filepath.Join(string(filepath.Separator), path)
	}
	_, err = io.WriteString(record, "[Trash Info]\nPath="+escapeTrashPath(original)+"\nDeletionDate="+time.Now().UTC().Format("2006-01-02T15:04:05")+"\n")
	if err == nil {
		err = record.Sync()
	}
	record.Close()
	if err != nil {
		f.root.Remove(recordPath)
		return FileResult{}, err
	}
	if err := f.root.rename(path, destination, true); err != nil {
		f.root.Remove(recordPath)
		return FileResult{}, err
	}
	entry, err := f.entry(destination, destination)
	return FileResult{Entry: &entry, Message: "Item moved to trash."}, err
}

func (f fileTree) restoreFile(path string, operation FileOperation, privileged bool) (FileResult, error) {
	directory := ".local/share/Trash/files/"
	if privileged {
		directory = "root/.local/share/Trash/files/"
	}
	if !strings.HasPrefix(path, directory) || strings.Contains(strings.TrimPrefix(path, directory), "/") {
		return FileResult{}, ErrInvalidFileOperation
	}
	info, err := f.root.Lstat(path)
	if err != nil {
		return FileResult{}, err
	}
	if operation.ExpectedFingerprint != "" && statFingerprint(info) != operation.ExpectedFingerprint {
		return FileResult{}, ErrFileConflict
	}
	recordPath := filepath.Join(filepath.Dir(filepath.Dir(path)), "info", filepath.Base(path)+".trashinfo")
	record, err := f.root.Open(recordPath)
	legacy := false
	if errors.Is(err, os.ErrNotExist) {
		recordPath = filepath.Join(filepath.Dir(filepath.Dir(path)), "info", filepath.Base(path)+".json")
		record, err = f.root.Open(recordPath)
		legacy = true
	}
	if err != nil {
		return FileResult{}, err
	}
	data, err := io.ReadAll(io.LimitReader(record, MaxFilePath+512))
	record.Close()
	original, parseErr := parseTrashOriginal(data, legacy)
	if err != nil || parseErr != nil {
		return FileResult{}, ErrInvalidFileOperation
	}
	originalPath, err := f.restoreOriginalPath(original, privileged)
	if err != nil {
		return FileResult{}, err
	}
	if err := f.root.rename(path, originalPath, true); err != nil {
		return FileResult{}, err
	}
	f.root.Remove(recordPath)
	entry, err := f.entry(originalPath, originalPath)
	return FileResult{Entry: &entry, Message: "Item restored."}, err
}

func escapeTrashPath(path string) string {
	return strings.ReplaceAll(url.QueryEscape(path), "+", "%20")
}

func parseTrashOriginal(data []byte, legacy bool) (string, error) {
	if legacy {
		var record struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(data, &record); err != nil {
			return "", err
		}
		return record.Path, nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Path=") {
			return url.QueryUnescape(strings.TrimPrefix(line, "Path="))
		}
	}
	return "", ErrInvalidFileOperation
}

func (f fileTree) restoreOriginalPath(original string, privileged bool) (string, error) {
	if original == "" || strings.ContainsAny(original, "\x00\r\n") {
		return "", ErrInvalidFileOperation
	}
	if privileged {
		if !filepath.IsAbs(original) {
			if !filepath.IsLocal(original) || original == "." {
				return "", ErrInvalidFileOperation
			}
			return original, nil
		}
		original = filepath.Clean(original)
		if original == "/" {
			return "", ErrInvalidFileOperation
		}
		return strings.TrimPrefix(original, "/"), nil
	}
	home, err := os.UserHomeDir()
	if !filepath.IsAbs(original) {
		if filepath.IsLocal(original) && original != "." {
			return original, nil
		}
		return "", ErrInvalidFileOperation
	}
	if err != nil {
		return "", ErrInvalidFileOperation
	}
	relative, err := filepath.Rel(filepath.Clean(home), filepath.Clean(original))
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return "", ErrInvalidFileOperation
	}
	return relative, nil
}

func (f fileTree) deleteFile(path string, operation FileOperation) (FileResult, error) {
	info, err := f.root.Lstat(path)
	if err != nil {
		return FileResult{}, err
	}
	if operation.ExpectedFingerprint != "" && statFingerprint(info) != operation.ExpectedFingerprint {
		return FileResult{}, ErrFileConflict
	}
	if info.IsDir() {
		if !operation.Recursive {
			return FileResult{}, errors.New("directory deletion requires recursive confirmation")
		}
		err = f.root.RemoveAll(path)
	} else {
		err = f.root.Remove(path)
	}
	return FileResult{Message: "Item deleted."}, err
}

func (f fileTree) searchFiles(ctx context.Context, path string, operation FileOperation) (FileResult, error) {
	maxEntries := operation.MaxEntries
	if maxEntries == 0 {
		maxEntries = 1000
	}
	result := FileSearchResult{Root: path, Query: operation.Query, Entries: []FileEntry{}}
	count := 0
	visited := 0
	needle := strings.ToLower(operation.Query)
	err := f.walk(ctx, path, func(current string, item os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		visited++
		if visited > MaxSearchEntries || strings.Count(current, "/")-strings.Count(path, "/") > MaxArchiveDepth {
			result.Limited = true
			return fs.SkipAll
		}
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrPermission) {
				return filepath.SkipDir
			}
			return nil
		}
		if current != path && strings.Contains(strings.ToLower(item.Name()), needle) {
			entry, entryErr := f.entry(current, current)
			if entryErr == nil {
				result.Entries = append(result.Entries, entry)
				count++
			}
		}
		if count >= maxEntries || count >= MaxSearchEntries {
			result.Limited = true
			return errors.New("search limit")
		}
		return nil
	})
	if errors.Is(err, ErrArchiveLimit) {
		result.Limited = true
		err = nil
	}
	if err != nil && err.Error() != "search limit" {
		return FileResult{}, err
	}
	return FileResult{Search: &result}, nil
}

func (f fileTree) createArchive(ctx context.Context, source, destination string, operation FileOperation) (FileResult, error) {
	sourceAbs, sourceErr := filepath.Abs(source)
	destinationAbs, destinationErr := filepath.Abs(destination)
	if sourceErr != nil || destinationErr != nil || pathWithin(sourceAbs, destinationAbs) {
		return FileResult{}, ErrInvalidFileOperation
	}
	output, err := f.root.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return FileResult{}, err
	}
	defer output.Close()
	removePartial := true
	defer func() {
		if removePartial {
			_ = f.root.Remove(destination)
		}
	}()
	gzipWriter := gzip.NewWriter(output)
	tarWriter := tar.NewWriter(gzipWriter)
	entries := 0
	bytesWritten := int64(0)
	err = f.walk(ctx, source, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, walkErr := item.Info()
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entries >= MaxArchiveEntries || bytesWritten >= MaxArchiveBytes {
			return ErrArchiveLimit
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafeArchive
		}
		relative, relErr := filepath.Rel(filepath.Dir(source), path)
		if relErr != nil || !safeArchiveName(relative) {
			return ErrUnsafeArchive
		}
		header, headerErr := tar.FileInfoHeader(info, "")
		if headerErr != nil {
			return headerErr
		}
		header.Name = filepath.ToSlash(relative)
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			if info.Size() > MaxArchiveEntryBytes || bytesWritten+info.Size() > MaxArchiveBytes {
				return ErrArchiveLimit
			}
			input, openErr := f.root.Open(path)
			if openErr != nil {
				return openErr
			}
			written, copyErr := io.Copy(tarWriter, input)
			_ = input.Close()
			if copyErr != nil {
				return copyErr
			}
			bytesWritten += written
		}
		entries++
		return nil
	})
	tarCloseErr := tarWriter.Close()
	gzipCloseErr := gzipWriter.Close()
	syncErr := output.Sync()
	outputCloseErr := output.Close()
	if err != nil {
		return FileResult{}, err
	}
	if tarCloseErr != nil {
		return FileResult{}, tarCloseErr
	}
	if gzipCloseErr != nil {
		return FileResult{}, gzipCloseErr
	}
	if syncErr != nil {
		return FileResult{}, syncErr
	}
	if outputCloseErr != nil {
		return FileResult{}, outputCloseErr
	}
	removePartial = false
	return FileResult{Message: fmt.Sprintf("Archive created with %d entries.", entries)}, nil
}

func (f fileTree) extractArchive(ctx context.Context, archivePath, destination string, operation FileOperation) (FileResult, error) {
	destinationRoot, err := f.root.OpenRoot(destination)
	if err != nil {
		return FileResult{}, err
	}
	defer destinationRoot.Close()
	created := []string{}
	completed := false
	defer func() {
		if completed {
			return
		}
		for index := len(created) - 1; index >= 0; index-- {
			_ = destinationRoot.RemoveAll(created[index])
		}
	}()
	input, err := f.root.Open(archivePath)
	if err != nil {
		return FileResult{}, err
	}
	defer input.Close()
	gzipReader, err := gzip.NewReader(input)
	if err != nil {
		return FileResult{}, err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	entries := 0
	bytesRead := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return FileResult{}, err
		}
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return FileResult{}, nextErr
		}
		entries++
		if entries > MaxArchiveEntries || header.Size < 0 || header.Size > MaxArchiveEntryBytes || bytesRead+header.Size > MaxArchiveBytes {
			return FileResult{}, ErrArchiveLimit
		}
		if !safeArchiveName(header.Name) || strings.Count(filepath.ToSlash(header.Name), "/") > MaxArchiveDepth {
			return FileResult{}, ErrUnsafeArchive
		}
		target := filepath.FromSlash(header.Name)
		if !filepath.IsLocal(target) {
			return FileResult{}, ErrUnsafeArchive
		}
		if header.FileInfo().IsDir() {
			if err := ensureExtractDirectory(destinationRoot, target, &created); err != nil {
				return FileResult{}, err
			}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return FileResult{}, ErrUnsafeArchive
		}
		if err := ensureExtractDirectory(destinationRoot, filepath.Dir(target), &created); err != nil {
			return FileResult{}, err
		}
		output, createErr := destinationRoot.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY|os.O_EXCL, 0o600)
		if createErr != nil {
			return FileResult{}, createErr
		}
		created = append(created, target)
		written, copyErr := io.CopyN(output, tarReader, header.Size)
		closeErr := output.Close()
		if copyErr != nil {
			return FileResult{}, copyErr
		}
		if closeErr != nil {
			return FileResult{}, closeErr
		}
		bytesRead += written
	}
	completed = true
	return FileResult{Message: fmt.Sprintf("Archive extracted with %d entries.", entries)}, nil
}

func ensureExtractDirectory(root *fileRoot, path string, created *[]string) error {
	if path == "." || path == "" {
		return nil
	}
	current := "."
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := root.Mkdir(current, 0o755); err != nil {
				return err
			}
			*created = append(*created, current)
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafeArchive
		}
	}
	return nil
}

func safeArchiveName(name string) bool {
	name = filepath.ToSlash(name)
	return name != "" && !strings.HasPrefix(name, "/") && name != "." && name != ".." && !strings.HasPrefix(name, "../") && !strings.Contains(name, "/../") && !strings.ContainsRune(name, '\x00')
}

func filePathWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (f fileTree) updateMetadata(path string, operation FileOperation, privileged bool) (FileResult, error) {
	if !privileged {
		return FileResult{}, ErrFilePermission
	}
	info, err := f.root.Lstat(path)
	if err != nil {
		return FileResult{}, err
	}
	if operation.ExpectedFingerprint != "" && statFingerprint(info) != operation.ExpectedFingerprint {
		return FileResult{}, ErrFileConflict
	}
	if operation.Mode != nil {
		if err := f.chmod(path, os.FileMode(*operation.Mode)&os.ModePerm); err != nil {
			return FileResult{}, err
		}
	}
	if operation.Owner != "" || operation.Group != "" {
		uid, gid := -1, -1
		if operation.Owner != "" {
			account, lookupErr := user.Lookup(operation.Owner)
			if lookupErr != nil {
				return FileResult{}, lookupErr
			}
			parsed, parseErr := strconv.Atoi(account.Uid)
			if parseErr != nil {
				return FileResult{}, parseErr
			}
			uid = parsed
		}
		if operation.Group != "" {
			group, lookupErr := user.LookupGroup(operation.Group)
			if lookupErr != nil {
				return FileResult{}, lookupErr
			}
			parsed, parseErr := strconv.Atoi(group.Gid)
			if parseErr != nil {
				return FileResult{}, parseErr
			}
			gid = parsed
		}
		if err := f.chown(path, uid, gid); err != nil {
			return FileResult{}, err
		}
	}
	entry, err := f.entry(path, path)
	return FileResult{Entry: &entry, Message: "File metadata updated."}, err
}

// ReadTextWindow exposes bounded line windows without loading a large file in
// the browser or gateway. Offset is a zero-based line number for text clients.
func (f fileTree) readTextWindow(path string, lineOffset, lineLimit int) (FileResult, error) {
	if lineOffset < 0 || lineLimit < 1 || lineLimit > 1000 {
		return FileResult{}, ErrInvalidFileOperation
	}
	file, err := f.root.Open(path)
	if err != nil {
		return FileResult{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, MaxTextFile+1))
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1<<20)
	line := 0
	lines := make([]string, 0, lineLimit)
	for scanner.Scan() {
		if line >= lineOffset && len(lines) < lineLimit {
			lines = append(lines, scanner.Text())
		}
		line++
		if len(lines) == lineLimit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return FileResult{}, err
	}
	payload, _ := json.Marshal(lines)
	info, _ := f.root.Stat(path)
	return FileResult{Content: payload, Offset: int64(lineOffset), Total: int64(line), EOF: line < lineOffset+lineLimit, Mime: "application/json", Fingerprint: statFingerprint(info)}, nil
}

// fileTree anchors every operation to an open directory rather than a checked pathname.
type fileTree struct{ root *fileRoot }

func (f fileTree) temporary(directory string) (*os.File, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return nil, err
	}
	return f.root.OpenFile(filepath.Join(directory, ".tako-write-"+hex.EncodeToString(value[:])), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
}
func (f fileTree) chmod(path string, mode os.FileMode) error {
	file, err := f.root.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Chmod(mode)
}
func (f fileTree) chown(path string, uid, gid int) error {
	file, err := f.root.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Chown(uid, gid)
}
func (f fileTree) preserveMetadata(path string, info os.FileInfo) error {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
		if err := f.chown(path, int(stat.Uid), int(stat.Gid)); err != nil {
			return err
		}
	}
	return f.chmod(path, info.Mode().Perm())
}
