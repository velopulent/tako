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

func TestGroupRoutesPreviewApplyAndRejectUnknownFields(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	var groupPreviewed platform.GroupMembershipOperation
	var roleApplied platform.AdministrativeRoleOperation
	server.previewGroupFn = func(_ context.Context, request auth.GroupMembershipRequest) (platform.GroupMembershipPreview, error) {
		groupPreviewed = request.Operation
		return platform.GroupMembershipPreview{Action: request.Operation.Action, Username: request.Operation.Username, Group: request.Operation.Group, Allowed: true}, nil
	}
	server.applyAdminRoleFn = func(_ context.Context, request auth.AdministrativeRoleRequest) (platform.AdministrativeRoleState, error) {
		roleApplied = request.Operation
		return platform.AdministrativeRoleState{Username: request.Operation.Username, Role: request.Operation.Role, Fingerprint: strings.Repeat("b", 64)}, nil
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if !server.sessions.SetAdministrative(created.ID, "admin-token", time.Now().Add(time.Hour)) {
		t.Fatal("failed to grant administrative test session")
	}
	cookie := &http.Cookie{Name: session.CookieName, Value: created.ID}
	preview := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/groups/membership/preview", bytes.NewBufferString(`{"action":"add","username":"target","group":"developers"}`))
	preview.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, preview)
	if recorder.Code != http.StatusOK || !groupPreviewed.Preview {
		t.Fatalf("group preview status=%d body=%s operation=%#v", recorder.Code, recorder.Body.String(), groupPreviewed)
	}
	apply := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/groups/admin-role", bytes.NewBufferString(`{"action":"grant","username":"target","role":"administrator","expectedFingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","confirmation":"GRANT ADMIN target"}`))
	apply.AddCookie(cookie)
	apply.Header.Set("X-CSRF-Token", created.CSRF)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, apply)
	if recorder.Code != http.StatusOK || roleApplied.Preview || roleApplied.Confirmation != "GRANT ADMIN target" {
		t.Fatalf("role apply status=%d body=%s operation=%#v", recorder.Code, recorder.Body.String(), roleApplied)
	}
	unsafe := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/groups/membership/preview", bytes.NewBufferString(`{"action":"add","username":"target","group":"developers","command":"id"}`))
	unsafe.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, unsafe)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown group field returned %d", recorder.Code)
	}
}
