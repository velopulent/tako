package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testAuthorizedKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB test-key"
const testAuthorizedKey2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJC second-key"

func testSSHKeyDependencies(home string) sshKeyDependencies {
	account := sshKeyAccount{username: "alice", home: home, uid: os.Getuid(), gid: os.Getgid()}
	return sshKeyDependencies{
		lookup: func(string) (sshKeyAccount, error) { return account, nil },
		path:   authorizedKeysPath,
	}
}

func TestSSHKeysValidateAndApplyAtomically(t *testing.T) {
	home := t.TempDir()
	dependencies := testSSHKeyDependencies(home)
	list := SSHKeyOperation{Action: "list", Username: "alice", Preview: true}
	state, err := readSSHKeyStateWithDependencies(context.Background(), list, "alice", false, dependencies)
	if err != nil || len(state.Keys) != 0 || state.Fingerprint == "" {
		t.Fatalf("empty authorized_keys state: %#v %v", state, err)
	}
	previewOperation := SSHKeyOperation{Action: "add", Username: "alice", Key: testAuthorizedKey, Preview: true}
	preview, err := PreviewSSHKeysWithDependencies(context.Background(), previewOperation, "alice", false, dependencies)
	if err != nil || !preview.Allowed || len(preview.Changes) != 1 {
		t.Fatalf("add preview failed: %#v %v", preview, err)
	}
	apply := previewOperation
	apply.Preview = false
	apply.ExpectedFingerprint = preview.Current.Fingerprint
	state, err = applySSHKeysWithDependencies(context.Background(), apply, "alice", false, dependencies)
	if err != nil || len(state.Keys) != 1 || state.Keys[0].Type != "ssh-ed25519" {
		t.Fatalf("add apply failed: %#v %v", state, err)
	}
	info, err := os.Stat(filepath.Join(home, ".ssh"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf(".ssh permissions = %v err=%v", info.Mode().Perm(), err)
	}
	keyPath := filepath.Join(home, ".ssh", "authorized_keys")
	keyInfo, err := os.Stat(keyPath)
	if err != nil || keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("authorized_keys permissions = %v err=%v", keyInfo.Mode().Perm(), err)
	}

	stale := apply
	stale.ExpectedFingerprint = preview.Current.Fingerprint
	if _, err := applySSHKeysWithDependencies(context.Background(), stale, "alice", false, dependencies); !errors.Is(err, ErrSSHKeyConflict) {
		t.Fatalf("stale write returned %v", err)
	}
	removePreviewOperation := SSHKeyOperation{Action: "remove", Username: "alice", Fingerprint: state.Keys[0].Fingerprint, Preview: true}
	removePreview, err := PreviewSSHKeysWithDependencies(context.Background(), removePreviewOperation, "alice", false, dependencies)
	if err != nil || !removePreview.Allowed {
		t.Fatalf("remove preview failed: %#v %v", removePreview, err)
	}
	remove := removePreviewOperation
	remove.Preview = false
	remove.ExpectedFingerprint = removePreview.Current.Fingerprint
	remove.Confirmation = "REMOVE KEY " + remove.Fingerprint
	state, err = applySSHKeysWithDependencies(context.Background(), remove, "alice", false, dependencies)
	if err != nil || len(state.Keys) != 0 {
		t.Fatalf("remove apply failed: %#v %v", state, err)
	}
}

func TestSSHKeysPreserveCommentsAndRejectProtectedPaths(t *testing.T) {
	home := t.TempDir()
	dependencies := testSSHKeyDependencies(home)
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(home, ".ssh", "authorized_keys")
	if err := os.WriteFile(keyPath, []byte("# managed by operator\n"+testAuthorizedKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := readSSHKeyStateWithDependencies(context.Background(), SSHKeyOperation{Action: "list", Username: "alice", Preview: true}, "alice", false, dependencies)
	if err != nil || len(state.Keys) != 1 {
		t.Fatalf("read with comment failed: %#v %v", state, err)
	}
	preview, err := PreviewSSHKeysWithDependencies(context.Background(), SSHKeyOperation{Action: "add", Username: "alice", Key: testAuthorizedKey2, Preview: true}, "alice", false, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	apply := SSHKeyOperation{Action: "add", Username: "alice", Key: testAuthorizedKey2, ExpectedFingerprint: preview.Current.Fingerprint}
	if updated, err := applySSHKeysWithDependencies(context.Background(), apply, "alice", false, dependencies); err != nil || len(updated.Keys) != 2 {
		t.Fatalf("second key add failed: %#v %v", updated, err)
	}
	content, err := os.ReadFile(keyPath)
	if err != nil || !strings.Contains(string(content), "# managed by operator") {
		t.Fatalf("comment was not preserved: %q err=%v", content, err)
	}

	protectedHome := t.TempDir()
	target := filepath.Join(protectedHome, "outside")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(protectedHome, ".ssh")); err != nil {
		t.Fatal(err)
	}
	protectedDependencies := testSSHKeyDependencies(protectedHome)
	_, err = readSSHKeyStateWithDependencies(context.Background(), SSHKeyOperation{Action: "list", Username: "alice", Preview: true}, "alice", false, protectedDependencies)
	if !errors.Is(err, ErrSSHKeyProtected) {
		t.Fatalf("symlink path returned %v", err)
	}
}

func TestSSHKeyValidationRejectsWeakOrMalformedEntries(t *testing.T) {
	for _, operation := range []SSHKeyOperation{
		{Action: "add", Username: "alice", Key: "ssh-dss AQID", Preview: true},
		{Action: "add", Username: "alice", Key: "ssh-ed25519 not-base64", Preview: true},
		{Action: "remove", Username: "alice", Fingerprint: "not-a-fingerprint", Preview: true},
		{Action: "remove", Username: "alice", Fingerprint: strings.Repeat("a", 64)},
	} {
		if err := ValidateSSHKeyOperation(operation); !errors.Is(err, ErrInvalidSSHKeyOperation) {
			t.Fatalf("malformed operation accepted: %#v err=%v", operation, err)
		}
	}
}
