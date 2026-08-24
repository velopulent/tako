package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
)

func adminSessionFor(t *testing.T, server *Server, username string) (*http.Cookie, string) {
	t.Helper()
	created, err := server.sessions.Create(auth.Identity{Username: username, BridgeToken: "bridge", AdminToken: "secret-admin"})
	if err != nil {
		t.Fatal(err)
	}
	if !server.sessions.SetAdministrative(created.ID, "secret-admin", time.Now().Add(time.Hour)) {
		t.Fatal("could not grant administrative access")
	}
	return &http.Cookie{Name: session.CookieName, Value: created.ID}, created.CSRF
}

func TestUpdateLiveAndHistoryEndpointsReturnBoundedReads(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	cookie, _ := adminSessionFor(t, server, "operator")

	liveRequest := httptest.NewRequest(http.MethodGet, "/api/v1/updates/live", nil)
	liveRequest.AddCookie(cookie)
	liveRecorder := httptest.NewRecorder()
	server.routes().ServeHTTP(liveRecorder, liveRequest)
	if liveRecorder.Code != http.StatusOK {
		t.Fatalf("live returned %d: %s", liveRecorder.Code, liveRecorder.Body.String())
	}
	var liveResponse struct {
		Live platform.UpdateLive `json:"live"`
		Log  []map[string]any    `json:"log"`
	}
	if err := json.Unmarshal(liveRecorder.Body.Bytes(), &liveResponse); err != nil {
		t.Fatal(err)
	}
	if liveResponse.Live.Active || liveResponse.Live.Percentage != -1 || liveResponse.Log == nil {
		t.Fatalf("unexpected inactive observation: %#v", liveResponse)
	}

	historyRequest := httptest.NewRequest(http.MethodGet, "/api/v1/updates/history", nil)
	historyRequest.AddCookie(cookie)
	historyRecorder := httptest.NewRecorder()
	server.routes().ServeHTTP(historyRecorder, historyRequest)
	if historyRecorder.Code != http.StatusOK {
		t.Fatalf("history returned %d: %s", historyRecorder.Code, historyRecorder.Body.String())
	}
}

