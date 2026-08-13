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
	"os/exec"
	"path/filepath"
	"time"

	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type frame struct {
	ID      string          `json:"id"`
	Method  string          `json:"method,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// Run serves framed bridge RPC until input closes or an I/O error occurs.
func Run(input io.Reader, output io.Writer, errorOutput io.Writer) error {
	encoder := zap.NewProductionEncoderConfig()
	encoder.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	logger := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(encoder), zapcore.AddSync(errorOutput), zapcore.InfoLevel), zap.AddCaller()).With(
		zap.String("service", "tako"),
		zap.String("mode", "bridge"),
	)
	defer func() { _ = logger.Sync() }()
	logger.Info("bridge started")
	reader := bufio.NewReader(input)
	writer := bufio.NewWriter(output)
	for {
		message, err := readFrame(reader)
		if err != nil {
			if err == io.EOF {
				logger.Info("bridge stopped")
				return nil
			}
			logger.Error("bridge frame read failed", zap.Error(err))
			return err
		}
		response := frame{ID: message.ID, Error: "unsupported-method"}
		if message.Method == "ping" {
			response.Error = ""
			response.Payload = json.RawMessage(`{"ok":true}`)
		}
		if message.Method == "timer.apply" {
			state, err := applyTimer(message.Payload)
			if err != nil {
				response.Error = timerErrorCode(err)
			} else {
				response.Error = ""
				response.Payload, _ = json.Marshal(state)
			}
		}
		if message.Method == "override.apply" {
			state, err := applyOverride(message.Payload)
			if err != nil {
				response.Error = overrideErrorCode(err)
			} else {
				response.Error = ""
				response.Payload, _ = json.Marshal(state)
			}
		}
		if message.Method == "file.apply" {
			result, err := applyFile(message.Payload)
			if err != nil {
				response.Error = fileErrorCode(err)
			} else {
				response.Error = ""
				response.Payload, _ = json.Marshal(result)
			}
		}
		if err := writeFrame(writer, response); err != nil {
			logger.Error("bridge frame write failed", zap.Error(err))
			return err
		}
	}
}

func applyFile(payload json.RawMessage) (platform.FileResult, error) {
	var operation platform.FileOperation
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&operation); err != nil {
		return platform.FileResult{}, platform.ErrInvalidFileOperation
	}
	if err := platform.ValidateFileOperation(operation); err != nil {
		return platform.FileResult{}, err
	}
	return platform.ApplyUserFileOperation(context.Background(), operation)
}

func fileErrorCode(err error) string {
	switch {
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

func applyTimer(payload json.RawMessage) (platform.TimerState, error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload").Run(); err != nil {
		if ctx.Err() != nil {
			return platform.TimerState{}, context.DeadlineExceeded
		}
		return platform.TimerState{}, err
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

func applyOverride(payload json.RawMessage) (platform.OverrideState, error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload").Run(); err != nil {
		if ctx.Err() != nil {
			return platform.OverrideState{}, context.DeadlineExceeded
		}
		return platform.OverrideState{}, err
	}
	if err := exec.CommandContext(ctx, "systemctl", "--user", "show", operation.Unit, "--property=LoadState", "--value").Run(); err != nil {
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
