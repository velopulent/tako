package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/velopulent/tako/internal/config"
)

func TestOriginAllowedDefaultsToRequestOrigin(t *testing.T) {
	tests := []struct {
		name   string
		target string
		origin string
		want   bool
	}{
		{name: "https ip address", target: "https://192.168.0.4:9090/api/v1/auth/login", origin: "https://192.168.0.4:9090", want: true},
		{name: "https hostname", target: "https://tako.local:9090/api/v1/auth/login", origin: "https://tako.local:9090", want: true},
		{name: "http development", target: "http://192.168.0.4:9090/api/v1/auth/login", origin: "http://192.168.0.4:9090", want: true},
		{name: "trailing slash", target: "https://192.168.0.4:9090/api/v1/auth/login", origin: "https://192.168.0.4:9090/", want: true},
		{name: "foreign origin", target: "https://192.168.0.4:9090/api/v1/auth/login", origin: "https://evil.example", want: false},
		{name: "missing origin", target: "https://192.168.0.4:9090/api/v1/auth/login", want: false},
		{name: "wrong scheme", target: "https://192.168.0.4:9090/api/v1/auth/login", origin: "http://192.168.0.4:9090", want: false},
	}

	server := &Server{config: config.Default()}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.target, nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if got := server.originAllowed(request); got != test.want {
				t.Fatalf("originAllowed() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestOriginAllowedExplicitOriginsOverrideSameOriginDefault(t *testing.T) {
	server := &Server{config: config.Config{AllowedOrigins: []string{"https://console.example"}}}

	allowed := httptest.NewRequest(http.MethodPost, "https://192.168.0.4:9090/api/v1/auth/login", nil)
	allowed.Header.Set("Origin", "https://console.example")
	if !server.originAllowed(allowed) {
		t.Fatal("configured origin was rejected")
	}

	direct := httptest.NewRequest(http.MethodPost, "https://192.168.0.4:9090/api/v1/auth/login", nil)
	direct.Header.Set("Origin", "https://192.168.0.4:9090")
	if server.originAllowed(direct) {
		t.Fatal("same-origin request bypassed explicit allowlist")
	}
}

func TestOriginAllowedDevelopmentAcceptsMissingOrigin(t *testing.T) {
	server := &Server{config: config.Config{Development: true}}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9090/api/v1/auth/login", nil)
	if !server.originAllowed(request) {
		t.Fatal("development request without Origin was rejected")
	}
}
