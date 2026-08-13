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
	go handleWithAllBackendsAndUpdates(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, systemTimerBackend{}, systemOverrideBackend{}, systemPasswordBackend{}, systemSSHKeysBackend{}, systemLocalAccountBackend{}, systemGroupMembershipBackend{}, systemAdministrativeRoleBackend{}, backend)
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
	go handleWithAllBackendsAndUpdates(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, systemTimerBackend{}, systemOverrideBackend{}, systemPasswordBackend{}, systemSSHKeysBackend{}, systemLocalAccountBackend{}, systemGroupMembershipBackend{}, systemAdministrativeRoleBackend{}, backend)
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
