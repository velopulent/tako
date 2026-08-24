package sessiond

import (
	"context"
	"errors"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

type updateBackend interface {
	Apply(context.Context, platform.UpdateOperation, auth.Identity) (platform.UpdateResult, error)
}

type systemUpdateBackend struct{}

func (systemUpdateBackend) Apply(ctx context.Context, operation platform.UpdateOperation, _ auth.Identity) (platform.UpdateResult, error) {
	return platform.ApplyUpdates(ctx, operation)
}

// autoUpdatesBackend isolates automatic-update configuration so tests can
// record operations instead of touching the host.
type autoUpdatesBackend interface {
	Apply(context.Context, platform.AutoUpdatesOperation, auth.Identity) (platform.AutoUpdatesConfig, error)
}

type systemAutoUpdatesBackend struct{}

func (systemAutoUpdatesBackend) Apply(ctx context.Context, operation platform.AutoUpdatesOperation, _ auth.Identity) (platform.AutoUpdatesConfig, error) {
	return platform.ApplyAutoUpdates(ctx, operation)
}

// kpatchBackend isolates kernel live-patch configuration for testing.
type kpatchBackend interface {
	Apply(context.Context, platform.KpatchOperation, auth.Identity) (platform.KpatchSettingsStatus, error)
}

type systemKpatchBackend struct{}

func (systemKpatchBackend) Apply(ctx context.Context, operation platform.KpatchOperation, _ auth.Identity) (platform.KpatchSettingsStatus, error) {
	return platform.ApplyKpatchSettings(ctx, operation)
}

func updateErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidUpdateOperation):
		return "invalid-update-operation"
	case errors.Is(err, platform.ErrUpdateConflict):
		return "update-conflict"
	case errors.Is(err, platform.ErrUpdateLocked):
		return "update-locked"
	case errors.Is(err, platform.ErrUpdateUnavailable):
		return "update-unavailable"
	case errors.Is(err, platform.ErrUpdateVerification):
		return "update-verification-failed"
	case errors.Is(err, platform.ErrUpdateApply):
		return "update-apply-failed"
	default:
		return "update-unavailable"
	}
}

func autoUpdatesErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidAutoUpdatesOperation):
		return "invalid-auto-updates-operation"
	case errors.Is(err, platform.ErrAutoUpdatesUnavailable):
		return "auto-updates-unavailable"
	default:
		return "auto-updates-apply-failed"
	}
}
