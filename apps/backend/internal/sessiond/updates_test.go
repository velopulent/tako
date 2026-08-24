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

type recordingUpdateBackend struct {
	operation platform.UpdateOperation
	identity  auth.Identity
	err       error
}

func (backend *recordingUpdateBackend) Apply(_ context.Context, operation platform.UpdateOperation, identity auth.Identity) (platform.UpdateResult, error) {
	backend.operation = operation
	backend.identity = identity
	if backend.err != nil {
		return platform.UpdateResult{}, backend.err
	}
	return platform.UpdateResult{Backend: "apt-get", Scope: operation.Scope, Packages: []string{"vim"}, Verified: true, Message: "Updates applied and verified.", Fingerprint: strings.Repeat("b", 64)}, nil
}

func TestUpdateSocketOperationRequiresAdminAndUsesTypedBackend(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridgeToken, err := store.add(auth.Identity{Username: "operator", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := store.authorize(context.Background(), bridgeToken, "secret", 300, func(context.Context, auth.Identity, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	backend := &recordingUpdateBackend{}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handleWithAllBackendsAndUpdates(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, systemTimerBackend{}, systemOverrideBackend{}, systemPasswordBackend{}, systemSSHKeysBackend{}, systemLocalAccountBackend{}, systemGroupMembershipBackend{}, systemAdministrativeRoleBackend{}, backend, nil, nil)
	operation := platform.UpdateOperation{Scope: "selected", Packages: []string{"vim"}, ExpectedFingerprint: strings.Repeat("a", 64), Confirmation: "APPLY UPDATES"}
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "updates", AdminToken: adminToken, Updates: &operation}); err != nil {
		t.Fatal(err)
	}
	var response auth.Response
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" || response.UpdateResult == nil || backend.operation.Scope != "selected" || backend.identity.Username != "operator" {
		t.Fatalf("update operation failed: response=%#v backend=%#v", response, backend)
	}

	serverConn, clientConn = net.Pipe()
	defer clientConn.Close()
	go handleWithAllBackendsAndUpdates(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, systemTimerBackend{}, systemOverrideBackend{}, systemPasswordBackend{}, systemSSHKeysBackend{}, systemLocalAccountBackend{}, systemGroupMembershipBackend{}, systemAdministrativeRoleBackend{}, backend, nil, nil)
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "updates", AdminToken: adminToken, Updates: &operation, Token: bridgeToken}); err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "invalid-update-operation" {
		t.Fatalf("mixed update credentials returned %q", response.Error)
	}
}

type recordingAutoUpdatesBackend struct {
	operation platform.AutoUpdatesOperation
	identity  auth.Identity
	config    platform.AutoUpdatesConfig
	err       error
}

func (backend *recordingAutoUpdatesBackend) Apply(_ context.Context, operation platform.AutoUpdatesOperation, identity auth.Identity) (platform.AutoUpdatesConfig, error) {
	backend.operation = operation
	backend.identity = identity
	if backend.err != nil {
		return platform.AutoUpdatesConfig{}, backend.err
	}
	return backend.config, nil
}

