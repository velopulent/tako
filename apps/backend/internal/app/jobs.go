package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
	"go.uber.org/zap"
)

const hostInventoryJob = "host-inventory"
const softwareUpdateJob = "software-update"

const (
	JobPending     = "pending"
	JobRunning     = "running"
	JobSucceeded   = "succeeded"
	JobFailed      = "failed"
	JobCanceled    = "canceled"
	JobInterrupted = "interrupted"
)

var (
	ErrJobQueueFull     = errors.New("diagnostic job queue is full")
	ErrJobManagerClosed = errors.New("diagnostic jobs are unavailable")
	ErrJobNotFound      = errors.New("job not found")
	ErrJobTerminal      = errors.New("job is already complete")
)

type Job struct {
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	Actor           string          `json:"actor"`
	Parameters      json.RawMessage `json:"-"`
	State           string          `json:"state"`
	Progress        int             `json:"progress"`
	Message         string          `json:"message"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           string          `json:"error,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	StartedAt       time.Time       `json:"startedAt,omitempty"`
	CompletedAt     time.Time       `json:"completedAt,omitempty"`
	CancelRequested bool            `json:"cancelRequested"`
	Dangerous       bool            `json:"dangerous"`
}

func jobID() string {
	buffer := make([]byte, 18)
	if _, err := rand.Read(buffer); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}

type diagnosticJobManager struct {
	queue   chan string
	ctx     context.Context
	cancel  context.CancelFunc
	execute func(context.Context, Job, func(int, string) error) (json.RawMessage, error)

	mu       sync.Mutex
	notifyMu sync.Mutex
	closed   bool
	jobs     map[string]*Job
	inflight map[string]context.CancelFunc
	pending  int
	busyHook func(bool)
	wg       sync.WaitGroup
}

func (manager *diagnosticJobManager) SetBusyHook(hook func(bool)) {
	manager.mu.Lock()
	manager.busyHook = hook
	manager.mu.Unlock()
	manager.notifyBusy()
}

func (manager *diagnosticJobManager) addPending(delta int) {
	manager.mu.Lock()
	wasBusy := manager.pending > 0
	manager.pending += delta
	if manager.pending < 0 {
		manager.pending = 0
	}
	busy := manager.pending > 0
	manager.mu.Unlock()
	if wasBusy != busy {
		manager.notifyBusy()
	}
}

func (manager *diagnosticJobManager) notifyBusy() {
	manager.notifyMu.Lock()
	defer manager.notifyMu.Unlock()
	manager.mu.Lock()
	hook := manager.busyHook
	busy := manager.pending > 0
	manager.mu.Unlock()
	if hook != nil {
		hook(busy)
	}
}

func newDiagnosticJobManager(parent context.Context, execute func(context.Context, Job, func(int, string) error) (json.RawMessage, error)) *diagnosticJobManager {
	ctx, cancel := context.WithCancel(parent)
	manager := &diagnosticJobManager{
		queue:    make(chan string, 32),
		ctx:      ctx,
		cancel:   cancel,
		execute:  execute,
		jobs:     make(map[string]*Job),
		inflight: make(map[string]context.CancelFunc),
	}
	for range 2 {
		manager.wg.Add(1)
		go manager.worker()
	}
	return manager
}

func (manager *diagnosticJobManager) createJob(kind, actor string, dangerous bool, parameters json.RawMessage) (Job, error) {
	if kind == "" || len(kind) > 64 || actor == "" || len(actor) > 256 {
		return Job{}, errors.New("invalid job")
	}
	if len(parameters) > 64<<10 || (len(parameters) > 0 && !json.Valid(parameters)) {
		return Job{}, errors.New("invalid job parameters")
	}
	job := Job{
		ID:         jobID(),
		Kind:       kind,
		Actor:      actor,
		Parameters: append(json.RawMessage(nil), parameters...),
		State:      JobPending,
		Message:    "Queued",
		CreatedAt:  time.Now().UTC(),
		Dangerous:  dangerous,
	}
	manager.mu.Lock()
	manager.jobs[job.ID] = &job
	manager.mu.Unlock()
	return job, nil
}

func (manager *diagnosticJobManager) GetJob(id string) (Job, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	job, ok := manager.jobs[id]
	if !ok {
		return Job{}, ErrJobNotFound
	}
	return *job, nil
}

func (manager *diagnosticJobManager) Jobs(limit int) []Job {
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	items := make([]Job, 0, len(manager.jobs))
	for _, job := range manager.jobs {
		items = append(items, *job)
	}
	// Newest first, bounded.
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].CreatedAt.After(items[i].CreatedAt) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (manager *diagnosticJobManager) Submit(ctx context.Context, kind, actor string) (Job, error) {
	return manager.SubmitWithSetup(ctx, kind, actor, nil)
}

