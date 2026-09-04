package sessiond

import (
	"context"
	"sync"
	"testing"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/platform"
)

type recordingUserBridge struct {
	mu    sync.Mutex
	calls []string
}

func (bridge *recordingUserBridge) record(call string) {
	bridge.mu.Lock()
	bridge.calls = append(bridge.calls, call)
	bridge.mu.Unlock()
}

func (bridge *recordingUserBridge) saw(call string) bool {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	for _, current := range bridge.calls {
		if current == call {
			return true
		}
	}
	return false
}

func (bridge *recordingUserBridge) Close() error { return nil }

func (bridge *recordingUserBridge) readServices(context.Context, auth.ServiceReadOperation) ([]platform.Unit, error) {
	bridge.record("services.read")
	return []platform.Unit{{Name: "from-user-bridge.service", Scope: "system"}}, nil
}

func (bridge *recordingUserBridge) readUnitDetails(context.Context, auth.ServiceReadOperation) (platform.UnitDetail, error) {
	bridge.record("services.detail")
	return platform.UnitDetail{}, nil
}

func (bridge *recordingUserBridge) readUnitConfiguration(context.Context, auth.ServiceReadOperation) (platform.UnitConfiguration, error) {
	bridge.record("services.configuration")
	return platform.UnitConfiguration{}, nil
}

func (bridge *recordingUserBridge) serviceAction(context.Context, platform.ServiceOperation) error {
	bridge.record("services.action")
	return nil
}

func (bridge *recordingUserBridge) readHostInfo(context.Context) (host.Info, error) {
	bridge.record("host.info")
	return host.Info{}, nil
}

func (bridge *recordingUserBridge) readHostConfiguration(context.Context) (platform.HostConfiguration, error) {
	bridge.record("host.configuration.read")
	return platform.HostConfiguration{}, nil
}

func (bridge *recordingUserBridge) readPowerStatus(context.Context) (platform.PowerStatus, error) {
	bridge.record("host.power.read")
	return platform.PowerStatus{}, nil
}

func (bridge *recordingUserBridge) previewProcessSignal(context.Context, platform.SignalOperation) (platform.SignalPreview, error) {
	bridge.record("processes.signal-preview")
	return platform.SignalPreview{}, nil
}

func (bridge *recordingUserBridge) signalProcesses(context.Context, platform.SignalOperation) (platform.SignalResult, error) {
	bridge.record("processes.signal")
	return platform.SignalResult{}, nil
}

func (bridge *recordingUserBridge) readIdentityInventory(context.Context) (platform.IdentityInventory, error) {
	bridge.record("identities.read")
	return platform.IdentityInventory{}, nil
}

func (bridge *recordingUserBridge) readFilesystems(context.Context) ([]platform.Filesystem, error) {
	bridge.record("storage.read")
	return []platform.Filesystem{}, nil
}

func (bridge *recordingUserBridge) readNetworkSnapshot(context.Context) (platform.NetworkSnapshot, error) {
	bridge.record("network.read")
	return platform.NetworkSnapshot{}, nil
}

func (bridge *recordingUserBridge) readUpdateStatus(context.Context) (platform.UpdateStatus, error) {
	bridge.record("updates.read")
	return platform.UpdateStatus{}, nil
}

func (bridge *recordingUserBridge) readUpdateHistory(context.Context) ([]platform.UpdateHistoryEntry, error) {
	bridge.record("updates.history")
	return []platform.UpdateHistoryEntry{}, nil
}

func (bridge *recordingUserBridge) readUpdateObservation(context.Context) (auth.UpdateObservation, error) {
	bridge.record("updates.live")
	return auth.UpdateObservation{}, nil
}

func (bridge *recordingUserBridge) readKpatch(context.Context) (platform.KpatchStatus, platform.KpatchSettingsStatus, error) {
	bridge.record("updates.kpatch.read")
	return platform.KpatchStatus{}, platform.KpatchSettingsStatus{}, nil
}

