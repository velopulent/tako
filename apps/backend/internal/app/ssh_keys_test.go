package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
)

func TestSSHKeyRoutesUseAuthorityStalePreviewAndSafeReceipts(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	var previewed, applied platform.SSHKeyOperation
	server.previewSSHKeysFn = func(_ context.Context, request auth.SSHKeyRequest) (platform.SSHKeyPreview, error) {
		previewed = request.Operation
		return platform.SSHKeyPreview{Action: request.Operation.Action, Username: request.Operation.Username, Current: platform.SSHKeyState{Username: request.Operation.Username, Fingerprint: strings.Repeat("a", 64)}, Changes: []string{"add key"}, Allowed: true}, nil
	}
	server.applySSHKeysFn = func(_ context.Context, request auth.SSHKeyRequest) (platform.SSHKeyState, error) {
		applied = request.Operation
		return platform.SSHKeyState{Username: request.Operation.Username, Fingerprint: strings.Repeat("b", 64)}, nil
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: session.CookieName, Value: created.ID}
	get := httptest.NewRequest(http.MethodGet, "/api/v1/users/ssh-keys", nil)
	get.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, get)
	if recorder.Code != http.StatusOK || previewed.Action != "list" || previewed.Username != "operator" {
		t.Fatalf("self key list returned %d body=%s operation=%#v", recorder.Code, recorder.Body.String(), previewed)
	}
	preview := httptest.NewRequest(http.MethodPost, "/api/v1/users/ssh-keys/preview", bytes.NewBufferString(`{"action":"add","username":"operator","key":"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB public-comment"}`))
	preview.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, preview)
	if recorder.Code != http.StatusOK || !previewed.Preview {
		t.Fatalf("key preview returned %d body=%s operation=%#v", recorder.Code, recorder.Body.String(), previewed)
	}
	apply := httptest.NewRequest(http.MethodPost, "/api/v1/users/ssh-keys", bytes.NewBufferString(`{"action":"add","username":"operator","key":"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB public-comment","expectedFingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	apply.AddCookie(cookie)
	apply.Header.Set("X-CSRF-Token", created.CSRF)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, apply)
	if recorder.Code != http.StatusOK || applied.Preview || applied.ExpectedFingerprint == "" {
		t.Fatalf("key apply returned %d body=%s operation=%#v", recorder.Code, recorder.Body.String(), applied)
	}
	receipts, err := server.preferences.OperationReceipts(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 || receipts[0].Target != "user/operator/ssh-keys/add" || strings.Contains(receipts[0].Target+receipts[0].Error, "public-comment") {
		t.Fatalf("unsafe SSH key receipt: %#v", receipts)
	}

	other := httptest.NewRequest(http.MethodGet, "/api/v1/users/ssh-keys?username=target", nil)
	other.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, other)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin target list returned %d", recorder.Code)
	}

	if !server.sessions.SetAdministrative(created.ID, "admin-token", time.Now().Add(time.Hour)) {
		t.Fatal("failed to grant administrative test session")
	}
	trailing := httptest.NewRequest(http.MethodPost, "/api/v1/users/ssh-keys/preview", bytes.NewBufferString(`{"action":"add","username":"target","key":"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB key"}{}`))
	trailing.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, trailing)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("trailing key payload returned %d", recorder.Code)
	}
}