func TestAutoUpdatesSocketOperationRequiresAdminAndUsesTypedBackend(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridgeToken, err := store.add(auth.Identity{Username: "operator", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := store.authorize(context.Background(), bridgeToken, "secret", 300, func(context.Context, auth.Identity, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	backend := &recordingAutoUpdatesBackend{config: platform.AutoUpdatesConfig{Available: true, Supported: true, Installed: true, Enabled: true, Type: "security", Provider: "dnf5-automatic"}}
	run := func(request auth.Request) auth.Response {
		serverConn, clientConn := net.Pipe()
		defer clientConn.Close()
		go handleWithAllBackendsAndUpdates(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, systemTimerBackend{}, systemOverrideBackend{}, systemPasswordBackend{}, systemSSHKeysBackend{}, systemLocalAccountBackend{}, systemGroupMembershipBackend{}, systemAdministrativeRoleBackend{}, nil, backend, nil)
		if err := json.NewEncoder(clientConn).Encode(request); err != nil {
			t.Fatal(err)
		}
		var response auth.Response
		if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
			t.Fatal(err)
		}
		return response
	}

	enabled := true
	security := "security"
	day := ""
	timeOfDay := "06:00"
	response := run(auth.Request{
		Operation:   "updates-auto",
		AdminToken:  adminToken,
		AutoUpdates: &platform.AutoUpdatesOperation{Enabled: &enabled, Type: &security, Day: &day, Time: &timeOfDay},
	})
	if response.Error != "" || response.AutoUpdatesConfig == nil || !response.AutoUpdatesConfig.Enabled || backend.operation.Enabled == nil || !*backend.operation.Enabled || backend.identity.Username != "operator" {
		t.Fatalf("auto-updates operation failed: response=%#v backend=%#v", response, backend)
	}

	// A bridge token must not authorize configuration changes.
	backend.operation = platform.AutoUpdatesOperation{}
	badDay := "funday"
	response = run(auth.Request{
		Operation:   "updates-auto",
		AdminToken:  adminToken,
		AutoUpdates: &platform.AutoUpdatesOperation{Enabled: &enabled, Day: &badDay},
	})
	if response.Error != "invalid-auto-updates-operation" {
		t.Fatalf("invalid schedule accepted: %q", response.Error)
	}
	if backend.operation.Day != nil || backend.operation.Enabled != nil {
		t.Fatalf("rejected operation reached the backend: %#v", backend.operation)
	}
}

type recordingKpatchBackend struct {
	operation platform.KpatchOperation
	err       error
}

func (backend *recordingKpatchBackend) Apply(_ context.Context, operation platform.KpatchOperation, _ auth.Identity) (platform.KpatchSettingsStatus, error) {
	backend.operation = operation
	if backend.err != nil {
		return platform.KpatchSettingsStatus{}, backend.err
	}
	return platform.KpatchSettingsStatus{Supported: true, Missing: []string{}, Unavailable: []string{}, Auto: true}, nil
}

func TestKpatchSocketOperationRejectsInvalidAndForwardsTypedOperation(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridgeToken, err := store.add(auth.Identity{Username: "operator", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := store.authorize(context.Background(), bridgeToken, "secret", 300, func(context.Context, auth.Identity, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	backend := &recordingKpatchBackend{}
	run := func(request auth.Request) auth.Response {
		serverConn, clientConn := net.Pipe()
		defer clientConn.Close()
		go handleWithAllBackendsAndUpdates(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, systemTimerBackend{}, systemOverrideBackend{}, systemPasswordBackend{}, systemSSHKeysBackend{}, systemLocalAccountBackend{}, systemGroupMembershipBackend{}, systemAdministrativeRoleBackend{}, nil, nil, backend)
		if err := json.NewEncoder(clientConn).Encode(request); err != nil {
			t.Fatal(err)
		}
		var response auth.Response
		if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
			t.Fatal(err)
		}
		return response
	}

	response := run(auth.Request{Operation: "kpatch", AdminToken: adminToken, Kpatch: &platform.KpatchOperation{}})
	if response.Error != "invalid-kpatch-operation" {
		t.Fatalf("nil apply accepted: %q", response.Error)
	}

	apply := true
	currentOnly := true
	response = run(auth.Request{Operation: "kpatch", AdminToken: adminToken, Kpatch: &platform.KpatchOperation{Apply: &apply, CurrentOnly: &currentOnly}, Updates: &platform.UpdateOperation{Scope: "all"}})
	if response.Error == "" {
		t.Fatal("mixed payload accepted")
	}
	if backend.operation.Apply != nil {
		t.Fatalf("rejected operation reached the backend: %#v", backend.operation)
	}

	response = run(auth.Request{Operation: "kpatch", AdminToken: adminToken, Kpatch: &platform.KpatchOperation{Apply: &apply, CurrentOnly: &currentOnly}})
	if response.Error != "" || response.KpatchSettings == nil || !response.KpatchSettings.Auto {
		t.Fatalf("valid kpatch operation failed: %#v", response)
	}
	if backend.operation.Apply == nil || !*backend.operation.Apply || !*backend.operation.CurrentOnly {
		t.Fatalf("operation not forwarded: %#v", backend.operation)
	}
}
