package sessiond

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

// add registers a user session without a bridge or PAM handle.
func (store *grantStore) add(identity auth.Identity) (string, error) {
	return store.addGrant(identity, nil, nil, nil)
}

var operator = auth.Identity{Username: "operator", UID: 1000, GID: 1000}

type recordingBridge struct {
	mu      sync.Mutex
	methods []string
	results map[string]string
}

func (bridge *recordingBridge) call(_ context.Context, _, method string, _ any) (json.RawMessage, error) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.methods = append(bridge.methods, method)
	if result, ok := bridge.results[method]; ok {
		return json.RawMessage(result), nil
	}
	return json.RawMessage(`{}`), nil
}

func (bridge *recordingBridge) Close() error { return nil }

func (bridge *recordingBridge) saw(method string) bool {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	for _, seen := range bridge.methods {
		if seen == method {
			return true
		}
	}
	return false
}

type fixture struct {
	t       *testing.T
	store   *grantStore
	handler *handler
	bridge  *recordingBridge
	session string
	admin   string
}

// newFixture registers one user session with a recording bridge and an
// administrative grant for the same user.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridge := &recordingBridge{results: map[string]string{}}
	session, err := store.addGrant(operator, nil, bridge, nil)
	if err != nil {
		t.Fatal(err)
	}
	store.confirm(session)
	admin, _, err := store.authorize(context.Background(), session, "secret", 300, func(context.Context, auth.Identity, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, store: store, handler: newHandler(auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), newHostRuntime()), bridge: bridge, session: session, admin: admin}
}

func (f *fixture) send(raw string) auth.Response {
	f.t.Helper()
	server, client := net.Pipe()
	defer client.Close()
	go f.handler.serve(server)
	if _, err := client.Write([]byte(raw + "\n")); err != nil {
		f.t.Fatal(err)
	}
	var response auth.Response
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		f.t.Fatal(err)
	}
	return response
}

func (f *fixture) call(operation string, credentials auth.Credentials, payload any) auth.Response {
	f.t.Helper()
	request := auth.Request{Operation: operation, Token: credentials.Token, AdminToken: credentials.AdminToken}
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			f.t.Fatal(err)
		}
		request.Payload = encoded
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.send(string(encoded))
}

func (f *fixture) user() auth.Credentials { return auth.Credentials{Token: f.session} }
func (f *fixture) root() auth.Credentials { return auth.Credentials{AdminToken: f.admin} }
func (f *fixture) both() auth.Credentials {
	return auth.Credentials{Token: f.session, AdminToken: f.admin}
}
func (f *fixture) forged() auth.Credentials { return auth.Credentials{AdminToken: "forged"} }

func expectError(t *testing.T, response auth.Response, code string) {
	t.Helper()
	if response.Error != code {
		t.Fatalf("error = %q, want %q (result %s)", response.Error, code, response.Result)
	}
}

func TestMalformedAndUnknownRequestsAreRejected(t *testing.T) {
	f := newFixture(t)
	expectError(t, f.send(`not json`), "invalid-request")
	expectError(t, f.send(`{"operation":"host.info","token":"x","extra":true}`), "invalid-request")
	expectError(t, f.call("authenticate", auth.Credentials{}, nil), "invalid-request")
	expectError(t, f.call("service-action", f.root(), nil), "invalid-request")
}

func TestCredentialLanesAreEnforced(t *testing.T) {
	f := newFixture(t)
	hostConfiguration := auth.HostConfigurationOperation{Hostname: "tako", Timezone: "UTC", ExpectedFingerprint: strings.Repeat("a", 64)}
	for _, test := range []struct {
		name        string
		operation   string
		credentials auth.Credentials
		payload     any
		code        string
	}{
		{"admin operation with session", "host.configure", f.user(), hostConfiguration, "invalid-request"},
		{"admin operation with forged grant", "host.configure", f.forged(), hostConfiguration, "invalid-admin-token"},
		{"login with credentials", "session.conversation", f.user(), auth.ConversationRequest{Username: "operator"}, "invalid-request"},
		{"either lane with both tokens", "host.info", f.both(), nil, "invalid-request"},
		{"either lane without tokens", "host.info", auth.Credentials{}, nil, "invalid-request"},
		{"unknown session", "host.info", auth.Credentials{Token: "missing"}, nil, "invalid-bridge-token"},
		{"session operation with admin grant", "terminal.resize", f.root(), auth.TerminalSize{Columns: 80, Rows: 24}, "invalid-request"},
	} {
		t.Run(test.name, func(t *testing.T) {
			expectError(t, f.call(test.operation, test.credentials, test.payload), test.code)
		})
	}
}

