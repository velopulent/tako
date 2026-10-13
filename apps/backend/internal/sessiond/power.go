package sessiond

import (
	"context"

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
