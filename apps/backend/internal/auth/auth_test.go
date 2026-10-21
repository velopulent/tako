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

	"github.com/msteinert/pam/v2"
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

func TestPasswordChangeOperationValidationAndClearing(t *testing.T) {
	change := PasswordChangeOperation{Action: "change", CurrentPassword: "old-secret", NewPassword: "new-secret", Confirmation: "new-secret"}
	if err := ValidatePasswordChangeOperation(change); err != nil {
		t.Fatal(err)
	}
	reset := PasswordChangeOperation{Action: "reset", Username: "target", NewPassword: "new-secret", Confirmation: "new-secret"}
	if err := ValidatePasswordChangeOperation(reset); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []PasswordChangeOperation{
		{Action: "change", CurrentPassword: "old", NewPassword: "new", Confirmation: "different"},
		{Action: "change", Username: "target", CurrentPassword: "old", NewPassword: "new", Confirmation: "new"},
		{Action: "reset", Username: "../target", NewPassword: "new", Confirmation: "new"},
		{Action: "change", CurrentPassword: "old\x00", NewPassword: "new", Confirmation: "new"},
	} {
		if err := ValidatePasswordChangeOperation(invalid); !errors.Is(err, ErrPasswordInvalid) {
			t.Fatalf("invalid operation accepted: %#v err=%v", invalid, err)
		}
	}
	change.Clear()
	if change.CurrentPassword != "" || change.NewPassword != "" || change.Confirmation != "" {
		t.Fatalf("password operation was not cleared: %#v", change)
	}
}

func TestPasswordConversationBoundsAndUsesReplacementOnlyAfterCurrent(t *testing.T) {
	conversation := &passwordConversation{current: "old-secret", new: "new-secret"}
	value, err := conversation.respond(PromptHidden, "Current password")
	if err != nil || value != "old-secret" {
		t.Fatalf("current password prompt returned %q, %v", value, err)
	}
	value, err = conversation.respond(PromptHidden, "New password")
	if err != nil || value != "new-secret" {
		t.Fatalf("replacement password prompt returned %q, %v", value, err)
	}
	for index := conversation.index; index < 6; index++ {
		if _, err := conversation.respond(PromptHidden, "additional"); err != nil {
			t.Fatalf("bounded prompt %d failed early: %v", index, err)
		}
	}
	if _, err := conversation.respond(PromptHidden, "too many"); !errors.Is(err, ErrPasswordInvalid) {
		t.Fatalf("unbounded hidden prompts accepted: %v", err)
	}
	conversation.current = ""
	conversation.new = ""
	if conversation.current != "" || conversation.new != "" {
		t.Fatal("password conversation secrets were not clearable")
	}
}

