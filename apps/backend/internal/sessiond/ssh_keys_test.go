package sessiond

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

type recordingSSHKeysBackend struct {
	previewed    platform.SSHKeyOperation
	previewAdmin bool
	applied      platform.SSHKeyOperation
	applyAdmin   bool
	previewError error
	applyError   error
}

func (backend *recordingSSHKeysBackend) Preview(_ context.Context, operation platform.SSHKeyOperation, _ auth.Identity, administrative bool) (platform.SSHKeyPreview, error) {
	backend.previewed = operation
	backend.previewAdmin = administrative
	if backend.previewError != nil {
		return platform.SSHKeyPreview{}, backend.previewError
	}
	return platform.SSHKeyPreview{Action: operation.Action, Username: operation.Username, Allowed: true, Current: platform.SSHKeyState{Username: operation.Username, Fingerprint: strings.Repeat("a", 64)}}, nil
}

func (backend *recordingSSHKeysBackend) Apply(_ context.Context, operation platform.SSHKeyOperation, _ auth.Identity, administrative bool) (platform.SSHKeyState, error) {
	backend.applied = operation
	backend.applyAdmin = administrative
	if backend.applyError != nil {
		return platform.SSHKeyState{}, backend.applyError
	}
	return platform.SSHKeyState{Username: operation.Username, Fingerprint: strings.Repeat("b", 64)}, nil
}

func TestSSHKeyOperationsUseSelfAndAdministrativeGrants(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridgeToken, err := store.add(auth.Identity{Username: "operator", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := store.authorize(context.Background(), bridgeToken, "policy-secret", 300, func(context.Context, auth.Identity, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	backend := &recordingSSHKeysBackend{}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handleWithSSHKeysBackend(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, systemTimerBackend{}, systemOverrideBackend{}, backend)
	list := platform.SSHKeyOperation{Action: "list", Username: "operator", Preview: true}
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "ssh-keys", Token: bridgeToken, SSHKeys: &list}); err != nil {
		t.Fatal(err)
	}
	var response auth.Response
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" || response.SSHKeyPreview == nil || backend.previewAdmin || !backend.previewed.Preview {
		t.Fatalf("self SSH key list failed: response=%#v backend=%+v", response, backend)
	}

	serverConn, clientConn = net.Pipe()
	defer clientConn.Close()
	go handleWithSSHKeysBackend(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, systemTimerBackend{}, systemOverrideBackend{}, backend)
	apply := platform.SSHKeyOperation{Action: "add", Username: "target", Key: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB target", ExpectedFingerprint: strings.Repeat("a", 64)}
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "ssh-keys", AdminToken: adminToken, SSHKeys: &apply}); err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" || response.SSHKeyState == nil || !backend.applyAdmin || backend.applied.Key == "" {
		t.Fatalf("administrative SSH key apply failed: response=%#v backend=%+v", response, backend)
	}

	serverConn, clientConn = net.Pipe()
	defer clientConn.Close()
	go handleWithSSHKeysBackend(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, systemTimerBackend{}, systemOverrideBackend{}, backend)
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "ssh-keys", Token: bridgeToken, SSHKeys: &apply}); err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "ssh-key-unauthorized" {
		t.Fatalf("self-service target mismatch returned %q", response.Error)
	}
}
