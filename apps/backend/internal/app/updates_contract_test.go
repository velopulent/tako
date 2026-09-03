package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
)

func TestUpdateHandlersEmitEmptyArraysNotNull(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	// Intentionally return zero values with nil slices, as fakes and future
	// producer paths can. Handlers must still emit [] for required arrays.
	server.readUpdatesFn = func(context.Context) platform.UpdateStatus {
		return platform.UpdateStatus{Available: true, Backend: "test", Contract: "test"}
	}
	server.previewUpdatesFn = func(_ context.Context, operation platform.UpdateOperation) (platform.UpdatePreview, error) {
		return platform.UpdatePreview{Operation: operation}, nil
	}
	server.readUpdateHistoryFn = func(context.Context) ([]platform.UpdateHistoryEntry, error) {
		return nil, nil
	}
	server.readUpdateLiveFn = func(context.Context) (auth.UpdateObservation, error) {
		return auth.UpdateObservation{}, nil
	}
	created, err := server.sessions.Create(auth.Identity{Username: "operator", BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: session.CookieName, Value: created.ID}

	get := func(target string) string {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		server.routes().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", target, recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}

	statusBody := get("/api/v1/updates")
	for _, key := range []string{`"packages":[]`} {
		if !strings.Contains(statusBody, key) {
			t.Fatalf("updates missing %s: %s", key, statusBody)
		}
	}
	if strings.Contains(statusBody, `"packages":null`) {
		t.Fatalf("updates contains null packages: %s", statusBody)
	}

	previewRequest := httptest.NewRequest(http.MethodPost, "/api/v1/updates/preview", strings.NewReader(`{"scope":"all"}`))
	previewRequest.AddCookie(cookie)
	previewRecorder := httptest.NewRecorder()
	server.routes().ServeHTTP(previewRecorder, previewRequest)
	if previewRecorder.Code != http.StatusOK {
		t.Fatalf("preview returned %d: %s", previewRecorder.Code, previewRecorder.Body.String())
	}
	previewBody := previewRecorder.Body.String()
	for _, key := range []string{`"selected":[]`, `"changes":[]`, `"warnings":[]`} {
		if !strings.Contains(previewBody, key) {
			t.Fatalf("preview missing %s: %s", key, previewBody)
		}
	}

	historyBody := get("/api/v1/updates/history")
	if !strings.Contains(historyBody, `"items":[]`) {
		t.Fatalf("history missing empty items: %s", historyBody)
	}

	liveBody := get("/api/v1/updates/live")
	if !strings.Contains(liveBody, `"log":[]`) {
		t.Fatalf("live missing empty log: %s", liveBody)
	}
}
