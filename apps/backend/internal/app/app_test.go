package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/config"
	"github.com/velopulent/tako/internal/session"
)

type unavailableAuthenticator struct{}

func (unavailableAuthenticator) Authenticate(context.Context, string, string) (auth.Identity, error) {
	return auth.Identity{}, auth.ErrServiceUnavailable
}

func TestDevelopmentLoginAndDashboard(t *testing.T) {
	cfg := config.Default()
	cfg.Development = true
	cfg.AllowedOrigins = []string{"http://127.0.0.1:9090"}
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	handler := server.routes()

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"","password":""}`))
	login.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, login)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var cookie *http.Cookie
	for _, candidate := range recorder.Result().Cookies() {
		if candidate.Name == session.CookieName {
			cookie = candidate
		}
	}
	if cookie == nil || cookie.Value == "" {
		t.Fatal("login did not issue session cookie")
	}

	dashboard := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	dashboard.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, dashboard)
	if recorder.Code != http.StatusOK {
		t.Fatalf("dashboard returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["host"] == nil || payload["metrics"] == nil {
		t.Fatalf("dashboard response incomplete: %#v", payload)
	}
}

func TestProtectedRouteRejectsMissingSession(t *testing.T) {
	cfg := config.Default()
	cfg.Development = true
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
}

func TestLoginReportsUnavailableAuthenticationService(t *testing.T) {
	cfg := config.Default()
	cfg.Development = true
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	server.authenticator = unavailableAuthenticator{}

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"octopus","password":"secret"}`))
	login.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, login)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != "authentication-unavailable" {
		t.Fatalf("unexpected problem response: %#v", payload)
	}
}

func TestLogoutRequiresCSRFToken(t *testing.T) {
	cfg := config.Default()
	cfg.Development = true
	cfg.AllowedOrigins = []string{"http://127.0.0.1:9090"}
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	handler := server.routes()
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"","password":""}`))
	login.RemoteAddr = "127.0.0.1:12345"
	login.Header.Set("Origin", "http://127.0.0.1:9090")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, login)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login returned %d", recorder.Code)
	}
	cookie := recorder.Result().Cookies()[0]
	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(cookie)
	logout.Header.Set("Origin", "http://127.0.0.1:9090")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, logout)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF token returned %d", recorder.Code)
	}
}

func TestGeneratedCertificateIsUsable(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	certificate, key, err := server.ensureCertificate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(certificate); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("private key permissions are %o", info.Mode().Perm())
	}
}
