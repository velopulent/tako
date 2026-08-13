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

	"github.com/velopulent/tako/internal/platform"
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

func TestApplyHostConfigurationUsesAdminGrantAndStrictPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("Unix listeners unavailable in this sandbox: %v", err)
	}
	defer listener.Close()
	requestSeen := make(chan Request, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var request Request
		if decodeErr := json.NewDecoder(connection).Decode(&request); decodeErr == nil {
			requestSeen <- request
			_ = json.NewEncoder(connection).Encode(Response{})
		}
	}()
	err = ApplyHostConfiguration(context.Background(), path, HostConfigurationRequest{
		AdminToken:          "admin-token",
		Hostname:            "tako",
		Timezone:            "UTC",
		NTPEnabled:          false,
		ExpectedFingerprint: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-requestSeen:
		if request.Operation != "host-config" || request.AdminToken != "admin-token" || request.Hostname != "tako" || request.ExpectedFingerprint != strings.Repeat("a", 64) {
			t.Fatalf("unexpected host configuration request: %#v", request)
		}
	case <-time.After(time.Second):
		t.Fatal("host configuration request was not received")
	}
}

func TestApplyTimerUsesScopedStructuredRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("Unix listeners unavailable in this sandbox: %v", err)
	}
	defer listener.Close()
	requestSeen := make(chan Request, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var request Request
		if decodeErr := json.NewDecoder(connection).Decode(&request); decodeErr == nil {
			requestSeen <- request
			_ = json.NewEncoder(connection).Encode(Response{TimerState: &platform.TimerState{Scope: "user", Name: "nightly"}})
		}
	}()
	state, err := ApplyTimer(context.Background(), path, TimerRequest{
		Token: "bridge-token",
		Operation: platform.TimerOperation{
			Action: "preview",
			Scope:  "user",
			Name:   "nightly",
		},
	})
	if err != nil || state.Name != "nightly" {
		t.Fatalf("timer request failed: %#v %v", state, err)
	}
	select {
	case request := <-requestSeen:
		if request.Operation != "timer" || request.Token != "bridge-token" || request.Timer == nil || request.Timer.Name != "nightly" {
			t.Fatalf("unexpected timer request: %#v", request)
		}
	case <-time.After(time.Second):
		t.Fatal("timer request was not received")
	}
}

func TestApplyOverrideUsesAdminGrantAndTypedPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("Unix listeners unavailable in this sandbox: %v", err)
	}
	defer listener.Close()
	requestSeen := make(chan Request, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var request Request
		if decodeErr := json.NewDecoder(connection).Decode(&request); decodeErr == nil {
			requestSeen <- request
			_ = json.NewEncoder(connection).Encode(Response{OverrideState: &platform.OverrideState{Scope: "system", Unit: "worker.service"}})
		}
	}()
	state, err := ApplyOverride(context.Background(), path, OverrideRequest{
		AdminToken: "admin-token",
		Operation:  platform.OverrideOperation{Action: "preview", Scope: "system", Unit: "worker.service"},
	})
	if err != nil || state.Unit != "worker.service" {
		t.Fatalf("override request failed: %#v %v", state, err)
	}
	select {
	case request := <-requestSeen:
		if request.Operation != "service-override" || request.AdminToken != "admin-token" || request.Override == nil || request.Override.Scope != "system" {
			t.Fatalf("unexpected override request: %#v", request)
		}
	case <-time.After(time.Second):
		t.Fatal("override request was not received")
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