func (bridge *recordingUserBridge) readCapabilities(context.Context) ([]platform.Capability, error) {
	bridge.record("capabilities.read")
	return []platform.Capability{}, nil
}

func (bridge *recordingUserBridge) readLoginHistory(context.Context, auth.LoginHistoryReadOperation) (platform.LoginHistoryPage, error) {
	bridge.record("login-history.read")
	return platform.LoginHistoryPage{}, nil
}

func newReadTestStore(bridge *recordingUserBridge) (*grantStore, string) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	token, err := store.addGrant(auth.Identity{Username: "operator", UID: 1000, GID: 1000}, bridge, nil)
	if err != nil {
		panic(err)
	}
	return store, token
}

func TestHostOperationUsesAuthenticatedUserBridge(t *testing.T) {
	bridge := &recordingUserBridge{}
	store, token := newReadTestStore(bridge)
	runtime := newHostRuntime()
	defer store.closeAll()
	defer runtime.close()

	result, err := dispatchHostOperation(context.Background(), auth.Request{
		Operation:   "services.read",
		Token:       token,
		ServiceRead: &auth.ServiceReadOperation{Scope: "system", Type: "service"},
	}, store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if result.lane != "user-bridge" || len(result.response.Units) != 1 || result.response.Units[0].Name != "from-user-bridge.service" {
		t.Fatalf("unexpected user-lane result: %+v", result)
	}
	if !bridge.saw("services.read") {
		t.Fatal("system service read bypassed authenticated user bridge")
	}
}

func TestUpdateStatusOperationUsesSharedSessiondProvider(t *testing.T) {
	bridge := &recordingUserBridge{}
	store, token := newReadTestStore(bridge)
	runtime := newHostRuntime()
	defer store.closeAll()
	defer runtime.close()

	// The gateway reads inventory as "updates.status"; it must dispatch like
	// "updates.read" instead of falling through with an empty response.
	for _, operation := range []string{"updates.read", "updates.status"} {
		result, err := dispatchHostOperation(context.Background(), auth.Request{
			Operation:  operation,
			Token:      token,
			UpdateRead: &auth.UpdateReadOperation{},
		}, store, runtime)
		if err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
		if result.lane != "root-sessiond" || result.response.UpdateStatus == nil {
			t.Fatalf("%s: unexpected sessiond result: %+v", operation, result)
		}
	}
	if bridge.saw("updates.read") {
		t.Fatal("update status should not start a per-user package-manager read")
	}
}

func TestHostOperationRoutesUserSignalToBridge(t *testing.T) {
	bridge := &recordingUserBridge{}
	store, token := newReadTestStore(bridge)
	runtime := newHostRuntime()
	defer store.closeAll()
	defer runtime.close()

	operation := platform.SignalOperation{
		Action:          "apply",
		Signal:          "TERM",
		Target:          platform.ProcessTarget{PID: 42, Started: 99},
		ExpectedTargets: []platform.ProcessTarget{{PID: 42, Started: 99}},
	}
	result, err := dispatchHostOperation(context.Background(), auth.Request{
		Operation:   "processes.signal",
		Token:       token,
		SignalApply: &operation,
	}, store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if result.lane != "user-bridge" || result.response.SignalResult == nil {
		t.Fatalf("unexpected user signal result: %+v", result)
	}
	if !bridge.saw("processes.signal") {
		t.Fatal("user process signal bypassed authenticated user bridge")
	}
}

func TestProcessInventoryUsesSharedSessiondReaderForOrdinaryGrant(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	token, err := store.add(auth.Identity{Username: "operator", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newHostRuntime()
	defer store.closeAll()
	defer runtime.close()

	result, err := dispatchHostOperation(context.Background(), auth.Request{
		Operation:   "processes.list",
		Token:       token,
		ProcessRead: &auth.ProcessReadOperation{},
	}, store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if result.lane != "root-sessiond" {
		t.Fatalf("ordinary process inventory lane = %q, want root-sessiond", result.lane)
	}
	if result.response.Processes == nil {
		t.Fatal("sessiond process inventory was nil")
	}

	detail, err := dispatchHostOperation(context.Background(), auth.Request{
		Operation:   "processes.detail",
		Token:       token,
		ProcessRead: &auth.ProcessReadOperation{PID: 0},
	}, store, runtime)
	if err == nil || err.Error() != "process not found" {
		t.Fatalf("ordinary process detail error = %v, want process not found", err)
	}
	if detail.lane != "root-sessiond" {
		t.Fatalf("ordinary process detail lane = %q, want root-sessiond", detail.lane)
	}
}

func TestHostOperationRejectsAdministrativeUserScope(t *testing.T) {
	bridge := &recordingUserBridge{}
	store, token := newReadTestStore(bridge)
	adminToken, _, err := store.authorize(context.Background(), token, "ignored", 300, func(context.Context, auth.Identity, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	runtime := newHostRuntime()
	defer store.closeAll()
	defer runtime.close()

	_, err = dispatchHostOperation(context.Background(), auth.Request{
		Operation:   "services.read",
		AdminToken:  adminToken,
		ServiceRead: &auth.ServiceReadOperation{Scope: "user", Type: "service"},
	}, store, runtime)
	if err == nil || err.Error() != "permission-denied" {
		t.Fatalf("administrative user-scope read error = %v, want permission-denied", err)
	}
	if bridge.saw("services.read") {
		t.Fatal("administrative user-scope read reached user bridge")
	}
}

func TestHostOperationRejectsForgedIdentityFields(t *testing.T) {
	request := auth.Request{
		Operation:   "services.read",
		Token:       "bridge-token",
		Username:    "forged-user",
		ServiceRead: &auth.ServiceReadOperation{Scope: "system", Type: "service"},
	}
	if validHostRequest(request) {
		t.Fatal("host operation accepted caller-supplied identity")
	}
	request.Username = ""
	request.AdminToken = "forged-admin"
	if validHostRequest(request) {
		t.Fatal("host operation accepted forged dual authority")
	}
}

func TestHostOperationRejectsForgedLoginHistoryIdentity(t *testing.T) {
	bridge := &recordingUserBridge{}
	store, token := newReadTestStore(bridge)
	runtime := newHostRuntime()
	defer store.closeAll()
	defer runtime.close()

	_, err := dispatchHostOperation(context.Background(), auth.Request{
		Operation: "login-history.read",
		Token:     token,
		LoginHistoryRead: &auth.LoginHistoryReadOperation{Query: platform.LoginHistoryQuery{
			Username: "another-user",
		}},
	}, store, runtime)
	if err == nil || err.Error() != "permission-denied" {
		t.Fatalf("forged login-history identity error = %v, want permission-denied", err)
	}
	if bridge.saw("login-history.read") {
		t.Fatal("forged login-history identity reached user bridge")
	}
}

func TestHostOperationReportsUnavailableBridgeWithoutFallback(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	token, err := store.add(auth.Identity{Username: "operator", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newHostRuntime()
	defer store.closeAll()
	defer runtime.close()

	_, err = dispatchHostOperation(context.Background(), auth.Request{
		Operation:   "services.read",
		Token:       token,
		ServiceRead: &auth.ServiceReadOperation{Scope: "system", Type: "service"},
	}, store, runtime)
	if err == nil || err.Error() != "user-manager-unavailable" {
		t.Fatalf("missing bridge error = %v, want user-manager-unavailable", err)
	}
}

func TestCertificateReadDoesNotRequireUserBus(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	token, err := store.add(auth.Identity{Username: "operator", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newHostRuntime()
	defer store.closeAll()
	defer runtime.close()

	result, err := dispatchHostOperation(context.Background(), auth.Request{
		Operation:       "certificate.read",
		Token:           token,
		CertificateRead: &auth.CertificateReadOperation{},
	}, store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if result.lane != "root-sessiond" || result.response.CertificateStatus == nil || result.response.CertificateStatus.Configured {
		t.Fatalf("unexpected certificate response: %+v", result.response.CertificateStatus)
	}
}
