package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
)

type administrativeTestAuthenticator struct {
	auth.DevelopmentAuthenticator
	mu       sync.Mutex
	denied   bool
	password string
	revoked  []string
}

func (authenticator *administrativeTestAuthenticator) AuthorizeAdministrative(_ context.Context, _ string, password string, _ time.Duration) (auth.AdministrativeAccess, error) {
	authenticator.mu.Lock()
	defer authenticator.mu.Unlock()
	authenticator.password = password
	if authenticator.denied {
		return auth.AdministrativeAccess{}, errors.New("policy denied")
	}
	return auth.AdministrativeAccess{Token: "admin-token", Until: time.Now().Add(time.Minute)}, nil
}

func (authenticator *administrativeTestAuthenticator) RevokeAdministrative(_ context.Context, token string) error {
	authenticator.mu.Lock()
	defer authenticator.mu.Unlock()
	authenticator.revoked = append(authenticator.revoked, token)
	return nil
}

func TestAdministrativeAccessUsesRootPolicyGrantAndClearsOnDrop(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	authenticator := &administrativeTestAuthenticator{}
	server.authenticator = authenticator
	cookie, csrf := loginForTest(t, server.routes())

	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/elevate", bytes.NewBufferString(`{"password":"secret"}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("elevation returned %d: %s", recorder.Code, recorder.Body.String())
	}
	authenticator.mu.Lock()
	if authenticator.password != "secret" {
		t.Fatalf("policy did not receive the bounded password: %q", authenticator.password)
	}
	authenticator.mu.Unlock()
	current, ok := server.sessions.Get(cookie.Value)
	if !ok || current.Identity.AdminToken != "admin-token" {
		t.Fatalf("session did not retain opaque grant: %+v, %v", current, ok)
	}

	drop := httptest.NewRequest(http.MethodPost, "/api/v1/admin/drop", nil)
	drop.AddCookie(cookie)
	drop.Header.Set("X-CSRF-Token", csrf)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, drop)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("drop returned %d: %s", recorder.Code, recorder.Body.String())
	}
	authenticator.mu.Lock()
	defer authenticator.mu.Unlock()
	if len(authenticator.revoked) != 1 || authenticator.revoked[0] != "admin-token" {
		t.Fatalf("root grant was not revoked: %#v", authenticator.revoked)
	}
}

func TestAdministrativeAccessPassesBoundedPAMResponsesToPolicy(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	authenticator := &administrativeTestAuthenticator{}
	server.authenticator = authenticator
	cookie, csrf := loginForTest(t, server.routes())

	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/elevate", bytes.NewBufferString(`{"password":"secret","responses":["123456"]}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("elevation returned %d: %s", recorder.Code, recorder.Body.String())
	}
	authenticator.mu.Lock()
	defer authenticator.mu.Unlock()
	if authenticator.password != "secret\n123456" {
		t.Fatalf("policy did not receive ordered PAM responses: %q", authenticator.password)
	}
}

func TestAdministrativeAccessRejectsMalformedAndDeniedRequests(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	defer server.preferences.Close()
	authenticator := &administrativeTestAuthenticator{denied: true}
	server.authenticator = authenticator
	cookie, csrf := loginForTest(t, server.routes())

	for _, payload := range []string{`{"password":"secret"}{}`, `{"password":"secret","unknown":true}`} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/elevate", bytes.NewBufferString(payload))
		request.AddCookie(cookie)
		request.Header.Set("X-CSRF-Token", csrf)
		recorder := httptest.NewRecorder()
		server.routes().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("malformed elevation %q returned %d", payload, recorder.Code)
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/elevate", bytes.NewBufferString(`{"password":"secret"}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("denied elevation returned %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestParseFileRangeSupportsSuffixAndOpenEndedRanges(t *testing.T) {
	tests := []struct {
		name       string
		rangeValue string
		start      int64
		end        int64
		valid      bool
	}{
		{name: "explicit", rangeValue: "bytes=2-5", start: 2, end: 5, valid: true},
		{name: "open ended", rangeValue: "bytes=8-", start: 8, end: 9, valid: true},
		{name: "suffix", rangeValue: "bytes=-3", start: 7, end: 9, valid: true},
		{name: "unsatisfiable", rangeValue: "bytes=10-", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, valid := parseFileRange(test.rangeValue, 10)
			if valid != test.valid || (valid && (start != test.start || end != test.end)) {
				t.Fatalf("parseFileRange(%q) = (%d, %d, %v)", test.rangeValue, start, end, valid)
			}
		})
	}
}