func TestPayloadsAreDecodedStrictly(t *testing.T) {
	f := newFixture(t)
	response := f.send(`{"operation":"timers.apply","token":"` + f.session + `","payload":{"action":"preview","scope":"user","name":"nightly","shell":"/bin/sh"}}`)
	expectError(t, response, "invalid-request")
}

func TestUserLaneReadsRunInTheUserBridge(t *testing.T) {
	f := newFixture(t)
	f.bridge.results["services.read"] = `[{"name":"demo.service"}]`
	response := f.call("services.list", f.user(), auth.ServiceReadOperation{Scope: "user", Type: "service"})
	if response.Error != "" || !strings.Contains(string(response.Result), "demo.service") {
		t.Fatalf("user service list = %#v", response)
	}
	for _, operation := range []string{"host.info", "identities.list", "network.read"} {
		if response := f.call(operation, f.user(), nil); response.Error != "" {
			t.Fatalf("%s = %q", operation, response.Error)
		}
	}
	for _, method := range []string{"services.read", "host.info", "identities.read", "network.read"} {
		if !f.bridge.saw(method) {
			t.Fatalf("bridge did not see %s; saw %v", method, f.bridge.methods)
		}
	}
}

func TestAdministrativeLaneNeverTouchesUserUnits(t *testing.T) {
	f := newFixture(t)
	expectError(t, f.call("services.list", f.root(), auth.ServiceReadOperation{Scope: "user"}), "permission-denied")
	expectError(t, f.call("services.action", f.root(), platform.ServiceOperation{Scope: "user", Unit: "demo.service", Action: "restart"}), "permission-denied")
	expectError(t, f.call("timers.apply", f.root(), platform.TimerOperation{Action: "preview", Scope: "user", Name: "nightly"}), "permission-denied")
}

type recordingServiceBackend struct{ operations []platform.ServiceOperation }

func (backend *recordingServiceBackend) Run(_ context.Context, operation platform.ServiceOperation) error {
	backend.operations = append(backend.operations, operation)
	return nil
}

func TestServiceActionsUseTheMatchingLane(t *testing.T) {
	f := newFixture(t)
	backend := &recordingServiceBackend{}
	f.handler.services = backend
	system := platform.ServiceOperation{Scope: "system", Unit: "sshd.service", Action: "restart"}
	expectError(t, f.call("services.action", f.user(), system), "permission-denied")
	if response := f.call("services.action", f.root(), system); response.Error != "" {
		t.Fatalf("system action = %q", response.Error)
	}
	if len(backend.operations) != 1 || backend.operations[0] != system {
		t.Fatalf("system backend saw %#v", backend.operations)
	}
	if response := f.call("services.action", f.user(), platform.ServiceOperation{Scope: "user", Unit: "demo.service", Action: "restart"}); response.Error != "" {
		t.Fatalf("user action = %q", response.Error)
	}
	if !f.bridge.saw("services.action") || len(backend.operations) != 1 {
		t.Fatal("user action did not run in the bridge")
	}
	expectError(t, f.call("services.action", f.root(), platform.ServiceOperation{Scope: "system", Unit: "../x", Action: "restart"}), "invalid-service-operation")
}

func TestUserLaneWithoutBridgeFailsClosed(t *testing.T) {
	f := newFixture(t)
	token, err := f.store.add(operator)
	if err != nil {
		t.Fatal(err)
	}
	expectError(t, f.call("host.info", auth.Credentials{Token: token}, nil), "user-bridge-unavailable")
}

func TestLoginHistoryLimitsSessionsToTheirOwnAccount(t *testing.T) {
	f := newFixture(t)
	expectError(t, f.call("login-history.read", f.user(), platform.LoginHistoryQuery{Username: "root", Limit: 10}), "permission-denied")
	if response := f.call("login-history.read", f.user(), platform.LoginHistoryQuery{Username: "operator", Limit: 10}); response.Error != "" {
		t.Fatalf("own history = %q", response.Error)
	}
}

type recordingSSHKeys struct{ administrative []bool }

func (backend *recordingSSHKeys) Preview(_ context.Context, operation platform.SSHKeyOperation, _ auth.Identity, administrative bool) (platform.SSHKeyPreview, error) {
	backend.administrative = append(backend.administrative, administrative)
	return platform.SSHKeyPreview{Username: operation.Username}, nil
}

