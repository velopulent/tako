package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/platform"
)

const maxUserCommandOutput = 16 << 10

type boundedCommandOutput struct {
	buffer    bytes.Buffer
	seen      int
	truncated bool
}

func (output *boundedCommandOutput) Write(payload []byte) (int, error) {
	output.seen += len(payload)
	if output.buffer.Len() < maxUserCommandOutput {
		remaining := maxUserCommandOutput - output.buffer.Len()
		if len(payload) < remaining {
			remaining = len(payload)
		}
		_, _ = output.buffer.Write(payload[:remaining])
	}
	if output.seen > maxUserCommandOutput {
		output.truncated = true
	}
	return len(payload), nil
}

func runUserSystemctl(ctx context.Context, arguments ...string) error {
	output := &boundedCommandOutput{}
	command := exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, arguments...)...)
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if output.truncated {
			return errors.New("command output limit exceeded")
		}
		if message := strings.TrimSpace(output.buffer.String()); message != "" {
			return errors.New(message)
		}
		return err
	}
	return nil
}

func handleHostRead(method string, payload json.RawMessage, services ...*platform.UpdateService) (any, bool, error) {
	if !isHostReadMethod(method) {
		return nil, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var updates *platform.UpdateService
	if len(services) > 0 {
		updates = services[0]
	}

	switch method {
	case "services.read", "services.list":
		var operation auth.ServiceReadOperation
		if err := decodeHostPayload(payload, &operation); err != nil || (operation.Scope != "system" && operation.Scope != "user") {
			return nil, true, platform.ErrInvalidServiceScope
		}
		if operation.Type != "" && !map[string]bool{"service": true, "target": true, "socket": true, "timer": true, "path": true}[operation.Type] {
			return nil, true, platform.ErrInvalidServiceUnit
		}
		items, err := platform.Units(ctx, operation.Scope, operation.Type)
		return items, true, err
	case "services.detail":
		var operation auth.ServiceReadOperation
		if err := decodeHostPayload(payload, &operation); err != nil {
			return nil, true, platform.ErrInvalidServiceUnit
		}
		item, err := platform.UnitDetails(ctx, operation.Scope, operation.Unit)
		return item, true, err
	case "services.configuration":
		var operation auth.ServiceReadOperation
		if err := decodeHostPayload(payload, &operation); err != nil {
			return nil, true, platform.ErrInvalidServiceUnit
		}
		item, err := platform.ReadUnitConfiguration(ctx, operation.Scope, operation.Unit)
		return item, true, err
	case "services.action":
		var operation platform.ServiceOperation
		if err := decodeHostPayload(payload, &operation); err != nil || operation.Scope != "user" {
			return nil, true, platform.ErrInvalidServiceAction
		}
		if _, err := platform.ParseServiceOperation(operation.Scope, operation.Unit, operation.Action); err != nil {
			return nil, true, err
		}
		commandCtx, commandCancel := context.WithTimeout(ctx, 20*time.Second)
		defer commandCancel()
		if err := runUserSystemctl(commandCtx, operation.Action, "--", operation.Unit); err != nil {
			return nil, true, err
		}
		return map[string]bool{"ok": true}, true, nil
	case "host.info":
		return host.ReadContext(ctx), true, nil
	case "host.configuration.read":
		item, err := platform.ReadHostConfiguration(ctx)
		return item, true, err
	case "host.power.read":
		item, err := platform.ReadPowerStatus(ctx)
		return item, true, err
	case "processes.signal-preview":
		var operation platform.SignalOperation
		if err := decodeHostPayload(payload, &operation); err != nil {
			return nil, true, platform.ErrInvalidSignalOperation
		}
		item, err := platform.PreviewSignal(ctx, operation)
		if err == nil {
			for _, target := range item.Targets {
				if target.UID != os.Getuid() {
					return nil, true, platform.ErrSignalUnauthorized
				}
			}
		}
		return item, true, err
	case "processes.signal":
		var operation platform.SignalOperation
		if err := decodeHostPayload(payload, &operation); err != nil || operation.Action != "apply" {
			return nil, true, platform.ErrInvalidSignalOperation
		}
		item, err := platform.ApplySignal(ctx, operation, func() *int {
			uid := os.Getuid()
			return &uid
		}())
		return item, true, err
	case "identities.read":
		item, err := platform.ListIdentityInventory(ctx)
		return item, true, err
	case "storage.read":
		items, err := platform.Filesystems()
		return items, true, err
	case "network.read":
		item, err := platform.NetworkSnapshotRead(ctx)
		return item, true, err
	case "updates.read", "updates.status":
		return updates.Status(ctx), true, nil
	case "updates.history":
		items, err := updates.History(ctx)
		return items, true, err
	case "updates.live":
		if updates == nil {
			return auth.UpdateObservation{Progress: platform.UpdateProgress{Phase: "idle", Percent: -1, Message: "No update is running."}, Output: []platform.UpdateOutput{}}, true, nil
		}
		return updates.Snapshot(), true, nil
	case "updates.kpatch.read":
		return map[string]any{"status": platform.InspectKpatchStatus(ctx), "settings": platform.InspectKpatchSettings(ctx)}, true, nil
	case "capabilities.read":
		return platform.Detect(ctx, updates), true, nil
	case "login-history.read":
		var operation auth.LoginHistoryReadOperation
		if err := decodeHostPayload(payload, &operation); err != nil {
			return nil, true, platform.ErrInvalidLoginHistoryQuery
		}
		item, err := platform.QueryLoginHistory(ctx, operation.Query)
		return item, true, err
	default:
		return nil, true, errors.New("unsupported host read")
	}
}

func isHostReadMethod(method string) bool {
	switch method {
	case "services.read", "services.list", "services.detail", "services.configuration", "services.action", "host.info", "host.configuration.read", "host.power.read", "processes.signal-preview", "processes.signal", "identities.read", "storage.read", "network.read", "updates.read", "updates.status", "updates.history", "updates.live", "updates.kpatch.read", "capabilities.read", "login-history.read":
		return true
	default:
		return false
	}
}

func decodeHostPayload(payload json.RawMessage, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("host payload contains trailing data")
	}
	return nil
}

