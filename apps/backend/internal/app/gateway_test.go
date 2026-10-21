package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/config"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	t.Setenv("TAKO_DEV_PASSWORD", "development")
	cfg := config.Default()
	cfg.Development = true
	cfg.DataDir = t.TempDir()
	return cfg
}

type brokerCall struct {
	operation   string
	credentials auth.Credentials
	payload     json.RawMessage
}

// stubBroker records every call and answers through respond.
type stubBroker struct {
	mu      sync.Mutex
	calls   []brokerCall
	respond func(operation string, payload json.RawMessage) (any, error)
}

func (broker *stubBroker) Call(_ context.Context, operation string, credentials auth.Credentials, payload, result any) error {
	encoded, _ := json.Marshal(payload)
	broker.mu.Lock()
	broker.calls = append(broker.calls, brokerCall{operation, credentials, encoded})
	respond := broker.respond
	broker.mu.Unlock()
	if respond == nil {
		return nil
	}
	value, err := respond(operation, encoded)
	if err != nil || result == nil || value == nil {
		return err
	}
	raw, _ := json.Marshal(value)
	return json.Unmarshal(raw, result)
}

func (broker *stubBroker) Stream(context.Context, string, auth.Credentials, any, func(json.RawMessage) error) error {
	return nil
}

func (broker *stubBroker) Open(context.Context, string, auth.Credentials, any) (net.Conn, error) {
	return nil, errors.New("unavailable")
}

func (broker *stubBroker) last() brokerCall {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if len(broker.calls) == 0 {
		return brokerCall{}
	}
	return broker.calls[len(broker.calls)-1]
}

func (broker *stubBroker) count() int {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	return len(broker.calls)
}

func newTestServer(t *testing.T) (*Server, *stubBroker) {
	t.Helper()
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	broker := &stubBroker{}
	server.broker = broker
	return server, broker
}

func signIn(t *testing.T, server *Server, administrative bool) session.Session {
	t.Helper()
	created, err := server.sessions.Create(auth.Identity{Username: "operator", UID: 1000, GID: 1000, BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	if administrative {
		server.sessions.SetAdministrative(created.ID, "admin-token", time.Now().Add(time.Minute))
	}
	current, _ := server.sessions.Get(created.ID)
	return current
}

func send(server *Server, current session.Session, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if current.ID != "" {
		request.AddCookie(&http.Cookie{Name: session.CookieName, Value: current.ID})
		request.Header.Set("X-CSRF-Token", current.CSRF)
	}
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	return recorder
}

func expectProblem(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var body struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	if recorder.Code != status || body.Code != code {
		t.Fatalf("got %d %q, want %d %q: %s", recorder.Code, body.Code, status, code, recorder.Body.String())
	}
}

func TestProtectedRoutesRequireASession(t *testing.T) {
	server, broker := newTestServer(t)
	for _, path := range []string{"/api/v1/dashboard", "/api/v1/files", "/api/v1/services", "/api/v1/updates", "/api/v1/terminal/ws"} {
		expectProblem(t, send(server, session.Session{}, http.MethodGet, path, ""), http.StatusUnauthorized, "no-session")
	}
	if broker.count() != 0 {
		t.Fatal("unauthenticated request reached the broker")
	}
}

func TestUnsafeMethodsRequireTheCSRFToken(t *testing.T) {
	server, broker := newTestServer(t)
	current := signIn(t, server, true)
	forged := current
	forged.CSRF = "forged"
	for _, path := range []string{"/api/v1/updates/preview", "/api/v1/updates/refresh", "/api/v1/host/power", "/api/v1/auth/logout"} {
		expectProblem(t, send(server, forged, http.MethodPost, path, `{}`), http.StatusForbidden, "csrf-failed")
	}
	if broker.count() != 0 {
		t.Fatal("forged request reached the broker")
	}
}

func TestAdministrativeRoutesRequireElevation(t *testing.T) {
	server, broker := newTestServer(t)
	current := signIn(t, server, false)
	for _, route := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/host/config"},
		{http.MethodPost, "/api/v1/host/power"},
		{http.MethodPost, "/api/v1/accounts/users/account"},
		{http.MethodPost, "/api/v1/accounts/groups/membership/preview"},
		{http.MethodPost, "/api/v1/updates"},
		{http.MethodPost, "/api/v1/updates/refresh"},
		{http.MethodGet, "/api/v1/firewall"},
		{http.MethodGet, "/api/v1/security"},
		{http.MethodPost, "/api/v1/storage"},
		{http.MethodGet, "/api/v1/jobs"},
		{http.MethodGet, "/api/v1/files?scope=system"},
	} {
		expectProblem(t, send(server, current, route.method, route.path, `{}`), http.StatusForbidden, "administrative-access-required")
	}
	if broker.count() != 0 {
		t.Fatalf("unelevated request reached the broker: %#v", broker.last())
	}
}

func TestCredentialsFollowScopeAndElevation(t *testing.T) {
	server, broker := newTestServer(t)
	user := signIn(t, server, false)
	admin := signIn(t, server, true)
	for _, test := range []struct {
		current session.Session
		path    string
		want    auth.Credentials
	}{
		{user, "/api/v1/services?scope=system", auth.Credentials{Token: "bridge-token"}},
		{admin, "/api/v1/services?scope=system", auth.Credentials{AdminToken: "admin-token"}},
		{admin, "/api/v1/services?scope=user", auth.Credentials{Token: "bridge-token"}},
		{admin, "/api/v1/files", auth.Credentials{Token: "bridge-token"}},
		{admin, "/api/v1/files?scope=system", auth.Credentials{AdminToken: "admin-token"}},
	} {
		if recorder := send(server, test.current, http.MethodGet, test.path, ""); recorder.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", test.path, recorder.Code, recorder.Body.String())
		}
		if got := broker.last().credentials; got != test.want {
			t.Fatalf("%s used %+v, want %+v", test.path, got, test.want)
		}
	}
}

