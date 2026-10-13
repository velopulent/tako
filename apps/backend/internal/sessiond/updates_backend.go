package sessiond

import (
	"context"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

type updateBackend interface {
	Apply(context.Context, platform.UpdateOperation, auth.Identity) (platform.UpdateResult, error)
}

type systemUpdateBackend struct{ service *platform.UpdateService }

func (backend systemUpdateBackend) Apply(ctx context.Context, operation platform.UpdateOperation, _ auth.Identity) (platform.UpdateResult, error) {
	if backend.service == nil {
		return platform.UpdateResult{}, platform.ErrUpdateUnavailable
	}
	return backend.service.Apply(ctx, operation)
}

// kpatchBackend isolates kernel live-patch configuration for testing.
type kpatchBackend interface {
	Apply(context.Context, platform.KpatchOperation, auth.Identity) (platform.KpatchSettingsStatus, error)
}

type systemKpatchBackend struct{}

func (systemKpatchBackend) Apply(ctx context.Context, operation platform.KpatchOperation, _ auth.Identity) (platform.KpatchSettingsStatus, error) {
	return platform.ApplyKpatchSettings(ctx, operation)
}
