// Package audit records host-changing operations as structured journal
// entries. Every record carries MessageID, so the whole trail is one query:
//
//	journalctl MESSAGE_ID=6f2a9c4e1b7d4f0a8e3c5b2d9a1f7e60 -o json
package audit

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// MessageID identifies Tako audit records in the journal.
const MessageID = "6f2a9c4e1b7d4f0a8e3c5b2d9a1f7e60"

// journalSocket is the native journald protocol socket; tests replace it.
var journalSocket = "/run/systemd/journal/socket"

// UseSocket sends records to path instead of journald and returns a function
// that restores the default. Tests use it to keep records out of the host
// journal.
func UseSocket(path string) func() {
	previous := journalSocket
	journalSocket = path
	return func() { journalSocket = previous }
}

// Record is one audited operation. Target holds only allowlisted payload
// fields, never secrets or file content.
type Record struct {
	Actor          string
	UID            int
	Administrative bool
	Operation      string
	Action         string
	Target         string
	Result         string
	ErrorCode      string
}

func (record Record) lane() string {
	if record.Administrative {
		return "administrative"
	}
	return "user"
}

// Message is the human-readable summary shown by plain journalctl.
func (record Record) Message() string {
	var message strings.Builder
	fmt.Fprintf(&message, "audit: %s %s", record.Actor, record.Operation)
	if record.Action != "" {
		fmt.Fprintf(&message, " %s", record.Action)
	}
	if record.Target != "" {
		fmt.Fprintf(&message, " %s", record.Target)
	}
	fmt.Fprintf(&message, " (%s) %s", record.lane(), record.Result)
	if record.ErrorCode != "" {
		fmt.Fprintf(&message, ": %s", record.ErrorCode)
	}
	return message.String()
}

// Emit writes record to the journal.
func Emit(record Record) error {
	priority := "5"
	if record.Result != "succeeded" {
		priority = "4"
	}
	fields := [][2]string{
		{"MESSAGE_ID", MessageID},
		{"MESSAGE", record.Message()},
		{"PRIORITY", priority},
		{"SYSLOG_IDENTIFIER", "tako-audit"},
		{"TAKO_AUDIT_ACTOR", record.Actor},
		{"TAKO_AUDIT_UID", strconv.Itoa(record.UID)},
		{"TAKO_AUDIT_LANE", record.lane()},
		{"TAKO_AUDIT_OPERATION", record.Operation},
		{"TAKO_AUDIT_ACTION", record.Action},
		{"TAKO_AUDIT_TARGET", record.Target},
		{"TAKO_AUDIT_RESULT", record.Result},
		{"TAKO_AUDIT_ERROR", record.ErrorCode},
	}
	connection, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: journalSocket, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer connection.Close()
	_, err = connection.Write(encode(fields))
	return err
}

// encode uses the journal export format: KEY=value lines, or the binary form
// for values containing a newline.
func encode(fields [][2]string) []byte {
	var buffer bytes.Buffer
	for _, field := range fields {
		key, value := field[0], field[1]
		if value == "" {
			continue
		}
		if !strings.Contains(value, "\n") {
			buffer.WriteString(key + "=" + value + "\n")
			continue
		}
		buffer.WriteString(key + "\n")
		_ = binary.Write(&buffer, binary.LittleEndian, uint64(len(value)))
		buffer.WriteString(value + "\n")
	}
	return buffer.Bytes()
}
