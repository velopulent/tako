package app

import (
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

func TestLoginHistoryRouteUsesBoundedJournalQueryAndIdentityMetadata(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	var received platform.LoginHistoryQuery
	server.queryLoginHistory = func(_ context.Context, query platform.LoginHistoryQuery) (platform.LoginHistoryPage, error) {
		received = query
		return platform.LoginHistoryPage{
			Identity:   platform.LoginHistoryIdentity{Username: "operator", Source: "local", Present: true},
			Items:      []platform.LoginHistoryEntry{{Timestamp: "2024-01-01T00:00:00Z", Event: "login", Outcome: "success", Service: "sshd", Remote: "192.0.2.10"}},
			NextCursor: platform.EncodeJournalCursor("next-cursor"),
		}, nil
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/operator/login-history?limit=10&outcome=success&since=2024-01-01T00:00:00Z&until=2024-01-02T00:00:00Z&cursor="+platform.EncodeJournalCursor("start"), nil)
	request.AddCookie(&http.Cookie{Name: session.CookieName, Value: created.ID})
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("history returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if received.Username != "operator" || received.Limit != 10 || received.Cursor != "start" || received.Outcome != "success" || received.Since.IsZero() || received.Until.IsZero() {
		t.Fatalf("unexpected history query: %#v", received)
	}
	if !strings.Contains(recorder.Body.String(), `"source":"local"`) || !strings.Contains(recorder.Body.String(), `"nextCursor":"`+platform.EncodeJournalCursor("next-cursor")+`"`) {
		t.Fatalf("identity or cursor metadata missing: %s", recorder.Body.String())
	}
}

func TestLoginHistoryRouteProtectsOtherIdentitiesAndAllowsDeletedAdminTarget(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	server.queryLoginHistory = func(_ context.Context, query platform.LoginHistoryQuery) (platform.LoginHistoryPage, error) {
		return platform.LoginHistoryPage{Identity: platform.LoginHistoryIdentity{Username: query.Username, Source: "deleted-unknown"}, Items: []platform.LoginHistoryEntry{}}, nil
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: session.CookieName, Value: created.ID}
	denied := httptest.NewRequest(http.MethodGet, "/api/v1/users/deleted-user/login-history", nil)
	denied.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, denied)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("other identity without admin returned %d", recorder.Code)
	}
	if !server.sessions.SetAdministrative(created.ID, "admin-token", time.Now().Add(time.Hour)) {
		t.Fatal("failed to grant administrative test session")
	}
	allowed := httptest.NewRequest(http.MethodGet, "/api/v1/users/deleted-user/login-history", nil)
	allowed.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, allowed)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "deleted-unknown") {
		t.Fatalf("deleted identity with admin returned %d: %s", recorder.Code, recorder.Body.String())
	}
	invalid := httptest.NewRequest(http.MethodGet, "/api/v1/users/operator/login-history?limit=101", nil)
	invalid.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, invalid)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("oversized history limit returned %d", recorder.Code)
	}
}