// SubmitWithSetup lets a caller attach short-lived in-memory authority to a
// job ID before a worker can observe it.
func (manager *diagnosticJobManager) SubmitWithSetup(ctx context.Context, kind, actor string, setup func(string)) (Job, error) {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return Job{}, ErrJobManagerClosed
	}
	manager.mu.Unlock()
	if kind != hostInventoryJob {
		return Job{}, fmt.Errorf("unsupported diagnostic job %q", kind)
	}
	job, err := manager.createJob(kind, actor, false, nil)
	if err != nil {
		return Job{}, err
	}
	if setup != nil {
		setup(job.ID)
	}
	manager.addPending(1)
	select {
	case manager.queue <- job.ID:
		return job, nil
	default:
		manager.addPending(-1)
		_ = manager.failJob(job.ID, "Not queued", ErrJobQueueFull)
		return Job{}, ErrJobQueueFull
	}
}

func (manager *diagnosticJobManager) SubmitParameterized(ctx context.Context, kind, actor string, dangerous bool, parameters json.RawMessage) (Job, error) {
	return manager.SubmitParameterizedWithSetup(ctx, kind, actor, dangerous, parameters, nil)
}

// SubmitParameterizedWithSetup keeps credentials out of the job record;
// setup runs after the ID exists but before workers observe it.
func (manager *diagnosticJobManager) SubmitParameterizedWithSetup(ctx context.Context, kind, actor string, dangerous bool, parameters json.RawMessage, setup func(string)) (Job, error) {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return Job{}, ErrJobManagerClosed
	}
	manager.mu.Unlock()
	if kind != softwareUpdateJob {
		return Job{}, fmt.Errorf("unsupported parameterized job %q", kind)
	}
	job, err := manager.createJob(kind, actor, dangerous, parameters)
	if err != nil {
		return Job{}, err
	}
	if setup != nil {
		setup(job.ID)
	}
	manager.addPending(1)
	select {
	case manager.queue <- job.ID:
		return job, nil
	default:
		manager.addPending(-1)
		_ = manager.failJob(job.ID, "Not queued", ErrJobQueueFull)
		return Job{}, ErrJobQueueFull
	}
}

func (manager *diagnosticJobManager) Cancel(ctx context.Context, id string) (Job, error) {
	manager.mu.Lock()
	job, ok := manager.jobs[id]
	if !ok {
		manager.mu.Unlock()
		return Job{}, ErrJobNotFound
	}
	if job.State != JobPending && job.State != JobRunning {
		snapshot := *job
		manager.mu.Unlock()
		return snapshot, ErrJobTerminal
	}
	job.State = JobCanceled
	job.CancelRequested = true
	job.Message = "Canceled by operator"
	job.CompletedAt = time.Now().UTC()
	snapshot := *job
	cancel, ok := manager.inflight[id]
	manager.mu.Unlock()
	if ok {
		cancel()
	}
	return snapshot, nil
}

func (manager *diagnosticJobManager) startJob(id string) bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	job, ok := manager.jobs[id]
	if !ok || job.State != JobPending || job.CancelRequested {
		return false
	}
	job.State = JobRunning
	job.StartedAt = time.Now().UTC()
	job.Message = "Running"
	job.Progress = 1
	return true
}

