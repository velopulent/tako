package sessiond

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/velopulent/tako/internal/platform"
)

const defaultFirewallRollbackDeadline = 120 * time.Second

type firewallPendingTransaction struct {
	backend    string
	checkpoint string
	token      string
	deadline   time.Time
	inverse    platform.FirewallOperation
	after      string
	timer      *time.Timer
}

type firewallCoordinator struct {
	mu       sync.Mutex
	pending  map[string]firewallPendingTransaction
	deadline time.Duration
	now      func() time.Time
	apply    func(context.Context, platform.FirewallOperation) (platform.FirewallState, error)
	read     func(context.Context) (platform.FirewallSnapshot, error)
}

func newFirewallCoordinator(deadline ...time.Duration) *firewallCoordinator {
	value := defaultFirewallRollbackDeadline
	if len(deadline) > 0 && deadline[0] > 0 {
		value = deadline[0]
	}
	return &firewallCoordinator{
		pending:  make(map[string]firewallPendingTransaction),
		deadline: value,
		now:      time.Now,
		apply:    platform.ApplyFirewallOperation,
		read:     platform.ReadFirewallStatus,
	}
}

func (coordinator *firewallCoordinator) Apply(ctx context.Context, operation platform.FirewallOperation) (platform.FirewallState, error) {
	if coordinator == nil {
		return platform.ApplyFirewallOperation(ctx, operation)
	}
	if operation.Action == "commit" || operation.Action == "rollback" {
		pending, ok := coordinator.take(operation.RollbackToken, operation.Checkpoint, operation.Backend)
		if !ok {
			return platform.FirewallState{}, platform.ErrFirewallCheckpoint
		}
		if operation.Action == "commit" {
			snapshot, err := coordinator.read(ctx)
			if err != nil {
				coordinator.restore(pending)
				return platform.FirewallState{}, err
			}
			if pending.after != "" && snapshot.Fingerprint != pending.after {
				coordinator.restore(pending)
				return platform.FirewallState{}, platform.ErrFirewallConflict
			}
			return platform.FirewallState{Snapshot: snapshot, Action: "commit", Applied: true, Committed: true, Checkpoint: pending.checkpoint, RollbackToken: pending.token, Warning: "Firewall change committed before timed rollback."}, nil
		}
		current, err := coordinator.read(ctx)
		if err != nil {
			coordinator.restore(pending)
			return platform.FirewallState{}, err
		}
		if pending.after != "" && current.Fingerprint != pending.after {
			coordinator.restore(pending)
			return platform.FirewallState{}, platform.ErrFirewallConflict
		}
		inverse := pending.inverse
		inverse.ExpectedFingerprint = current.Fingerprint
		inverse.RollbackSeconds = 0
		state, err := coordinator.apply(ctx, inverse)
		if err != nil {
			coordinator.restore(pending)
			return platform.FirewallState{}, err
		}
		state.Action = "rollback"
		state.Checkpoint = pending.checkpoint
		state.RollbackToken = pending.token
		state.Warning = "Firewall change rolled back by explicit confirmation."
		return state, nil
	}
	if operation.RollbackSeconds == 0 || !firewallRollbackable(operation) {
		return coordinator.apply(ctx, operation)
	}
	before, err := coordinator.read(ctx)
	if err != nil {
		return platform.FirewallState{}, err
	}
	if operation.Backend == "auto" {
		operation.Backend = before.Backend
	}
	state, err := coordinator.apply(ctx, operation)
	if err != nil {
		return platform.FirewallState{}, err
	}
	after := state.Snapshot.Fingerprint
	if after == "" {
		updated, readErr := coordinator.read(ctx)
		if readErr != nil {
			return platform.FirewallState{}, readErr
		}
		after = updated.Fingerprint
	}
	token := newFirewallToken()
	checkpoint := "tako-firewall-" + token
	pending := firewallPendingTransaction{
		backend: operation.Backend, checkpoint: checkpoint, token: token,
		deadline: coordinator.now().Add(time.Duration(operation.RollbackSeconds) * time.Second),
		inverse:  firewallInverse(operation, before), after: after,
	}
	pending.timer = time.AfterFunc(time.Until(pending.deadline), func() { coordinator.expire(token) })
	coordinator.mu.Lock()
	coordinator.pending[token] = pending
	coordinator.mu.Unlock()
	state.RollbackRequired = true
	state.Checkpoint = checkpoint
	state.RollbackToken = token
	state.RollbackDeadline = pending.deadline
	state.Warning = "Management-access guard active; commit after verifying a fresh session or rollback before deadline."
	return state, nil
}

