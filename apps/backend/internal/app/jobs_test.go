package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/config"
	"github.com/velopulent/tako/internal/platform"
)

func waitForJob(t *testing.T, manager *diagnosticJobManager, id string, want func(Job) bool) Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.GetJob(id)
		if err == nil && want(job) {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, err := manager.GetJob(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("job did not reach expected state: %#v", job)
	return Job{}
}

func TestDiagnosticJobManagerPersistsProgressAndResult(t *testing.T) {
	manager := newDiagnosticJobManager(context.Background(), func(_ context.Context, _ Job, update func(int, string) error) (json.RawMessage, error) {
		if err := update(45, "Collecting"); err != nil {
			return nil, err
		}
		return json.RawMessage(`{"host":{"hostname":"test"}}`), nil
	})
	defer manager.Close(context.Background())
	job, err := manager.Submit(context.Background(), hostInventoryJob, "operator")
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForJob(t, manager, job.ID, func(item Job) bool {
		return item.State == JobSucceeded
	})
	if completed.Progress != 100 || string(completed.Result) != `{"host":{"hostname":"test"}}` {
		t.Fatalf("job result was not persisted: %#v", completed)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticJobCancellationStopsExecution(t *testing.T) {
	started := make(chan struct{})
	manager := newDiagnosticJobManager(context.Background(), func(ctx context.Context, _ Job, _ func(int, string) error) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	defer manager.Close(context.Background())
	job, err := manager.Submit(context.Background(), hostInventoryJob, "operator")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("job did not start")
	}
	canceled, err := manager.Cancel(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if canceled.State != JobCanceled {
		t.Fatalf("cancel response was %#v", canceled)
	}
	waitForJob(t, manager, job.ID, func(item Job) bool {
		return item.State == JobCanceled
	})
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticJobManagerReportsBusyState(t *testing.T) {
	release := make(chan struct{})
	manager := newDiagnosticJobManager(context.Background(), func(context.Context, Job, func(int, string) error) (json.RawMessage, error) {
		<-release
		return json.RawMessage(`{}`), nil
	})
	defer manager.Close(context.Background())
	states := make(chan bool, 4)
	manager.SetBusyHook(func(busy bool) { states <- busy })
	if _, err := manager.Submit(context.Background(), hostInventoryJob, "operator"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []bool{false, true} {
		select {
		case got := <-states:
			if got != want {
				t.Fatalf("busy=%t, want %t", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing busy=%t notification", want)
		}
	}
	close(release)
	select {
	case busy := <-states:
		if busy {
			t.Fatal("manager remained busy after job completion")
		}
	case <-time.After(time.Second):
		t.Fatal("missing idle notification")
	}
}

func TestProductionInventoryJobRequiresEphemeralAuthority(t *testing.T) {
	server := &Server{
		config:               config.Default(),
		inventoryCredentials: make(map[string]auth.HostReadCredentials),
	}
	_, err := server.runDiagnosticJob(context.Background(), Job{ID: "recovered", Kind: hostInventoryJob}, func(int, string) error { return nil })
	if !errors.Is(err, auth.ErrServiceUnavailable) {
		t.Fatalf("inventory without live host grant returned %v, want service unavailable", err)
	}
}

func TestDiagnosticJobsHTTPWorkflow(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		server.cancel()
		_ = server.jobs.Close(context.Background())
	}()
	server.detectCapabilities = func(context.Context) []platform.Capability { return nil }
	cookie, csrf := loginForTest(t, server.routes())

	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/host-inventory", bytes.NewBufferString(`{}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("job start returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var startResponse struct {
		Job Job `json:"job"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &startResponse); err != nil {
		t.Fatal(err)
	}
	if startResponse.Job.ID == "" || startResponse.Job.State != JobPending {
		t.Fatalf("invalid start response: %#v", startResponse)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+startResponse.Job.ID, nil)
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("job detail returned %d: %s", recorder.Code, recorder.Body.String())
	}

	for _, payload := range []string{`{"unexpected":true}`, `{} {}`} {
		request = httptest.NewRequest(http.MethodPost, "/api/v1/jobs/host-inventory", bytes.NewBufferString(payload))
		request.AddCookie(cookie)
		request.Header.Set("X-CSRF-Token", csrf)
		recorder = httptest.NewRecorder()
		server.routes().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid job payload %q returned %d", payload, recorder.Code)
		}
	}
}

func TestDiagnosticJobsHTTPCanBeCanceled(t *testing.T) {
	server, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		server.cancel()
		_ = server.jobs.Close(context.Background())
	}()
	server.detectCapabilities = func(ctx context.Context) []platform.Capability {
		<-ctx.Done()
		return nil
	}
	cookie, csrf := loginForTest(t, server.routes())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/host-inventory", bytes.NewBufferString(`{}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("job start returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Job Job `json:"job"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	waitForJob(t, server.jobs, response.Job.ID, func(job Job) bool {
		return job.State == JobRunning
	})

	request = httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+response.Job.ID+"/cancel", nil)
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"state":"canceled"`)) {
		t.Fatalf("job cancel returned %d: %s", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+response.Job.ID+"/cancel", nil)
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cancel without CSRF returned %d", recorder.Code)
	}
}