func (manager *diagnosticJobManager) updateProgress(id string, progress int, message string) error {
	if progress < 0 || progress > 100 || len(message) > 256 {
		return errors.New("invalid job progress")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	job, ok := manager.jobs[id]
	if !ok || job.State != JobRunning {
		return ErrJobTerminal
	}
	job.Progress = progress
	job.Message = message
	return nil
}

func (manager *diagnosticJobManager) completeJob(id string, result json.RawMessage) error {
	if len(result) > 1<<20 || (len(result) > 0 && !json.Valid(result)) {
		return errors.New("invalid job result")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	job, ok := manager.jobs[id]
	if !ok || job.State != JobRunning || job.CancelRequested {
		return ErrJobTerminal
	}
	job.State = JobSucceeded
	job.Progress = 100
	job.Message = "Completed"
	job.Result = result
	job.CompletedAt = time.Now().UTC()
	return nil
}

func (manager *diagnosticJobManager) failJob(id, message string, jobErr error) error {
	if message == "" || len(message) > 256 {
		return errors.New("invalid job failure")
	}
	if jobErr == nil {
		jobErr = errors.New("job failed")
	}
	publicError := jobErr.Error()
	if len(publicError) > 1024 {
		publicError = publicError[:1024]
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	job, ok := manager.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	if job.State != JobPending && job.State != JobRunning {
		return ErrJobTerminal
	}
	job.State = JobFailed
	job.Message = message
	job.Error = publicError
	job.CompletedAt = time.Now().UTC()
	return nil
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
	defer manager.addPending(-1)
	if manager.ctx.Err() != nil {
		return
	}
	if !manager.startJob(id) {
		return
	}
	job, err := manager.GetJob(id)
	if err != nil || job.State != JobRunning || job.CancelRequested {
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
		return manager.updateProgress(id, progress, message)
	})
	if jobCtx.Err() != nil || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		return
	}
	if runErr != nil {
		_ = manager.failJob(id, "Diagnostic collection failed", errors.New("diagnostic collection failed"))
		return
	}
	_ = manager.completeJob(id, result)
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
	writeJSON(writer, http.StatusOK, map[string]any{"items": server.jobs.Jobs(limit)})
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
	credentials := credentialsFromContext(request.Context())
	var submittedID string
	job, err := server.jobs.SubmitWithSetup(request.Context(), hostInventoryJob, current.Identity.Username, func(id string) {
		submittedID = id
		server.inventoryCredentialsMu.Lock()
		if server.inventoryCredentials == nil {
			server.inventoryCredentials = make(map[string]auth.HostReadCredentials)
		}
		server.inventoryCredentials[id] = credentials
		server.inventoryCredentialsMu.Unlock()
	})
	if errors.Is(err, ErrJobQueueFull) {
		server.deleteInventoryCredentials(submittedID)
		problem(writer, http.StatusTooManyRequests, "jobs-busy", "Diagnostic job queue is full")
		return
	}
	if err != nil {
		server.deleteInventoryCredentials(submittedID)
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
	job, err := server.jobs.GetJob(id)
	if errors.Is(err, ErrJobNotFound) {
		problem(writer, http.StatusNotFound, "job-not-found", "Diagnostic job was not found")
		return
	}
	payload := map[string]any{"job": job}
	// Software-update jobs carry a live observation of the PackageKit
	// transaction so the UI can render real progress even though sessiond
	// performs the update.
	if job.Kind == softwareUpdateJob {
		observation := auth.UpdateObservation{Live: platform.InactiveUpdateLive()}
		if server.readUpdateLiveFn != nil {
			if current, readErr := server.readUpdateLiveFn(request.Context()); readErr == nil {
				observation = current
			}
		}
		payload["update"] = observation
	}
	writeJSON(writer, http.StatusOK, payload)
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
	server.deleteInventoryCredentials(id)
	if errors.Is(err, ErrJobNotFound) {
		problem(writer, http.StatusNotFound, "job-not-found", "Diagnostic job was not found")
		return
	}
	if errors.Is(err, ErrJobTerminal) {
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

func (server *Server) runDiagnosticJob(ctx context.Context, job Job, update func(int, string) error) (json.RawMessage, error) {
	if job.Kind == softwareUpdateJob {
		return server.runSoftwareUpdateJob(ctx, job, update)
	}
	if job.Kind != hostInventoryJob {
		return nil, fmt.Errorf("unsupported diagnostic job %q", job.Kind)
	}
	if err := update(10, "Reading host identity"); err != nil {
		return nil, err
	}
	credentials := server.takeInventoryCredentials(job.ID)
	if !server.config.Development && credentials.Token == "" && credentials.AdminToken == "" {
		// Jobs are in-memory only. Missing authority means the request
		// context is gone; never fall back to gateway-local reads.
		return nil, auth.ErrServiceUnavailable
	}
	jobContext := context.WithValue(ctx, hostCredentialsKey{}, credentials)
	info := server.hostInfo(jobContext)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := update(55, "Inspecting runtime capabilities"); err != nil {
		return nil, err
	}
	capabilities := server.detectCapabilities(jobContext)
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

func (server *Server) takeInventoryCredentials(id string) auth.HostReadCredentials {
	server.inventoryCredentialsMu.Lock()
	defer server.inventoryCredentialsMu.Unlock()
	credentials := server.inventoryCredentials[id]
	delete(server.inventoryCredentials, id)
	return credentials
}

func (server *Server) deleteInventoryCredentials(id string) {
	if id == "" {
		return
	}
	server.inventoryCredentialsMu.Lock()
	delete(server.inventoryCredentials, id)
	server.inventoryCredentialsMu.Unlock()
}

func (server *Server) clearInventoryCredentials() {
	server.inventoryCredentialsMu.Lock()
	server.inventoryCredentials = make(map[string]auth.HostReadCredentials)
	server.inventoryCredentialsMu.Unlock()
}
