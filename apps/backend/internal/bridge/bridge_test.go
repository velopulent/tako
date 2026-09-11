package bridge

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/velopulent/tako/internal/platform"
)

func TestFrameRoundTrip(t *testing.T) {
	buffer := bytes.NewBuffer(nil)
	writer := bufio.NewWriter(buffer)
	want := frame{ID: "request-1", Method: "ping", Payload: json.RawMessage(`{"value":1}`)}
	if err := writeFrame(writer, want); err != nil {
		t.Fatal(err)
	}
	got, err := readFrame(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Method != want.Method || string(got.Payload) != string(want.Payload) {
		t.Fatalf("round trip mismatch: %#v", got)
	}
}

func TestFrameRejectsOversizePayload(t *testing.T) {
	buffer := bytes.NewBuffer([]byte{0, 0x10, 0, 1})
	if _, err := readFrame(buffer); err == nil {
		t.Fatal("oversized frame accepted")
	}
}

func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1, '{'})
	f.Add([]byte{0, 0, 0, 0})
	f.Fuzz(func(_ *testing.T, payload []byte) {
		_, _ = readFrame(bytes.NewReader(payload))
	})
}

func FuzzDecodeHostPayload(f *testing.F) {
	f.Add(`{"scope":"system"}`)
	f.Add(`{"scope":"system"}{}`)
	f.Fuzz(func(_ *testing.T, payload string) {
		var operation struct {
			Scope string `json:"scope"`
		}
		_ = decodeHostPayload([]byte(payload), &operation)
	})
}

func TestTypedHostReadRejectsUnknownAndTrailingFields(t *testing.T) {
	input := bytes.NewBuffer(nil)
	if err := writeFrame(bufio.NewWriter(input), frame{ID: "host-1", Method: "services.read", Payload: json.RawMessage(`{"scope":"system","unknown":true}`)}); err != nil {
		t.Fatal(err)
	}
	output := bytes.NewBuffer(nil)
	if err := Run(input, output, io.Discard); err != nil {
		t.Fatal(err)
	}
	response, err := readFrame(output)
	if err != nil {
		t.Fatal(err)
	}
	if response.Error != "invalid-service-operation" {
		t.Fatalf("unknown host field returned %q", response.Error)
	}

	var operation struct {
		Scope string `json:"scope"`
	}
	if err := decodeHostPayload(json.RawMessage(`{"scope":"system"}{}`), &operation); err == nil {
		t.Fatal("trailing host payload accepted")
	}
}

func TestUserBridgeDoesNotOwnProcessInventory(t *testing.T) {
	if isHostReadMethod("processes.list") || isHostReadMethod("processes.detail") {
		t.Fatal("process inventory must be collected by sessiond, not the user bridge")
	}
}

func TestHostReadErrorCodeClassifiesUserBusPermissionFailure(t *testing.T) {
	err := errors.New("dial unix /run/user/63438/bus: connect: permission denied")
	if code := hostReadErrorCode(err); code != "user-manager-unavailable" {
		t.Fatalf("user bus permission failure code = %q, want user-manager-unavailable", code)
	}
}

func TestRunTimerPreviewIsStructuredAndReadOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	operation := platform.TimerOperation{Action: "preview", Scope: "user", Name: "nightly"}
	input := bytes.NewBuffer(nil)
	if err := writeFrame(bufio.NewWriter(input), frame{ID: "timer-1", Method: "timer.apply", Payload: mustJSON(operation)}); err != nil {
		t.Fatal(err)
	}
	output := bytes.NewBuffer(nil)
	if err := Run(input, output, io.Discard); err != nil {
		t.Fatal(err)
	}
	response, err := readFrame(output)
	if err != nil {
		t.Fatal(err)
	}
	if response.Error != "" {
		t.Fatalf("timer preview failed: %q", response.Error)
	}
	var state platform.TimerState
	if err := json.Unmarshal(response.Payload, &state); err != nil {
		t.Fatal(err)
	}
	if state.Scope != "user" || state.Name != "nightly" || state.Exists {
		t.Fatalf("unexpected timer state: %#v", state)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "systemd", "user")); !os.IsNotExist(err) {
		t.Fatalf("preview created user systemd directory: %v", err)
	}
}

func TestRunServiceOverridePreviewIsReadOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	operation := platform.OverrideOperation{Action: "preview", Scope: "user", Unit: "worker.service"}
	input := bytes.NewBuffer(nil)
	if err := writeFrame(bufio.NewWriter(input), frame{ID: "override-1", Method: "override.apply", Payload: mustJSON(operation)}); err != nil {
		t.Fatal(err)
	}
	output := bytes.NewBuffer(nil)
	if err := Run(input, output, io.Discard); err != nil {
		t.Fatal(err)
	}
	response, err := readFrame(output)
	if err != nil {
		t.Fatal(err)
	}
	if response.Error != "" {
		t.Fatalf("override preview failed: %q", response.Error)
	}
	var state platform.OverrideState
	if err := json.Unmarshal(response.Payload, &state); err != nil {
		t.Fatal(err)
	}
	if state.Scope != "user" || state.Unit != "worker.service" || state.Exists {
		t.Fatalf("unexpected override state: %#v", state)
	}
}

func mustJSON(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return payload
}
