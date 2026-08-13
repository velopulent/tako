package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/config"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/preferences"
	"github.com/velopulent/tako/internal/session"
)

type unavailableAuthenticator struct{}

func (unavailableAuthenticator) Authenticate(context.Context, string, string) (auth.Identity, error) {
	return auth.Identity{}, auth.ErrServiceUnavailable
}

type conversationalAuthenticator struct {
	canceled  atomic.Bool
	confirmed atomic.Bool
	closed    atomic.Bool
}

func (authenticator *conversationalAuthenticator) ConfirmUserSession(_ context.Context, token string) error {
	authenticator.confirmed.Store(token == "bridge-token")
	return nil
}

func (authenticator *conversationalAuthenticator) CloseUserSession(_ context.Context, token string) error {
	authenticator.closed.Store(token == "bridge-token")
	return nil
}

func (*conversationalAuthenticator) Authenticate(context.Context, string, string) (auth.Identity, error) {
	return auth.Identity{}, errors.New("one-shot authentication should not be used")
}

func (*conversationalAuthenticator) AdvanceConversation(_ context.Context, request *auth.ConversationRequest) (auth.ConversationResponse, error) {
	if request.ConversationID == "" {
		if request.Username != "octopus" || request.Password != "secret" {
			return auth.ConversationResponse{Error: "authentication-failed"}, nil
		}
		return auth.ConversationResponse{
			ConversationID: "conversation-token",
			Prompts:        []auth.Prompt{{ID: "otp", Style: auth.PromptText, Message: "Verification code:"}},
		}, nil
	}
	if request.ConversationID == "conversation-token" && len(request.Responses) == 1 && request.Responses[0].ID == "otp" && request.Responses[0].Value == "123456" {
		return auth.ConversationResponse{
			Identity:    &auth.Identity{Username: "octopus", UID: 1000, GID: 1000},
			BridgeToken: "bridge-token",
		}, nil
	}
	return auth.ConversationResponse{Error: "invalid-conversation"}, nil
}

func (authenticator *conversationalAuthenticator) CancelConversation(_ context.Context, id string) error {
	authenticator.canceled.Store(id == "conversation-token")
	return nil
}

func TestLoginRelaysMultiplePAMRounds(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	authenticator := &conversationalAuthenticator{}
	server.authenticator = authenticator
	handler := server.routes()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"octopus","password":"secret"}`))
	request.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("first round returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"conversationId":"conversation-token"`)) || bytes.Contains(recorder.Body.Bytes(), []byte("secret")) {
		t.Fatalf("unsafe or incomplete challenge: %s", recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"conversationId":"conversation-token","responses":[{"id":"otp","value":"123456"}]}`))
	request.RemoteAddr = "127.0.0.1:12345"
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("second round returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(recorder.Result().Cookies()) == 0 || !authenticator.confirmed.Load() || bytes.Contains(recorder.Body.Bytes(), []byte("bridge-token")) || bytes.Contains(recorder.Body.Bytes(), []byte("123456")) {
		t.Fatalf("session response leaked secrets or omitted cookie: %s", recorder.Body.String())
	}
	var sessionResponse struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &sessionResponse); err != nil {
		t.Fatal(err)
	}
	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(recorder.Result().Cookies()[0])
	logout.Header.Set("X-CSRF-Token", sessionResponse.CSRFToken)
	logoutRecorder := httptest.NewRecorder()
	handler.ServeHTTP(logoutRecorder, logout)
	for deadline := time.Now().Add(time.Second); !authenticator.closed.Load() && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if logoutRecorder.Code != http.StatusNoContent || !authenticator.closed.Load() {
		t.Fatalf("logout returned %d, user session closed=%v", logoutRecorder.Code, authenticator.closed.Load())
	}
}

func TestLoginConversationCanBeCanceled(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	authenticator := &conversationalAuthenticator{}
	server.authenticator = authenticator
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"conversationId":"conversation-token","cancel":true}`))
	request.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || !authenticator.canceled.Load() {
		t.Fatalf("cancel returned %d, canceled=%v", recorder.Code, authenticator.canceled.Load())
	}
}

