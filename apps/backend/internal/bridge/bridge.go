// Package bridge runs inside an authenticated UNIX user session. The framed RPC
// transport is intentionally small so module adapters can move behind this
// process without changing HTTP contracts.
package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/velopulent/tako/internal/logging"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

type frame struct {
	ID      string          `json:"id"`
	Method  string          `json:"method,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

const (
	// cancelMethod cancels the in-flight request with the same ID. It is a
	// control frame and gets no response.
	cancelMethod = "$cancel"
	// maxConcurrentRequests bounds how much work one login runs at once.
	maxConcurrentRequests = 16
	// drainTimeout lets in-flight requests finish after input closes before
	// they are canceled.
	drainTimeout = time.Second
)

// handler answers one request frame.
type handler func(context.Context, frame) frame

// Run serves a user's framed bridge RPC until input closes or an I/O error
// occurs.
func Run(input io.Reader, output io.Writer, errorOutput io.Writer) error {
	logger, err := newLogger(errorOutput)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()
	return serve(logger, input, output, dispatch)
}

func newLogger(output io.Writer) (*zap.Logger, error) {
	return logging.NewWithOutput("bridge", output)
}

// serve runs requests concurrently, so a slow call never blocks the others,
// and a caller that gives up cancels only its own request.
func serve(logger *zap.Logger, input io.Reader, output io.Writer, handle handler) error {
	logger.Info("bridge started")
	reader := bufio.NewReader(input)
	responses := &frameWriter{writer: bufio.NewWriter(output)}
	requests := newRequestSet()
	for {
		message, err := readFrame(reader)
		if err != nil {
			requests.drain(drainTimeout)
			if err == io.EOF {
				logger.Info("bridge stopped")
				return nil
			}
			logger.Error("bridge frame read failed", zap.Error(err))
			return err
		}
		if message.Method == cancelMethod {
			requests.cancel(message.ID)
			continue
		}
		ctx, finish, ok := requests.start(message.ID)
		if !ok {
			if err := responses.write(frame{ID: message.ID, Error: "invalid-request"}); err != nil {
				logger.Error("bridge frame write failed", zap.Error(err))
			}
			continue
		}
		go func() {
			defer finish()
			if !requests.acquire(ctx) {
				return
			}
			defer requests.release()
			if err := responses.write(handle(ctx, message)); err != nil {
				logger.Error("bridge frame write failed", zap.Error(err))
			}
		}()
	}
}

// frameWriter serializes response frames from concurrent requests.
type frameWriter struct {
	mu     sync.Mutex
	writer *bufio.Writer
}

func (writer *frameWriter) write(message frame) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writeFrame(writer.writer, message)
}

// requestSet tracks in-flight requests by ID so they can be canceled.
type requestSet struct {
	mu       sync.Mutex
	inFlight map[string]context.CancelFunc
	slots    chan struct{}
	group    sync.WaitGroup
}

func newRequestSet() *requestSet {
	return &requestSet{inFlight: make(map[string]context.CancelFunc), slots: make(chan struct{}, maxConcurrentRequests)}
}

func (set *requestSet) start(id string) (context.Context, func(), bool) {
	set.mu.Lock()
	defer set.mu.Unlock()
	if id == "" || set.inFlight[id] != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	set.inFlight[id] = cancel
	set.group.Add(1)
	return ctx, func() {
		set.mu.Lock()
		delete(set.inFlight, id)
		set.mu.Unlock()
		cancel()
		set.group.Done()
	}, true
}

func (set *requestSet) cancel(id string) {
	set.mu.Lock()
	cancel := set.inFlight[id]
	set.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (set *requestSet) acquire(ctx context.Context) bool {
	select {
	case set.slots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (set *requestSet) release() { <-set.slots }

// drain waits for in-flight requests, canceling whatever is still running
// after timeout.
func (set *requestSet) drain(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		set.group.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(timeout):
	}
	set.mu.Lock()
	for _, cancel := range set.inFlight {
		cancel()
	}
	set.mu.Unlock()
	<-done
}

func dispatch(ctx context.Context, message frame) frame {
	var result any
	var err error
	var code func(error) string
	switch message.Method {
	case "ping":
		return frame{ID: message.ID, Payload: json.RawMessage(`{"ok":true}`)}
	case "timer.apply":
		result, err = applyTimer(ctx, message.Payload)
		code = timerErrorCode
	case "override.apply":
		result, err = applyOverride(ctx, message.Payload)
		code = overrideErrorCode
	case "file.apply":
		result, err = applyFile(ctx, message.Payload)
		code = fileErrorCode
	default:
		var handled bool
		result, handled, err = handleHostRead(ctx, message.Method, message.Payload)
		if !handled {
			return frame{ID: message.ID, Error: "unsupported-method"}
		}
		code = hostReadErrorCode
	}
	if err != nil {
		return frame{ID: message.ID, Error: code(err)}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return frame{ID: message.ID, Error: "encode-failed"}
	}
	return frame{ID: message.ID, Payload: payload}
}

func applyFile(ctx context.Context, payload json.RawMessage) (platform.FileResult, error) {
	var operation platform.FileOperation
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&operation); err != nil {
		return platform.FileResult{}, platform.ErrInvalidFileOperation
	}
	if err := platform.ValidateFileOperation(operation); err != nil {
		return platform.FileResult{}, err
	}
	return platform.ApplyUserFileOperation(ctx, operation)
}

func fileErrorCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout"
	case errors.Is(err, platform.ErrInvalidFileOperation):
		return "invalid-file-operation"
	case errors.Is(err, platform.ErrFileNotFound):
		return "file-not-found"
	case errors.Is(err, platform.ErrFilePermission):
		return "file-permission-denied"
	case errors.Is(err, platform.ErrFileConflict):
		return "file-conflict"
	case errors.Is(err, platform.ErrFileTooLarge):
		return "file-too-large"
	case errors.Is(err, platform.ErrUnsafeArchive):
		return "unsafe-archive"
	case errors.Is(err, platform.ErrArchiveLimit):
		return "archive-limit"
	default:
		return "file-operation-failed"
	}
}

func applyTimer(ctx context.Context, payload json.RawMessage) (platform.TimerState, error) {
	var operation platform.TimerOperation
	if err := json.Unmarshal(payload, &operation); err != nil {
		return platform.TimerState{}, platform.ErrInvalidTimerOperation
	}
	if operation.Scope != "user" {
		return platform.TimerState{}, platform.ErrInvalidTimerOperation
	}
	if err := platform.ValidateTimerOperation(operation); err != nil {
		return platform.TimerState{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.TimerState{}, err
	}
	root := filepath.Join(home, ".config", "systemd", "user")
	if operation.Action != "preview" {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return platform.TimerState{}, err
		}
	}
	state, err := platform.ApplyTimerFiles(root, operation)
	if err != nil || operation.Action == "preview" {
		return state, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := runUserSystemctl(ctx, "daemon-reload"); err != nil {
		return platform.TimerState{}, err
	}
	if verb := platform.TimerActivation(operation.Action, state); verb != "" {
		if err := runUserSystemctl(ctx, verb, "--", operation.Name+".timer"); err != nil && verb != "stop" {
			return platform.TimerState{}, err
		}
	}
	return state, nil
}

func timerErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidTimerOperation):
		return "invalid-timer-operation"
	case errors.Is(err, platform.ErrTimerConflict):
		return "timer-conflict"
	case errors.Is(err, platform.ErrTimerNotFound):
		return "timer-not-found"
	case errors.Is(err, platform.ErrTimerPairIncomplete):
		return "timer-pair-incomplete"
	default:
		return "timer-operation-failed"
	}
}

func applyOverride(ctx context.Context, payload json.RawMessage) (platform.OverrideState, error) {
	var operation platform.OverrideOperation
	if err := json.Unmarshal(payload, &operation); err != nil || operation.Scope != "user" {
		return platform.OverrideState{}, platform.ErrInvalidOverrideOperation
	}
	if err := platform.ValidateOverrideOperation(operation); err != nil {
		return platform.OverrideState{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.OverrideState{}, err
	}
	root := filepath.Join(home, ".config", "systemd", "user")
	state, err := platform.ApplyServiceOverride(root, operation)
	if err != nil || operation.Action == "preview" {
		return state, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := runUserSystemctl(ctx, "daemon-reload"); err != nil {
		if ctx.Err() != nil {
			return platform.OverrideState{}, context.DeadlineExceeded
		}
		return platform.OverrideState{}, err
	}
	if err := runUserSystemctl(ctx, "show", operation.Unit, "--property=LoadState", "--value"); err != nil {
		if ctx.Err() != nil {
			return platform.OverrideState{}, context.DeadlineExceeded
		}
		return platform.OverrideState{}, err
	}
	return state, nil
}

func overrideErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidOverrideOperation):
		return "invalid-service-override"
	case errors.Is(err, platform.ErrOverrideConflict):
		return "service-override-conflict"
	case errors.Is(err, platform.ErrOverrideNotFound):
		return "service-override-not-found"
	case errors.Is(err, platform.ErrOverrideUnmanaged):
		return "service-override-unmanaged"
	default:
		return "service-override-failed"
	}
}

func readFrame(reader io.Reader) (frame, error) {
	var size uint32
	if err := binary.Read(reader, binary.BigEndian, &size); err != nil {
		return frame{}, err
	}
	if size == 0 || size > 8<<20 {
		return frame{}, fmt.Errorf("invalid frame size: %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return frame{}, err
	}
	var message frame
	return message, json.Unmarshal(payload, &message)
}

func writeFrame(writer *bufio.Writer, message frame) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if err := binary.Write(writer, binary.BigEndian, uint32(len(payload))); err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.Flush()
}
