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

func TestLocalAccountRoutesPreviewApplyAndRequireAdmin(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	var previewed, applied platform.LocalAccountOperation
	server.previewAccountFn = func(_ context.Context, request auth.LocalAccountRequest) (platform.LocalAccountPreview, error) {
		previewed = request.Operation
		return platform.LocalAccountPreview{Action: request.Operation.Action, Username: request.Operation.Username, Current: platform.LocalAccountState{Username: request.Operation.Username, Fingerprint: strings.Repeat("a", 64)}, Allowed: true}, nil
	}
	server.applyAccountFn = func(_ context.Context, request auth.LocalAccountRequest) (platform.LocalAccountState, error) {
		applied = request.Operation
		return platform.LocalAccountState{Username: request.Operation.Username, Exists: true, Fingerprint: strings.Repeat("b", 64)}, nil
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if !server.sessions.SetAdministrative(created.ID, "admin-token", time.Now().Add(time.Hour)) {
		t.Fatal("failed to grant administrative test session")
	}
	cookie := &http.Cookie{Name: session.CookieName, Value: created.ID}
	preview := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/users/account/preview", bytes.NewBufferString(`{"action":"update","username":"target","name":"Target"}`))
	preview.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, preview)
	if recorder.Code != http.StatusOK || !previewed.Preview {
		t.Fatalf("preview status=%d body=%s operation=%#v", recorder.Code, recorder.Body.String(), previewed)
	}
	apply := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/users/account", bytes.NewBufferString(`{"action":"delete","username":"target","expectedFingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","confirmation":"DELETE target"}`))
	apply.AddCookie(cookie)
	apply.Header.Set("X-CSRF-Token", created.CSRF)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, apply)
	if recorder.Code != http.StatusOK || applied.Preview || applied.Confirmation != "DELETE target" {
		t.Fatalf("apply status=%d body=%s operation=%#v", recorder.Code, recorder.Body.String(), applied)
	}
	trailing := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/users/account/preview", bytes.NewBufferString(`{"action":"update","username":"target","name":"Target"}{}`))
	trailing.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, trailing)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("trailing account request returned %d", recorder.Code)
	}
	readonly, err := server.sessions.Create(auth.Identity{Username: "readonly"})
	if err != nil {
		t.Fatal(err)
	}
	denied := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/users/account/preview", bytes.NewBufferString(`{"action":"create","username":"newuser"}`))
	denied.AddCookie(&http.Cookie{Name: session.CookieName, Value: readonly.ID})
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, denied)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin account preview returned %d", recorder.Code)
	}
}
