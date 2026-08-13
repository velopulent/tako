package platform

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
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
}

type FileDirectory struct {
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
}

// FileOperation is the narrow wire contract shared by the gateway, sessiond,
// and user bridge. User-scoped paths are relative to the authenticated home;
// privileged paths may be absolute and are still checked against protected
// system locations and operation-specific limits.
type FileOperation struct {
	Action              string  `json:"action"`
	Path                string  `json:"path,omitempty"`
	Destination         string  `json:"destination,omitempty"`
	Kind                string  `json:"kind,omitempty"`
	Content             []byte  `json:"content,omitempty"`
	ContentSHA256       string  `json:"contentSha256,omitempty"`
	Offset              int64   `json:"offset,omitempty"`
	Limit               int64   `json:"limit,omitempty"`
	LineOffset          int     `json:"lineOffset,omitempty"`
	LineLimit           int     `json:"lineLimit,omitempty"`
	ExpectedFingerprint string  `json:"expectedFingerprint,omitempty"`
	ShowHidden          bool    `json:"showHidden,omitempty"`
	Recursive           bool    `json:"recursive,omitempty"`
	Permanent           bool    `json:"permanent,omitempty"`
	Confirmation        string  `json:"confirmation,omitempty"`
	Query               string  `json:"query,omitempty"`
	MaxEntries          int     `json:"maxEntries,omitempty"`
	ArchivePath         string  `json:"archivePath,omitempty"`
	Mode                *uint32 `json:"mode,omitempty"`
	Owner               string  `json:"owner,omitempty"`
	Group               string  `json:"group,omitempty"`
}

