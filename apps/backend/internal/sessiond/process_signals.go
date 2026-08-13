package sessiond

import (
	"context"
	"errors"
	"os"
	"syscall"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"golang.org/x/sys/unix"
)

func executeProcessSignal(ctx context.Context, identity auth.Identity, administrative bool, operation platform.SignalOperation) (platform.SignalResult, error) {
	preview, err := platform.PreviewSignal(ctx, operation)
	if err != nil {
		return platform.SignalResult{}, err
	}
	if !administrative {
		for _, target := range preview.Targets {
			if target.UID != identity.UID {
				return platform.SignalResult{}, platform.ErrSignalUnauthorized
			}
		}
	}
	result := platform.SignalResult{Signal: preview.Signal, Tree: preview.Tree, Targets: preview.Targets, Signaled: make([]platform.SignalTarget, 0, len(preview.Targets)), Failures: make([]platform.SignalFailure, 0)}
	for _, target := range preview.Targets {
		if err := sendVerifiedSignal(ctx, target, preview.Signal); err != nil {
			result.Failures = append(result.Failures, platform.SignalFailure{PID: target.PID, Error: signalErrorCode(err)})
			continue
		}
		result.Signaled = append(result.Signaled, target)
	}
	return result, nil
}

func sendVerifiedSignal(ctx context.Context, target platform.SignalTarget, name string) error {
	signal, ok := unixSignal(name)
	if !ok {
		return platform.ErrInvalidSignalOperation
	}
	fd, err := unix.PidfdOpen(target.PID, 0)
	if err == nil {
		defer unix.Close(fd)
		return unix.PidfdSendSignal(fd, signal, nil, 0)
	}
	if !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.EPERM) {
		return err
	}
	// Older kernels may not expose pidfds. Re-read the start identity directly
	// before the fallback kill so a recycled PID is rejected.
	details, inspectErr := platform.InspectProcess(ctx, target.PID, target.Started)
	if inspectErr != nil {
		return inspectErr
	}
	if details.Process.UID != target.UID {
		return platform.ErrProcessReused
	}
	return syscall.Kill(target.PID, syscall.Signal(signal))
}

func unixSignal(name string) (unix.Signal, bool) {
	switch name {
	case "HUP":
		return unix.SIGHUP, true
	case "INT":
		return unix.SIGINT, true
	case "TERM":
		return unix.SIGTERM, true
	case "KILL":
		return unix.SIGKILL, true
	case "STOP":
		return unix.SIGSTOP, true
	case "CONT":
		return unix.SIGCONT, true
	case "USR1":
		return unix.SIGUSR1, true
	case "USR2":
		return unix.SIGUSR2, true
	default:
		return 0, false
	}
}

func signalErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidSignalOperation):
		return "invalid-signal-operation"
	case errors.Is(err, platform.ErrProcessNotFound):
		return "process-not-found"
	case errors.Is(err, platform.ErrProcessReused), errors.Is(err, platform.ErrSignalConflict):
		return "process-signal-conflict"
	case errors.Is(err, platform.ErrSignalUnauthorized):
		return "process-signal-unauthorized"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "process-signal-timeout"
	case errors.Is(err, os.ErrPermission), errors.Is(err, unix.EPERM):
		return "process-signal-unauthorized"
	default:
		return "process-signal-failed"
	}
}
