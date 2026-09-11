package platform

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	MaxUpdatePackages = 500
	MaxUpdateHistory  = 20
	maxUpdateOutput   = 4 << 20
	maxLiveOutput     = 1 << 20
	maxLiveEvents     = 500
	maxLiveLine       = 8 << 10
)

var ansiUpdatePattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

var (
	ErrInvalidUpdateOperation = errors.New("invalid update operation")
	ErrUpdateConflict         = errors.New("update inventory changed")
	ErrUpdateLocked           = errors.New("package manager lock is held")
	ErrUpdateUnavailable      = errors.New("update backend unavailable")
	ErrUpdateVerification     = errors.New("update verification failed")
	ErrUpdateApply            = errors.New("update command failed")
	ErrUpdateRiskNotAccepted  = errors.New("risky update plan was not accepted")
)

type UpdatePackage struct {
	Name             string   `json:"name"`
	Architecture     string   `json:"architecture,omitempty"`
	CurrentVersion   string   `json:"currentVersion,omitempty"`
	CandidateVersion string   `json:"candidateVersion"`
	Severity         string   `json:"severity,omitempty"`
	SecSeverity      string   `json:"secSeverity,omitempty"`
	Size             uint64   `json:"size,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	Details          string   `json:"details,omitempty"`
	AdvisoryID       string   `json:"advisoryId,omitempty"`
	CVEUrls          []string `json:"cveUrls,omitempty"`
	BugUrls          []string `json:"bugUrls,omitempty"`
	VendorUrls       []string `json:"vendorUrls,omitempty"`
	Description      string   `json:"description,omitempty"`
	Markdown         bool     `json:"markdown,omitempty"`
	GroupKey         string   `json:"groupKey,omitempty"`
	Dependencies     []string `json:"dependencies,omitempty"`
}

type UpdateRecovery struct {
	Authoritative   bool     `json:"authoritative"`
	RebootRequired  bool     `json:"rebootRequired"`
	RestartServices []string `json:"restartServices"`
	RebootPackages  []string `json:"rebootPackages,omitempty"`
	ManualPackages  []string `json:"manualPackages,omitempty"`
	Hints           []string `json:"hints"`
	Source          string   `json:"source"`
	Reason          string   `json:"reason,omitempty"`
}

type UpdateStatus struct {
	Available        bool            `json:"available"`
	Backend          string          `json:"backend"`
	Version          string          `json:"version,omitempty"`
	Contract         string          `json:"contract"`
	Packages         []UpdatePackage `json:"packages"`
	Fingerprint      string          `json:"fingerprint"`
	ExternalLock     bool            `json:"externalLock"`
	LockReason       string          `json:"lockReason,omitempty"`
	Message          string          `json:"message"`
	Reason           string          `json:"reason,omitempty"`
	Recovery         UpdateRecovery  `json:"recovery"`
	LastChecked      string          `json:"lastChecked,omitempty"`
	TimeSinceRefresh *int64          `json:"timeSinceRefresh,omitempty"`
}

type UpdateChange struct {
	Action            string `json:"action"`
	Name              string `json:"name"`
	Architecture      string `json:"architecture,omitempty"`
	CurrentVersion    string `json:"currentVersion,omitempty"`
	CandidateVersion  string `json:"candidateVersion,omitempty"`
	CurrentRepository string `json:"currentRepository,omitempty"`
	TargetRepository  string `json:"targetRepository,omitempty"`
	CurrentVendor     string `json:"currentVendor,omitempty"`
	TargetVendor      string `json:"targetVendor,omitempty"`
}

func (change UpdateChange) Risky() bool {
	return change.Action == "remove" || change.Action == "downgrade" || change.Action == "replace" ||
		(change.CurrentRepository != "" && change.TargetRepository != "" && change.CurrentRepository != change.TargetRepository) ||
		(change.CurrentVendor != "" && change.TargetVendor != "" && change.CurrentVendor != change.TargetVendor)
}

type UpdateOperation struct {
	ExpectedFingerprint string `json:"expectedFingerprint"`
	Confirmed           bool   `json:"confirmed"`
	RiskAccepted        bool   `json:"riskAccepted,omitempty"`
	JobID               string `json:"-"`
}

type UpdatePreview struct {
	Current                  UpdateStatus   `json:"current"`
	Changes                  []UpdateChange `json:"changes"`
	Warnings                 []string       `json:"warnings"`
	Fingerprint              string         `json:"fingerprint"`
	Stale                    bool           `json:"stale"`
	Allowed                  bool           `json:"allowed"`
	RequiresConfirmation     bool           `json:"requiresConfirmation"`
	RequiresRiskConfirmation bool           `json:"requiresRiskConfirmation"`
	Reason                   string         `json:"reason,omitempty"`
}

type UpdateResult struct {
	Backend     string         `json:"backend"`
	Changes     []UpdateChange `json:"changes"`
	Verified    bool           `json:"verified"`
	Message     string         `json:"message"`
	Fingerprint string         `json:"fingerprint"`
	Recovery    UpdateRecovery `json:"recovery"`
}

type UpdateHistoryEntry struct {
	Time     int64             `json:"time"`
	Packages map[string]string `json:"packages"`
}

type UpdateProgress struct {
	Sequence   uint64 `json:"sequence"`
	JobID      string `json:"jobId,omitempty"`
	Active     bool   `json:"active"`
	Phase      string `json:"phase"`
	Package    string `json:"package,omitempty"`
	Current    int    `json:"current"`
	Total      int    `json:"total"`
	Percent    int    `json:"percent"`
	Message    string `json:"message"`
	Cancelable bool   `json:"cancelable"`
	Timestamp  string `json:"timestamp"`
}

type UpdateOutput struct {
	Sequence  uint64 `json:"sequence"`
	JobID     string `json:"jobId,omitempty"`
	Stream    string `json:"stream"`
	Line      string `json:"line"`
	Timestamp string `json:"timestamp"`
}

type UpdateStreamEvent struct {
	Kind     string         `json:"kind"`
	Progress UpdateProgress `json:"progress,omitempty"`
	Output   UpdateOutput   `json:"output,omitempty"`
}

type UpdateObservation struct {
	Progress UpdateProgress      `json:"progress"`
	Output   []UpdateOutput      `json:"output"`
	Events   []UpdateStreamEvent `json:"events,omitempty"`
}

type UpdateProvider interface {
	Name() string
	Probe(context.Context) (string, error)
	Inventory(context.Context) ([]UpdatePackage, error)
	Refresh(context.Context, bool, func(UpdateStreamEvent)) error
	Plan(context.Context) ([]UpdateChange, error)
	Apply(context.Context, func(UpdateStreamEvent)) error
	History(context.Context, int) ([]UpdateHistoryEntry, error)
	Recovery(context.Context) UpdateRecovery
	LockStatus(context.Context) (bool, string)
}

type updateWorkerEnvelope struct {
	Event  *UpdateStreamEvent `json:"event,omitempty"`
	Result *UpdateResult      `json:"result,omitempty"`
	Error  string             `json:"error,omitempty"`
}

type updateWorkerJob struct {
	ExpectedFingerprint string `json:"expectedFingerprint"`
	Confirmed           bool   `json:"confirmed"`
	RiskAccepted        bool   `json:"riskAccepted,omitempty"`
	JobID               string `json:"jobId"`
}

type UpdateService struct {
	provider         UpdateProvider
	operationMu      sync.Mutex
	mu               sync.Mutex
	sequence         uint64
	progress         UpdateProgress
	events           []UpdateStreamEvent
	eventBytes       int
	subscribers      map[chan UpdateStreamEvent]struct{}
	activeJob        string
	workerExecutable string
	eventHook        func(UpdateStreamEvent)
}

func NewUpdateService(provider UpdateProvider) *UpdateService {
	return &UpdateService{provider: provider, progress: UpdateProgress{Phase: "idle", Percent: -1, Message: "No update is running."}, subscribers: make(map[chan UpdateStreamEvent]struct{})}
}

func (service *UpdateService) UseWorker(executable string)               { service.workerExecutable = executable }
func (service *UpdateService) SetEventHook(hook func(UpdateStreamEvent)) { service.eventHook = hook }
func (service *UpdateService) ProviderName() string {
	if service == nil || service.provider == nil {
		return "none"
	}
	return service.provider.Name()
}

func ValidateUpdateOperation(operation UpdateOperation, applying bool) error {
	if len(operation.ExpectedFingerprint) != sha256.Size*2 {
		return ErrInvalidUpdateOperation
	}
	if _, err := hex.DecodeString(operation.ExpectedFingerprint); err != nil {
		return ErrInvalidUpdateOperation
	}
	if applying && !operation.Confirmed {
		return ErrInvalidUpdateOperation
	}
	if len(operation.JobID) > 128 || strings.ContainsAny(operation.JobID, "\x00\r\n/") {
		return ErrInvalidUpdateOperation
	}
	return nil
}

func (service *UpdateService) Status(ctx context.Context) UpdateStatus {
	if service == nil || service.provider == nil {
		return unavailableUpdateStatus("No distro update provider was configured")
	}
	deadline, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	version, err := service.provider.Probe(deadline)
	if err != nil {
		return unavailableUpdateStatus(err.Error())
	}
	packages, err := service.provider.Inventory(deadline)
	if err != nil {
		status := unavailableUpdateStatus(err.Error())
		status.Backend, status.Version = service.provider.Name(), version
		return status
	}
	packages = SortUpdatePackages(packages)
	locked, reason := service.provider.LockStatus(deadline)
	status := UpdateStatus{Available: true, Backend: service.provider.Name(), Version: version, Contract: "native-distro-provider", Packages: packages, ExternalLock: locked, LockReason: reason, LastChecked: time.Now().UTC().Format(time.RFC3339), Recovery: service.provider.Recovery(deadline)}
	status.Message = updateMessage(len(packages))
	status.Fingerprint = UpdateFingerprint(status)
	normalizeUpdateStatus(&status)
	return status
}

func unavailableUpdateStatus(reason string) UpdateStatus {
	status := UpdateStatus{Backend: "none", Contract: "unavailable", Packages: []UpdatePackage{}, Message: "No supported update backend is available", Reason: reason}
	normalizeUpdateStatus(&status)
	status.Fingerprint = UpdateFingerprint(status)
	return status
}

func normalizeUpdateStatus(status *UpdateStatus) {
	if status.Packages == nil {
		status.Packages = []UpdatePackage{}
	}
	if status.Recovery.RestartServices == nil {
		status.Recovery.RestartServices = []string{}
	}
	if status.Recovery.Hints == nil {
		status.Recovery.Hints = []string{}
	}
}

func (service *UpdateService) Refresh(ctx context.Context, force bool) error {
	if service == nil || service.provider == nil {
		return ErrUpdateUnavailable
	}
	service.operationMu.Lock()
	defer service.operationMu.Unlock()
	service.beginJob("refresh-" + strconv.FormatInt(time.Now().UnixNano(), 36))
	service.emitProgress(UpdateProgress{Active: true, Phase: "refreshing", Percent: -1, Message: "Refreshing package metadata.", Cancelable: true})
	err := service.provider.Refresh(ctx, force, service.publish)
	if err != nil {
		service.emitProgress(UpdateProgress{Phase: "failed", Percent: -1, Message: boundedUpdateError(err, "")})
		return err
	}
	service.emitProgress(UpdateProgress{Phase: "completed", Percent: 100, Message: "Package metadata refreshed."})
	return nil
}

func (service *UpdateService) Preview(ctx context.Context, operation UpdateOperation) (UpdatePreview, error) {
	if err := ValidateUpdateOperation(operation, false); err != nil {
		return UpdatePreview{}, err
	}
	service.operationMu.Lock()
	defer service.operationMu.Unlock()
	status := service.Status(ctx)
	preview := UpdatePreview{Current: status, Changes: []UpdateChange{}, Warnings: []string{}}
	if operation.ExpectedFingerprint != "" && operation.ExpectedFingerprint != status.Fingerprint {
		preview.Stale, preview.Reason = true, "Available update inventory changed; refresh before applying."
		return preview, nil
	}
	if !status.Available {
		preview.Reason = status.Reason
		return preview, nil
	}
	if status.ExternalLock {
		preview.Reason = status.LockReason
		return preview, nil
	}
	changes, err := service.provider.Plan(ctx)
	if err != nil {
		return preview, err
	}
	preview.Changes = SortUpdateChanges(changes)
	preview.Fingerprint = UpdatePlanFingerprint(service.provider.Name(), preview.Changes)
	if len(preview.Changes) == 0 {
		preview.Reason = "No updates are available."
		return preview, nil
	}
	preview.Allowed, preview.RequiresConfirmation = true, true
	for _, change := range preview.Changes {
		if change.Risky() {
			preview.RequiresRiskConfirmation = true
			break
		}
	}
	preview.Warnings = append(preview.Warnings, "Updates can restart services or require a host reboot.")
	if preview.RequiresRiskConfirmation {
		preview.Warnings = append(preview.Warnings, "Plan contains removals, replacements, downgrades, or repository/vendor changes.")
	}
	return preview, nil
}

// PreviewUpdateStatus provides a deterministic preview for cached readers.
// Authoritative mutation still replans through UpdateService immediately
// before commit and requires the exact plan fingerprint.
func PreviewUpdateStatus(status UpdateStatus, operation UpdateOperation) (UpdatePreview, error) {
	if err := ValidateUpdateOperation(operation, false); err != nil {
		return UpdatePreview{}, err
	}
	changes := make([]UpdateChange, 0, len(status.Packages))
	for _, item := range status.Packages {
		changes = append(changes, UpdateChange{Action: "upgrade", Name: item.Name, Architecture: item.Architecture, CurrentVersion: item.CurrentVersion, CandidateVersion: item.CandidateVersion})
	}
	changes = SortUpdateChanges(changes)
	preview := UpdatePreview{Current: status, Changes: changes, Warnings: []string{}, Fingerprint: UpdatePlanFingerprint(status.Backend, changes), Allowed: status.Available && !status.ExternalLock && len(changes) > 0, RequiresConfirmation: len(changes) > 0}
	if operation.ExpectedFingerprint != "" && operation.ExpectedFingerprint != status.Fingerprint && operation.ExpectedFingerprint != preview.Fingerprint {
		preview.Stale = true
		preview.Allowed = false
		preview.Reason = "Available update inventory changed; refresh before applying."
	}
	if status.ExternalLock {
		preview.Reason = status.LockReason
	}
	return preview, nil
}

func (service *UpdateService) Apply(ctx context.Context, operation UpdateOperation) (UpdateResult, error) {
	if err := ValidateUpdateOperation(operation, true); err != nil {
		return UpdateResult{}, err
	}
	if service == nil || service.provider == nil {
		return UpdateResult{}, ErrUpdateUnavailable
	}
	service.operationMu.Lock()
	defer service.operationMu.Unlock()
	if service.workerExecutable != "" {
		return service.applyWithWorker(ctx, operation)
	}
	return service.applyDirect(ctx, operation)
}

func (service *UpdateService) applyDirect(ctx context.Context, operation UpdateOperation) (UpdateResult, error) {
	locked, _ := service.provider.LockStatus(ctx)
	if locked {
		return UpdateResult{}, ErrUpdateLocked
	}
	changes, err := service.provider.Plan(ctx)
	if err != nil {
		return UpdateResult{}, err
	}
	changes = SortUpdateChanges(changes)
	if UpdatePlanFingerprint(service.provider.Name(), changes) != operation.ExpectedFingerprint {
		return UpdateResult{}, ErrUpdateConflict
	}
	for _, change := range changes {
		if change.Risky() && !operation.RiskAccepted {
			return UpdateResult{}, ErrUpdateRiskNotAccepted
		}
	}
	jobID := operation.JobID
	if jobID == "" {
		jobID = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	service.beginJob(jobID)
	applyCtx := context.WithoutCancel(ctx)
	service.emitProgress(UpdateProgress{JobID: jobID, Active: true, Phase: "applying", Total: len(changes), Percent: -1, Message: "Applying system updates.", Cancelable: false})
	if err := service.provider.Apply(applyCtx, service.publish); err != nil {
		service.emitProgress(UpdateProgress{JobID: jobID, Phase: "failed", Total: len(changes), Percent: -1, Message: boundedUpdateError(err, "")})
		return UpdateResult{}, fmt.Errorf("%w: %s", ErrUpdateApply, boundedUpdateError(err, ""))
	}
	service.emitProgress(UpdateProgress{JobID: jobID, Active: true, Phase: "verifying", Total: len(changes), Percent: -1, Message: "Verifying update result."})
	final := service.Status(applyCtx)
	if !final.Available {
		return UpdateResult{}, ErrUpdateVerification
	}
	service.emitProgress(UpdateProgress{JobID: jobID, Phase: "completed", Current: len(changes), Total: len(changes), Percent: 100, Message: "System updates completed."})
	return UpdateResult{Backend: service.provider.Name(), Changes: changes, Verified: true, Message: "Updates applied and verified.", Fingerprint: final.Fingerprint, Recovery: final.Recovery}, nil
}

func (service *UpdateService) applyWithWorker(ctx context.Context, operation UpdateOperation) (UpdateResult, error) {
	jobFile, err := writeUpdateWorkerJob(operation)
	if err != nil {
		return UpdateResult{}, err
	}
	defer os.Remove(jobFile)
	jobID := operation.JobID
	if jobID == "" {
		jobID = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	service.beginJob(jobID)
	service.emitProgress(UpdateProgress{JobID: jobID, Active: true, Phase: "starting", Percent: -1, Message: "Starting privileged update worker.", Cancelable: false})
	unit := "tako-update-" + safeUnitFragment(operation.JobID)
	if strings.HasSuffix(unit, "-") {
		unit += strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	arguments := []string{"--quiet", "--pipe", "--wait", "--collect", "--service-type=exec", "--unit=" + unit, "--property=User=root", "--property=PrivateTmp=true", "--property=ProtectHome=true", "--property=ProtectKernelTunables=true", "--property=ProtectKernelModules=true", service.workerExecutable, "update-worker", "--job-file", jobFile}
	command := exec.CommandContext(context.WithoutCancel(ctx), "systemd-run", arguments...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return UpdateResult{}, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return UpdateResult{}, err
	}
	if err := command.Start(); err != nil {
		return UpdateResult{}, err
	}
	go streamUpdateOutput(stderr, "stderr", operation.JobID, service.publish)
	var result UpdateResult
	var workerError string
	scanner := bufio.NewScanner(io.LimitReader(stdout, maxUpdateOutput))
	scanner.Buffer(make([]byte, 64<<10), 256<<10)
	for scanner.Scan() {
		var envelope updateWorkerEnvelope
		if json.Unmarshal(scanner.Bytes(), &envelope) != nil {
			continue
		}
		if envelope.Event != nil {
			service.publish(*envelope.Event)
		}
		if envelope.Result != nil {
			result = *envelope.Result
		}
		if envelope.Error != "" {
			workerError = envelope.Error
		}
	}
	waitErr := command.Wait()
	if workerError != "" {
		return UpdateResult{}, fmt.Errorf("%w: %s", ErrUpdateApply, workerError)
	}
	if waitErr != nil {
		return UpdateResult{}, fmt.Errorf("%w: %s", ErrUpdateApply, boundedUpdateError(waitErr, ""))
	}
	if !result.Verified {
		return UpdateResult{}, ErrUpdateVerification
	}
	return result, nil
}

func writeUpdateWorkerJob(operation UpdateOperation) (string, error) {
	if os.Geteuid() != 0 {
		return "", errors.New("update worker jobs require root")
	}
	file, err := os.CreateTemp("/run/tako", "update-job-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	encoder := json.NewEncoder(file)
	document := updateWorkerJob{ExpectedFingerprint: operation.ExpectedFingerprint, Confirmed: operation.Confirmed, RiskAccepted: operation.RiskAccepted, JobID: operation.JobID}
	if err := encoder.Encode(document); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func ReadUpdateWorkerJob(path string) (UpdateOperation, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return UpdateOperation{}, ErrInvalidUpdateOperation
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return UpdateOperation{}, ErrInvalidUpdateOperation
	}
	file, err := os.Open(path)
	if err != nil {
		return UpdateOperation{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16<<10))
	decoder.DisallowUnknownFields()
	var document updateWorkerJob
	if err := decoder.Decode(&document); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return UpdateOperation{}, ErrInvalidUpdateOperation
	}
	operation := UpdateOperation{ExpectedFingerprint: document.ExpectedFingerprint, Confirmed: document.Confirmed, RiskAccepted: document.RiskAccepted, JobID: document.JobID}
	if ValidateUpdateOperation(operation, true) != nil {
		return UpdateOperation{}, ErrInvalidUpdateOperation
	}
	return operation, nil
}

func safeUnitFragment(value string) string {
	var result strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			result.WriteRune(character)
		}
	}
	if result.Len() > 48 {
		return result.String()[:48]
	}
	return result.String()
}

func (service *UpdateService) RunWorker(ctx context.Context, operation UpdateOperation, output io.Writer) error {
	encoder := json.NewEncoder(output)
	service.SetEventHook(func(event UpdateStreamEvent) { _ = encoder.Encode(updateWorkerEnvelope{Event: &event}) })
	result, err := service.Apply(ctx, operation)
	if err != nil {
		_ = encoder.Encode(updateWorkerEnvelope{Error: err.Error()})
		return err
	}
	return encoder.Encode(updateWorkerEnvelope{Result: &result})
}

func (service *UpdateService) History(ctx context.Context) ([]UpdateHistoryEntry, error) {
	if service == nil || service.provider == nil {
		return nil, ErrUpdateUnavailable
	}
	return service.provider.History(ctx, MaxUpdateHistory)
}

func (service *UpdateService) Snapshot() UpdateObservation {
	service.mu.Lock()
	defer service.mu.Unlock()
	output := make([]UpdateOutput, 0)
	for _, event := range service.events {
		if event.Kind == "output" {
			output = append(output, event.Output)
		}
	}
	events := append([]UpdateStreamEvent(nil), service.events...)
	return UpdateObservation{Progress: service.progress, Output: output, Events: events}
}

func (service *UpdateService) Subscribe(after uint64) (<-chan UpdateStreamEvent, func()) {
	channel := make(chan UpdateStreamEvent, maxLiveEvents)
	service.mu.Lock()
	for _, event := range service.events {
		sequence := event.Progress.Sequence
		if event.Kind == "output" {
			sequence = event.Output.Sequence
		}
		if sequence > after {
			channel <- event
		}
	}
	service.subscribers[channel] = struct{}{}
	service.mu.Unlock()
	return channel, func() {
		service.mu.Lock()
		if _, ok := service.subscribers[channel]; ok {
			delete(service.subscribers, channel)
			close(channel)
		}
		service.mu.Unlock()
	}
}

func (service *UpdateService) beginJob(jobID string) {
	service.mu.Lock()
	service.events = nil
	service.eventBytes = 0
	service.activeJob = jobID
	service.mu.Unlock()
}
func (service *UpdateService) emitProgress(progress UpdateProgress) {
	service.publish(UpdateStreamEvent{Kind: "progress", Progress: progress})
}

func (service *UpdateService) publish(event UpdateStreamEvent) {
	if service == nil {
		return
	}
	service.mu.Lock()
	service.sequence++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if event.Kind == "output" {
		event.Output.Sequence, event.Output.Timestamp = service.sequence, now
		event.Output.Line = sanitizeUpdateLine(event.Output.Line)
		if event.Output.JobID == "" {
			event.Output.JobID = service.activeJob
		}
		service.eventBytes += len(event.Output.Line)
	} else {
		event.Kind = "progress"
		event.Progress.Sequence, event.Progress.Timestamp = service.sequence, now
		if event.Progress.JobID == "" {
			event.Progress.JobID = service.activeJob
		}
		service.progress = event.Progress
		if !event.Progress.Active && (event.Progress.Phase == "completed" || event.Progress.Phase == "failed" || event.Progress.Phase == "canceled") {
			service.activeJob = ""
		}
	}
	service.events = append(service.events, event)
	for len(service.events) > maxLiveEvents || service.eventBytes > maxLiveOutput {
		removed := service.events[0]
		service.events = service.events[1:]
		if removed.Kind == "output" {
			service.eventBytes -= len(removed.Output.Line)
		}
	}
	for subscriber := range service.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
	hook := service.eventHook
	service.mu.Unlock()
	if hook != nil {
		hook(event)
	}
}

func sanitizeUpdateLine(line string) string {
	if !utf8.ValidString(line) {
		line = strings.ToValidUTF8(line, "�")
	}
	line = ansiUpdatePattern.ReplaceAllString(line, "")
	line = strings.Map(func(r rune) rune {
		if r == '\t' || r >= 0x20 {
			return r
		}
		return -1
	}, line)
	if len(line) > maxLiveLine {
		line = line[:maxLiveLine]
		for !utf8.ValidString(line) {
			line = line[:len(line)-1]
		}
	}
	return line
}

type UpdateCommandResult struct {
	Output   string
	ExitCode int
	Err      error
}

func RunUpdateCommand(ctx context.Context, name string, arguments []string, environment []string, emit func(UpdateStreamEvent)) UpdateCommandResult {
	if err := ctx.Err(); err != nil {
		return UpdateCommandResult{ExitCode: -1, Err: err}
	}
	command := exec.CommandContext(ctx, name, arguments...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	command.Env = append(command.Env, environment...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return UpdateCommandResult{ExitCode: -1, Err: err}
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return UpdateCommandResult{ExitCode: -1, Err: err}
	}
	if err := command.Start(); err != nil {
		return UpdateCommandResult{ExitCode: -1, Err: err}
	}
	var output strings.Builder
	var mu sync.Mutex
	copyStream := func(reader io.Reader, stream string) {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 32<<10), maxUpdateOutput)
		for scanner.Scan() {
			line := sanitizeUpdateLine(scanner.Text())
			mu.Lock()
			if output.Len() < maxUpdateOutput {
				remaining := maxUpdateOutput - output.Len()
				if len(line)+1 > remaining {
					line = line[:max(0, remaining-1)]
					for !utf8.ValidString(line) {
						line = line[:len(line)-1]
					}
				}
				output.WriteString(line)
				output.WriteByte('\n')
			}
			mu.Unlock()
			if emit != nil {
				emit(UpdateStreamEvent{Kind: "output", Output: UpdateOutput{Stream: stream, Line: line}})
			}
		}
	}
	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); copyStream(stdout, "stdout") }()
	go func() { defer wait.Done(); copyStream(stderr, "stderr") }()
	err = command.Wait()
	wait.Wait()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return UpdateCommandResult{Output: output.String(), ExitCode: exitCode, Err: err}
}

func streamUpdateOutput(reader io.Reader, stream, jobID string, emit func(UpdateStreamEvent)) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 32<<10), maxLiveLine*2)
	for scanner.Scan() {
		emit(UpdateStreamEvent{Kind: "output", Output: UpdateOutput{JobID: jobID, Stream: stream, Line: scanner.Text()}})
	}
}
func CommandExists(name string) bool { _, err := exec.LookPath(name); return err == nil }
func commandExists(name string) bool { return CommandExists(name) }

// Kept private because kpatch shares the same bounded command runner without
// becoming coupled to a distro update adapter.
type updateCommandResult = UpdateCommandResult

func runUpdateCommand(ctx context.Context, name string, arguments ...string) updateCommandResult {
	return RunUpdateCommand(ctx, name, arguments, nil, nil)
}

var versionAtLeastPattern = regexp.MustCompile(`(?:^|\s)([0-9]+)(?:\.[0-9]+)?`)

func versionAtLeast(version string, minimumMajor int) bool {
	match := versionAtLeastPattern.FindStringSubmatch(version)
	if len(match) < 2 {
		return false
	}
	major, err := strconv.Atoi(match[1])
	return err == nil && major >= minimumMajor
}

func UpdateLockHeld(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false
}

func UpdateFingerprint(status UpdateStatus) string {
	packages := SortUpdatePackages(append([]UpdatePackage(nil), status.Packages...))
	payload, _ := json.Marshal(struct {
		Backend      string
		Packages     []UpdatePackage
		ExternalLock bool
	}{status.Backend, packages, status.ExternalLock})
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}
func UpdatePlanFingerprint(backend string, changes []UpdateChange) string {
	payload, _ := json.Marshal(struct {
		Backend string
		Changes []UpdateChange
	}{backend, SortUpdateChanges(append([]UpdateChange(nil), changes...))})
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}
func SortUpdatePackages(packages []UpdatePackage) []UpdatePackage {
	if packages == nil {
		return []UpdatePackage{}
	}
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Name != packages[j].Name {
			return packages[i].Name < packages[j].Name
		}
		return packages[i].Architecture < packages[j].Architecture
	})
	return packages
}
func SortUpdateChanges(changes []UpdateChange) []UpdateChange {
	if changes == nil {
		return []UpdateChange{}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Name != changes[j].Name {
			return changes[i].Name < changes[j].Name
		}
		if changes[i].Action != changes[j].Action {
			return changes[i].Action < changes[j].Action
		}
		return changes[i].Architecture < changes[j].Architecture
	})
	return changes
}
func updateMessage(count int) string {
	if count == 0 {
		return "No installed-software updates are currently available."
	}
	suffix := "s"
	if count == 1 {
		suffix = ""
	}
	return fmt.Sprintf("%d installed-software update%s available.", count, suffix)
}
func boundedUpdateError(err error, output string) string {
	message := strings.TrimSpace(output)
	if message == "" && err != nil {
		message = err.Error()
	}
	if len(message) > 1024 {
		message = message[:1024]
	}
	if message == "" {
		return "package manager rejected the update request"
	}
	return message
}
