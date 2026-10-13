package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/velopulent/tako/internal/platform"
)

const (
	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
	JobCanceled  = "canceled"

	softwareUpdateJob = "software-update"
	// maxFinishedJobs bounds the in-memory history of completed jobs.
	maxFinishedJobs = 50
	jobTimeout      = 30 * time.Minute
)

var (
	ErrJobBusy          = errors.New("a job is already queued")
	ErrJobManagerClosed = errors.New("jobs are unavailable")
	ErrJobNotFound      = errors.New("job not found")
	ErrJobTerminal      = errors.New("job is already complete")
)

// Job is a long-running administrative operation. Its credentials live only
// in the closure the job runs; they never appear in the record.
type Job struct {
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	Actor           string          `json:"actor"`
	State           string          `json:"state"`
	Progress        int             `json:"progress"`
	Message         string          `json:"message"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           string          `json:"error,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	StartedAt       time.Time       `json:"startedAt,omitzero"`
	CompletedAt     time.Time       `json:"completedAt,omitzero"`
	CancelRequested bool            `json:"cancelRequested"`
}

// jobWork runs a job. progress records a step; it fails once the job was
// canceled. The returned code is the job's public error on failure.
type jobWork func(ctx context.Context, progress func(int, string) error) (json.RawMessage, error)

// jobManager runs one job at a time. Jobs are in memory and end with the
// gateway; an update in flight keeps running inside sessiond.
type jobManager struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	closed   bool
	jobs     map[string]*Job
	inflight map[string]context.CancelFunc
	busy     bool
	busyHook func(bool)
}

func newJobManager(parent context.Context) *jobManager {
	ctx, cancel := context.WithCancel(parent)
	return &jobManager{ctx: ctx, cancel: cancel, jobs: make(map[string]*Job), inflight: make(map[string]context.CancelFunc)}
}

func (manager *jobManager) SetBusyHook(hook func(bool)) {
	manager.mu.Lock()
	manager.busyHook = hook
	busy := manager.busy
	manager.mu.Unlock()
	hook(busy)
}

func (manager *jobManager) setBusyLocked(busy bool) func() {
	if manager.busy == busy || manager.busyHook == nil {
		manager.busy = busy
		return func() {}
	}
	manager.busy = busy
	hook := manager.busyHook
	return func() { hook(busy) }
}

func newJobID() string {
	buffer := make([]byte, 18)
	_, _ = rand.Read(buffer)
	return base64.RawURLEncoding.EncodeToString(buffer)
}

// Submit starts work unless another job is pending or running.
func (manager *jobManager) Submit(kind, actor string, work jobWork) (Job, error) {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return Job{}, ErrJobManagerClosed
	}
	if manager.busy {
		manager.mu.Unlock()
		return Job{}, ErrJobBusy
	}
	job := &Job{ID: newJobID(), Kind: kind, Actor: actor, State: JobPending, Message: "Queued", CreatedAt: time.Now().UTC()}
	manager.jobs[job.ID] = job
	ctx, cancel := context.WithTimeout(manager.ctx, jobTimeout)
	manager.inflight[job.ID] = cancel
	notify := manager.setBusyLocked(true)
	snapshot := *job
	manager.wg.Add(1)
	manager.mu.Unlock()
	notify()
	go manager.run(ctx, job.ID, work)
	return snapshot, nil
}

func (manager *jobManager) run(ctx context.Context, id string, work jobWork) {
	defer manager.wg.Done()
	manager.update(id, func(job *Job) {
		job.State, job.StartedAt, job.Message, job.Progress = JobRunning, time.Now().UTC(), "Running", 1
	})
	result, err := work(ctx, func(progress int, message string) error {
		var canceled bool
		manager.update(id, func(job *Job) {
			if job.State != JobRunning {
				canceled = true
				return
			}
			job.Progress, job.Message = progress, message
		})
		if canceled {
			return context.Canceled
		}
		return nil
	})
	manager.update(id, func(job *Job) {
		job.CompletedAt = time.Now().UTC()
		switch {
		case job.State == JobCanceled:
		case err != nil && ctx.Err() != nil:
			job.State, job.Message, job.Error = JobCanceled, "Canceled", "canceled"
		case err != nil:
			job.State, job.Message, job.Error = JobFailed, "Failed", err.Error()
		default:
			job.State, job.Message, job.Progress, job.Result = JobSucceeded, "Completed", 100, result
		}
	})
	manager.mu.Lock()
	if cancel, ok := manager.inflight[id]; ok {
		cancel()
		delete(manager.inflight, id)
	}
	manager.pruneLocked()
	notify := manager.setBusyLocked(false)
	manager.mu.Unlock()
	notify()
}