func TestAutomaticUpdatesStatusUsesSeam(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	server.autoUpdatesStatusFn = func(context.Context) platform.AutoUpdatesConfig {
		return platform.AutoUpdatesConfig{Available: true, Supported: true, Installed: true, Enabled: true, Type: "security", Day: "mon", Time: "6:00", Provider: "dnf4-automatic"}
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/updates/automatic", nil)
	request.AddCookie(&http.Cookie{Name: session.CookieName, Value: created.ID})
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("automatic status returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var config platform.AutoUpdatesConfig
	if err := json.Unmarshal(recorder.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if !config.Enabled || config.Day != "mon" || config.Provider != "dnf4-automatic" {
		t.Fatalf("unexpected automatic config: %#v", config)
	}
}

func TestApplyAutomaticUpdatesRequiresAdministrativeAccess(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/updates/automatic", bytes.NewBufferString(`{"enabled":true}`))
	request.AddCookie(&http.Cookie{Name: session.CookieName, Value: created.ID})
	request.Header.Set("X-CSRF-Token", created.CSRF)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin apply returned %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestApplyAutomaticUpdatesRoutesOperationWithAdminToken(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	calls := make(chan auth.AutoUpdatesRequest, 1)
	server.autoUpdatesFn = func(_ context.Context, request auth.AutoUpdatesRequest) (platform.AutoUpdatesConfig, error) {
		calls <- request
		return platform.AutoUpdatesConfig{Available: true, Supported: true, Installed: true, Enabled: true, Type: "all", Provider: "dnf5-automatic"}, nil
	}
	cookie, csrf := adminSessionFor(t, server, "operator")
	request := httptest.NewRequest(http.MethodPut, "/api/v1/updates/automatic", bytes.NewBufferString(`{"enabled":true,"type":"all","day":"","time":"06:00"}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("apply returned %d: %s", recorder.Code, recorder.Body.String())
	}
	select {
	case received := <-calls:
		if received.AdminToken != "secret-admin" {
			t.Fatalf("admin token missing from sessiond call: %#v", received)
		}
		if received.Operation.Enabled == nil || !*received.Operation.Enabled {
			t.Fatalf("operation not forwarded: %#v", received.Operation)
		}
	default:
		t.Fatal("autoUpdatesFn was not invoked")
	}
	badBody := `{"enabled":true,"day":"funday"}`
	rejected := httptest.NewRequest(http.MethodPut, "/api/v1/updates/automatic", strings.NewReader(badBody))
	rejected.AddCookie(cookie)
	rejected.Header.Set("X-CSRF-Token", csrf)
	badRecorder := httptest.NewRecorder()
	server.routes().ServeHTTP(badRecorder, rejected)
	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid operation returned %d: %s", badRecorder.Code, badRecorder.Body.String())
	}
}

func TestCancelRunningUpdateRequiresAdministrativeAccess(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/updates/cancel", nil)
	request.AddCookie(&http.Cookie{Name: session.CookieName, Value: created.ID})
	request.Header.Set("X-CSRF-Token", created.CSRF)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin cancel returned %d: %s", recorder.Code, recorder.Body.String())
	}

	cookie, csrf := adminSessionFor(t, server, "operator")
	adminRequest := httptest.NewRequest(http.MethodPost, "/api/v1/updates/cancel", nil)
	adminRequest.AddCookie(cookie)
	adminRequest.Header.Set("X-CSRF-Token", csrf)
	adminRecorder := httptest.NewRecorder()
	server.routes().ServeHTTP(adminRecorder, adminRequest)
	if adminRecorder.Code != http.StatusOK {
		t.Fatalf("admin cancel returned %d: %s", adminRecorder.Code, adminRecorder.Body.String())
	}
	var result struct {
		Canceled bool `json:"canceled"`
	}
	if err := json.Unmarshal(adminRecorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	// No update transaction is running in the test environment.
	if result.Canceled {
		t.Fatal("cancel reported success without a running transaction")
	}
}

func TestApplyKpatchSettingsRequiresAdministrativeAccessAndRoutesOperation(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge", AdminToken: "secret-admin"})
	if err != nil {
		t.Fatal(err)
	}
	// Non-admin is rejected before any privileged call.
	request := httptest.NewRequest(http.MethodPut, "/api/v1/updates/kpatch", bytes.NewBufferString(`{"apply":true}`))
	request.AddCookie(&http.Cookie{Name: session.CookieName, Value: created.ID})
	request.Header.Set("X-CSRF-Token", created.CSRF)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin kpatch apply returned %d: %s", recorder.Code, recorder.Body.String())
	}

	if !server.sessions.SetAdministrative(created.ID, "secret-admin", time.Now().Add(time.Hour)) {
		t.Fatal("could not grant administrative access")
	}
	calls := make(chan auth.KpatchRequest, 1)
	server.kpatchSettingsFn = func(_ context.Context, request auth.KpatchRequest) (platform.KpatchSettingsStatus, error) {
		calls <- request
		return platform.KpatchSettingsStatus{Supported: true, Missing: []string{}, Unavailable: []string{}, Auto: true}, nil
	}
	adminRequest := httptest.NewRequest(http.MethodPut, "/api/v1/updates/kpatch", bytes.NewBufferString(`{"apply":true,"currentOnly":false}`))
	adminRequest.AddCookie(&http.Cookie{Name: session.CookieName, Value: created.ID})
	adminRequest.Header.Set("X-CSRF-Token", created.CSRF)
	adminRecorder := httptest.NewRecorder()
	server.routes().ServeHTTP(adminRecorder, adminRequest)
	if adminRecorder.Code != http.StatusOK {
		t.Fatalf("admin kpatch apply returned %d: %s", adminRecorder.Code, adminRecorder.Body.String())
	}
	select {
	case received := <-calls:
		if received.AdminToken != "secret-admin" || received.Operation.Apply == nil || !*received.Operation.Apply {
			t.Fatalf("operation not forwarded: %#v", received)
		}
	default:
		t.Fatal("kpatchSettingsFn was not invoked")
	}

	badRequest := httptest.NewRequest(http.MethodPut, "/api/v1/updates/kpatch", strings.NewReader(`{"currentOnly":true}`))
	badRequest.AddCookie(&http.Cookie{Name: session.CookieName, Value: created.ID})
	badRequest.Header.Set("X-CSRF-Token", created.CSRF)
	badRecorder := httptest.NewRecorder()
	server.routes().ServeHTTP(badRecorder, badRequest)
	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid kpatch operation returned %d: %s", badRecorder.Code, badRecorder.Body.String())
	}
}

func TestSyncUpdateNotificationsOpenAndResolve(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	status := platform.UpdateStatus{Available: true, Backend: "dnf", Packages: []platform.UpdatePackage{
		{Name: "openssl", Severity: "security"},
		{Name: "vim", Severity: "bugfix"},
	}}
	server.syncUpdateNotifications(status)
	items := server.notifications.list("")
	if len(items) != 1 || items[0].ID != "software-update-security" || items[0].Severity != "warning" {
		t.Fatalf("expected security notification: %#v", items)
	}
	status.Packages = nil
	server.syncUpdateNotifications(status)
	if remaining := server.notifications.list("open"); len(remaining) != 0 {
		t.Fatalf("notifications not resolved: %#v", remaining)
	}
}
