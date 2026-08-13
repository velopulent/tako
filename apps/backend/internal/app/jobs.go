package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/preferences"
	"github.com/velopulent/tako/internal/session"
	"go.uber.org/zap"
)

const hostInventoryJob = "host-inventory"
const softwareUpdateJob = "software-update"

var (
	ErrJobQueueFull     = errors.New("diagnostic job queue is full")
	ErrJobManagerClosed = errors.New("diagnostic jobs are unavailable")
)

type diagnosticJobManager struct {
	store   *preferences.Store
	queue   chan string
	ctx     context.Context
	cancel  context.CancelFunc
	execute func(context.Context, preferences.Job, func(int, string) error) (json.RawMessage, error)

	mu       sync.Mutex
	closed   bool
	inflight map[string]context.CancelFunc
	wg       sync.WaitGroup
}

func newDiagnosticJobManager(parent context.Context, store *preferences.Store, execute func(context.Context, preferences.Job, func(int, string) error) (json.RawMessage, error)) *diagnosticJobManager {
	ctx, cancel := context.WithCancel(parent)
	manager := &diagnosticJobManager{
		store:    store,
		queue:    make(chan string, 32),
		ctx:      ctx,
		cancel:   cancel,
		execute:  execute,
		inflight: make(map[string]context.CancelFunc),
	}
	for range 2 {
		manager.wg.Add(1)
		go manager.worker()
	}
	return manager
}

func (manager *diagnosticJobManager) Submit(ctx context.Context, kind, actor string) (preferences.Job, error) {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return preferences.Job{}, ErrJobManagerClosed
	}
	manager.mu.Unlock()
	if kind != hostInventoryJob {
		return preferences.Job{}, fmt.Errorf("unsupported diagnostic job %q", kind)
	}
	job, err := manager.store.CreateJob(ctx, kind, actor, false)
	if err != nil {
		return preferences.Job{}, err
	}
	select {
	case manager.queue <- job.ID:
		return job, nil
	default:
		_ = manager.store.FailJob(context.Background(), job.ID, "Not queued", ErrJobQueueFull)
		return preferences.Job{}, ErrJobQueueFull
	}
}

func (manager *diagnosticJobManager) SubmitParameterized(ctx context.Context, kind, actor string, dangerous bool, parameters json.RawMessage) (preferences.Job, error) {
	return manager.SubmitParameterizedWithSetup(ctx, kind, actor, dangerous, parameters, nil)
}

// SubmitParameterizedWithSetup persists a job and runs setup after the job ID
// exists but before it becomes visible to a worker. Setup is intentionally
// in-memory only; callers use it for short-lived credentials that must never be
// persisted with durable job parameters.
func (manager *diagnosticJobManager) SubmitParameterizedWithSetup(ctx context.Context, kind, actor string, dangerous bool, parameters json.RawMessage, setup func(string)) (preferences.Job, error) {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return preferences.Job{}, ErrJobManagerClosed
	}
	manager.mu.Unlock()
	if kind != softwareUpdateJob {
		return preferences.Job{}, fmt.Errorf("unsupported parameterized job %q", kind)
	}
	job, err := manager.store.CreateParameterizedJob(ctx, kind, actor, dangerous, parameters)
	if err != nil {
		return preferences.Job{}, err
	}
	if setup != nil {
		setup(job.ID)
	}
	select {
	case manager.queue <- job.ID:
		return job, nil
	default:
		_ = manager.store.FailJob(context.Background(), job.ID, "Not queued", ErrJobQueueFull)
		return preferences.Job{}, ErrJobQueueFull
	}
}

func (manager *diagnosticJobManager) Cancel(ctx context.Context, id string) (preferences.Job, error) {
	job, err := manager.store.CancelJob(ctx, id)
	if err != nil && !errors.Is(err, preferences.ErrJobTerminal) {
		return preferences.Job{}, err
	}
	manager.mu.Lock()
	if cancel, ok := manager.inflight[id]; ok {
		cancel()
	}
	manager.mu.Unlock()
	return job, err
}

func (manager *diagnosticJobManager) worker() {
	defer manager.wg.Done()
	for {
		select {
		case <-manager.ctx.Done():
			return
		case id := <-manager.queue:
			manager.run(id)
		}
	}
}

func (manager *diagnosticJobManager) run(id string) {
	if manager.ctx.Err() != nil {
		return
	}
	if err := manager.store.StartJob(context.Background(), id); err != nil {
		return
	}
	job, err := manager.store.GetJob(context.Background(), id)
	if err != nil || job.State != preferences.JobRunning || job.CancelRequested {
		return
	}
	timeout := 45 * time.Second
	if job.Kind == softwareUpdateJob {
		timeout = 30 * time.Minute
	}
	jobCtx, jobCancel := context.WithTimeout(manager.ctx, timeout)
	manager.mu.Lock()
	manager.inflight[id] = jobCancel
	manager.mu.Unlock()
	defer func() {
		jobCancel()
		manager.mu.Lock()
		delete(manager.inflight, id)
		manager.mu.Unlock()
	}()

	result, runErr := manager.execute(jobCtx, job, func(progress int, message string) error {
		return manager.store.UpdateJobProgress(context.Background(), id, progress, message)
	})
	if jobCtx.Err() != nil {
		return
	}
	if runErr != nil {
		finishCtx, finishCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = manager.store.FailJob(finishCtx, id, "Diagnostic collection failed", errors.New("diagnostic collection failed"))
		finishCancel()
		return
	}
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = manager.store.CompleteJob(finishCtx, id, result)
	finishCancel()
}

