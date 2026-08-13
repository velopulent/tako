package bridge

import (
	"bufio"
	"bytes"
	"encoding/json"
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

func mustJSON(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return payload
}
