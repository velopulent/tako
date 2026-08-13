package sessiond

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

func TestParsePowerOperationRequiresTypedConfirmationAndFingerprint(t *testing.T) {
	if err := parsePowerOperation("reboot", "REBOOT", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		action       string
		confirmation string
		fingerprint  string
	}{
		{action: "reboot", confirmation: "shutdown", fingerprint: strings.Repeat("a", 64)},
		{action: "poweroff", confirmation: "SHUTDOWN", fingerprint: strings.Repeat("a", 64)},
		{action: "shutdown", confirmation: "SHUTDOWN", fingerprint: "short"},
	} {
		if err := parsePowerOperation(test.action, test.confirmation, test.fingerprint); err == nil {
			t.Fatalf("unsafe power operation accepted: %#v", test)
		}
	}
}

type fakePowerBackend struct {
	status platform.PowerStatus
	action string
}

func (backend *fakePowerBackend) Read(context.Context) (platform.PowerStatus, error) {
	return backend.status, nil
}

func (backend *fakePowerBackend) Request(_ context.Context, action string) error {
	backend.action = action
	return nil
}

func TestPowerRequestRequiresAdminGrantAndFingerprint(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	bridgeToken, err := store.add(auth.Identity{Username: "octopus", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := store.authorize(context.Background(), bridgeToken, "secret", 300, func(context.Context, auth.Identity, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	status := platform.PowerStatus{Available: true, Reboot: platform.PowerActionStatus{State: "available", Available: true}, Shutdown: platform.PowerActionStatus{State: "available", Available: true}}
	status.Fingerprint = "a" + strings.Repeat("b", 63)
	backend := &fakePowerBackend{status: status}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handleWithBackends(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, backend)
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "power", AdminToken: adminToken, PowerAction: "reboot", PowerConfirmation: "REBOOT", ExpectedFingerprint: status.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	var response auth.Response
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" || backend.action != "reboot" {
		t.Fatalf("power request was not accepted: response=%#v action=%q", response, backend.action)
	}

	serverConn, clientConn = net.Pipe()
	defer clientConn.Close()
	go handleWithBackends(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop(), systemHostConfigBackend{}, backend)
	_ = json.NewEncoder(clientConn).Encode(auth.Request{Operation: "power", AdminToken: adminToken, PowerAction: "shutdown", PowerConfirmation: "SHUTDOWN", ExpectedFingerprint: strings.Repeat("0", 64)})
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "power-conflict" {
		t.Fatalf("stale power request returned %q", response.Error)
	}
}