func (manager *diagnosticJobManager) Close(ctx context.Context) error {
	manager.mu.Lock()
	if !manager.closed {
		manager.closed = true
		manager.cancel()
	}
	manager.mu.Unlock()
	done := make(chan struct{})
	go func() {
		manager.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (server *Server) jobsList(writer http.ResponseWriter, request *http.Request) {
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			problem(writer, http.StatusBadRequest, "invalid-limit", "Limit must be a positive integer")
			return
		}
		limit = parsed
	}
	items, err := server.preferences.Jobs(request.Context(), limit)
	if err != nil {
		server.logger.Error("diagnostic jobs unavailable", zap.Error(err))
		problem(writer, http.StatusInternalServerError, "jobs-unavailable", "Diagnostic jobs are unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}

func (server *Server) startHostInventoryJob(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, 4<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var body struct{}
	if err := decoder.Decode(&body); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-job-request", "Job request must be an empty JSON object")
		return
	}
	if server.jobs == nil {
		problem(writer, http.StatusServiceUnavailable, "jobs-unavailable", "Diagnostic jobs are unavailable")
		return
	}
	current := request.Context().Value(sessionKey{}).(session.Session)
	job, err := server.jobs.Submit(request.Context(), hostInventoryJob, current.Identity.Username)
	if errors.Is(err, ErrJobQueueFull) {
		problem(writer, http.StatusTooManyRequests, "jobs-busy", "Diagnostic job queue is full")
		return
	}
	if err != nil {
		server.logger.Error("diagnostic job submission failed", zap.Error(err))
		problem(writer, http.StatusInternalServerError, "jobs-unavailable", "Could not start diagnostic job")
		return
	}
	writeJSON(writer, http.StatusAccepted, map[string]any{"job": job})
}

func (server *Server) jobDetail(writer http.ResponseWriter, request *http.Request) {
	id := chi.URLParam(request, "id")
	if id == "" || len(id) > 64 {
		problem(writer, http.StatusBadRequest, "invalid-job-id", "Job id is invalid")
		return
	}
	job, err := server.preferences.GetJob(request.Context(), id)
	if errors.Is(err, preferences.ErrJobNotFound) {
		problem(writer, http.StatusNotFound, "job-not-found", "Diagnostic job was not found")
		return
	}
	if err != nil {
		server.logger.Error("diagnostic job unavailable", zap.Error(err))
		problem(writer, http.StatusInternalServerError, "jobs-unavailable", "Diagnostic jobs are unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"job": job})
}

func (server *Server) cancelJob(writer http.ResponseWriter, request *http.Request) {
	id := chi.URLParam(request, "id")
	if id == "" || len(id) > 64 {
		problem(writer, http.StatusBadRequest, "invalid-job-id", "Job id is invalid")
		return
	}
	if server.jobs == nil {
		problem(writer, http.StatusServiceUnavailable, "jobs-unavailable", "Diagnostic jobs are unavailable")
		return
	}
	job, err := server.jobs.Cancel(request.Context(), id)
	if errors.Is(err, preferences.ErrJobNotFound) {
		problem(writer, http.StatusNotFound, "job-not-found", "Diagnostic job was not found")
		return
	}
	if errors.Is(err, preferences.ErrJobTerminal) {
		problem(writer, http.StatusConflict, "job-not-running", "Diagnostic job is no longer running")
		return
	}
	if err != nil {
		server.logger.Error("diagnostic job cancellation failed", zap.Error(err))
		problem(writer, http.StatusInternalServerError, "jobs-unavailable", "Could not cancel diagnostic job")
		return
	}
	server.deleteUpdateToken(id)
	writeJSON(writer, http.StatusOK, map[string]any{"job": job})
}

func (server *Server) runDiagnosticJob(ctx context.Context, job preferences.Job, update func(int, string) error) (json.RawMessage, error) {
	if job.Kind == softwareUpdateJob {
		return server.runSoftwareUpdateJob(ctx, job, update)
	}
	if job.Kind != hostInventoryJob {
		return nil, fmt.Errorf("unsupported diagnostic job %q", job.Kind)
	}
	if err := update(10, "Reading host identity"); err != nil {
		return nil, err
	}
	info := server.hostInfo(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := update(55, "Inspecting runtime capabilities"); err != nil {
		return nil, err
	}
	capabilities := server.detectCapabilities(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := update(85, "Preparing inventory report"); err != nil {
		return nil, err
	}
	report, err := json.Marshal(struct {
		Host         host.Info             `json:"host"`
		Capabilities []platform.Capability `json:"capabilities"`
	}{Host: info, Capabilities: capabilities})
	if err != nil {
		return nil, err
	}
	return report, update(100, "Inventory ready")
}
