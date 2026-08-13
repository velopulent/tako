package sessiond

import (
	"context"
	"errors"

	"github.com/velopulent/tako/internal/platform"
)

type powerBackend interface {
	Read(context.Context) (platform.PowerStatus, error)
	Request(context.Context, string) error
}

type systemPowerBackend struct{}

func (systemPowerBackend) Read(ctx context.Context) (platform.PowerStatus, error) {
	return platform.ReadPowerStatus(ctx)
}

func (systemPowerBackend) Request(ctx context.Context, action string) error {
	return platform.RequestPower(ctx, action)
}

func parsePowerOperation(action, confirmation, expectedFingerprint string) error {
	if action != "reboot" && action != "shutdown" {
		return errors.New("invalid-power-action")
	}
	want := "REBOOT"
	if action == "shutdown" {
		want = "SHUTDOWN"
	}
	if confirmation != want {
		return errors.New("invalid-power-confirmation")
	}
	if len(expectedFingerprint) != 64 {
		return errors.New("invalid-power-fingerprint")
	}
	return nil
}
