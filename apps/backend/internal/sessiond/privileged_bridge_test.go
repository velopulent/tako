package sessiond

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

// recordingPrivilegedBridge records administrative calls.
type recordingPrivilegedBridge struct {
	mu      sync.Mutex
	methods []string
	closed  atomic.Bool
}

func (bridge *recordingPrivilegedBridge) call(_ context.Context, method string, _ any) (json.RawMessage, error) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.methods = append(bridge.methods, method)
	return json.RawMessage(`{}`), nil
}

func (bridge *recordingPrivilegedBridge) Close() error {
	bridge.closed.Store(true)
	return nil
}

func (bridge *recordingPrivilegedBridge) alive() bool { return !bridge.closed.Load() }

type privilegedRecorder struct {
	mu      sync.Mutex
	bridges []*recordingPrivilegedBridge
}

func (recorder *privilegedRecorder) start(context.Context) (privilegedBridge, error) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	bridge := &recordingPrivilegedBridge{}
	recorder.bridges = append(recorder.bridges, bridge)
	return bridge, nil
}

func (recorder *privilegedRecorder) count() int {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return len(recorder.bridges)
}

func TestAdministrativeFileWorkRunsInTheAdministrativeBridge(t *testing.T) {
	f := newFixture(t)
	expectError(t, f.call("files.apply", f.root(), platform.FileOperation{Action: "list", Path: "/usr/local"}), "system-backend-unavailable")

	recorder := &privilegedRecorder{}
	f.handler.privileged = recorder.start
	for range 2 {
		if response := f.call("files.apply", f.root(), platform.FileOperation{Action: "list", Path: "/usr/local"}); response.Error != "" {
			t.Fatalf("administrative file call failed: %s", response.Error)
		}
	}
	if recorder.count() != 1 || len(recorder.bridges[0].methods) != 2 || recorder.bridges[0].methods[0] != "file.apply" {
		t.Fatalf("administrative bridge was not shared: %d bridges", recorder.count())
	}
	// The user's own file work never touches the administrative bridge.
	f.call("files.apply", f.user(), platform.FileOperation{Action: "list", Path: "."})
	if !f.bridge.saw("file.apply") || len(recorder.bridges[0].methods) != 2 {
		t.Fatal("user lane file work reached the administrative bridge")
	}
}

func TestAdministrativeBridgeStopsWithTheGrant(t *testing.T) {
	f := newFixture(t)
	recorder := &privilegedRecorder{}
	if _, err := f.store.administrativeBridge(context.Background(), f.admin, recorder.start); err != nil {
		t.Fatal(err)
	}
	first := recorder.bridges[0]

	// Elevating again replaces the grant and its bridge.
	admin, _, err := f.store.authorize(context.Background(), f.session, "secret", 300, func(context.Context, auth.Identity, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !first.closed.Load() {
		t.Fatal("re-elevation left the previous administrative bridge running")
	}
	if _, err := f.store.administrativeBridge(context.Background(), f.admin, recorder.start); err == nil {
		t.Fatal("the replaced administrative token still opened a bridge")
	}
	if _, err := f.store.administrativeBridge(context.Background(), admin, recorder.start); err != nil {
		t.Fatal(err)
	}
	second := recorder.bridges[len(recorder.bridges)-1]
	if !f.store.revokeAdmin(admin) || !second.closed.Load() {
		t.Fatal("dropping administrative access left its bridge running")
	}

	admin, _, _ = f.store.authorize(context.Background(), f.session, "secret", 300, func(context.Context, auth.Identity, string) error { return nil })
	if _, err := f.store.administrativeBridge(context.Background(), admin, recorder.start); err != nil {
		t.Fatal(err)
	}
	third := recorder.bridges[len(recorder.bridges)-1]
	f.store.close(f.session)
	if !third.closed.Load() {
		t.Fatal("signing out left the administrative bridge running")
	}
}

// pipeTerminalBridge hands out one end of an in-memory pipe as the terminal.
type pipeTerminalBridge struct {
	recordingBridge
	shell chan net.Conn
}

func (bridge *pipeTerminalBridge) openTerminal(auth.TerminalSize) (net.Conn, error) {
	sessiond, shell := net.Pipe()
	bridge.shell <- shell
	return sessiond, nil
}

func TestTerminalBytesAreRelayedToTheUsersBridge(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridge := &pipeTerminalBridge{recordingBridge: recordingBridge{results: map[string]string{}}, shell: make(chan net.Conn, 1)}
	token, err := store.addGrant(operator, nil, bridge, nil)
	if err != nil {
		t.Fatal(err)
	}
	store.confirm(token)
	handler := newHandler(auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), newHostRuntime())
	gateway, server := net.Pipe()
	go handler.serve(server)
	request, _ := json.Marshal(auth.Request{Operation: "terminal.open", Token: token, Payload: json.RawMessage(`{"columns":80,"rows":24}`)})
	if _, err := gateway.Write(append(request, '\n')); err != nil {
		t.Fatal(err)
	}
	shell := <-bridge.shell
	go func() { _, _ = gateway.Write([]byte("echo hi\n")) }()
	buffer := make([]byte, 8)
	_ = shell.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(shell, buffer); err != nil || string(buffer) != "echo hi\n" {
		t.Fatalf("shell received %q: %v", buffer, err)
	}
	go func() { _, _ = shell.Write([]byte("hi\r\n")) }()
	reply := make([]byte, 4)
	_ = gateway.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(gateway, reply); err != nil || string(reply) != "hi\r\n" {
		t.Fatalf("gateway received %q: %v", reply, err)
	}
	if _, ok := store.claim(token); ok {
		t.Fatal("a second terminal was allowed while one is open")
	}
	shell.Close()
	gateway.Close()
	time.Sleep(20 * time.Millisecond)
	if _, ok := store.claim(token); !ok {
		t.Fatal("closing the terminal did not release the session's terminal slot")
	}
}
