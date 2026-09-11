package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
)

func TestUpdatesRouteUsesControlledReadOnlyInventorySeam(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	server.readUpdatesFn = func(context.Context) (platform.UpdateStatus, error) {
		return platform.UpdateStatus{
			Available:    true,
			Backend:      "apt-get",
			Version:      "apt 3.0",
			Contract:     "bounded-command-read-only",
			Fingerprint:  strings.Repeat("a", 64),
			ExternalLock: true,
			LockReason:   "A package-manager lock is held",
			Packages: []platform.UpdatePackage{{
				Name:             "openssl",
				CurrentVersion:   "3.0.11",
				CandidateVersion: "3.0.14",
				Architecture:     "amd64",
			}},
			Message: "1 installed-software update available.",
		}, nil
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/updates", nil)
	request.AddCookie(&http.Cookie{Name: session.CookieName, Value: created.ID})
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("updates returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var status platform.UpdateStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Backend != "apt-get" || !status.ExternalLock || len(status.Packages) != 1 || status.Packages[0].Name != "openssl" {
		t.Fatalf("unexpected update response: %#v", status)
	}
}

func TestUpdatePreviewAndJobKeepAdminTokenOutOfDurableParameters(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.jobs.Close(context.Background())
	fingerprint := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	status := platform.UpdateStatus{
		Available: true, Backend: "apt-get", Version: "apt 3.0", Contract: "bounded-command-read-only",
		Fingerprint: fingerprint,
		Packages:    []platform.UpdatePackage{{Name: "openssl", CandidateVersion: "3.0.14"}},
		Message:     "1 installed-software update available.",
	}
	server.readUpdatesFn = func(context.Context) (platform.UpdateStatus, error) { return status, nil }
	server.previewUpdatesFn = func(_ context.Context, operation platform.UpdateOperation) (platform.UpdatePreview, error) {
		return platform.UpdatePreview{Current: status, Changes: []platform.UpdateChange{{Action: "upgrade", Name: "openssl", CandidateVersion: "3.0.14"}}, Warnings: []string{}, Fingerprint: fingerprint, Allowed: true, RequiresConfirmation: true}, nil
	}
	called := make(chan auth.UpdateRequest, 1)
	server.applyUpdatesFn = func(_ context.Context, request auth.UpdateRequest) (platform.UpdateResult, error) {
		called <- request
		return platform.UpdateResult{Backend: "apt-get", Changes: []platform.UpdateChange{{Action: "upgrade", Name: "openssl", CandidateVersion: "3.0.14"}}, Verified: true, Message: "Updates applied and verified.", Fingerprint: fingerprint}, nil
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge", AdminToken: "secret-admin"})
	if err != nil {
		t.Fatal(err)
	}
	if !server.sessions.SetAdministrative(created.ID, "secret-admin", time.Now().Add(time.Hour)) {
		t.Fatal("could not grant administrative access")
	}
	previewRequest := httptest.NewRequest(http.MethodPost, "/api/v1/updates/preview", bytes.NewBufferString(`{"expectedFingerprint":"`+fingerprint+`"}`))
	previewRequest.AddCookie(&http.Cookie{Name: session.CookieName, Value: created.ID})
	previewRecorder := httptest.NewRecorder()
	server.routes().ServeHTTP(previewRecorder, previewRequest)
	if previewRecorder.Code != http.StatusOK {
		t.Fatalf("preview returned %d: %s", previewRecorder.Code, previewRecorder.Body.String())
	}
	applyRequest := httptest.NewRequest(http.MethodPost, "/api/v1/updates", bytes.NewBufferString(`{"expectedFingerprint":"`+fingerprint+`","confirmed":true}`))
	applyRequest.AddCookie(&http.Cookie{Name: session.CookieName, Value: created.ID})
	applyRequest.Header.Set("X-CSRF-Token", created.CSRF)
	applyRecorder := httptest.NewRecorder()
	server.routes().ServeHTTP(applyRecorder, applyRequest)
	if applyRecorder.Code != http.StatusAccepted {
		t.Fatalf("apply returned %d: %s", applyRecorder.Code, applyRecorder.Body.String())
	}
	var accepted struct {
		Job Job `json:"job"`
	}
	if err := json.Unmarshal(applyRecorder.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(accepted.Job.Parameters, []byte("secret-admin")) {
		t.Fatalf("durable update parameters contain a secret: %s", accepted.Job.Parameters)
	}
	select {
	case request := <-called:
		if request.AdminToken != "secret-admin" {
			t.Fatalf("worker did not receive in-memory administrative token: %#v", request)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("update worker did not run")
	}
}

func TestUpdatesRouteSurfacesBackendFailureInsteadOfEmpty(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	server.readUpdatesFn = func(context.Context) (platform.UpdateStatus, error) {
		return platform.UpdateStatus{}, errors.New("system-backend-unavailable")
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/updates", nil)
	request.AddCookie(&http.Cookie{Name: session.CookieName, Value: created.ID})
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("updates failure returned %d, want 503: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "updates-unavailable") {
		t.Fatalf("updates failure hides backend error: %s", recorder.Body.String())
	}
}