func ValidateFileOperation(operation FileOperation) error {
	validActions := map[string]bool{"list": true, "stat": true, "read": true, "read-window": true, "write": true, "create": true, "rename": true, "move": true, "copy": true, "trash": true, "delete": true, "search": true, "archive": true, "extract": true, "metadata": true}
	if !validActions[operation.Action] || len(operation.Path) > MaxFilePath || len(operation.Destination) > MaxFilePath || len(operation.ArchivePath) > MaxFilePath || strings.ContainsAny(operation.Path+operation.Destination+operation.ArchivePath, "\x00\r\n") {
		return ErrInvalidFileOperation
	}
	if operation.Path == "" && operation.Action != "archive" {
		return ErrInvalidFileOperation
	}
	if operation.Offset < 0 || operation.Limit < 0 || operation.Limit > MaxFileChunk || len(operation.Content) > MaxFileChunk {
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
	if operation.ContentSHA256 != "" && (len(operation.ContentSHA256) != sha256.Size*2 || !isHex(operation.ContentSHA256)) {
		return ErrInvalidFileOperation
	}
	if len(operation.Confirmation) > 128 || strings.ContainsAny(operation.Confirmation, "\x00\r\n") {
		return ErrInvalidFileOperation
	}
	if operation.Permanent || operation.Recursive || operation.Action == "metadata" {
		if operation.Confirmation != "CONFIRM FILE OPERATION" {
			return ErrInvalidFileOperation
		}
	}
	if operation.Action == "write" && operation.Limit == 0 && len(operation.Content) == 0 {
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
	resolve := func(value string) (string, error) { return resolveFilePath(root, value, privileged) }
	path, err := resolve(operation.Path)
	if operation.Action == "archive" && operation.Path == "" {
		path = root
	}
	if err != nil {
		return FileResult{}, err
	}
	switch operation.Action {
	case "list":
		return listDirectory(path, operation.ShowHidden)
	case "stat":
		entry, err := fileEntry(path, operation.Path)
		return FileResult{Entry: &entry}, err
	case "read":
		return readFile(path, operation.Offset, operation.Limit)
	case "read-window":
		return ReadTextWindow(path, operation.LineOffset, operation.LineLimit)
	case "write":
		return writeFile(path, operation)
	case "create":
		return createFile(path, operation)
	case "rename", "move", "copy":
		destination, resolveErr := resolve(operation.Destination)
		if resolveErr != nil {
			return FileResult{}, resolveErr
		}
		return transferFile(operation.Action, path, destination, operation)
	case "trash":
		return trashFile(path, root, operation)
	case "delete":
		return deleteFile(path, operation)
	case "search":
		return searchFiles(path, operation)
	case "archive":
		archive, resolveErr := resolve(operation.ArchivePath)
		if resolveErr != nil {
			return FileResult{}, resolveErr
		}
		return createArchive(path, archive, operation)
	case "extract":
		archive, resolveErr := resolve(operation.ArchivePath)
		if resolveErr != nil {
			return FileResult{}, resolveErr
		}
		return extractArchive(archive, path, operation)
	case "metadata":
		return updateMetadata(path, operation, privileged)
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
	}
	return path, nil
}

func listDirectory(path string, showHidden bool) (FileResult, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return FileResult{}, ErrFileNotFound
	}
	if errors.Is(err, os.ErrPermission) {
		return FileResult{}, ErrFilePermission
	}
	if err != nil {
		return FileResult{}, err
	}
	if len(entries) > MaxFileEntries {
		entries = entries[:MaxFileEntries]
	}
	result := make([]FileEntry, 0, len(entries))
	for _, item := range entries {
		if !showHidden && strings.HasPrefix(item.Name(), ".") {
			continue
		}
		entry, entryErr := fileEntry(filepath.Join(path, item.Name()), item.Name())
		if entryErr != nil {
			if errors.Is(entryErr, os.ErrPermission) {
				result = append(result, FileEntry{Name: item.Name(), Path: item.Name(), Hidden: strings.HasPrefix(item.Name(), "."), PermissionDenied: true, Reason: "metadata is not readable"})
				continue
			}
			continue
		}
		result = append(result, entry)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Kind != result[right].Kind {
			return result[left].Kind == "directory"
		}
		return strings.ToLower(result[left].Name) < strings.ToLower(result[right].Name)
	})
	stat, _ := os.Stat(path)
	return FileResult{Directory: &FileDirectory{Path: path, Entries: result, ShowHidden: showHidden, Fingerprint: statFingerprint(stat)}}, nil
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

func statFingerprint(info os.FileInfo) string {
	if info == nil {
		return ""
	}
	value := fmt.Sprintf("%s:%d:%d:%o", info.Mode().String(), info.Size(), info.ModTime().UnixNano(), info.Mode().Perm())
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func readFile(path string, offset, limit int64) (FileResult, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return FileResult{}, ErrFileNotFound
	}
	if err != nil {
		return FileResult{}, err
	}
	if info.IsDir() || info.Size() > MaxFileChunk*1024 {
		return FileResult{}, ErrFileTooLarge
	}
	if limit == 0 {
		limit = MaxFileChunk
	}
	file, err := os.Open(path)
	if err != nil {
		return FileResult{}, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return FileResult{}, err
	}
	content, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return FileResult{}, err
	}
	return FileResult{Content: content, Offset: offset, Total: info.Size(), EOF: offset+int64(len(content)) >= info.Size(), Mime: mime.TypeByExtension(filepath.Ext(path)), Fingerprint: statFingerprint(info)}, nil
}

func readText(path string, offset, limit int64) (FileResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileResult{}, err
	}
	if info.Size() > MaxTextFile {
		return FileResult{}, ErrFileTooLarge
	}
	result, err := readFile(path, offset, limit)
	if err == nil {
		result.Mime = "text/plain"
	}
	return result, err
}

