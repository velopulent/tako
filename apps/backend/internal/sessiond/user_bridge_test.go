package sessiond

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
)

func TestBridgeEnvironmentUsesPAMAndCanonicalAccountValues(t *testing.T) {
	account := &user.User{Username: "octopus", HomeDir: "/home/octopus"}
	environment := bridgeEnvironment(map[string]string{
		"XDG_RUNTIME_DIR": "/run/user/1000",
		"HOME":            "/wrong",
		"USER":            "wrong",
	}, account)
	for _, expected := range []string{
		"HOME=/home/octopus",
		"USER=octopus",
		"LOGNAME=octopus",
		"XDG_RUNTIME_DIR=/run/user/1000",
	} {
		if !slices.Contains(environment, expected) {
			t.Fatalf("bridge environment lacks %q: %#v", expected, environment)
		}
	}
}

func TestUserCredentialIncludesSupplementaryGroups(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := lookupTestIdentity(account)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := userCredential(account, identity)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Uid != uint32(identity.UID) || credential.Gid != uint32(identity.GID) || len(credential.Groups) == 0 {
		t.Fatalf("incomplete user credential: %#v", credential)
	}
}

func lookupTestIdentity(account *user.User) (auth.Identity, error) {
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return auth.Identity{}, err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return auth.Identity{}, err
	}
	return auth.Identity{Username: account.Username, UID: uid, GID: gid}, nil
}

// fakeBridge answers framed requests over pipes. Methods in hold never get a
// response; every frame the bridge receives is recorded.
type fakeBridge struct {
	mu       sync.Mutex
	received []bridgeRequest
	hold     map[string]bool
	output   *io.PipeWriter
	release  chan string
}

func startFakeBridge(t *testing.T, hold ...string) (*userBridgeProcess, *fakeBridge) {
	t.Helper()
	requestReader, requestWriter := io.Pipe()
	responseReader, responseWriter := io.Pipe()
	bridge := &fakeBridge{hold: map[string]bool{}, output: responseWriter, release: make(chan string, 4)}
	for _, method := range hold {
		bridge.hold[method] = true
	}
	go bridge.serve(requestReader)
	process := newUserBridgeProcess(requestWriter, responseReader)
	t.Cleanup(func() {
		_ = process.Close()
		_ = responseWriter.Close()
	})
	return process, bridge
}

func (bridge *fakeBridge) serve(input io.Reader) {
	var writeMu sync.Mutex
	respond := func(id string) {
		encoded, _ := json.Marshal(map[string]any{"id": id, "payload": map[string]string{"id": id}})
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = binary.Write(bridge.output, binary.BigEndian, uint32(len(encoded)))
		_, _ = bridge.output.Write(encoded)
	}
	go func() {
		for id := range bridge.release {
			respond(id)
		}
	}()
	for {
		var size uint32
		if binary.Read(input, binary.BigEndian, &size) != nil {
			return
		}
		encoded := make([]byte, size)
		if _, err := io.ReadFull(input, encoded); err != nil {
			return
		}
		var request bridgeRequest
		_ = json.Unmarshal(encoded, &request)
		bridge.mu.Lock()
		bridge.received = append(bridge.received, request)
		bridge.mu.Unlock()
		if request.Method != bridgeCancelMethod && !bridge.hold[request.Method] {
			respond(request.ID)
		}
	}
}

func (bridge *fakeBridge) frames() []bridgeRequest {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	return append([]bridgeRequest(nil), bridge.received...)
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTimedOutBridgeCallCancelsOnlyThatRequest(t *testing.T) {
	process, bridge := startFakeBridge(t, "files.search")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := process.call(ctx, "files.search", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow call error = %v, want deadline exceeded", err)
	}
	waitFor(t, func() bool {
		frames := bridge.frames()
		return len(frames) == 2 && frames[1].Method == bridgeCancelMethod && frames[1].ID == frames[0].ID
	})
	// The login survives: the same bridge keeps answering.
	if _, err := process.call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("bridge stopped answering after a timeout: %v", err)
	}
}

func TestBridgeResponsesReachTheirOwnCallers(t *testing.T) {
	process, bridge := startFakeBridge(t, "first")
	firstResult := make(chan json.RawMessage, 1)
	go func() {
		raw, _ := process.call(context.Background(), "first", nil)
		firstResult <- raw
	}()
	waitFor(t, func() bool { return len(bridge.frames()) == 1 })
	second, err := process.call(context.Background(), "second", nil)
	if err != nil {
		t.Fatal(err)
	}
	firstID := bridge.frames()[0].ID
	bridge.release <- firstID
	first := <-firstResult
	if !strings.Contains(string(first), firstID) || strings.Contains(string(second), `"`+firstID+`"`) {
		t.Fatalf("responses crossed: first=%s second=%s", first, second)
	}
}

func TestStoppedBridgeFailsPendingAndLaterCalls(t *testing.T) {
	process, bridge := startFakeBridge(t, "hang")
	result := make(chan error, 1)
	go func() {
		_, err := process.call(context.Background(), "hang", nil)
		result <- err
	}()
	waitFor(t, func() bool { return len(bridge.frames()) == 1 })
	_ = bridge.output.Close()
	if err := <-result; err == nil {
		t.Fatal("pending call succeeded after the bridge stopped")
	}
	if _, err := process.call(context.Background(), "ping", nil); err == nil {
		t.Fatal("call succeeded after the bridge stopped")
	}
}
