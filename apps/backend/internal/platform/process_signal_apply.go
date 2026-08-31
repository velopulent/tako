package platform

import (
	"context"
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// ApplySignal performs the preview-and-apply sequence in the process that owns
// the authority. ownerUID limits the operation to that identity; nil is used
// only by the root sessiond adapter after the request has been authorized.
func ApplySignal(ctx context.Context, operation SignalOperation, ownerUID *int) (SignalResult, error) {
	preview, err := PreviewSignal(ctx, operation)
	if err != nil {
		return SignalResult{}, err
	}
	if ownerUID != nil {
		for _, target := range preview.Targets {
			if target.UID != *ownerUID {
				return SignalResult{}, ErrSignalUnauthorized
			}
		}
	}
	result := SignalResult{
		Signal:   preview.Signal,
		Tree:     preview.Tree,
		Targets:  preview.Targets,
		Signaled: make([]SignalTarget, 0, len(preview.Targets)),
		Failures: make([]SignalFailure, 0),
	}
	for _, target := range preview.Targets {
		if err := sendVerifiedSignal(ctx, target, preview.Signal); err != nil {
			result.Failures = append(result.Failures, SignalFailure{PID: target.PID, Error: SignalFailureCode(err)})
			continue
		}
		result.Signaled = append(result.Signaled, target)
	}
	return result, nil
}

func sendVerifiedSignal(ctx context.Context, target SignalTarget, name string) error {
	signal, ok := unixSignal(name)
	if !ok {
		return ErrInvalidSignalOperation
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
	details, inspectErr := InspectProcess(ctx, target.PID, target.Started)
	if inspectErr != nil {
		return inspectErr
	}
	if details.Process.UID != target.UID {
		return ErrProcessReused
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

// SignalFailureCode is deliberately stable and suitable for a wire response.
func SignalFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidSignalOperation):
		return "invalid-signal-operation"
	case errors.Is(err, ErrProcessNotFound):
		return "process-not-found"
	case errors.Is(err, ErrProcessReused), errors.Is(err, ErrSignalConflict):
		return "process-signal-conflict"
	case errors.Is(err, ErrSignalUnauthorized):
		return "process-signal-unauthorized"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "process-signal-timeout"
	case errors.Is(err, os.ErrPermission), errors.Is(err, unix.EPERM):
		return "process-signal-unauthorized"
	default:
		return "process-signal-failed"
	}
}
