package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func TestArchiveRejectsUnsafeEntriesAndBounds(t *testing.T) {
	if safeArchiveName("../etc/passwd") || safeArchiveName("/etc/passwd") || safeArchiveName("a/../../b") {
		t.Fatal("unsafe archive path accepted")
	}
	if !safeArchiveName("folder/file.txt") {
		t.Fatal("safe archive path rejected")
	}
}
