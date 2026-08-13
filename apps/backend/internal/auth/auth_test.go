package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestIdentityDoesNotExposeAdministrativeToken(t *testing.T) {
	payload, err := json.Marshal(Identity{Username: "octopus", AdminToken: "admin-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "admin-secret") || strings.Contains(string(payload), "AdminToken") {
		t.Fatalf("administrative token leaked: %s", payload)
	}
}

func TestSocketConversationStopsReadingWhenContextIsCanceled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("Unix listeners unavailable in this sandbox: %v", err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		close(accepted)
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, callErr := (SocketAuthenticator{Path: path}).AdvanceConversation(ctx, &ConversationRequest{Username: "octopus"})
		done <- callErr
	}()
	<-accepted
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled conversation unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled context did not interrupt socket read")
	}
}

func TestServiceActionStopsReadingWhenContextIsCanceled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("Unix listeners unavailable in this sandbox: %v", err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		close(accepted)
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServiceAction(ctx, path, "bridge", "user", "shell.service", "restart")
	}()
	<-accepted
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled service action unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled service action did not interrupt socket read")
	}
}

func TestSocketAuthenticatorReportsUnavailableService(t *testing.T) {
	authenticator := SocketAuthenticator{Path: filepath.Join(t.TempDir(), "missing.sock")}
	_, err := authenticator.Authenticate(context.Background(), "octopus", "secret")
	if !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("expected unavailable service, got %v", err)
	}
}

func TestConversationRequestShapesAreExclusive(t *testing.T) {
	tests := []struct {
		name  string
		value ConversationRequest
		valid bool
	}{
		{name: "start", value: ConversationRequest{Username: "octopus", Password: "secret"}, valid: true},
		{name: "continue", value: ConversationRequest{ConversationID: "conversation", Responses: []PromptResponse{{ID: "otp", Value: "123456"}}}, valid: true},
		{name: "cancel", value: ConversationRequest{ConversationID: "conversation", Cancel: true}, valid: true},
		{name: "empty", value: ConversationRequest{}, valid: false},
		{name: "mixed", value: ConversationRequest{Username: "octopus", ConversationID: "conversation", Responses: []PromptResponse{{ID: "otp"}}}, valid: false},
		{name: "cancel with response", value: ConversationRequest{ConversationID: "conversation", Cancel: true, Responses: []PromptResponse{{ID: "otp"}}}, valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.value.Valid() != test.valid {
				t.Fatalf("Valid() = %v, want %v", test.value.Valid(), test.valid)
			}
		})
	}
}
