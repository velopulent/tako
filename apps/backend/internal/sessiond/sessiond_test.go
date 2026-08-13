package sessiond

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

func TestGrantStore(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	identity := auth.Identity{Username: "octopus", UID: 1000, GID: 1000}
	token, err := store.add(identity)
	if err != nil || token == "" {
		t.Fatalf("add grant: token=%q err=%v", token, err)
	}
	got, ok := store.get(token)
	if !ok || got.Username != identity.Username {
		t.Fatalf("get grant: got=%+v ok=%v", got, ok)
	}
	if _, ok := store.claim(token); !ok {
		t.Fatal("first terminal claim rejected")
	}
	if _, ok := store.claim(token); ok {
		t.Fatal("concurrent terminal claim accepted")
	}
	store.release(token)
	store.values[token] = bridgeGrant{identity: identity, expires: time.Now().Add(-time.Second)}
	if _, ok := store.get(token); ok {
		t.Fatal("expired grant accepted")
	}
}

func TestExpiredGrantCleansTerminalBridgeAndPAM(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	reader, terminal, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	bridge := &trackingCloser{}
	var pamClosed atomic.Bool
	store.values["expired"] = bridgeGrant{
		identity: auth.Identity{Username: "octopus"},
		expires:  time.Now().Add(-time.Second),
		terminal: terminal,
		bridge:   bridge,
		closePAM: func() {
			_ = bridge.Close()
			pamClosed.Store(true)
		},
	}
	if _, ok := store.get("expired"); ok {
		t.Fatal("expired grant was accepted")
	}
	if _, err := terminal.Write([]byte("x")); err == nil {
		t.Fatal("expired grant left terminal open")
	}
	if bridge.calls.Load() != 1 {
		t.Fatalf("expired grant closed bridge %d times, want 1", bridge.calls.Load())
	}
	if !pamClosed.Load() {
		t.Fatal("expired grant did not close PAM resources")
	}
}

func TestTerminalRegistrationFailsAfterGrantCloses(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	token, err := store.add(auth.Identity{Username: "octopus"})
	if err != nil {
		t.Fatal(err)
	}
	store.close(token)
	reader, terminal, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer terminal.Close()
	if store.setTerminal(token, terminal) {
		t.Fatal("terminal registered after grant closure")
	}
}

type multiRoundOpener struct {
	canceled atomic.Bool
	closed   atomic.Bool
}

type endlessOpener struct{}

func (*endlessOpener) OpenSessionWithConversation(_ string, conversation auth.Conversation) (auth.UserSession, error) {
	for index := 0; index <= maxConversationRounds; index++ {
		if _, err := conversation(auth.PromptInfo, "Continue"); err != nil {
			return auth.UserSession{}, err
		}
	}
	return auth.UserSession{}, errors.New("unexpected completion")
}

type blockedOpener struct{ release <-chan struct{} }

func (opener *blockedOpener) OpenSessionWithConversation(_ string, _ auth.Conversation) (auth.UserSession, error) {
	<-opener.release
	return auth.UserSession{}, errors.New("released")
}

type trackingCloser struct{ calls atomic.Int32 }

func (closer *trackingCloser) Close() error {
	closer.calls.Add(1)
	return nil
}

func (opener *multiRoundOpener) OpenSessionWithConversation(username string, conversation auth.Conversation) (auth.UserSession, error) {
	password, err := conversation(auth.PromptHidden, "Password:")
	if err != nil {
		opener.canceled.Store(true)
		return auth.UserSession{}, err
	}
	if _, err := conversation(auth.PromptInfo, "Additional verification required"); err != nil {
		opener.canceled.Store(true)
		return auth.UserSession{}, err
	}
	code, err := conversation(auth.PromptText, "Verification code:")
	if err != nil {
		opener.canceled.Store(true)
		return auth.UserSession{}, err
	}
	if username != "octopus" || password != "secret" || code != "123456" {
		return auth.UserSession{}, errors.New("authentication failed")
	}
	return auth.UserSession{
		Identity: auth.Identity{Username: username, UID: 1000, GID: 1000},
		Close:    func() { opener.closed.Store(true) },
	}, nil
}

