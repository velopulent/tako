package sessiond

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/audit"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

func plan(t *testing.T, operation string, payload any) (auditPlan, bool) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return planAudit(operation, encoded)
}

func TestAuditNamesTargetsButNeverSecrets(t *testing.T) {
	for _, test := range []struct {
		operation string
		payload   map[string]any
		target    string
		secret    string
	}{
		{"passwords.change", map[string]any{"action": "reset", "username": "bob", "newPassword": "hunter2", "confirmation": "hunter2"}, "username=bob", "hunter2"},
		{"timers.apply", map[string]any{"action": "create", "scope": "system", "name": "backup", "command": "curl -H 'token: s3cret'"}, "scope=system name=backup", "s3cret"},
		{"storage.apply", map[string]any{"action": "persistent-mount", "device": "/dev/sdb1", "target": "/srv", "options": "credentials=/root/cifs"}, "device=/dev/sdb1 target=/srv", "credentials"},
		{"ssh-keys.apply", map[string]any{"action": "add", "username": "bob", "key": "ssh-ed25519 AAAAsecret"}, "username=bob", "AAAAsecret"},
		{"overrides.apply", map[string]any{"action": "apply", "scope": "system", "unit": "app.service", "environment": map[string]string{"TOKEN": "s3cret"}}, "scope=system unit=app.service", "s3cret"},
		{"processes.signal", map[string]any{"action": "apply", "signal": "SIGTERM", "target": map[string]any{"pid": 42, "started": 7}}, "signal=SIGTERM pid=42", "started"},
		{"session.conversation", map[string]any{"username": "alice", "password": "s3cret"}, "username=alice", "s3cret"},
	} {
		result, audited := plan(t, test.operation, test.payload)
		if !audited {
			t.Fatalf("%s was not audited", test.operation)
		}
		if result.target != test.target {
			t.Fatalf("%s target = %q, want %q", test.operation, result.target, test.target)
		}
		if strings.Contains(result.target+result.action, test.secret) {
			t.Fatalf("%s leaked %q into the audit record", test.operation, test.secret)
		}
	}
}

func TestAuditSkipsReadsAndPreviews(t *testing.T) {
	for _, test := range []struct {
		operation string
		payload   map[string]any
		audited   bool
	}{
		{"services.list", map[string]any{"scope": "system"}, false},
		{"accounts.local", map[string]any{"action": "delete", "username": "bob", "preview": true}, false},
		{"accounts.local", map[string]any{"action": "delete", "username": "bob"}, true},
		{"network.apply", map[string]any{"action": "preview"}, false},
		{"security.apply", map[string]any{"action": "inspect"}, false},
		{"files.apply", map[string]any{"action": "read", "path": "/etc/hosts"}, false},
		{"files.apply", map[string]any{"action": "write-chunk", "path": "big.iso", "offset": 4194304}, false},
		{"files.apply", map[string]any{"action": "write-chunk", "path": "big.iso", "offset": 0}, true},
		{"files.apply", map[string]any{"action": "delete", "path": "/srv/old"}, true},
	} {
		if _, audited := plan(t, test.operation, test.payload); audited != test.audited {
			t.Fatalf("%s %v audited = %v, want %v", test.operation, test.payload, audited, test.audited)
		}
	}
}

func TestAuditRecordsOperationOutcomesInTheJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.socket")
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	defer audit.UseSocket(path)()

	f := newFixture(t)
	f.handler.services = &recordingServiceBackend{}
	if response := f.call("services.action", f.root(), platform.ServiceOperation{Scope: "system", Unit: "nginx.service", Action: "restart"}); response.Error != "" {
		t.Fatalf("service action failed: %s", response.Error)
	}
	expectError(t, f.call("services.action", f.user(), platform.ServiceOperation{Scope: "system", Unit: "nginx.service", Action: "stop"}), "permission-denied")
	f.call("services.list", f.user(), auth.ServiceReadOperation{Scope: "system"})

	read := func() string {
		_ = listener.SetReadDeadline(time.Now().Add(time.Second))
		buffer := make([]byte, 8192)
		count, err := listener.Read(buffer)
		if err != nil {
			return ""
		}
		return string(buffer[:count])
	}
	succeeded, denied := read(), read()
	for _, field := range []string{"TAKO_AUDIT_ACTOR=operator\n", "TAKO_AUDIT_LANE=administrative\n", "TAKO_AUDIT_ACTION=restart\n", "TAKO_AUDIT_TARGET=scope=system unit=nginx.service\n", "TAKO_AUDIT_RESULT=succeeded\n"} {
		if !strings.Contains(succeeded, field) {
			t.Fatalf("success record lacks %q:\n%s", field, succeeded)
		}
	}
	for _, field := range []string{"TAKO_AUDIT_LANE=user\n", "TAKO_AUDIT_RESULT=failed\n", "TAKO_AUDIT_ERROR=permission-denied\n"} {
		if !strings.Contains(denied, field) {
			t.Fatalf("denial record lacks %q:\n%s", field, denied)
		}
	}
	if extra := read(); extra != "" {
		t.Fatalf("a read was audited:\n%s", extra)
	}
}