func TestPasswordErrorsAreSafeAndDistinct(t *testing.T) {
	if !errors.Is(passwordError(pam.ErrAuth), ErrPasswordAuthenticationFailed) {
		t.Fatal("PAM authentication error was not classified as authentication failure")
	}
	if !errors.Is(passwordError(pam.ErrNewAuthtokReqd), ErrPasswordExpired) {
		t.Fatal("expired password error was not classified distinctly")
	}
	if !errors.Is(passwordError(pam.ErrAuthtok), ErrPasswordPolicy) {
		t.Fatal("password policy error was not classified distinctly")
	}
	if strings.Contains(passwordError(pam.ErrAuthtok).Error(), "PAM") {
		t.Fatal("password error exposed PAM implementation details")
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

// serveOnce answers one request on a temporary socket and reports it.
func serveOnce(t *testing.T, response Response) (string, <-chan Request) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("Unix listeners unavailable in this sandbox: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	seen := make(chan Request, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var request Request
		if json.NewDecoder(connection).Decode(&request) == nil {
			seen <- request
			_ = json.NewEncoder(connection).Encode(response)
		}
	}()
	return path, seen
}

func TestCallSendsEnvelopeAndDecodesResult(t *testing.T) {
	path, seen := serveOnce(t, Response{Result: json.RawMessage(`{"scope":"user","name":"nightly"}`)})
	var state platform.TimerState
	operation := platform.TimerOperation{Action: "preview", Scope: "user", Name: "nightly"}
	if err := Call(context.Background(), path, "timers.apply", Credentials{Token: "bridge-token"}, operation, &state, CallOptions{}); err != nil || state.Name != "nightly" {
		t.Fatalf("call failed: %#v %v", state, err)
	}
	request := <-seen
	var payload platform.TimerOperation
	if request.Operation != "timers.apply" || request.Token != "bridge-token" || request.AdminToken != "" || json.Unmarshal(request.Payload, &payload) != nil || payload.Name != "nightly" {
		t.Fatalf("unexpected request: %#v", request)
	}
}

func TestCallMapsRemoteCodesToSentinels(t *testing.T) {
	path, _ := serveOnce(t, Response{Error: "timer-conflict"})
	err := Call(context.Background(), path, "timers.apply", Credentials{AdminToken: "admin"}, nil, nil, CallOptions{})
	if !errors.Is(err, platform.ErrTimerConflict) || err.Error() != "timer-conflict" {
		t.Fatalf("remote error = %v", err)
	}
	if code := ErrorCode(err, "fallback"); code != "timer-conflict" {
		t.Fatalf("ErrorCode(remote) = %q", code)
	}
	if code := ErrorCode(platform.ErrFileConflict, "fallback"); code != "file-conflict" {
		t.Fatalf("ErrorCode(sentinel) = %q", code)
	}
	if code := ErrorCode(errors.New("private detail"), "fallback"); code != "fallback" {
		t.Fatalf("ErrorCode(unknown) = %q", code)
	}
}

func TestSocketAuthenticatorReportsUnavailableService(t *testing.T) {
	authenticator := SocketAuthenticator{Path: filepath.Join(t.TempDir(), "missing.sock")}
	_, err := authenticator.AdvanceConversation(context.Background(), &ConversationRequest{Username: "octopus", Password: "secret"})
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

func TestDevelopmentAuthenticatorRequiresPasswordAndOwnAccount(t *testing.T) {
	authenticator := DevelopmentAuthenticator{Password: "dev-secret"}
	if _, err := authenticator.AdvanceConversation(context.Background(), &ConversationRequest{Password: "wrong"}); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("wrong password = %v", err)
	}
	if _, err := authenticator.AdvanceConversation(context.Background(), &ConversationRequest{Username: "someone-else-entirely", Password: "dev-secret"}); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("other account = %v", err)
	}
	request := &ConversationRequest{Password: "dev-secret"}
	response, err := authenticator.AdvanceConversation(context.Background(), request)
	if err != nil || response.Identity == nil || request.Password != "" {
		t.Fatalf("own account = %#v, %v; password cleared=%v", response, err, request.Password == "")
	}
	if _, err := (DevelopmentAuthenticator{}).AdvanceConversation(context.Background(), &ConversationRequest{}); err == nil {
		t.Fatal("empty development password accepted")
	}
}

func TestDevelopmentAdministrativeAccessRequiresThePassword(t *testing.T) {
	authenticator := DevelopmentAuthenticator{Password: "development"}
	if _, err := authenticator.AuthorizeAdministrative(context.Background(), "", "wrong", time.Minute); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("wrong password err = %v", err)
	}
	access, err := authenticator.AuthorizeAdministrative(context.Background(), "", "development\n123456", time.Minute)
	if err != nil || access.Token == "" || time.Until(access.Until) <= 0 {
		t.Fatalf("access = %+v, %v", access, err)
	}
	if _, err := (DevelopmentAuthenticator{}).AuthorizeAdministrative(context.Background(), "", "", time.Minute); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("empty configured password err = %v", err)
	}
}

func TestConversationRemoteAddressIsAnIPOnTheFirstRequest(t *testing.T) {
	for _, test := range []struct {
		request ConversationRequest
		valid   bool
	}{
		{ConversationRequest{Username: "alice", RemoteAddress: "192.0.2.4"}, true},
		{ConversationRequest{Username: "alice", RemoteAddress: "2001:db8::1"}, true},
		{ConversationRequest{Username: "alice", RemoteAddress: "evil.example\nX"}, false},
		{ConversationRequest{Username: "alice", RemoteAddress: "host.example"}, false},
		{ConversationRequest{ConversationID: "c1", Responses: []PromptResponse{{ID: "p", Value: "x"}}, RemoteAddress: "192.0.2.4"}, false},
		{ConversationRequest{ConversationID: "c1", Cancel: true, RemoteAddress: "192.0.2.4"}, false},
	} {
		if test.request.Valid() != test.valid {
			t.Fatalf("%+v valid = %v, want %v", test.request, !test.valid, test.valid)
		}
	}
}