func hostReadErrorCode(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout"
	case errors.Is(err, platform.ErrInvalidServiceScope), errors.Is(err, platform.ErrInvalidServiceUnit), errors.Is(err, platform.ErrInvalidServiceAction):
		return "invalid-service-operation"
	case errors.Is(err, platform.ErrInvalidSignalOperation):
		return "invalid-signal-operation"
	case errors.Is(err, platform.ErrSignalUnauthorized):
		return "permission-denied"
	case errors.Is(err, platform.ErrProcessNotFound):
		return "process-not-found"
	case errors.Is(err, platform.ErrProcessReused):
		return "process-reused"
	case errors.Is(err, platform.ErrInvalidLoginHistoryQuery):
		return "invalid-login-history-query"
	case strings.Contains(message, "/run/user/"), strings.Contains(message, "session bus"), strings.Contains(message, "user manager"), strings.Contains(message, "failed to connect to bus"), strings.Contains(message, "no medium found"):
		return "user-manager-unavailable"
	case errors.Is(err, os.ErrPermission):
		return "permission-denied"
	case errors.Is(err, platform.ErrUpdateUnavailable):
		return "system-backend-unavailable"
	case strings.Contains(message, "permission denied"), strings.Contains(message, "access denied"):
		return "permission-denied"
	case strings.Contains(message, "limit"), strings.Contains(message, "bound"):
		return "bounded-output"
	case strings.Contains(message, "conflict"), strings.Contains(message, "changed"):
		return "conflict"
	default:
		return "system-backend-unavailable"
	}
}