func TestBrokerErrorsBecomeStableProblems(t *testing.T) {
	server, broker := newTestServer(t)
	current := signIn(t, server, true)
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{auth.Code("file-conflict"), http.StatusConflict, "file-conflict"},
		{platform.ErrFileNotFound, http.StatusNotFound, "file-not-found"},
		{auth.Code("invalid-admin-token"), http.StatusForbidden, "administrative-access-required"},
		{auth.Code("invalid-bridge-token"), http.StatusUnauthorized, "session-expired"},
		{errors.New("/etc/shadow: secret detail"), http.StatusBadGateway, "file-operation-failed"},
		{auth.ErrServiceUnavailable, http.StatusServiceUnavailable, "service-unavailable"},
	} {
		broker.respond = func(string, json.RawMessage) (any, error) { return nil, test.err }
		recorder := send(server, current, http.MethodGet, "/api/v1/files?path=.", "")
		expectProblem(t, recorder, test.status, test.code)
		if strings.Contains(recorder.Body.String(), "shadow") {
			t.Fatalf("internal error text leaked: %s", recorder.Body.String())
		}
	}
}

func TestFileContentNeverRendersActiveContent(t *testing.T) {
	server, broker := newTestServer(t)
	current := signIn(t, server, false)
	for _, test := range []struct {
		mime        string
		disposition string
	}{
		{"text/html; charset=utf-8", "attachment"},
		{"image/svg+xml", "attachment"},
		{"text/javascript; charset=utf-8", "attachment"},
		{"image/png", "inline"},
		{"text/plain; charset=utf-8", "inline"},
	} {
		broker.respond = func(string, json.RawMessage) (any, error) {
			return platform.FileResult{Content: []byte("<script>alert(1)</script>"), Total: 25, EOF: true, Mime: test.mime, Fingerprint: "f"}, nil
		}
		recorder := send(server, current, http.MethodGet, "/api/v1/files/content?path=page.html", "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s returned %d", test.mime, recorder.Code)
		}
		if !strings.HasPrefix(recorder.Header().Get("Content-Disposition"), test.disposition) {
			t.Fatalf("%s disposition = %q", test.mime, recorder.Header().Get("Content-Disposition"))
		}
		if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "sandbox") {
			t.Fatalf("%s is not sandboxed: %q", test.mime, recorder.Header().Get("Content-Security-Policy"))
		}
	}
}

