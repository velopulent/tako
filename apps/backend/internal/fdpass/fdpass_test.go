package fdpass

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func packetPair(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	left, right, err := Pair(unix.SOCK_SEQPACKET)
	if err != nil {
		t.Fatal(err)
	}
	return left, right
}

func TestSendAndReceiveCarryFiles(t *testing.T) {
	left, right := packetPair(t)
	sender, err := Conn(left)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	receiver, err := Conn(right)
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()

	path := filepath.Join(t.TempDir(), "passed")
	if err := os.WriteFile(path, []byte("handed over"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Send(sender, []byte(`{"type":"opened"}`), file); err != nil {
		t.Fatal(err)
	}
	file.Close()
	payload, files, err := Receive(receiver, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseAll(files)
	if string(payload) != `{"type":"opened"}` || len(files) != 1 {
		t.Fatalf("received %q with %d files", payload, len(files))
	}
	content, err := io.ReadAll(files[0])
	if err != nil || string(content) != "handed over" {
		t.Fatalf("passed file reads %q: %v", content, err)
	}
}

func TestReceiveRejectsOversizeMessagesAndReportsEOF(t *testing.T) {
	left, right := packetPair(t)
	sender, _ := Conn(left)
	receiver, _ := Conn(right)
	defer receiver.Close()
	if err := Send(sender, make([]byte, 64)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Receive(receiver, 16); err == nil {
		t.Fatal("truncated message accepted")
	}
	sender.Close()
	if _, _, err := Receive(receiver, 16); !errors.Is(err, io.EOF) {
		t.Fatalf("closed peer read as %v, want EOF", err)
	}
}
