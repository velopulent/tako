package platform

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserFileOperationsRejectTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveFilePath(root, "../secret", false); !errors.Is(err, ErrFilePermission) {
		t.Fatalf("traversal error = %v", err)
	}
	if _, err := resolveFilePath(root, "escape/secret", false); !errors.Is(err, ErrFilePermission) {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestFileWriteConflictAndChunkChecksum(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := fileEntry(path, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyFileOperation(context.Background(), FileOperation{Action: "write-text", Path: "note.txt", Content: []byte("new"), ExpectedFingerprint: "bad"}, root, true); !errors.Is(err, ErrInvalidFileOperation) {
		t.Fatalf("invalid fingerprint error = %v", err)
	}
	if _, err := applyFileOperation(context.Background(), FileOperation{Action: "write-text", Path: "note.txt", Content: []byte("new"), ExpectedFingerprint: entry.Fingerprint}, root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFileOperation(context.Background(), FileOperation{Action: "write-chunk", Path: "note.txt", Offset: 0, Content: []byte("x"), ContentSHA256: "bad"}, root, true); !errors.Is(err, ErrInvalidFileOperation) {
		t.Fatalf("invalid checksum error = %v", err)
	}
}

func TestFileReadRejectsStalePreviewFingerprint(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := fileEntry(path, path)
	if err != nil {
		t.Fatal(err)
	}
	stale := "0" + entry.Fingerprint[1:]
	if stale == entry.Fingerprint {
		stale = "1" + entry.Fingerprint[1:]
	}
	if _, err := applyFileOperation(context.Background(), FileOperation{Action: "read", Path: "note.txt", ExpectedFingerprint: stale}, root, true); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("stale preview read error = %v", err)
	}
}

func TestArchiveRejectsUnsafeEntriesAndBounds(t *testing.T) {
	if safeArchiveName("../etc/passwd") || safeArchiveName("/etc/passwd") || safeArchiveName("a/../../b") {
		t.Fatal("unsafe archive path accepted")
	}
	if !safeArchiveName("folder/file.txt") {
		t.Fatal("safe archive path rejected")
	}
}

func TestRootedExtractionRejectsExistingSymlinkAncestor(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "destination"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "destination", "link")); err != nil {
		t.Fatal(err)
	}
	archive, err := os.Create(filepath.Join(root, "input.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(archive)
	writer := tar.NewWriter(compressed)
	if err := writer.WriteHeader(&tar.Header{Name: "link/payload", Mode: 0600, Size: 1}); err != nil {
		t.Fatal(err)
	}
	writer.Write([]byte("x"))
	writer.Close()
	compressed.Close()
	archive.Close()
	_, err = applyFileOperation(context.Background(), FileOperation{Action: "extract", Path: "destination", ArchivePath: "input.tar.gz", Confirmation: "CONFIRM FILE OPERATION"}, root, false)
	if err == nil {
		t.Fatal("symlink ancestor accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "payload")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside destination changed: %v", err)
	}
}

func TestExtractionRemovesPartialOutputAfterFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "destination"), 0700); err != nil {
		t.Fatal(err)
	}
	archive, err := os.Create(filepath.Join(root, "partial.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(archive)
	writer := tar.NewWriter(compressed)
	if err := writer.WriteHeader(&tar.Header{Name: "created.txt", Mode: 0600, Size: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHeader(&tar.Header{Name: "unsafe", Typeflag: tar.TypeSymlink, Linkname: "/tmp/outside"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = applyFileOperation(context.Background(), FileOperation{
		Action: "extract", Path: "destination", ArchivePath: "partial.tar.gz", Confirmation: "CONFIRM FILE OPERATION",
	}, root, false)
	if err == nil {
		t.Fatal("unsafe archive unexpectedly extracted")
	}
	if _, err := os.Stat(filepath.Join(root, "destination", "created.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial output remained: %v", err)
	}
}

func TestRootedFileOperationsRejectSymlinkParents(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	os.Symlink(outside, filepath.Join(root, "link"))
	for _, operation := range []FileOperation{{Action: "create", Path: "link/file"}, {Action: "write-text", Path: "link/file", Content: []byte("x")}, {Action: "metadata", Path: "link/file", Confirmation: "CONFIRM FILE OPERATION"}} {
		if _, err := applyFileOperation(context.Background(), operation, root, true); err == nil {
			t.Fatalf("%s followed symlink", operation.Action)
		}
	}
}

func TestUploadStagesAndValidatesRetries(t *testing.T) {
	root := t.TempDir()
	chunk := func(offset int64, id string, content string) (FileResult, error) {
		sum := sha256.Sum256([]byte(content))
		return applyFileOperation(context.Background(), FileOperation{Action: "write-chunk", Path: "upload", Offset: offset, TotalSize: 6, UploadID: id, Content: []byte(content), ContentSHA256: hex.EncodeToString(sum[:])}, root, false)
	}
	first, err := chunk(0, "", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "upload")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial upload published")
	}
	if _, err := chunk(0, first.UploadID, "bad"); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("changed retry accepted: %v", err)
	}
	retry, err := chunk(0, first.UploadID, "abc")
	if err != nil || retry.Offset != 3 {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	last, err := chunk(3, first.UploadID, "def")
	if err != nil || !last.EOF {
		t.Fatalf("completion: %+v %v", last, err)
	}
	content, err := os.ReadFile(filepath.Join(root, "upload"))
	if err != nil || string(content) != "abcdef" {
		t.Fatalf("content %q %v", content, err)
	}
	status, err := applyFileOperation(context.Background(), FileOperation{Action: "upload-status", Path: "upload", UploadID: first.UploadID}, root, false)
	if err != nil || len(status.Uploads) != 1 || !status.Uploads[0].Completed || status.Uploads[0].Offset != 6 {
		t.Fatalf("completed upload status = %+v, err=%v", status.Uploads, err)
	}
}

func TestRecursiveDirectoryCopyAndUploadStatus(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "source", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source", "nested", "value"), []byte("copy me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFileOperation(context.Background(), FileOperation{Action: "copy", Path: "source", Destination: "copied", Recursive: true, Confirmation: "CONFIRM FILE OPERATION"}, root, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "copied", "nested", "value"))
	if err != nil || string(data) != "copy me" {
		t.Fatalf("copied content %q, err=%v", data, err)
	}
	if _, err := applyFileOperation(context.Background(), FileOperation{Action: "copy", Path: "source", Destination: "source/child", Recursive: true, Confirmation: "CONFIRM FILE OPERATION"}, root, false); !errors.Is(err, ErrInvalidFileOperation) {
		t.Fatalf("copy into source error = %v", err)
	}

	chunk := []byte("abc")
	sum := sha256.Sum256(chunk)
	first, err := applyFileOperation(context.Background(), FileOperation{Action: "write-chunk", Path: "upload", Offset: 0, TotalSize: 6, Content: chunk, ContentSHA256: hex.EncodeToString(sum[:])}, root, false)
	if err != nil {
		t.Fatal(err)
	}
	status, err := applyFileOperation(context.Background(), FileOperation{Action: "upload-status", Path: "upload"}, root, false)
	if err != nil || len(status.Uploads) != 1 || status.Uploads[0].UploadID != first.UploadID || status.Uploads[0].Offset != 3 {
		t.Fatalf("upload status = %+v, err=%v", status.Uploads, err)
	}
}

func TestFileOverwriteRequiresExplicitConfirmationAndReplacesRegularFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "destination"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	operation := FileOperation{Action: "copy", Path: "source", Destination: "destination", Overwrite: true}
	if _, err := applyFileOperation(context.Background(), operation, root, false); !errors.Is(err, ErrInvalidFileOperation) {
		t.Fatalf("missing overwrite confirmation error = %v", err)
	}
	operation.Confirmation = "CONFIRM FILE OVERWRITE"
	if _, err := applyFileOperation(context.Background(), operation, root, false); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "destination"))
	if err != nil || string(content) != "new" {
		t.Fatalf("overwritten content %q, err=%v", content, err)
	}

	if err := os.Mkdir(filepath.Join(root, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	operation.Destination = "directory"
	if _, err := applyFileOperation(context.Background(), operation, root, false); !errors.Is(err, ErrInvalidFileOperation) {
		t.Fatalf("directory overwrite error = %v", err)
	}
}

func TestTrashUsesFreedesktopMetadataAndRestores(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note"), []byte("trash"), 0o600); err != nil {
		t.Fatal(err)
	}
	moved, err := applyFileOperation(context.Background(), FileOperation{Action: "trash", Path: "note"}, root, false)
	if err != nil || moved.Entry == nil {
		t.Fatalf("trash result = %+v, err=%v", moved, err)
	}
	metadata := filepath.Join(root, ".local/share/Trash/info", filepath.Base(moved.Entry.Path)+".trashinfo")
	content, err := os.ReadFile(metadata)
	if err != nil || !strings.Contains(string(content), "[Trash Info]") || !strings.Contains(string(content), "Path=") {
		t.Fatalf("trash metadata = %q, err=%v", content, err)
	}
	restored, err := applyFileOperation(context.Background(), FileOperation{Action: "restore", Path: moved.Entry.Path}, root, false)
	if err != nil || restored.Entry == nil {
		t.Fatalf("restore result = %+v, err=%v", restored, err)
	}
	if _, err := os.Stat(filepath.Join(root, "note")); err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
}

func TestFilePagesAndEmptyTextSave(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".hidden", "first", "second", "third"} {
		os.WriteFile(filepath.Join(root, name), []byte("text"), 0600)
	}
	seen := map[string]bool{}
	offset := int64(0)
	for {
		page, err := applyFileOperation(context.Background(), FileOperation{Action: "list", Path: ".", Limit: 1, Offset: offset}, root, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Directory.Entries {
			if seen[entry.Name] {
				t.Fatal("duplicate entry")
			}
			seen[entry.Name] = true
		}
		if !page.Directory.HasMore {
			break
		}
		offset = page.Directory.NextOffset
	}
	if len(seen) != 3 || seen[".hidden"] {
		t.Fatalf("listing: %v", seen)
	}
	if _, err := applyFileOperation(context.Background(), FileOperation{Action: "write-text", Path: "first", Content: []byte{}}, root, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "first"))
	if len(data) != 0 {
		t.Fatal("empty save retained content")
	}
}