func (backend *recordingSSHKeys) Apply(_ context.Context, operation platform.SSHKeyOperation, _ auth.Identity, administrative bool) (platform.SSHKeyState, error) {
	backend.administrative = append(backend.administrative, administrative)
	return platform.SSHKeyState{Username: operation.Username}, nil
}

func TestSSHKeysNeedAdministrationForOtherAccounts(t *testing.T) {
	f := newFixture(t)
	backend := &recordingSSHKeys{}
	f.handler.sshKeys = backend
	list := func(username string) platform.SSHKeyOperation {
		return platform.SSHKeyOperation{Action: "list", Username: username, Preview: true}
	}
	expectError(t, f.call("ssh-keys.apply", f.user(), list("root")), "ssh-key-unauthorized")
	if response := f.call("ssh-keys.apply", f.user(), list("operator")); response.Error != "" {
		t.Fatalf("own keys = %q", response.Error)
	}
	if response := f.call("ssh-keys.apply", f.root(), list("root")); response.Error != "" {
		t.Fatalf("administrative keys = %q", response.Error)
	}
	if len(backend.administrative) != 2 || backend.administrative[0] || !backend.administrative[1] {
		t.Fatalf("backend lanes = %v", backend.administrative)
	}
}

type recordingAccounts struct{ previews, applies int }

func (backend *recordingAccounts) Preview(context.Context, platform.LocalAccountOperation, auth.Identity) (platform.LocalAccountPreview, error) {
	backend.previews++
	return platform.LocalAccountPreview{}, nil
}

func (backend *recordingAccounts) Apply(context.Context, platform.LocalAccountOperation, auth.Identity) (platform.LocalAccountState, error) {
	backend.applies++
	return platform.LocalAccountState{}, platform.ErrLocalAccountProtected
}

func TestLocalAccountsUsePreviewFlagAndStableErrors(t *testing.T) {
	f := newFixture(t)
	backend := &recordingAccounts{}
	f.handler.accounts = backend
	operation := platform.LocalAccountOperation{Action: "update", Username: "target", Name: "Target", Preview: true}
	expectError(t, f.call("accounts.local", f.user(), operation), "invalid-request")
	if response := f.call("accounts.local", f.root(), operation); response.Error != "" {
		t.Fatalf("preview = %q", response.Error)
	}
	operation.Preview = false
	operation.ExpectedFingerprint = strings.Repeat("a", 64)
	expectError(t, f.call("accounts.local", f.root(), operation), "local-account-protected")
	if backend.previews != 1 || backend.applies != 1 {
		t.Fatalf("backend calls = %+v", backend)
	}
}

type recordingPasswords struct{ changed, reset string }

func (backend *recordingPasswords) Change(_ context.Context, username, current, replacement string) error {
	if current != "old" {
		return auth.ErrPasswordAuthenticationFailed
	}
	backend.changed = username
	return nil
}

func (backend *recordingPasswords) Reset(_ context.Context, username, _ string) error {
	backend.reset = username
	return nil
}

func TestPasswordChangeAndResetUseSeparateLanes(t *testing.T) {
	f := newFixture(t)
	backend := &recordingPasswords{}
	f.handler.passwords = backend
	change := auth.PasswordChangeOperation{Action: "change", CurrentPassword: "old", NewPassword: "new", Confirmation: "new"}
	reset := auth.PasswordChangeOperation{Action: "reset", Username: "target", NewPassword: "new", Confirmation: "new"}
	expectError(t, f.call("passwords.change", f.root(), change), "invalid-password-operation")
	expectError(t, f.call("passwords.change", f.user(), reset), "invalid-password-operation")
	wrong := change
	wrong.CurrentPassword = "wrong"
	expectError(t, f.call("passwords.change", f.user(), wrong), "password-authentication-failed")
	if response := f.call("passwords.change", f.user(), change); response.Error != "" || backend.changed != "operator" {
		t.Fatalf("change = %q, backend %+v", response.Error, backend)
	}
	if response := f.call("passwords.change", f.root(), reset); response.Error != "" || backend.reset != "target" {
		t.Fatalf("reset = %q, backend %+v", response.Error, backend)
	}
}

type fakeHostConfig struct {
	current platform.HostConfiguration
	applied bool
}

func (backend *fakeHostConfig) Read(context.Context) (platform.HostConfiguration, error) {
	return backend.current, nil
}

func (backend *fakeHostConfig) Apply(context.Context, platform.HostConfiguration, platform.HostConfiguration) error {
	backend.applied = true
	return nil
}

