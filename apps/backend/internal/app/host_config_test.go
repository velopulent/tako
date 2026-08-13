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
	"sync/atomic"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

func TestHostConfigurationPreviewAndStaleWrite(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	current := platform.NewHostConfiguration("old-host", "UTC", true)
	server.readHostConfiguration = func(context.Context) (platform.HostConfiguration, error) { return current, nil }
	cookie, csrf := loginForTest(t, server.routes())

	preview := httptest.NewRequest(http.MethodPost, "/api/v1/host/config/preview", bytes.NewBufferString(`{"hostname":"new-host","timezone":"Asia/Kolkata","ntpEnabled":false,"expectedFingerprint":"`+current.Fingerprint+`"}`))
	preview.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, preview)
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"hostname":"new-host"`)) || !bytes.Contains(recorder.Body.Bytes(), []byte(`"stale":false`)) {
		t.Fatalf("preview returned %d: %s", recorder.Code, recorder.Body.String())
	}

	update := httptest.NewRequest(http.MethodPut, "/api/v1/host/config", bytes.NewBufferString(`{"hostname":"new-host","timezone":"UTC","ntpEnabled":true,"expectedFingerprint":"`+strings.Repeat("0", 64)+`"}`))
	update.AddCookie(cookie)
	update.Header.Set("X-CSRF-Token", csrf)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, update)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unauthorized host update returned %d", recorder.Code)
	}

	if !server.sessions.SetAdministrative(cookie.Value, "admin-token", time.Now().Add(time.Minute)) {
		t.Fatal("could not set test administrative grant")
	}
	stale := httptest.NewRequest(http.MethodPut, "/api/v1/host/config", bytes.NewBufferString(`{"hostname":"new-host","timezone":"UTC","ntpEnabled":true,"expectedFingerprint":"`+strings.Repeat("0", 64)+`"}`))
	stale.AddCookie(cookie)
	stale.Header.Set("X-CSRF-Token", csrf)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, stale)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("stale host update returned %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestHostConfigurationUpdateUsesPrivilegedSocketAndRecordsReceipt(t *testing.T) {
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
	current := platform.NewHostConfiguration("old-host", "UTC", true)
	updated := platform.NewHostConfiguration("new-host", "Asia/Kolkata", false)
	var readCurrent atomic.Bool
	server.readHostConfiguration = func(context.Context) (platform.HostConfiguration, error) {
		if readCurrent.Load() {
			return updated, nil
		}
		return current, nil
	}
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var request auth.Request
		if json.NewDecoder(connection).Decode(&request) == nil && request.Operation == "host-config" {
			readCurrent.Store(true)
			_ = json.NewEncoder(connection).Encode(auth.Response{})
		}
	}()
	cookie, csrf := loginForTest(t, server.routes())
	if !server.sessions.SetAdministrative(cookie.Value, "admin-token", time.Now().Add(time.Minute)) {
		t.Fatal("could not set test administrative grant")
	}
	body := `{"hostname":"new-host","timezone":"Asia/Kolkata","ntpEnabled":false,"expectedFingerprint":"` + current.Fingerprint + `"}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/host/config", bytes.NewBufferString(body))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"hostname":"new-host"`)) {
		t.Fatalf("host update returned %d: %s", recorder.Code, recorder.Body.String())
	}
	items, err := server.preferences.OperationReceipts(context.Background(), 10)
	if err != nil || len(items) != 1 || items[0].Target != "host/config" || items[0].Result != "succeeded" {
		t.Fatalf("host update receipt missing: %#v, %v", items, err)
	}
}
