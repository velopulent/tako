package app

import (
	"bytes"
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
		{name: "notifications", method: http.MethodGet, path: "/api/v1/notifications"},
		{name: "certificates", method: http.MethodGet, path: "/api/v1/certificates"},
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
		{name: "notification mutation", method: http.MethodPost, path: "/api/v1/notifications/1/read", body: `{}`},
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
