//go:build linux && integration

package platform

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestAuthorizedSSHKeysFilesystemSeam runs against a disposable VM-provided
// home directory. It never touches a real operator's authorized_keys file.
func TestAuthorizedSSHKeysFilesystemSeam(t *testing.T) {
	home := os.Getenv("TAKO_TEST_SSH_HOME")
	username := os.Getenv("TAKO_TEST_SSH_USER")
	if home == "" || username == "" {
		t.Skip("set TAKO_TEST_SSH_HOME and TAKO_TEST_SSH_USER in a disposable VM")
	}
	if filepath.IsAbs(home) == false {
		t.Fatal("TAKO_TEST_SSH_HOME must be absolute")
	}
	account := sshKeyAccount{username: username, home: filepath.Clean(home), uid: os.Getuid(), gid: os.Getgid()}
	dependencies := sshKeyDependencies{
		lookup: func(string) (sshKeyAccount, error) { return account, nil },
		path:   authorizedKeysPath,
	}
	preview, err := PreviewSSHKeysWithDependencies(context.Background(), SSHKeyOperation{Action: "add", Username: username, Key: testAuthorizedKey, Preview: true}, username, false, dependencies)
	if err != nil {
		t.Fatal("SSH key preview failed")
	}
	state, err := applySSHKeysWithDependencies(context.Background(), SSHKeyOperation{Action: "add", Username: username, Key: testAuthorizedKey, ExpectedFingerprint: preview.Current.Fingerprint}, username, false, dependencies)
	if err != nil || len(state.Keys) != 1 {
		t.Fatal("SSH key add failed")
	}
	removePreview, err := PreviewSSHKeysWithDependencies(context.Background(), SSHKeyOperation{Action: "remove", Username: username, Fingerprint: state.Keys[0].Fingerprint, Preview: true}, username, false, dependencies)
	if err != nil {
		t.Fatal("SSH key remove preview failed")
	}
	if _, err := applySSHKeysWithDependencies(context.Background(), SSHKeyOperation{Action: "remove", Username: username, Fingerprint: state.Keys[0].Fingerprint, ExpectedFingerprint: removePreview.Current.Fingerprint, Confirmation: "REMOVE KEY " + state.Keys[0].Fingerprint}, username, false, dependencies); err != nil {
		t.Fatal("SSH key remove failed")
	}
}
