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
