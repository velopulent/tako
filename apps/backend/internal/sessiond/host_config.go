package sessiond

import (
	"context"

	"github.com/velopulent/tako/internal/platform"
)

type hostConfigBackend interface {
	Read(context.Context) (platform.HostConfiguration, error)
	Apply(context.Context, platform.HostConfiguration, platform.HostConfiguration) error
}

type systemHostConfigBackend struct{}

func (systemHostConfigBackend) Read(ctx context.Context) (platform.HostConfiguration, error) {
	return platform.ReadHostConfiguration(ctx)
}

func (systemHostConfigBackend) Apply(ctx context.Context, current, desired platform.HostConfiguration) error {
	return platform.ApplyHostConfiguration(ctx, current, desired)
}
