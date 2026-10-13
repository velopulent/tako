package sessiond

import (
	"context"
	"os/exec"

	"github.com/velopulent/tako/internal/platform"
)

type timerBackend interface {
	Apply(context.Context, platform.TimerOperation, bridgeCaller) (platform.TimerState, error)
}

type systemTimerBackend struct{}

func (systemTimerBackend) Apply(ctx context.Context, operation platform.TimerOperation, bridge bridgeCaller) (platform.TimerState, error) {
	if operation.Scope == "user" {
		return callBridge[platform.TimerState](ctx, bridge, "timer.apply", operation)
	}
	state, err := platform.ApplyTimerFiles("/etc/systemd/system", operation)
	if err != nil || operation.Action == "preview" {
		return state, err
	}
	if err := exec.CommandContext(ctx, "systemctl", "daemon-reload").Run(); err != nil {
		return platform.TimerState{}, err
	}
	if verb := platform.TimerActivation(operation.Action, state); verb != "" {
		if err := exec.CommandContext(ctx, "systemctl", verb, "--", operation.Name+".timer").Run(); err != nil && verb != "stop" {
			return platform.TimerState{}, err
		}
	}
	return state, nil
}
