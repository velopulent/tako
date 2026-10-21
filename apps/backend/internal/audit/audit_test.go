package audit

import (
	"bytes"
	"encoding/binary"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmitWritesStructuredJournalFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.socket")
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	defer UseSocket(path)()

	record := Record{Actor: "alice", UID: 1000, Administrative: true, Operation: "services.action", Action: "restart", Target: "unit=nginx.service", Result: "succeeded"}
	if err := Emit(record); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 8192)
	count, err := listener.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	entry := string(buffer[:count])
	for _, field := range []string{
		"MESSAGE_ID=" + MessageID + "\n",
		"MESSAGE=audit: alice services.action restart unit=nginx.service (administrative) succeeded\n",
		"PRIORITY=5\n",
		"TAKO_AUDIT_ACTOR=alice\n",
		"TAKO_AUDIT_LANE=administrative\n",
		"TAKO_AUDIT_TARGET=unit=nginx.service\n",
	} {
		if !strings.Contains(entry, field) {
			t.Fatalf("journal entry lacks %q:\n%s", field, entry)
		}
	}
	if strings.Contains(entry, "TAKO_AUDIT_ERROR") {
		t.Fatal("empty fields were sent")
	}
}

func TestEncodeUsesBinaryFormForMultilineValues(t *testing.T) {
	encoded := encode([][2]string{{"MESSAGE", "line one\nline two"}})
	var expected bytes.Buffer
	expected.WriteString("MESSAGE\n")
	_ = binary.Write(&expected, binary.LittleEndian, uint64(len("line one\nline two")))
	expected.WriteString("line one\nline two\n")
	if !bytes.Equal(encoded, expected.Bytes()) {
		t.Fatalf("encoded %q", encoded)
	}
}
