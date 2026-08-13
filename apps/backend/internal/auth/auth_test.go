package auth

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityDoesNotExposeBridgeToken(t *testing.T) {
	payload, err := json.Marshal(Identity{Username: "octopus", BridgeToken: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "secret") || strings.Contains(string(payload), "BridgeToken") {
		t.Fatalf("bridge token leaked: %s", payload)
	}
}

func TestSocketAuthenticatorReportsUnavailableService(t *testing.T) {
	authenticator := SocketAuthenticator{Path: filepath.Join(t.TempDir(), "missing.sock")}
	_, err := authenticator.Authenticate(context.Background(), "octopus", "secret")
	if !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("expected unavailable service, got %v", err)
	}
}
