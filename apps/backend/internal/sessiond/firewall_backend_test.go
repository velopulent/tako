package sessiond

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/platform"
)

func TestFirewallCoordinatorRequiresCommitOrRollback(t *testing.T) {
	coordinator := newFirewallCoordinator(time.Minute)
	reads := []platform.FirewallSnapshot{
		{Backend: "UFW", Active: true, Fingerprint: "before"},
		{Backend: "UFW", Active: true, Fingerprint: "after"},
	}
	coordinator.read = func(context.Context) (platform.FirewallSnapshot, error) {
		value := reads[0]
		reads = reads[1:]
		return value, nil
	}
	var applied []platform.FirewallOperation
	coordinator.apply = func(_ context.Context, operation platform.FirewallOperation) (platform.FirewallState, error) {
		applied = append(applied, operation)
		return platform.FirewallState{Snapshot: platform.FirewallSnapshot{Backend: "UFW", Active: true, Fingerprint: "after"}, Applied: true}, nil
	}
	state, err := coordinator.Apply(context.Background(), platform.FirewallOperation{
		Backend: "UFW", Action: "add-port", Port: "443/tcp", ExpectedFingerprint: "before", Confirmation: "CONFIRM FIREWALL CHANGE", RollbackSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !state.RollbackRequired || state.Checkpoint == "" || state.RollbackToken == "" {
		t.Fatalf("guard state = %+v", state)
	}
	commit, err := coordinator.Apply(context.Background(), platform.FirewallOperation{
		Backend: "UFW", Action: "commit", Checkpoint: state.Checkpoint, RollbackToken: state.RollbackToken, Confirmation: "CONFIRM FIREWALL ACCESS",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !commit.Committed || len(applied) != 1 {
		t.Fatalf("commit state = %+v, applied = %+v", commit, applied)
	}
}

func TestFirewallCoordinatorCloseAppliesInverse(t *testing.T) {
	coordinator := newFirewallCoordinator(time.Minute)
	coordinator.read = func(context.Context) (platform.FirewallSnapshot, error) {
		return platform.FirewallSnapshot{Backend: "UFW", Active: true, Fingerprint: "after"}, nil
	}
	var applied []platform.FirewallOperation
	coordinator.apply = func(_ context.Context, operation platform.FirewallOperation) (platform.FirewallState, error) {
		applied = append(applied, operation)
		return platform.FirewallState{}, nil
	}
	state, err := coordinator.Apply(context.Background(), platform.FirewallOperation{
		Backend: "UFW", Action: "add-port", Port: "443/tcp", ExpectedFingerprint: "before", Confirmation: "CONFIRM FIREWALL CHANGE", RollbackSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.Close()
	if len(applied) != 2 || applied[1].Action != "remove-port" || applied[1].ExpectedFingerprint != "after" {
		t.Fatalf("close rollback = %+v, state=%+v", applied, state)
	}
	if _, err := coordinator.Apply(context.Background(), platform.FirewallOperation{Backend: "UFW", Action: "commit", Checkpoint: state.Checkpoint, RollbackToken: state.RollbackToken, Confirmation: "CONFIRM FIREWALL ACCESS"}); !errors.Is(err, platform.ErrFirewallCheckpoint) {
		t.Fatalf("closed checkpoint error = %v", err)
	}
}

func TestFirewallCoordinatorDoesNotUndoExternalChange(t *testing.T) {
	coordinator := newFirewallCoordinator(time.Minute)
	reads := []platform.FirewallSnapshot{
		{Backend: "UFW", Active: true, Fingerprint: "before"},
		{Backend: "UFW", Active: true, Fingerprint: "external"},
	}
	coordinator.read = func(context.Context) (platform.FirewallSnapshot, error) {
		if len(reads) == 0 {
			return platform.FirewallSnapshot{Backend: "UFW", Active: true, Fingerprint: "external"}, nil
		}
		value := reads[0]
		reads = reads[1:]
		return value, nil
	}
	var applied []platform.FirewallOperation
	coordinator.apply = func(_ context.Context, operation platform.FirewallOperation) (platform.FirewallState, error) {
		applied = append(applied, operation)
		return platform.FirewallState{Snapshot: platform.FirewallSnapshot{Backend: "UFW", Active: true, Fingerprint: "after"}, Applied: true}, nil
	}
	state, err := coordinator.Apply(context.Background(), platform.FirewallOperation{
		Backend: "UFW", Action: "add-port", Port: "443/tcp", ExpectedFingerprint: "before", Confirmation: "CONFIRM FIREWALL CHANGE", RollbackSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Apply(context.Background(), platform.FirewallOperation{
		Backend: "UFW", Action: "rollback", Checkpoint: state.Checkpoint, RollbackToken: state.RollbackToken, Confirmation: "CONFIRM FIREWALL ACCESS",
	}); !errors.Is(err, platform.ErrFirewallConflict) {
		t.Fatalf("external-change rollback error = %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("external change was overwritten: %+v", applied)
	}
}