func TestConversationSupportsMultiplePromptTypesAndRounds(t *testing.T) {
	opener := &multiRoundOpener{}
	store := newConversationStore(opener, func(session auth.UserSession) (string, error) {
		return "bridge-token", nil
	})
	defer store.closeAll()

	response, err := store.advance(context.Background(), auth.ConversationRequest{Username: "octopus"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Prompts) != 1 || response.Prompts[0].Style != auth.PromptHidden {
		t.Fatalf("unexpected first prompt: %#v", response)
	}
	response, err = store.advance(context.Background(), auth.ConversationRequest{
		ConversationID: response.ConversationID,
		Responses:      []auth.PromptResponse{{ID: response.Prompts[0].ID, Value: "secret"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Prompts[0].Style != auth.PromptInfo {
		t.Fatalf("unexpected information prompt: %#v", response)
	}
	response, err = store.advance(context.Background(), auth.ConversationRequest{
		ConversationID: response.ConversationID,
		Responses:      []auth.PromptResponse{{ID: response.Prompts[0].ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Prompts[0].Style != auth.PromptText {
		t.Fatalf("unexpected verification prompt: %#v", response)
	}
	response, err = store.advance(context.Background(), auth.ConversationRequest{
		ConversationID: response.ConversationID,
		Responses:      []auth.PromptResponse{{ID: response.Prompts[0].ID, Value: "123456"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Identity == nil || response.BridgeToken != "bridge-token" || response.Identity.Username != "octopus" {
		t.Fatalf("unexpected completion: %#v", response)
	}
}

func TestConversationRejectsMismatchedAndOversizedResponses(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		id    string
	}{
		{name: "mismatched prompt", id: "wrong", value: "secret"},
		{name: "oversized response", value: strings.Repeat("x", maxResponseBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			opener := &multiRoundOpener{}
			store := newConversationStore(opener, func(auth.UserSession) (string, error) { return "", nil })
			response, err := store.advance(context.Background(), auth.ConversationRequest{Username: "octopus"})
			if err != nil {
				t.Fatal(err)
			}
			id := test.id
			if id == "" {
				id = response.Prompts[0].ID
			}
			_, err = store.advance(context.Background(), auth.ConversationRequest{
				ConversationID: response.ConversationID,
				Responses:      []auth.PromptResponse{{ID: id, Value: test.value}},
			})
			if !errors.Is(err, errConversationInvalid) {
				t.Fatalf("expected invalid conversation, got %v", err)
			}
			for deadline := time.Now().Add(time.Second); !opener.canceled.Load() && time.Now().Before(deadline); {
				time.Sleep(time.Millisecond)
			}
			if !opener.canceled.Load() {
				t.Fatal("canceled conversation did not stop PAM worker")
			}
		})
	}
}

func TestConversationStopsWhenRequestIsCanceled(t *testing.T) {
	opener := &multiRoundOpener{}
	store := newConversationStore(opener, func(auth.UserSession) (string, error) { return "", nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.advance(ctx, auth.ConversationRequest{Username: "octopus"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled conversation, got %v", err)
	}
	for deadline := time.Now().Add(time.Second); !opener.canceled.Load() && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if !opener.canceled.Load() {
		t.Fatal("request cancellation did not stop PAM worker")
	}
}

func TestConversationEnforcesRoundLimit(t *testing.T) {
	store := newConversationStore(&endlessOpener{}, func(auth.UserSession) (string, error) { return "", nil })
	response, err := store.advance(context.Background(), auth.ConversationRequest{Username: "octopus"})
	if err != nil {
		t.Fatal(err)
	}
	for round := 1; round < maxConversationRounds; round++ {
		response, err = store.advance(context.Background(), auth.ConversationRequest{
			ConversationID: response.ConversationID,
			Responses:      []auth.PromptResponse{{ID: response.Prompts[0].ID}},
		})
		if err != nil {
			t.Fatalf("round %d failed early: %v", round, err)
		}
	}
	_, err = store.advance(context.Background(), auth.ConversationRequest{
		ConversationID: response.ConversationID,
		Responses:      []auth.PromptResponse{{ID: response.Prompts[0].ID}},
	})
	if !errors.Is(err, errConversationBounds) {
		t.Fatalf("expected round bound, got %v", err)
	}
}

func TestConversationCapsConcurrentAttemptsPerUser(t *testing.T) {
	store := newConversationStore(&multiRoundOpener{}, func(auth.UserSession) (string, error) { return "", nil })
	defer store.closeAll()
	for attempt := 0; attempt < maxConversationsPerUser; attempt++ {
		if _, err := store.advance(context.Background(), auth.ConversationRequest{Username: "octopus"}); err != nil {
			t.Fatalf("attempt %d rejected early: %v", attempt, err)
		}
	}
	if _, err := store.advance(context.Background(), auth.ConversationRequest{Username: "octopus"}); !errors.Is(err, errConversationBusy) {
		t.Fatalf("expected busy response, got %v", err)
	}
}

func TestCanceledBlockedPAMWorkersStillCountTowardCap(t *testing.T) {
	release := make(chan struct{})
	store := newConversationStore(&blockedOpener{release: release}, func(auth.UserSession) (string, error) { return "", nil })
	for index := 0; index < maxConcurrentConversations; index++ {
		username := "user-" + strconv.Itoa(index)
		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, _ = store.advance(ctx, auth.ConversationRequest{Username: username})
		}()
	}
	for deadline := time.Now().Add(time.Second); ; {
		store.mu.Lock()
		workers := store.workers
		store.mu.Unlock()
		if workers == maxConcurrentConversations {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("started %d PAM workers, want %d", workers, maxConcurrentConversations)
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := store.advance(context.Background(), auth.ConversationRequest{Username: "new-user"}); !errors.Is(err, errConversationBusy) {
		t.Fatalf("expected busy while PAM workers remain blocked, got %v", err)
	}
	close(release)
}

func TestGrantCloseEndsPAMSession(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	var closed atomic.Bool
	bridge := &trackingCloser{}
	token, err := store.addGrant(auth.Identity{Username: "octopus"}, bridge, func() { closed.Store(true) })
	if err != nil {
		t.Fatal(err)
	}
	store.close(token)
	if !closed.Load() {
		t.Fatal("grant close did not end PAM session")
	}
	if bridge.calls.Load() != 1 {
		t.Fatalf("grant close stopped user bridge %d times, want 1", bridge.calls.Load())
	}
	store.close(token)
	if bridge.calls.Load() != 1 {
		t.Fatal("repeated grant close stopped user bridge twice")
	}
}

func TestGrantStoreAdministrativeGrantIsSeparateAndRevocable(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridgeToken, err := store.add(auth.Identity{Username: "octopus", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	var receivedPassword string
	adminToken, until, err := store.authorize(context.Background(), bridgeToken, "secret", 300, func(_ context.Context, identity auth.Identity, password string) error {
		if identity.Username != "octopus" {
			t.Fatalf("policy identity = %+v", identity)
		}
		receivedPassword = password
		return nil
	})
	if err != nil || adminToken == "" || until.Before(time.Now()) {
		t.Fatalf("authorize returned token=%q until=%s err=%v", adminToken, until, err)
	}
	if receivedPassword != "secret" {
		t.Fatalf("policy received password %q", receivedPassword)
	}
	if identity, ok := store.adminIdentity(adminToken); !ok || identity.Username != "octopus" {
		t.Fatalf("administrative grant not usable: %+v, %v", identity, ok)
	}
	if !store.revokeAdmin(adminToken) {
		t.Fatal("administrative grant was not revoked")
	}
	if _, ok := store.adminIdentity(adminToken); ok {
		t.Fatal("revoked administrative grant remained usable")
	}
	if _, ok := store.get(bridgeToken); !ok {
		t.Fatal("revoking administrative access closed the user session")
	}
}

func TestSystemServiceActionRequiresAdministrativeGrant(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridgeToken, err := store.add(auth.Identity{Username: "octopus", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handle(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop())
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "service-action", Token: bridgeToken, Scope: "system", Unit: "sshd.service", Action: "restart"}); err != nil {
		t.Fatal(err)
	}
	var response auth.Response
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "invalid-administrative-request" {
		t.Fatalf("system action without admin grant returned %q", response.Error)
	}
}

type recordingServiceBackend struct {
	mu     sync.Mutex
	called []serviceOperation
	err    error
}

func (backend *recordingServiceBackend) Run(_ context.Context, operation serviceOperation) error {
	backend.mu.Lock()
	backend.called = append(backend.called, operation)
	backend.mu.Unlock()
	return backend.err
}

func (backend *recordingServiceBackend) operations() []serviceOperation {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return append([]serviceOperation(nil), backend.called...)
}

func TestServiceActionsUseTheMatchingUNIXGrant(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridgeToken, err := store.add(auth.Identity{Username: "octopus", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := store.authorize(context.Background(), bridgeToken, "secret", 300, func(context.Context, auth.Identity, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		request auth.Request
		want    serviceOperation
	}{
		{name: "user", request: auth.Request{Operation: "service-action", Token: bridgeToken, Scope: "user", Unit: "demo.service", Action: "restart"}, want: serviceOperation{Scope: "user", Unit: "demo.service", Action: "restart"}},
		{name: "system", request: auth.Request{Operation: "service-action", AdminToken: adminToken, Scope: "system", Unit: "demo.service", Action: "reload"}, want: serviceOperation{Scope: "system", Unit: "demo.service", Action: "reload"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &recordingServiceBackend{}
			serverConn, clientConn := net.Pipe()
			defer clientConn.Close()
			go handleWithBackends(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, backend)
			if err := json.NewEncoder(clientConn).Encode(test.request); err != nil {
				t.Fatal(err)
			}
			var response auth.Response
			if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if response.Error != "" {
				t.Fatalf("service action failed: %q", response.Error)
			}
			called := backend.operations()
			if len(called) != 1 || called[0] != test.want {
				t.Fatalf("backend calls = %#v, want %#v", called, test.want)
			}
		})
	}
}

type recordingTimerBackend struct {
	operation platform.TimerOperation
	bridge    io.Closer
	state     platform.TimerState
}

func (backend *recordingTimerBackend) Apply(_ context.Context, operation platform.TimerOperation, bridge io.Closer) (platform.TimerState, error) {
	backend.operation = operation
	backend.bridge = bridge
	return backend.state, nil
}

func TestTimerOperationUsesUserBridgeAndStructuredPayload(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridge := &trackingCloser{}
	token, err := store.addGrant(auth.Identity{Username: "octopus", UID: 1000, GID: 1000}, bridge, nil)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recordingTimerBackend{state: platform.TimerState{Scope: "user", Name: "nightly", TimerUnit: "nightly.timer", ServiceUnit: "nightly.service"}}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handleWithTimerBackends(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, backend, systemOverrideBackend{})
	operation := platform.TimerOperation{Action: "preview", Scope: "user", Name: "nightly"}
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "timer", Token: token, Timer: &operation}); err != nil {
		t.Fatal(err)
	}
	var response auth.Response
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" || response.TimerState == nil {
		t.Fatalf("timer operation failed: %#v", response)
	}
	if backend.operation != operation || backend.bridge != bridge {
		t.Fatalf("timer backend received operation=%#v bridge=%T, want operation=%#v bridge=%T", backend.operation, backend.bridge, operation, bridge)
	}
}

func TestTimerOperationRejectsMixedFields(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	backend := &recordingTimerBackend{}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handleWithTimerBackends(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, backend, systemOverrideBackend{})
	operation := platform.TimerOperation{Action: "preview", Scope: "user", Name: "nightly"}
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "timer", Token: "token", Unit: "unsafe", Timer: &operation}); err != nil {
		t.Fatal(err)
	}
	var response auth.Response
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "invalid-timer-request" {
		t.Fatalf("mixed timer request error=%q", response.Error)
	}
}

type recordingOverrideBackend struct {
	operation platform.OverrideOperation
	bridge    io.Closer
	state     platform.OverrideState
}

func (backend *recordingOverrideBackend) Apply(_ context.Context, operation platform.OverrideOperation, bridge io.Closer) (platform.OverrideState, error) {
	backend.operation = operation
	backend.bridge = bridge
	return backend.state, nil
}

func TestServiceOverrideUsesAdminGrantAndAllowlistedPayload(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridge := &trackingCloser{}
	bridgeToken, err := store.addGrant(auth.Identity{Username: "octopus", UID: 1000, GID: 1000}, bridge, nil)
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := store.authorize(context.Background(), bridgeToken, "secret", 300, func(context.Context, auth.Identity, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	backend := &recordingOverrideBackend{state: platform.OverrideState{Scope: "system", Unit: "worker.service", Exists: true}}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handleWithTimerBackends(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, systemPowerBackend{}, systemTimerBackend{}, backend)
	operation := platform.OverrideOperation{Action: "preview", Scope: "system", Unit: "worker.service"}
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "service-override", AdminToken: adminToken, Override: &operation}); err != nil {
		t.Fatal(err)
	}
	var response auth.Response
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" || response.OverrideState == nil || !reflect.DeepEqual(backend.operation, operation) || backend.bridge != nil {
		t.Fatalf("override response=%#v operation=%#v bridge=%T", response, backend.operation, backend.bridge)
	}
}

type fakeHostConfigBackend struct {
	current platform.HostConfiguration
	applied platform.HostConfiguration
}

func (backend *fakeHostConfigBackend) Read(context.Context) (platform.HostConfiguration, error) {
	return backend.current, nil
}

func (backend *fakeHostConfigBackend) Apply(_ context.Context, _, desired platform.HostConfiguration) error {
	backend.applied = desired
	backend.current = desired
	return nil
}

func TestHostConfigurationUsesAdminGrantAndRejectsStaleWrites(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridgeToken, err := store.add(auth.Identity{Username: "octopus", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := store.authorize(context.Background(), bridgeToken, "secret", 300, func(context.Context, auth.Identity, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeHostConfigBackend{current: platform.NewHostConfiguration("old-host", "UTC", true)}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handle(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), backend)
	if err := json.NewEncoder(clientConn).Encode(auth.Request{
		Operation:           "host-config",
		AdminToken:          adminToken,
		Hostname:            "new-host",
		Timezone:            "Asia/Kolkata",
		NTPEnabled:          false,
		ExpectedFingerprint: backend.current.Fingerprint,
	}); err != nil {
		t.Fatal(err)
	}
	var response auth.Response
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" || backend.applied.Hostname != "new-host" || backend.applied.NTPEnabled {
		t.Fatalf("host configuration was not applied: response=%#v applied=%#v", response, backend.applied)
	}

	serverConn, clientConn = net.Pipe()
	defer clientConn.Close()
	go handle(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), backend)
	_ = json.NewEncoder(clientConn).Encode(auth.Request{
		Operation:           "host-config",
		AdminToken:          adminToken,
		Hostname:            "another-host",
		Timezone:            "UTC",
		NTPEnabled:          true,
		ExpectedFingerprint: "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "host-config-conflict" {
		t.Fatalf("stale host configuration returned %q", response.Error)
	}
}
