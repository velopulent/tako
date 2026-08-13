package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

func TestPowerEndpointRejectsUnauthorizedAndStaleRequests(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	status := platform.PowerStatus{Available: true, Reboot: platform.PowerActionStatus{State: "available", Available: true}, Shutdown: platform.PowerActionStatus{State: "available", Available: true}, Fingerprint: strings.Repeat("a", 64)}
	server.readPowerStatusFn = func(context.Context) (platform.PowerStatus, error) { return status, nil }
	cookie, csrf := loginForTest(t, server.routes())
	body := `{"action":"reboot","confirmation":"REBOOT","expectedFingerprint":"` + status.Fingerprint + `"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/host/power", bytes.NewBufferString(body))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unauthorized power request returned %d", recorder.Code)
	}
	if !server.sessions.SetAdministrative(cookie.Value, "admin-token", time.Now().Add(time.Minute)) {
		t.Fatal("could not set test administrative grant")
	}
	stale := strings.Repeat("0", 64)
	request = httptest.NewRequest(http.MethodPost, "/api/v1/host/power", bytes.NewBufferString(`{"action":"reboot","confirmation":"REBOOT","expectedFingerprint":"`+stale+`"}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("stale power request returned %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPowerEndpointUsesTypedConfirmationAndReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("Unix listeners unavailable in this sandbox: %v", err)
	}
	defer listener.Close()
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	server.config.SessionSocket = path
	status := platform.PowerStatus{Available: true, Reboot: platform.PowerActionStatus{State: "available", Available: true}, Shutdown: platform.PowerActionStatus{State: "available", Available: true}, Fingerprint: strings.Repeat("a", 64)}
	server.readPowerStatusFn = func(context.Context) (platform.PowerStatus, error) { return status, nil }
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var request auth.Request
		if json.NewDecoder(connection).Decode(&request) == nil && request.Operation == "power" && request.PowerAction == "reboot" {
			_ = json.NewEncoder(connection).Encode(auth.Response{})
		}
	}()
	cookie, csrf := loginForTest(t, server.routes())
	if !server.sessions.SetAdministrative(cookie.Value, "admin-token", time.Now().Add(time.Minute)) {
		t.Fatal("could not set test administrative grant")
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/host/power", bytes.NewBufferString(`{"action":"reboot","confirmation":"wrong","expectedFingerprint":"`+status.Fingerprint+`"}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("wrong confirmation returned %d", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/host/power", bytes.NewBufferString(`{"action":"reboot","confirmation":"REBOOT","expectedFingerprint":"`+status.Fingerprint+`"}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("power request returned %d: %s", recorder.Code, recorder.Body.String())
	}
	items, err := server.preferences.OperationReceipts(context.Background(), 10)
	if err != nil || len(items) != 1 || items[0].Target != "host/reboot" || items[0].Result != "succeeded" {
		t.Fatalf("power receipt missing: %#v, %v", items, err)
	}
}
