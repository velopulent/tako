package sessiond

import (
	"context"
	"os/exec"

	"github.com/velopulent/tako/internal/platform"
)

type overrideBackend interface {
	Apply(context.Context, platform.OverrideOperation, bridgeCaller) (platform.OverrideState, error)
}

type systemOverrideBackend struct{}

func (systemOverrideBackend) Apply(ctx context.Context, operation platform.OverrideOperation, bridge bridgeCaller) (platform.OverrideState, error) {
	if operation.Scope == "user" {
		return callBridge[platform.OverrideState](ctx, bridge, "override.apply", operation)
	}
	state, err := platform.ApplyServiceOverride("/etc/systemd/system", operation)
	if err != nil || operation.Action == "preview" {
		return state, err
	}
	if err := exec.CommandContext(ctx, "systemctl", "daemon-reload").Run(); err != nil {
		return platform.OverrideState{}, err
	}
	return state, nil
}
