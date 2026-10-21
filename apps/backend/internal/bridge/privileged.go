package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

// RunPrivileged serves administrative work that needs the real host: file
// changes anywhere on the system and SELinux or AppArmor remediation.
// Sessiond starts it as root in its own transient service for one
// administrative grant and closes input when the grant ends. It serves only
// these typed operations, never the user's own.
func RunPrivileged(input io.Reader, output io.Writer, errorOutput io.Writer) error {
	if os.Geteuid() != 0 {
		return errors.New("the administrative bridge must run as root")
	}
	logger, err := newLogger(errorOutput)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()
	return serve(logger, input, output, dispatchPrivileged)
}

func dispatchPrivileged(ctx context.Context, message frame) frame {
	var result any
	var err error
	fallback := "invalid-request"
	switch message.Method {
	case "ping":
		return frame{ID: message.ID, Payload: json.RawMessage(`{"ok":true}`)}
	case "file.apply":
		fallback = "file-operation-failed"
		var operation platform.FileOperation
		if err = decodeStrict(message.Payload, &operation, platform.ErrInvalidFileOperation); err == nil {
			result, err = platform.ApplyPrivilegedFileOperation(ctx, operation)
		}
	case "security.apply":
		fallback = "security-operation-failed"
		var operation platform.SecurityOperation
		if err = decodeStrict(message.Payload, &operation, platform.ErrInvalidSecurityOperation); err == nil {
			if operation.Action == "" || operation.Action == "inspect" {
				err = platform.ErrInvalidSecurityOperation
			} else if err = platform.ValidateSecurityOperation(operation); err == nil {
				result, err = platform.ApplySecurityOperation(ctx, operation)
			}
		}
	default:
		return frame{ID: message.ID, Error: "unsupported-method"}
	}
	if err != nil {
		return frame{ID: message.ID, Error: auth.ErrorCode(err, fallback)}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return frame{ID: message.ID, Error: "encode-failed"}
	}
	return frame{ID: message.ID, Payload: payload}
}

// decodeStrict decodes payload into destination, rejecting unknown fields
// with invalid.
func decodeStrict(payload json.RawMessage, destination any, invalid error) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return invalid
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return invalid
	}
	return nil
}
