package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestViteDevTargetAllowsOnlyLoopback(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty disables proxy", value: "", want: false},
		{name: "loopback ipv4", value: "http://127.0.0.1:5173", want: true},
		{name: "localhost", value: "http://localhost:5173", want: true},
		{name: "loopback ipv6", value: "http://[::1]:5173", want: true},
		{name: "missing port", value: "http://127.0.0.1", want: false},
		{name: "non-http scheme", value: "ftp://127.0.0.1:5173", want: false},
		{name: "lan address rejected", value: "http://192.168.1.10:5173", want: false},
		{name: "external host rejected", value: "http://example.com:5173", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TAKO_VITE_URL", test.value)
			if got := viteDevTarget() != nil; got != test.want {
				t.Fatalf("viteDevTarget() active=%v, want %v for %q", got, test.want, test.value)
			}
		})
	}
}

func TestSpaHandlerProxiesToViteDev(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	vite := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte("vite-dev:" + request.URL.Path))
	}))
	vite.Listener = listener
	vite.Start()
	defer vite.Close()
	t.Setenv("TAKO_VITE_URL", vite.URL)
	t.Setenv("TAKO_DASHBOARD_DIR", "")

	recorder := httptest.NewRecorder()
	spaHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/@vite/client", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "vite-dev:/@vite/client" {
		t.Fatalf("proxy returned %d: %q", recorder.Code, recorder.Body.String())
	}
}

func TestSpaHandlerFallsBackWhenViteDown(t *testing.T) {
	t.Setenv("TAKO_VITE_URL", "http://127.0.0.1:1")
	t.Setenv("TAKO_DASHBOARD_DIR", "")

	recorder := httptest.NewRecorder()
	spaHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("fallback returned %d, want 200 with built assets", recorder.Code)
	}
}

func TestSpaHandlerServesBuiltAssetsWithoutProxy(t *testing.T) {
	t.Setenv("TAKO_VITE_URL", "")
	t.Setenv("TAKO_DASHBOARD_DIR", "")

	recorder := httptest.NewRecorder()
	spaHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("built assets returned %d", recorder.Code)
	}
}

func TestContentSecurityPolicyRelaxesOnlyForDevProxy(t *testing.T) {
	t.Setenv("TAKO_VITE_URL", "")
	t.Setenv("TAKO_DASHBOARD_DIR", "")
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.cancel()
	handler := server.routes()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	prod := recorder.Header().Get("Content-Security-Policy")
	if strings.Contains(prod, "'unsafe-eval'") || !strings.Contains(prod, "default-src 'self'") {
		t.Fatalf("production CSP is not strict: %q", prod)
	}

	t.Setenv("TAKO_VITE_URL", "http://127.0.0.1:5173")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	dev := recorder.Header().Get("Content-Security-Policy")
	if !strings.Contains(dev, "script-src 'self' 'unsafe-inline' 'unsafe-eval'") {
		t.Fatalf("dev proxy CSP does not allow Vite preamble: %q", dev)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil))
	api := recorder.Header().Get("Content-Security-Policy")
	if api != prod {
		t.Fatalf("API CSP changed under dev proxy: got %q want %q", api, prod)
	}
}