func (manager *jobManager) update(id string, change func(*Job)) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if job, ok := manager.jobs[id]; ok {
		change(job)
	}
}

// pruneLocked keeps the newest finished jobs.
func (manager *jobManager) pruneLocked() {
	finished := make([]*Job, 0, len(manager.jobs))
	for _, job := range manager.jobs {
		if !job.CompletedAt.IsZero() {
			finished = append(finished, job)
		}
	}
	if len(finished) <= maxFinishedJobs {
		return
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].CompletedAt.After(finished[j].CompletedAt) })
	for _, job := range finished[maxFinishedJobs:] {
		delete(manager.jobs, job.ID)
	}
}

func (manager *jobManager) Get(id string) (Job, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	job, ok := manager.jobs[id]
	if !ok {
		return Job{}, ErrJobNotFound
	}
	return *job, nil
}

func (manager *jobManager) List(limit int) []Job {
	manager.mu.Lock()
	items := make([]Job, 0, len(manager.jobs))
	for _, job := range manager.jobs {
		items = append(items, *job)
	}
	manager.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items[:min(len(items), limit)]
}

// Cancel stops a pending or running job.
func (manager *jobManager) Cancel(id string) (Job, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	job, ok := manager.jobs[id]
	if !ok {
		return Job{}, ErrJobNotFound
	}
	if job.State != JobPending && job.State != JobRunning {
		return *job, ErrJobTerminal
	}
	job.State, job.CancelRequested, job.Message = JobCanceled, true, "Canceled by operator"
	if cancel, ok := manager.inflight[id]; ok {
		cancel()
	}
	return *job, nil
}

func (manager *jobManager) Close(ctx context.Context) error {
	manager.mu.Lock()
	manager.closed = true
	manager.mu.Unlock()
	manager.cancel()
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

// Jobs are administrative records: listing and inspecting them needs the same
// authority that starts them.
func (server *Server) jobsList(writer http.ResponseWriter, request *http.Request) {
	if !requireAdmin(writer, currentSession(request)) {
		return
	}
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			problem(writer, http.StatusBadRequest, "invalid-limit", "Limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": server.jobs.List(limit)})
}

func (server *Server) jobDetail(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	if !requireAdmin(writer, current) {
		return
	}
	job, err := server.jobs.Get(chi.URLParam(request, "id"))
	if err != nil {
		problem(writer, http.StatusNotFound, "job-not-found", "The job was not found")
		return
	}
	payload := map[string]any{"job": job}
	if job.Kind == softwareUpdateJob {
		var observation platform.UpdateObservation
		if server.broker.Call(request.Context(), "updates.live", sessionCredentials(current), nil, &observation) == nil {
			payload["update"] = observation
		}
	}
	writeJSON(writer, http.StatusOK, payload)
}

func (server *Server) cancelJob(writer http.ResponseWriter, request *http.Request) {
	if !requireAdmin(writer, currentSession(request)) {
		return
	}
	id := chi.URLParam(request, "id")
	current, err := server.jobs.Get(id)
	if err != nil {
		problem(writer, http.StatusNotFound, "job-not-found", "The job was not found")
		return
	}
	if current.Kind == softwareUpdateJob && current.State == JobRunning && current.Progress >= 20 {
		problem(writer, http.StatusConflict, "update-not-cancelable", "The package transaction has started and cannot be canceled safely")
		return
	}
	job, err := server.jobs.Cancel(id)
	if errors.Is(err, ErrJobTerminal) {
		problem(writer, http.StatusConflict, "job-not-running", "The job is no longer running")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"job": job})
}