func TestPreviewTokensAreBoundToSessionPathAndVersion(t *testing.T) {
	server, broker := newTestServer(t)
	owner := signIn(t, server, false)
	other := signIn(t, server, false)
	broker.respond = func(operation string, _ json.RawMessage) (any, error) {
		if operation == "files.apply" {
			return platform.FileResult{Directory: &platform.FileDirectory{Path: ".", Entries: []platform.FileEntry{{Name: "a.png", Path: "a.png", Kind: "file", Fingerprint: "v1"}}}}, nil
		}
		return nil, nil
	}
	var listing platform.FileResult
	if err := json.Unmarshal(send(server, owner, http.MethodGet, "/api/v1/files", "").Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	token := listing.Directory.Entries[0].PreviewToken
	if token == "" {
		t.Fatal("listing did not issue a preview token")
	}
	if _, ok := server.previews.fingerprint(owner.ID, token, "a.png"); !ok {
		t.Fatal("owner token rejected")
	}
	if _, ok := server.previews.fingerprint(other.ID, token, "a.png"); ok {
		t.Fatal("token accepted for another session")
	}
	if _, ok := server.previews.fingerprint(owner.ID, token, "b.png"); ok {
		t.Fatal("token accepted for another path")
	}
	expectProblem(t, send(server, other, http.MethodGet, "/api/v1/files/content?path=a.png&token="+token, ""), http.StatusForbidden, "file-preview-expired")
}

func TestSecurityPreviewIsReadOnly(t *testing.T) {
	server, broker := newTestServer(t)
	current := signIn(t, server, true)
	send(server, current, http.MethodPost, "/api/v1/security/preview", `{"action":"selinux-boolean","framework":"SELinux","boolean":"httpd_can_network_connect","value":true}`)
	if broker.last().operation != "security.preview" {
		t.Fatalf("preview used %q", broker.last().operation)
	}
}

type stubAuthenticator struct {
	responses []auth.ConversationResponse
	err       error
	requests  []auth.ConversationRequest
}

func (authenticator *stubAuthenticator) AdvanceConversation(_ context.Context, request *auth.ConversationRequest) (auth.ConversationResponse, error) {
	authenticator.requests = append(authenticator.requests, *request)
	if authenticator.err != nil {
		return auth.ConversationResponse{}, authenticator.err
	}
	response := authenticator.responses[0]
	authenticator.responses = authenticator.responses[1:]
	return response, nil
}

func (*stubAuthenticator) CancelConversation(context.Context, string) error { return nil }

func TestLoginRelaysPromptsThenCreatesSession(t *testing.T) {
	server, _ := newTestServer(t)
	identity := auth.Identity{Username: "operator", UID: 1000}
	server.authenticator = &stubAuthenticator{responses: []auth.ConversationResponse{
		{ConversationID: "c1", Prompts: []auth.Prompt{{ID: "otp", Style: auth.PromptText, Message: "Code:"}}},
		{Identity: &identity},
	}}
	first := send(server, session.Session{}, http.MethodPost, "/api/v1/auth/login", `{"username":"operator","password":"secret"}`)
	if first.Code != http.StatusAccepted || !strings.Contains(first.Body.String(), `"otp"`) {
		t.Fatalf("first round = %d %s", first.Code, first.Body.String())
	}
	second := send(server, session.Session{}, http.MethodPost, "/api/v1/auth/login", `{"conversationId":"c1","responses":[{"id":"otp","value":"123456"}]}`)
	if second.Code != http.StatusOK || len(second.Result().Cookies()) != 1 || !strings.Contains(second.Body.String(), "csrfToken") {
		t.Fatalf("second round = %d %s", second.Code, second.Body.String())
	}
	if cookie := second.Result().Cookies()[0]; !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie is not protected: %+v", cookie)
	}
}

func TestLoginMapsFailuresAndRateLimits(t *testing.T) {
	server, _ := newTestServer(t)
	server.authenticator = &stubAuthenticator{err: auth.Code("invalid-conversation")}
	expectProblem(t, send(server, session.Session{}, http.MethodPost, "/api/v1/auth/login", `{"conversationId":"c1","responses":[{"id":"x","value":"y"}]}`), http.StatusBadRequest, "invalid-conversation")
	server.authenticator = &stubAuthenticator{err: auth.Code("authentication-failed")}
	for range 5 {
		expectProblem(t, send(server, session.Session{}, http.MethodPost, "/api/v1/auth/login", `{"username":"operator","password":"wrong"}`), http.StatusUnauthorized, "authentication-failed")
	}
	expectProblem(t, send(server, session.Session{}, http.MethodPost, "/api/v1/auth/login", `{"username":"operator","password":"wrong"}`), http.StatusTooManyRequests, "rate-limited")
	expectProblem(t, send(server, session.Session{}, http.MethodPost, "/api/v1/auth/login", `{"username":"operator","password":"x","extra":1}`), http.StatusBadRequest, "invalid-request")
}

