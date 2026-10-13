package app

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/metrics"
	"github.com/velopulent/tako/internal/platform"
)

// localHostBroker backs `tako serve --dev`. Reads run in-process as the
// developer's own account, the way the per-login bridge runs them in
// production. Host mutations are disabled; the developer's own files and a
// terminal work so the UI can be exercised end to end.
type localHostBroker struct {
	sampler   *metrics.Sampler
	processes *platform.ProcessTracker
}

func newLocalHostBroker(ctx context.Context) *localHostBroker {
	sampler := metrics.NewSampler(int(time.Hour/time.Second) + 1)
	sampler.Configure(5*time.Second, time.Hour)
	go sampler.Run(ctx, 0)
	return &localHostBroker{sampler: sampler, processes: platform.NewProcessTracker()}
}

var errDevelopmentReadOnly = auth.Code("development-unsupported")

func decodePayload[T any](payload any) (T, error) {
	var value T
	encoded, err := json.Marshal(payload)
	if err != nil {
		return value, err
	}
	if string(encoded) == "null" {
		return value, nil
	}
	return value, json.Unmarshal(encoded, &value)
}

func (broker *localHostBroker) Call(ctx context.Context, operation string, _ auth.Credentials, payload, result any) error {
	value, err := broker.dispatch(ctx, operation, payload)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, result)
}

func (broker *localHostBroker) dispatch(ctx context.Context, operation string, payload any) (any, error) {
	switch operation {
	case "host.info":
		return host.ReadContext(ctx), nil
	case "host.configuration":
		return platform.ReadHostConfiguration(ctx)
	case "power.status":
		return platform.ReadPowerStatus(ctx)
	case "capabilities.read":
		return platform.Detect(ctx), nil
	case "metrics.history":
		request, err := decodePayload[auth.MetricsHistoryOperation](payload)
		if err != nil {
			return nil, err
		}
		return broker.sampler.HistorySince(request.Since, request.Maximum), nil
	case "services.list":
		request, err := decodePayload[auth.ServiceReadOperation](payload)
		if err != nil {
			return nil, err
		}
		return platform.Units(ctx, request.Scope, request.Type)
	case "services.detail":
		request, err := decodePayload[auth.ServiceReadOperation](payload)
		if err != nil {
			return nil, err
		}
		return platform.UnitDetails(ctx, request.Scope, request.Unit)
	case "services.configuration":
		request, err := decodePayload[auth.ServiceReadOperation](payload)
		if err != nil {
			return nil, err
		}
		return platform.ReadUnitConfiguration(ctx, request.Scope, request.Unit)
	case "processes.list":
		return broker.processes.Snapshot()
	case "processes.detail":
		request, err := decodePayload[auth.ProcessReadOperation](payload)
		if err != nil {
			return nil, err
		}
		return broker.processes.Inspect(ctx, request.PID, request.Started)
	case "processes.signal-preview":
		request, err := decodePayload[platform.SignalOperation](payload)
		if err != nil {
			return nil, err
		}
		return platform.PreviewSignal(ctx, request)
	case "identities.list":
		return platform.ListIdentityInventory(ctx)
	case "storage.read":
		if snapshot, err := platform.ReadStorageSnapshot(ctx); err == nil {
			return snapshot, nil
		}
		filesystems, err := platform.Filesystems()
		if err != nil {
			return nil, err
		}
		return platform.StorageSnapshotFromFilesystems(filesystems, "Hardware inventory is not read in development mode."), nil
	case "network.read":
		return platform.NetworkSnapshotRead(ctx)
	case "updates.status":
		return platform.UpdateStatus{Backend: "development", Contract: "development", Message: "Software updates are not available in development mode.", Packages: []platform.UpdatePackage{}}, nil
	case "updates.history":
		return []platform.UpdateHistoryEntry{}, nil
	case "updates.live":
		return platform.UpdateObservation{Progress: platform.UpdateProgress{Phase: "idle", Percent: -1, Message: "No update is running."}, Output: []platform.UpdateOutput{}}, nil
	case "kpatch.read":
		return auth.KpatchReadResult{Status: platform.InspectKpatchStatus(ctx), Settings: platform.InspectKpatchSettings(ctx)}, nil
	case "login-history.read":
		request, err := decodePayload[platform.LoginHistoryQuery](payload)
		if err != nil {
			return nil, err
		}
		return platform.QueryLoginHistory(ctx, request)
	case "journal.query":
		request, err := decodePayload[platform.JournalQuery](payload)
		if err != nil {
			return nil, err
		}
		return platform.QueryLogs(ctx, request)
	case "files.apply":
		request, err := decodePayload[platform.FileOperation](payload)
		if err != nil {
			return nil, err
		}
		if err := platform.ValidateFileOperation(request); err != nil {
			return nil, err
		}
		return platform.ApplyUserFileOperation(ctx, request)
	case "ssh-keys.apply":
		request, err := decodePayload[platform.SSHKeyOperation](payload)
		if err != nil {
			return nil, err
		}
		if !request.Preview {
			return nil, errDevelopmentReadOnly
		}
		return platform.PreviewSSHKeys(ctx, request, request.Username, false)
	case "terminal.resize":
		return nil, nil
	}
	return nil, errDevelopmentReadOnly
}

func (broker *localHostBroker) Stream(ctx context.Context, operation string, _ auth.Credentials, payload any, emit func(json.RawMessage) error) error {
	send := func(value any) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return emit(encoded)
	}
	switch operation {
	case "metrics.follow":
		request, err := decodePayload[auth.MetricsFollowOperation](payload)
		if err != nil {
			return err
		}
		stream, unsubscribe := broker.sampler.SubscribeEvery(max(request.Interval, time.Second))
		defer unsubscribe()
		for {
			select {
			case sample, ok := <-stream:
				if !ok || send(sample) != nil {
					return nil
				}
			case <-ctx.Done():
				return nil
			}
		}
	case "journal.follow":
		request, err := decodePayload[platform.JournalQuery](payload)
		if err != nil {
			return err
		}
		return platform.FollowJournalAs(ctx, request, nil, func(entry platform.LogEntry) error { return send(entry) })
	}
	return errDevelopmentReadOnly
}

// Open starts a login shell for the developer on a pseudo-terminal.
func (broker *localHostBroker) Open(ctx context.Context, operation string, _ auth.Credentials, payload any) (net.Conn, error) {
	if operation != "terminal.open" {
		return nil, errDevelopmentReadOnly
	}
	size, err := decodePayload[auth.TerminalSize](payload)
	if err != nil {
		return nil, err
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	command := exec.CommandContext(ctx, shell, "-l")
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	if home, err := os.UserHomeDir(); err == nil {
		command.Dir = home
	}
	if size.Columns == 0 || size.Rows == 0 {
		size = auth.TerminalSize{Columns: 120, Rows: 30}
	}
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Cols: size.Columns, Rows: size.Rows})
	if err != nil {
		return nil, err
	}
	local, remote := net.Pipe()
	go func() {
		defer terminal.Close()
		done := make(chan struct{}, 2)
		go func() { _, _ = io.Copy(terminal, remote); done <- struct{}{} }()
		go func() { _, _ = io.Copy(remote, terminal); done <- struct{}{} }()
		<-done
		_ = remote.Close()
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	return local, nil
}
