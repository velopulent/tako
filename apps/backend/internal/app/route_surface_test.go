package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/session"
)

func TestProtectedRouteSurfaceRequiresSession(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "session", method: http.MethodGet, path: "/api/v1/auth/session"},
		{name: "capabilities", method: http.MethodGet, path: "/api/v1/capabilities"},
		{name: "metrics", method: http.MethodGet, path: "/api/v1/metrics"},
		{name: "terminal status", method: http.MethodGet, path: "/api/v1/terminal"},
		{name: "jobs", method: http.MethodGet, path: "/api/v1/jobs"},
		{name: "storage", method: http.MethodGet, path: "/api/v1/storage"},
		{name: "network", method: http.MethodGet, path: "/api/v1/network"},
		{name: "firewall", method: http.MethodGet, path: "/api/v1/firewall"},
		{name: "security", method: http.MethodGet, path: "/api/v1/security"},
		{name: "incidents", method: http.MethodGet, path: "/api/v1/incidents"},
		{name: "files", method: http.MethodGet, path: "/api/v1/files?path=."},
		{name: "file search", method: http.MethodGet, path: "/api/v1/files/search?path=.&query=x"},
		{name: "file content", method: http.MethodGet, path: "/api/v1/files/content?path=x"},
		{name: "processes", method: http.MethodGet, path: "/api/v1/processes"},
		{name: "updates", method: http.MethodGet, path: "/api/v1/updates"},
		{name: "services", method: http.MethodGet, path: "/api/v1/services"},
		{name: "timers", method: http.MethodPost, path: "/api/v1/timers", body: `{}`},
		{name: "storage mutation", method: http.MethodPost, path: "/api/v1/storage", body: `{}`},
		{name: "network mutation", method: http.MethodPost, path: "/api/v1/network", body: `{}`},
		{name: "firewall mutation", method: http.MethodPost, path: "/api/v1/firewall", body: `{}`},
		{name: "security mutation", method: http.MethodPost, path: "/api/v1/security", body: `{}`},
		{name: "file mutation", method: http.MethodPost, path: "/api/v1/files", body: `{}`},
		{name: "incident mutation", method: http.MethodPost, path: "/api/v1/incidents/1/acknowledged", body: `{}`},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			request.RemoteAddr = "127.0.0.1:12345"
			recorder := httptest.NewRecorder()
			server.routes().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("protected route returned %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestMutationRoutesRejectUnknownJSONFields(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	created, err := server.sessions.Create(auth.Identity{
		Username:    "octopus",
		AdminToken:  "test-admin-token",
		BridgeToken: "test-bridge-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !server.sessions.SetAdministrative(created.ID, created.Identity.AdminToken, time.Now().Add(time.Hour)) {
		t.Fatal("could not grant test administrative session")
	}
	cookie := &http.Cookie{Name: session.CookieName, Value: created.ID}
	csrf := created.CSRF

	cases := []struct {
		name string
		path string
	}{
		{name: "storage", path: "/api/v1/storage/preview"},
		{name: "network", path: "/api/v1/network/preview"},
		{name: "firewall", path: "/api/v1/firewall/preview"},
		{name: "security", path: "/api/v1/security/preview"},
		{name: "updates", path: "/api/v1/updates/preview"},
		{name: "files", path: "/api/v1/files"},
		{name: "file upload", path: "/api/v1/files/upload"},
		{name: "service action", path: "/api/v1/services/user/demo.service/actions/preview"},
		{name: "service override", path: "/api/v1/services/user/demo.service/overrides/preview"},
		{name: "timer", path: "/api/v1/timers/preview"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(`{"unknown":true}`))
			request.AddCookie(cookie)
			request.Header.Set("X-CSRF-Token", csrf)
			recorder := httptest.NewRecorder()
			server.routes().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("unknown field returned %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestBackgroundRequestsDoNotCountAsActivity(t *testing.T) {
	server, _ := newTestServer(t)
	current := signIn(t, server, false)
	before, _ := server.sessions.Peek(current.ID)
	time.Sleep(5 * time.Millisecond)

	for _, path := range []string{"/api/v1/auth/session", "/api/v1/metrics/stream?interval=1s&background=1"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(&http.Cookie{Name: session.CookieName, Value: current.ID})
		if path == "/api/v1/auth/session" {
			request.Header.Set(backgroundHeader, "1")
		}
		if !isBackground(request) {
			t.Fatalf("%s was not treated as background", path)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	request.AddCookie(&http.Cookie{Name: session.CookieName, Value: current.ID})
	request.Header.Set(backgroundHeader, "1")
	server.routes().ServeHTTP(httptest.NewRecorder(), request)
	if after, _ := server.sessions.Peek(current.ID); !after.SeenAt.Equal(before.SeenAt) {
		t.Fatal("background request extended the session")
	}

	activity := httptest.NewRequest(http.MethodPost, "/api/v1/auth/activity", nil)
	activity.Host = "tako.test"
	activity.AddCookie(&http.Cookie{Name: session.CookieName, Value: current.ID})
	activity.Header.Set("X-CSRF-Token", current.CSRF)
	activity.Header.Set("Origin", "http://tako.test")
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, activity)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("activity returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if after, _ := server.sessions.Peek(current.ID); !after.SeenAt.After(before.SeenAt) {
		t.Fatal("activity did not extend the session")
	}
}

func TestUsersSeeAndRevokeOnlyTheirOwnSessions(t *testing.T) {
	server, _ := newTestServer(t)
	current := signIn(t, server, false)
	other, err := server.sessions.Create(auth.Identity{Username: "operator", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := server.sessions.Create(auth.Identity{Username: "stranger", UID: 1001, GID: 1001})
	if err != nil {
		t.Fatal(err)
	}
	send := func(method, path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, nil)
		request.Host = "tako.test"
		request.AddCookie(&http.Cookie{Name: session.CookieName, Value: current.ID})
		request.Header.Set("X-CSRF-Token", current.CSRF)
		request.Header.Set("Origin", "http://tako.test")
		recorder := httptest.NewRecorder()
		server.routes().ServeHTTP(recorder, request)
		return recorder
	}

	listing := send(http.MethodGet, "/api/v1/auth/sessions")
	var body struct {
		Sessions []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"sessions"`
	}
	if listing.Code != http.StatusOK || json.Unmarshal(listing.Body.Bytes(), &body) != nil || len(body.Sessions) != 2 {
		t.Fatalf("listing = %d %s", listing.Code, listing.Body.String())
	}
	for _, item := range body.Sessions {
		if item.ID == current.ID || item.ID == other.ID {
			t.Fatal("listing exposed a cookie value")
		}
		if item.Current != (item.ID == current.PublicID()) {
			t.Fatalf("current flag wrong for %s", item.ID)
		}
	}
	if code := send(http.MethodPost, "/api/v1/auth/sessions/"+stranger.PublicID()+"/revoke").Code; code != http.StatusNotFound {
		t.Fatalf("revoking another account's session returned %d", code)
	}
	if code := send(http.MethodPost, "/api/v1/auth/sessions/"+other.PublicID()+"/revoke").Code; code != http.StatusNoContent {
		t.Fatalf("revoking own session returned %d", code)
	}
	if _, ok := server.sessions.Peek(other.ID); ok {
		t.Fatal("revoked session still works")
	}
	if _, ok := server.sessions.Peek(stranger.ID); !ok {
		t.Fatal("another account's session ended")
	}
}