func TestHostConfigurationRejectsStaleAndInvalidWrites(t *testing.T) {
	f := newFixture(t)
	backend := &fakeHostConfig{current: platform.NewHostConfiguration("old", "UTC", true)}
	f.handler.hostConfig = backend
	request := auth.HostConfigurationOperation{Hostname: "tako", Timezone: "UTC", ExpectedFingerprint: strings.Repeat("a", 64)}
	expectError(t, f.call("host.configure", f.root(), request), "host-config-conflict")
	for _, invalid := range []auth.HostConfigurationOperation{
		{Hostname: "../tako", Timezone: "UTC", ExpectedFingerprint: backend.current.Fingerprint},
		{Hostname: "tako", Timezone: "../etc", ExpectedFingerprint: backend.current.Fingerprint},
		{Hostname: "tako", Timezone: "UTC", ExpectedFingerprint: "deadbeef"},
	} {
		expectError(t, f.call("host.configure", f.root(), invalid), "invalid-host-configuration")
	}
	request.ExpectedFingerprint = backend.current.Fingerprint
	if response := f.call("host.configure", f.root(), request); response.Error != "" || !backend.applied {
		t.Fatalf("configure = %q applied=%v", response.Error, backend.applied)
	}
}

type fakePower struct {
	status    platform.PowerStatus
	requested string
}

func (backend *fakePower) Read(context.Context) (platform.PowerStatus, error) {
	return backend.status, nil
}
func (backend *fakePower) Request(_ context.Context, action string) error {
	backend.requested = action
	return nil
}

func TestPowerRequestsNeedConfirmationFingerprintAndAvailability(t *testing.T) {
	f := newFixture(t)
	fingerprint := strings.Repeat("b", 64)
	backend := &fakePower{status: platform.PowerStatus{Fingerprint: fingerprint, Reboot: platform.PowerActionStatus{State: "available", Available: true}, Shutdown: platform.PowerActionStatus{State: "inhibited"}}}
	f.handler.power = backend
	expectError(t, f.call("power.request", f.root(), auth.PowerOperation{Action: "reboot", Confirmation: "reboot", ExpectedFingerprint: fingerprint}), "invalid-power-confirmation")
	expectError(t, f.call("power.request", f.root(), auth.PowerOperation{Action: "reboot", Confirmation: "REBOOT", ExpectedFingerprint: strings.Repeat("c", 64)}), "power-conflict")
	expectError(t, f.call("power.request", f.root(), auth.PowerOperation{Action: "shutdown", Confirmation: "SHUTDOWN", ExpectedFingerprint: fingerprint}), "power-inhibited")
	if response := f.call("power.request", f.root(), auth.PowerOperation{Action: "reboot", Confirmation: "REBOOT", ExpectedFingerprint: fingerprint}); response.Error != "" || backend.requested != "reboot" {
		t.Fatalf("reboot = %q requested=%q", response.Error, backend.requested)
	}
}

func TestSecurityApplyNeverInspectsAndPreviewNeverApplies(t *testing.T) {
	f := newFixture(t)
	expectError(t, f.call("security.apply", f.root(), platform.SecurityOperation{Action: "inspect", Framework: "SELinux"}), "invalid-security-operation")
	if operations["security.preview"].run == nil || operations["security.preview"].lane != laneAdmin {
		t.Fatal("security preview must be a separate administrative operation")
	}
}

func TestJournalRejectsInvalidQueries(t *testing.T) {
	f := newFixture(t)
	expectError(t, f.call("journal.query", f.user(), platform.JournalQuery{Limit: 100000}), "invalid-journal-query")
	expectError(t, f.call("journal.query", auth.Credentials{Token: "missing"}, platform.JournalQuery{Limit: 10}), "invalid-bridge-token")
}

func TestConversationSessionFailureIsReportedOnTheWire(t *testing.T) {
	f := newFixture(t)
	f.handler.conversations = newConversationStore(&immediateOpener{}, func(auth.UserSession) (string, error) {
		return "", errors.New("fork/exec /usr/bin/tako: operation not permitted")
	})
	defer f.handler.conversations.closeAll()
	expectError(t, f.call("session.conversation", auth.Credentials{}, auth.ConversationRequest{Username: "operator", Password: "secret"}), "session-failed")
}

func TestEveryOperationDeclaresAHandler(t *testing.T) {
	for name, spec := range operations {
		if (spec.run == nil) == (spec.takeover == nil) {
			t.Fatalf("%s must define exactly one of run or takeover", name)
		}
	}
}
