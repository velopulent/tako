package sessiond

import (
	"context"
	"errors"
	"io"
	"os/exec"

	"github.com/velopulent/tako/internal/platform"
)

type overrideBackend interface {
	Apply(context.Context, platform.OverrideOperation, io.Closer) (platform.OverrideState, error)
}

type systemOverrideBackend struct{}

func (systemOverrideBackend) Apply(ctx context.Context, operation platform.OverrideOperation, bridge io.Closer) (platform.OverrideState, error) {
	if operation.Scope == "user" {
		userBridge, ok := bridge.(*userBridgeProcess)
		if !ok || userBridge == nil {
			return platform.OverrideState{}, errors.New("user bridge unavailable")
		}
		return userBridge.applyOverride(ctx, operation)
	}
	state, err := platform.ApplyServiceOverride("/etc/systemd/system", operation)
	if err != nil || operation.Action == "preview" {
		return state, err
	}
	if err := exec.CommandContext(ctx, "systemctl", "daemon-reload").Run(); err != nil {
		if ctx.Err() != nil {
			return platform.OverrideState{}, context.DeadlineExceeded
		}
		return platform.OverrideState{}, err
	}
	if err := exec.CommandContext(ctx, "systemctl", "show", operation.Unit, "--property=LoadState", "--value").Run(); err != nil {
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
	case errors.Is(err, context.DeadlineExceeded):
		return "service-override-timeout"
	default:
		return "service-override-failed"
	}
}
