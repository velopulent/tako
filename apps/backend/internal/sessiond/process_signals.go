package sessiond

import (
	"context"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

func executeProcessSignal(ctx context.Context, identity auth.Identity, administrative bool, operation platform.SignalOperation) (platform.SignalResult, error) {
	if administrative {
		return platform.ApplySignal(ctx, operation, nil)
	}
	return platform.ApplySignal(ctx, operation, &identity.UID)
}

func signalErrorCode(err error) string {
	return platform.SignalFailureCode(err)
}