func TestLoginRejectsMixedConversationShape(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	server.config.Development = false
	server.authenticator = &conversationalAuthenticator{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"octopus","conversationId":"conversation-token","responses":[{"id":"otp","value":"123456"}]}`))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Origin", server.config.AllowedOrigins[0])
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("mixed request returned %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestDevelopmentLoginAndDashboard(t *testing.T) {
	cfg := testConfig(t)
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
	var payload struct {
		Host    host.Info `json:"host"`
		Metrics any       `json:"metrics"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Host.Hostname == "" || payload.Host.Hardware.CPUCores < 1 || payload.Metrics == nil {
		t.Fatalf("dashboard response incomplete: %#v", payload)
	}
}

func TestCapabilitiesExposeRuntimeContractsAndGuidance(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	server.detectCapabilities = func(context.Context) []platform.Capability {
		return []platform.Capability{
			{ID: "services", State: platform.StateReady, Backend: "systemd", Version: "systemd 257", Readable: true, ReadAuthority: "session", MutationAuthority: "none", Contract: "dbus-read-only"},
			{ID: "network", State: platform.StateConflicted, Backend: "NetworkManager+networkd", Readable: true, ReadAuthority: "session", MutationAuthority: "none", Contract: "conflicted-read-only", Reason: "Multiple managers are active", SetupGuidance: "Choose one network manager."},
			{ID: "updates", State: platform.StateDegraded, Backend: "apt-get", Version: "apt 3.0", Readable: true, ReadAuthority: "session", MutationAuthority: "none", Contract: "bounded-command-read-only", Reason: "PackageKit unavailable", SetupGuidance: "Install PackageKit.", MissingDependency: "org.freedesktop.PackageKit"},
		}
	}
	cookie, _ := loginForTest(t, server.routes())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("capabilities returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Capabilities []struct {
			ID             string `json:"id"`
			State          string `json:"state"`
			Contract       string `json:"contract"`
			Mutable        bool   `json:"mutable"`
			ReadAuthority  string `json:"readAuthority"`
			WriteAuthority string `json:"mutationAuthority"`
			Reason         string `json:"reason"`
			SetupGuidance  string `json:"setupGuidance"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Capabilities) != 3 {
		t.Fatalf("capability inventory is incomplete: %#v", response.Capabilities)
	}
	for _, capability := range response.Capabilities {
		if capability.ID == "" || capability.State == "" || capability.Contract == "" || capability.ReadAuthority == "" || capability.WriteAuthority == "" {
			t.Fatalf("capability lacks contract fields: %#v", capability)
		}
		if capability.State != "ready" && (capability.Reason == "" || capability.SetupGuidance == "") {
			t.Fatalf("unready capability lacks guidance: %#v", capability)
		}
		if capability.State == "conflicted" && capability.Mutable {
			t.Fatalf("conflicted capability allows mutation: %#v", capability)
		}
	}
}

func TestOperationReceiptsEndpointReturnsSanitizedHistory(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	if _, err := server.preferences.RecordOperation(context.Background(), preferences.OperationReceipt{
		Actor:          "octopus",
		Target:         "system/sshd.service/restart",
		Result:         "succeeded",
		Administrative: true,
	}); err != nil {
		t.Fatal(err)
	}
	cookie, _ := loginForTest(t, server.routes())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations?limit=1", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte("sshd.service")) {
		t.Fatalf("operation history returned %d: %s", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/operations?limit=0", nil)
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid operation limit returned %d", recorder.Code)
	}
}

func TestServiceRoutesRejectUnsafeTargetsAndMissingAuthority(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	cookie, csrf := loginForTest(t, server.routes())

	request := httptest.NewRequest(http.MethodGet, "/api/v1/services/global/demo.service", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid service target returned %d: %s", recorder.Code, recorder.Body.String())
	}

	for _, test := range []struct {
		name   string
		path   string
		body   string
		status int
	}{
		{name: "unknown action", path: "/api/v1/services/user/demo.service/actions", body: `{"action":"exec"}`, status: http.StatusBadRequest},
		{name: "unknown field", path: "/api/v1/services/user/demo.service/actions", body: `{"action":"restart","command":"id"}`, status: http.StatusBadRequest},
		{name: "system needs administrative bridge", path: "/api/v1/services/system/demo.service/actions", body: `{"action":"restart"}`, status: http.StatusForbidden},
		{name: "user needs unix bridge", path: "/api/v1/services/user/demo.service/actions", body: `{"action":"restart"}`, status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			request.AddCookie(cookie)
			request.Header.Set("X-CSRF-Token", csrf)
			recorder := httptest.NewRecorder()
			server.routes().ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("service request returned %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestProtectedRouteRejectsMissingSession(t *testing.T) {
	cfg := testConfig(t)
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
	cfg := testConfig(t)
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
	cfg := testConfig(t)
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

func TestMonitoringPreferenceSurvivesServerRestart(t *testing.T) {
	cfg := config.Default()
	cfg.Development = true
	cfg.DataDir = t.TempDir()

	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForTest(t, first.routes())
	request := httptest.NewRequest(http.MethodPut, "/api/v1/preferences/monitoring", bytes.NewBufferString(`{"defaultInterval":"30s","expectedRevision":0}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	first.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update returned %d: %s", recorder.Code, recorder.Body.String())
	}
	first.cancel()
	if err := first.preferences.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.cancel()
	defer second.preferences.Close()
	cookie, _ = loginForTest(t, second.routes())
	request = httptest.NewRequest(http.MethodGet, "/api/v1/preferences/monitoring", nil)
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	second.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("read returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var preference struct {
		DefaultInterval string `json:"defaultInterval"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &preference); err != nil {
		t.Fatal(err)
	}
	if preference.DefaultInterval != "30s" {
		t.Fatalf("preference did not survive restart: %#v", preference)
	}
}

func TestMonitoringPreferenceRejectsStaleAndMalformedWrites(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	handler := server.routes()
	cookie, csrf := loginForTest(t, handler)

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{name: "first write", body: `{"defaultInterval":"30s","expectedRevision":0}`, status: http.StatusOK},
		{name: "stale write", body: `{"defaultInterval":"5s","expectedRevision":0}`, status: http.StatusConflict},
		{name: "trailing object", body: `{"defaultInterval":"5s","expectedRevision":1}{}`, status: http.StatusBadRequest},
		{name: "unknown secret field", body: `{"defaultInterval":"5s","expectedRevision":1,"password":"do-not-store"}`, status: http.StatusBadRequest},
		{name: "invalid interval", body: `{"defaultInterval":"2s","expectedRevision":1}`, status: http.StatusBadRequest},
		{name: "negative revision", body: `{"defaultInterval":"5s","expectedRevision":-1}`, status: http.StatusBadRequest},
		{name: "oversized", body: `{"defaultInterval":"5s","expectedRevision":1,"padding":"` + string(bytes.Repeat([]byte("x"), 5<<10)) + `"}`, status: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/api/v1/preferences/monitoring", bytes.NewBufferString(test.body))
			request.AddCookie(cookie)
			request.Header.Set("X-CSRF-Token", csrf)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("expected %d, got %d: %s", test.status, recorder.Code, recorder.Body.String())
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/preferences/monitoring", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"defaultInterval":"30s"`)) {
		t.Fatalf("rejected writes changed preference: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMonitoringPreferenceUsesConfiguredDefault(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	cookie, _ := loginForTest(t, server.routes())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/preferences/monitoring", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("read returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var preference struct {
		DefaultInterval string `json:"defaultInterval"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &preference); err != nil {
		t.Fatal(err)
	}
	if preference.DefaultInterval != "1m" {
		t.Fatalf("configured default is not API interval: %#v", preference)
	}
}

func loginForTest(t *testing.T, handler http.Handler) (*http.Cookie, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"","password":""}`))
	request.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == session.CookieName {
			return cookie, response.CSRFToken
		}
	}
	t.Fatal("login did not issue session cookie")
	return nil, ""
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Development = true
	cfg.DataDir = t.TempDir()
	return cfg
}
