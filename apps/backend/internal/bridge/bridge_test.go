package bridge

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"
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
