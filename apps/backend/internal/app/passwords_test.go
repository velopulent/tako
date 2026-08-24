package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/session"
)

func TestPasswordRouteKeepsSecretsOutOfResponsesAndReceipts(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	var captured auth.PasswordChangeOperation
	server.changePasswordFn = func(_ context.Context, request auth.PasswordChangeRequest) error {
		captured = request.Operation
		return nil
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: session.CookieName, Value: created.ID}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/users/password", bytes.NewBufferString(`{"action":"change","currentPassword":"old-secret","newPassword":"new-secret","confirmation":"new-secret"}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", created.CSRF)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Fatalf("password route returned %d body=%q", recorder.Code, recorder.Body.String())
	}
	if captured.Action != "change" || captured.CurrentPassword != "old-secret" || captured.NewPassword != "new-secret" {
		t.Fatalf("unexpected password request: %#v", captured)
	}
	receipts, err := server.preferences.OperationReceipts(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 || receipts[0].Target != "user/operator/password-change" || strings.Contains(receipts[0].Target+receipts[0].Error, "secret") {
		t.Fatalf("unsafe password receipt: %#v", receipts)
	}
	captured.Clear()
}

func TestPasswordRouteRequiresAdministrativeResetAndMapsPolicy(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	server.changePasswordFn = func(context.Context, auth.PasswordChangeRequest) error {
		return auth.ErrPasswordPolicy
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: session.CookieName, Value: created.ID}
	reset := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/users/password", bytes.NewBufferString(`{"action":"reset","username":"target","newPassword":"new-secret","confirmation":"new-secret"}`))
	reset.AddCookie(cookie)
	reset.Header.Set("X-CSRF-Token", created.CSRF)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, reset)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "administrative-access-required") {
		t.Fatalf("unauthorized reset returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if !server.sessions.SetAdministrative(created.ID, "admin-token", time.Now().Add(time.Hour)) {
		t.Fatal("failed to grant test administrative access")
	}
	reset = httptest.NewRequest(http.MethodPost, "/api/v1/accounts/users/password", bytes.NewBufferString(`{"action":"reset","username":"target","newPassword":"new-secret","confirmation":"new-secret"}`))
	reset.AddCookie(cookie)
	reset.Header.Set("X-CSRF-Token", created.CSRF)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, reset)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "password-policy-failed") || strings.Contains(recorder.Body.String(), "new-secret") {
		t.Fatalf("policy response leaked or mapped incorrectly: %d %s", recorder.Code, recorder.Body.String())
	}

	server.changePasswordFn = func(context.Context, auth.PasswordChangeRequest) error {
		return errors.New("password-authentication-failed")
	}
	change := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/users/password", bytes.NewBufferString(`{"action":"change","currentPassword":"old-secret","newPassword":"new-secret","confirmation":"new-secret"}`))
	change.AddCookie(cookie)
	change.Header.Set("X-CSRF-Token", created.CSRF)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, change)
	if recorder.Code != http.StatusForbidden || strings.Contains(recorder.Body.String(), "old-secret") {
		t.Fatalf("authentication failure response unsafe: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestPasswordRouteRejectsTrailingAndMismatchedPayload(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: session.CookieName, Value: created.ID}
	for _, payload := range []string{
		`{"action":"change","currentPassword":"old","newPassword":"new","confirmation":"different"}`,
		`{"action":"change","currentPassword":"old","newPassword":"new","confirmation":"new"}{}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/users/password", bytes.NewBufferString(payload))
		request.AddCookie(cookie)
		request.Header.Set("X-CSRF-Token", created.CSRF)
		recorder := httptest.NewRecorder()
		server.routes().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid password payload returned %d: %s", recorder.Code, recorder.Body.String())
		}
	}
}
