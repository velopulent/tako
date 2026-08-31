package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
)

func closeIdleTestServer(server *Server) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func TestActiveHTTPRequestBlocksIdleExit(t *testing.T) {
	cfg := testConfig(t)
	cfg.ServiceIdleTimeout = 30 * time.Millisecond
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeIdleTestServer(server)
	started := make(chan struct{})
	release := make(chan struct{})
	handler := server.activityMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	}))
	server.idle.Start()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		close(done)
	}()
	<-started
	time.Sleep(50 * time.Millisecond)
	select {
	case <-server.Idle():
		t.Fatal("gateway became idle during active request")
	default:
	}
	close(release)
	<-done
	select {
	case <-server.Idle():
	case <-time.After(time.Second):
		t.Fatal("gateway did not become idle after request finished")
	}
}

func TestLoginSessionBlocksIdleExitUntilDeleted(t *testing.T) {
	cfg := testConfig(t)
	cfg.ServiceIdleTimeout = 20 * time.Millisecond
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeIdleTestServer(server)
	created, err := server.sessions.Create(auth.Identity{Username: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	server.idle.Start()
	time.Sleep(35 * time.Millisecond)
	select {
	case <-server.Idle():
		t.Fatal("gateway became idle with valid session")
	default:
	}
	server.sessions.Delete(created.ID)
	select {
	case <-server.Idle():
	case <-time.After(time.Second):
		t.Fatal("gateway did not become idle after session deletion")
	}
}

func TestDirectGatewayDoesNotStartIdleTracker(t *testing.T) {
	cfg := testConfig(t)
	cfg.ServiceIdleTimeout = 10 * time.Millisecond
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeIdleTestServer(server)
	select {
	case <-server.Idle():
		t.Fatal("direct gateway idle tracker started")
	case <-time.After(30 * time.Millisecond):
	}
}