func firewallRollbackable(operation platform.FirewallOperation) bool {
	return operation.Action == "disable" || operation.Action == "default-zone" || operation.Action == "remove-service" || operation.Action == "remove-port" || operation.Action == "remove-source" || operation.Action == "add-service" || operation.Action == "add-port" || operation.Action == "add-source"
}

func firewallInverse(operation platform.FirewallOperation, before platform.FirewallSnapshot) platform.FirewallOperation {
	inverse := operation
	switch operation.Action {
	case "disable":
		inverse.Action = "enable"
	case "remove-service":
		inverse.Action = "add-service"
	case "remove-port":
		inverse.Action = "add-port"
	case "remove-source":
		inverse.Action = "add-source"
	case "add-service":
		inverse.Action = "remove-service"
	case "add-port":
		inverse.Action = "remove-port"
	case "add-source":
		inverse.Action = "remove-source"
	case "default-zone":
		inverse.DefaultZone = before.DefaultZone
	}
	inverse.ExpectedFingerprint = ""
	inverse.RollbackSeconds = 0
	inverse.Checkpoint = ""
	inverse.RollbackToken = ""
	inverse.Confirmation = "CONFIRM FIREWALL ACCESS"
	return inverse
}

func (coordinator *firewallCoordinator) take(token, checkpoint, backend string) (firewallPendingTransaction, bool) {
	if token == "" || checkpoint == "" {
		return firewallPendingTransaction{}, false
	}
	coordinator.mu.Lock()
	pending, ok := coordinator.pending[token]
	if ok && (pending.backend != backend || pending.checkpoint != checkpoint || !coordinator.now().Before(pending.deadline)) {
		ok = false
	}
	if ok {
		delete(coordinator.pending, token)
		if pending.timer != nil {
			pending.timer.Stop()
		}
	}
	coordinator.mu.Unlock()
	return pending, ok
}

func (coordinator *firewallCoordinator) restore(pending firewallPendingTransaction) {
	if pending.token == "" || !coordinator.now().Before(pending.deadline) {
		return
	}
	pending.timer = time.AfterFunc(time.Until(pending.deadline), func() { coordinator.expire(pending.token) })
	coordinator.mu.Lock()
	coordinator.pending[pending.token] = pending
	coordinator.mu.Unlock()
}

func (coordinator *firewallCoordinator) expire(token string) {
	coordinator.mu.Lock()
	pending, ok := coordinator.pending[token]
	if ok {
		delete(coordinator.pending, token)
	}
	coordinator.mu.Unlock()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	current, err := coordinator.read(ctx)
	if err != nil {
		return
	}
	if pending.after != "" && current.Fingerprint != pending.after {
		return
	}
	inverse := pending.inverse
	inverse.ExpectedFingerprint = current.Fingerprint
	_, _ = coordinator.apply(ctx, inverse)
}

func (coordinator *firewallCoordinator) Close() {
	if coordinator == nil {
		return
	}
	coordinator.mu.Lock()
	pending := make([]firewallPendingTransaction, 0, len(coordinator.pending))
	for token, item := range coordinator.pending {
		delete(coordinator.pending, token)
		if item.timer != nil {
			item.timer.Stop()
		}
		pending = append(pending, item)
	}
	coordinator.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, item := range pending {
		current, err := coordinator.read(ctx)
		if err != nil {
			continue
		}
		if item.after != "" && current.Fingerprint != item.after {
			continue
		}
		inverse := item.inverse
		inverse.ExpectedFingerprint = current.Fingerprint
		_, _ = coordinator.apply(ctx, inverse)
	}
}

func newFirewallToken() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().String()))[:24]
	}
	return hex.EncodeToString(value[:])
}
