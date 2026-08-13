package sessiond

import (
	"context"
	"errors"
	"io"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

type fileBackend interface {
	Apply(context.Context, platform.FileOperation, auth.Identity, bool, io.Closer) (platform.FileResult, error)
}

type systemFileBackend struct{}

func (systemFileBackend) Apply(ctx context.Context, operation platform.FileOperation, identity auth.Identity, administrative bool, bridge io.Closer) (platform.FileResult, error) {
	if administrative {
		return platform.ApplyPrivilegedFileOperation(ctx, operation)
	}
	userBridge, ok := bridge.(*userBridgeProcess)
	if !ok || userBridge == nil {
		return platform.FileResult{}, errors.New("user bridge unavailable")
	}
	return userBridge.applyFile(ctx, operation)
}

func fileErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidFileOperation):
		return "invalid-file-operation"
	case errors.Is(err, platform.ErrFileNotFound):
		return "file-not-found"
	case errors.Is(err, platform.ErrFilePermission):
		return "file-permission-denied"
	case errors.Is(err, platform.ErrFileConflict):
		return "file-conflict"
	case errors.Is(err, platform.ErrFileTooLarge):
		return "file-too-large"
	case errors.Is(err, platform.ErrUnsafeArchive):
		return "unsafe-archive"
	case errors.Is(err, platform.ErrArchiveLimit):
		return "archive-limit"
	case errors.Is(err, context.DeadlineExceeded):
		return "file-operation-timeout"
	default:
		return "file-operation-failed"
	}
}
