package sessiond

import (
	"context"
	"errors"
	"io"
	"os/exec"

	"github.com/velopulent/tako/internal/platform"
)

type timerBackend interface {
	Apply(context.Context, platform.TimerOperation, io.Closer) (platform.TimerState, error)
}

type systemTimerBackend struct{}

func (systemTimerBackend) Apply(ctx context.Context, operation platform.TimerOperation, bridge io.Closer) (platform.TimerState, error) {
	if operation.Scope == "user" {
		userBridge, ok := bridge.(*userBridgeProcess)
		if !ok || userBridge == nil {
			return platform.TimerState{}, errors.New("user bridge unavailable")
		}
		return userBridge.applyTimer(ctx, operation)
	}
	state, err := platform.ApplyTimerFiles("/etc/systemd/system", operation)
	if err != nil || operation.Action == "preview" {
		return state, err
	}
	if err := exec.CommandContext(ctx, "systemctl", "daemon-reload").Run(); err != nil {
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
	case errors.Is(err, context.DeadlineExceeded):
		return "timer-operation-timeout"
	case err != nil && err.Error() == "timer-conflict":
		return "timer-conflict"
	case err != nil && err.Error() == "invalid-timer-operation":
		return "invalid-timer-operation"
	case err != nil && err.Error() == "timer-pair-incomplete":
		return "timer-pair-incomplete"
	default:
		return "timer-operation-failed"
	}
}
