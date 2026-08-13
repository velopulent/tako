package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
	defer server.preferences.Close()
	server.readUpdatesFn = func(context.Context) platform.UpdateStatus {
		return platform.UpdateStatus{
			Available:    true,
			Backend:      "apt-get",
			Version:      "apt 3.0",
			Contract:     "bounded-command-read-only",
			ExternalLock: true,
			LockReason:   "A package-manager lock is held",
			Packages: []platform.UpdatePackage{{
				Name:             "openssl",
				CurrentVersion:   "3.0.11",
				CandidateVersion: "3.0.14",
				Architecture:     "amd64",
			}},
			Message: "1 installed-software update available.",
		}
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