func TestUpdateJobsRunOneAtATimeWithAdministrativeCredentials(t *testing.T) {
	server, broker := newTestServer(t)
	current := signIn(t, server, true)
	release := make(chan struct{})
	broker.respond = func(operation string, _ json.RawMessage) (any, error) {
		if operation == "updates.apply" {
			<-release
			return platform.UpdateResult{Backend: "test", Verified: true}, nil
		}
		return nil, nil
	}
	body := `{"expectedFingerprint":"` + strings.Repeat("a", 64) + `","confirmed":true}`
	first := send(server, current, http.MethodPost, "/api/v1/updates", body)
	if first.Code != http.StatusAccepted {
		t.Fatalf("start = %d %s", first.Code, first.Body.String())
	}
	expectProblem(t, send(server, current, http.MethodPost, "/api/v1/updates", body), http.StatusConflict, "jobs-busy")
	close(release)
	var started struct{ Job Job }
	_ = json.Unmarshal(first.Body.Bytes(), &started)
	deadline := time.Now().Add(2 * time.Second)
	for {
		job, err := server.jobs.Get(started.Job.ID)
		if err == nil && job.State == JobSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not finish: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, call := range broker.calls {
		if call.operation == "updates.apply" && call.credentials != (auth.Credentials{AdminToken: "admin-token"}) {
			t.Fatalf("update ran with %+v", call.credentials)
		}
	}
}

func TestIncidentAcknowledgementIsReflected(t *testing.T) {
	server, broker := newTestServer(t)
	current := signIn(t, server, true)
	occurred := time.Now().Add(-time.Minute).UTC()
	broker.respond = func(string, json.RawMessage) (any, error) {
		return platform.JournalPage{Items: []platform.LogEntry{{ID: "e1", Timestamp: occurred.Format(time.RFC3339Nano), Priority: "2", Unit: "demo.service", Message: "Out of memory: Killed process 42"}}}, nil
	}
	var timeline platform.IncidentTimeline
	_ = json.Unmarshal(send(server, current, http.MethodGet, "/api/v1/incidents", "").Body.Bytes(), &timeline)
	if len(timeline.Items) != 1 || timeline.Items[0].State != "open" {
		t.Fatalf("timeline = %+v", timeline)
	}
	if response := send(server, signIn(t, server, false), http.MethodPost, "/api/v1/incidents/"+timeline.Items[0].ID+"/acknowledged", ""); response.Code != http.StatusForbidden {
		t.Fatalf("unprivileged acknowledgement = %d", response.Code)
	}
	send(server, current, http.MethodPost, "/api/v1/incidents/"+timeline.Items[0].ID+"/acknowledged", "")
	_ = json.Unmarshal(send(server, current, http.MethodGet, "/api/v1/incidents", "").Body.Bytes(), &timeline)
	if timeline.Items[0].State != "acknowledged" {
		t.Fatalf("acknowledgement not reflected: %+v", timeline.Items[0])
	}
	occurred = time.Now().Add(time.Second).UTC()
	_ = json.Unmarshal(send(server, current, http.MethodGet, "/api/v1/incidents", "").Body.Bytes(), &timeline)
	if timeline.Items[0].State != "open" {
		t.Fatalf("recurrence did not reopen: %+v", timeline.Items[0])
	}
}

func TestJournalExportNeutralizesSpreadsheetFormulas(t *testing.T) {
	payload, err := encodeJournalCSV([]platform.LogEntry{{Message: "=HYPERLINK(\"x\")"}})
	if err != nil || !strings.Contains(string(payload), `'=HYPERLINK`) {
		t.Fatalf("export = %q, %v", payload, err)
	}
}

func TestParseFileRange(t *testing.T) {
	for _, test := range []struct {
		value      string
		start, end int64
		valid      bool
	}{
		{"bytes=0-9", 0, 9, true},
		{"bytes=90-", 90, 99, true},
		{"bytes=-10", 90, 99, true},
		{"bytes=100-", 0, 0, false},
		{"bytes=0-1,4-5", 0, 0, false},
		{"items=0-1", 0, 0, false},
	} {
		start, end, valid := parseFileRange(test.value, 100)
		if valid != test.valid || (valid && (start != test.start || end != test.end)) {
			t.Fatalf("%s = %d-%d %v", test.value, start, end, valid)
		}
	}
}

func TestGeneratedCertificateIsUsable(t *testing.T) {
	server, _ := newTestServer(t)
	certificate, key, err := server.ensureCertificate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tls.LoadX509KeyPair(certificate, key); err != nil {
		t.Fatal(err)
	}
	again, _, err := server.ensureCertificate()
	if err != nil || again != certificate {
		t.Fatalf("certificate was not reused: %q %v", again, err)
	}
}

func TestLoginSourceAddressComesFromTheConnection(t *testing.T) {
	server, _ := newTestServer(t)
	identity := auth.Identity{Username: "operator", UID: 1000}
	stub := &stubAuthenticator{responses: []auth.ConversationResponse{{Identity: &identity}}}
	server.authenticator = stub
	response := send(server, session.Session{}, http.MethodPost, "/api/v1/auth/login", `{"username":"operator","password":"secret","remoteAddress":"10.9.9.9"}`)
	if response.Code != http.StatusOK || len(stub.requests) != 1 {
		t.Fatalf("login = %d %s", response.Code, response.Body.String())
	}
	if got := stub.requests[0].RemoteAddress; got != "192.0.2.1" {
		t.Fatalf("PAM_RHOST would be %q, want the connection address", got)
	}
}