func writeFile(path string, operation FileOperation) (FileResult, error) {
	if operation.ContentSHA256 != "" {
		digest := sha256.Sum256(operation.Content)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), operation.ContentSHA256) {
			return FileResult{}, ErrFileConflict
		}
	}
	info, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return FileResult{}, err
	}
	if info != nil && operation.ExpectedFingerprint != "" && statFingerprint(info) != operation.ExpectedFingerprint {
		return FileResult{}, ErrFileConflict
	}
	if operation.Offset > 0 {
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
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
		temporary, createErr := os.CreateTemp(filepath.Dir(path), ".tako-write-*")
		if createErr != nil {
			return FileResult{}, createErr
		}
		temporaryName := temporary.Name()
		defer os.Remove(temporaryName)
		if _, createErr = temporary.Write(operation.Content); createErr == nil {
			createErr = temporary.Sync()
		}
		if closeErr := temporary.Close(); createErr == nil {
			createErr = closeErr
		}
		if createErr == nil {
			if info != nil {
				_ = os.Chmod(temporaryName, info.Mode().Perm())
			}
			createErr = os.Rename(temporaryName, path)
		}
		if createErr != nil {
			return FileResult{}, createErr
		}
	}
	entry, err := fileEntry(path, path)
	return FileResult{Entry: &entry, Fingerprint: entry.Fingerprint}, err
}

func createFile(path string, operation FileOperation) (FileResult, error) {
	if operation.Kind == "directory" {
		if err := os.Mkdir(path, 0o755); err != nil {
			return FileResult{}, err
		}
	} else {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return FileResult{}, err
		}
		_ = file.Close()
	}
	entry, err := fileEntry(path, path)
	return FileResult{Entry: &entry}, err
}

func transferFile(action, source, destination string, operation FileOperation) (FileResult, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return FileResult{}, err
	}
	if operation.ExpectedFingerprint != "" && statFingerprint(info) != operation.ExpectedFingerprint {
		return FileResult{}, ErrFileConflict
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return FileResult{}, errors.New("symlink transfers require explicit link handling")
	}
	if action == "copy" {
		if info.IsDir() {
			return FileResult{}, errors.New("directory copy requires archive or recursive operation")
		}
		input, openErr := os.Open(source)
		if openErr != nil {
			return FileResult{}, openErr
		}
		output, createErr := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if createErr != nil {
			_ = input.Close()
			return FileResult{}, createErr
		}
		_, copyErr := io.CopyN(output, input, MaxArchiveEntryBytes+1)
		_ = input.Close()
		_ = output.Close()
		if copyErr != nil && !errors.Is(copyErr, io.EOF) {
			_ = os.Remove(destination)
			return FileResult{}, copyErr
		}
	} else if err := os.Rename(source, destination); err != nil {
		if errors.Is(err, syscall.EXDEV) && action == "move" {
			if _, copyErr := transferFile("copy", source, destination, operation); copyErr != nil {
				return FileResult{}, copyErr
			}
			if removeErr := os.Remove(source); removeErr != nil {
				return FileResult{}, removeErr
			}
		} else {
			return FileResult{}, err
		}
	}
	entry, err := fileEntry(destination, destination)
	return FileResult{Entry: &entry}, err
}

func trashFile(path, root string, operation FileOperation) (FileResult, error) {
	if operation.Permanent {
		return deleteFile(path, operation)
	}
	trashRoot := filepath.Join(root, ".local", "share", "Trash", "files")
	if err := os.MkdirAll(trashRoot, 0o700); err != nil {
		return FileResult{}, err
	}
	destination := filepath.Join(trashRoot, filepath.Base(path)+"-"+fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := os.Rename(path, destination); err != nil {
		return FileResult{Warnings: []string{"Trash is unavailable across filesystems; the item was not removed."}}, err
	}
	return FileResult{Message: "Item moved to the user trash."}, nil
}

func deleteFile(path string, operation FileOperation) (FileResult, error) {
	info, err := os.Lstat(path)
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
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	return FileResult{Message: "Item deleted."}, err
}

func searchFiles(path string, operation FileOperation) (FileResult, error) {
	maxEntries := operation.MaxEntries
	if maxEntries == 0 {
		maxEntries = 1000
	}
	result := FileSearchResult{Root: path, Query: operation.Query, Entries: []FileEntry{}}
	count := 0
	err := filepath.WalkDir(path, func(current string, item os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrPermission) {
				return filepath.SkipDir
			}
			return nil
		}
		if current != path && strings.Contains(strings.ToLower(item.Name()), strings.ToLower(operation.Query)) {
			entry, entryErr := fileEntry(current, current)
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
	if err != nil && err.Error() != "search limit" {
		return FileResult{}, err
	}
	return FileResult{Search: &result}, nil
}

func createArchive(source, destination string, operation FileOperation) (FileResult, error) {
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return FileResult{}, err
	}
	defer output.Close()
	gzipWriter := gzip.NewWriter(output)
	tarWriter := tar.NewWriter(gzipWriter)
	entries := 0
	bytesWritten := int64(0)
	err = filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
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
			input, openErr := os.Open(path)
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
	_ = tarWriter.Close()
	_ = gzipWriter.Close()
	if err != nil {
		return FileResult{}, err
	}
	return FileResult{Message: fmt.Sprintf("Archive created with %d entries.", entries)}, nil
}

func extractArchive(archivePath, destination string, operation FileOperation) (FileResult, error) {
	input, err := os.Open(archivePath)
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
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return FileResult{}, nextErr
		}
		entries++
		if entries > MaxArchiveEntries || header.Size > MaxArchiveEntryBytes || bytesRead+header.Size > MaxArchiveBytes {
			return FileResult{}, ErrArchiveLimit
		}
		if !safeArchiveName(header.Name) || strings.Count(filepath.ToSlash(header.Name), "/") > MaxArchiveDepth {
			return FileResult{}, ErrUnsafeArchive
		}
		target := filepath.Join(destination, filepath.FromSlash(header.Name))
		if !filePathWithin(destination, target) {
			return FileResult{}, ErrUnsafeArchive
		}
		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return FileResult{}, err
			}
			continue
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return FileResult{}, ErrUnsafeArchive
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return FileResult{}, err
		}
		output, createErr := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY|os.O_EXCL, 0o600)
		if createErr != nil {
			return FileResult{}, createErr
		}
		written, copyErr := io.CopyN(output, tarReader, header.Size)
		_ = output.Close()
		if copyErr != nil {
			return FileResult{}, copyErr
		}
		bytesRead += written
	}
	return FileResult{Message: fmt.Sprintf("Archive extracted with %d entries.", entries)}, nil
}

func safeArchiveName(name string) bool {
	name = filepath.ToSlash(name)
	return name != "" && !strings.HasPrefix(name, "/") && name != "." && !strings.HasPrefix(name, "../") && !strings.Contains(name, "/../") && !strings.ContainsRune(name, '\x00')
}

func filePathWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func updateMetadata(path string, operation FileOperation, privileged bool) (FileResult, error) {
	if !privileged {
		return FileResult{}, ErrFilePermission
	}
	info, err := os.Lstat(path)
	if err != nil {
		return FileResult{}, err
	}
	if operation.ExpectedFingerprint != "" && statFingerprint(info) != operation.ExpectedFingerprint {
		return FileResult{}, ErrFileConflict
	}
	if operation.Mode != nil {
		if err := os.Chmod(path, os.FileMode(*operation.Mode)&os.ModePerm); err != nil {
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
		if err := os.Chown(path, uid, gid); err != nil {
			return FileResult{}, err
		}
	}
	entry, err := fileEntry(path, path)
	return FileResult{Entry: &entry, Message: "File metadata updated."}, err
}

// ReadTextWindow exposes bounded line windows without loading a large file in
// the browser or gateway. Offset is a zero-based line number for text clients.
func ReadTextWindow(path string, lineOffset, lineLimit int) (FileResult, error) {
	if lineOffset < 0 || lineLimit < 1 || lineLimit > 1000 {
		return FileResult{}, ErrInvalidFileOperation
	}
	file, err := os.Open(path)
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
	info, _ := os.Stat(path)
	return FileResult{Content: payload, Offset: int64(lineOffset), Total: int64(line), EOF: line < lineOffset+lineLimit, Mime: "application/json", Fingerprint: statFingerprint(info)}, nil
}
