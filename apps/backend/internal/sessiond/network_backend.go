package sessiond

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/velopulent/tako/internal/platform"
)

const defaultNetworkRollbackDeadline = 120 * time.Second

type networkPendingTransaction struct {
	backend    string
	checkpoint string
	token      string
	deadline   time.Time
	timer      *time.Timer
}

// networkCoordinator lives for the lifetime of sessiond rather than a single
// browser request. A dropped browser connection therefore cannot abandon a
// NetworkManager checkpoint; the deadline rolls it back and a later request
// must present the same reconnect token before it can commit.
type networkCoordinator struct {
	mu       sync.Mutex
	pending  map[string]networkPendingTransaction
	deadline time.Duration
	now      func() time.Time
	apply    func(context.Context, platform.NetworkOperation) (platform.NetworkState, error)
}

func newNetworkCoordinator(deadline ...time.Duration) *networkCoordinator {
	value := defaultNetworkRollbackDeadline
	if len(deadline) > 0 && deadline[0] > 0 {
		value = deadline[0]
	}
	return &networkCoordinator{
		pending:  make(map[string]networkPendingTransaction),
		deadline: value,
		now:      time.Now,
		apply:    platform.ApplyNetworkOperation,
	}
}

func (coordinator *networkCoordinator) Apply(ctx context.Context, operation platform.NetworkOperation) (platform.NetworkState, error) {
	if coordinator == nil {
		return platform.ApplyNetworkOperation(ctx, operation)
	}
	if operation.Action == "commit" || operation.Action == "rollback" {
		pending, ok := coordinator.takePending(operation.ReconnectToken, operation.Checkpoint, operation.Backend)
		if !ok {
			return platform.NetworkState{}, platform.ErrNetworkCheckpoint
		}
		if operation.Checkpoint == "" {
			operation.Checkpoint = pending.checkpoint
		}
		state, err := coordinator.apply(ctx, operation)
		if err != nil {
			coordinator.restorePending(pending)
			return platform.NetworkState{}, err
		}
		return state, nil
	}
	state, err := coordinator.apply(ctx, operation)
	if err != nil {
		return platform.NetworkState{}, err
	}
	if state.ReconnectRequired && state.Checkpoint != "" && state.ReconnectToken != "" {
		coordinator.track(state, operation.Backend)
	}
	return state, nil
}

func (coordinator *networkCoordinator) track(state platform.NetworkState, backend string) {
	deadline := coordinator.now().Add(coordinator.deadline)
	pending := networkPendingTransaction{backend: backend, checkpoint: state.Checkpoint, token: state.ReconnectToken, deadline: deadline}
	pending.timer = time.AfterFunc(coordinator.deadline, func() {
		coordinator.expire(state.ReconnectToken)
	})
	coordinator.mu.Lock()
	if previous, ok := coordinator.pending[pending.token]; ok && previous.timer != nil {
		previous.timer.Stop()
	}
	coordinator.pending[pending.token] = pending
	coordinator.mu.Unlock()
}

func (coordinator *networkCoordinator) takePending(token, checkpoint, backend string) (networkPendingTransaction, bool) {
	if token == "" {
		return networkPendingTransaction{}, false
	}
	coordinator.mu.Lock()
	pending, ok := coordinator.pending[token]
	if ok && (pending.backend != backend || (checkpoint != "" && pending.checkpoint != checkpoint) || !coordinator.now().Before(pending.deadline)) {
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

func (coordinator *networkCoordinator) restorePending(pending networkPendingTransaction) {
	if pending.token == "" || !coordinator.now().Before(pending.deadline) {
		return
	}
	pending.timer = time.AfterFunc(time.Until(pending.deadline), func() {
		coordinator.expire(pending.token)
	})
	coordinator.mu.Lock()
	coordinator.pending[pending.token] = pending
	coordinator.mu.Unlock()
}

func (coordinator *networkCoordinator) expire(token string) {
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
	_, _ = coordinator.apply(ctx, platform.NetworkOperation{
		Backend:        pending.backend,
		Action:         "rollback",
		Checkpoint:     pending.checkpoint,
		ReconnectToken: pending.token,
		Confirmation:   "CONFIRM NETWORK RECONNECT",
	})
}

func (coordinator *networkCoordinator) Close() {
	if coordinator == nil {
		return
	}
	coordinator.mu.Lock()
	pending := make([]networkPendingTransaction, 0, len(coordinator.pending))
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
		_, _ = coordinator.apply(ctx, platform.NetworkOperation{Backend: item.backend, Action: "rollback", Checkpoint: item.checkpoint, ReconnectToken: item.token, Confirmation: "CONFIRM NETWORK RECONNECT"})
	}
}

func networkErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidNetworkOperation):
		return "invalid-network-operation"
	case errors.Is(err, platform.ErrNetworkConflict):
		return "network-conflict"
	case errors.Is(err, platform.ErrNetworkOwnership):
		return "network-ownership-conflict"
	case errors.Is(err, platform.ErrNetworkCheckpoint):
		return "network-checkpoint-invalid"
	case errors.Is(err, platform.ErrNetworkUnavailable):
		return "network-unavailable"
	default:
		return "network-operation-failed"
	}
}

func storageErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidStorageOperation):
		return "invalid-storage-operation"
	case errors.Is(err, platform.ErrStorageConflict):
		return "storage-conflict"
	case errors.Is(err, platform.ErrStorageUnsafe):
		return "storage-unsafe"
	case errors.Is(err, platform.ErrStorageBusy):
		return "storage-busy"
	case errors.Is(err, platform.ErrStorageUnavailable):
		return "storage-unavailable"
	default:
		return "storage-operation-failed"
	}
}

func firewallErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidFirewallOperation):
		return "invalid-firewall-operation"
	case errors.Is(err, platform.ErrFirewallConflict):
		return "firewall-conflict"
	case errors.Is(err, platform.ErrFirewallOwnership):
		return "firewall-ownership-conflict"
	case errors.Is(err, platform.ErrFirewallAccessRisk):
		return "firewall-access-risk"
	case errors.Is(err, platform.ErrFirewallUnavailable):
		return "firewall-unavailable"
	default:
		return "firewall-operation-failed"
	}
}

func securityErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidSecurityOperation):
		return "invalid-security-operation"
	case errors.Is(err, platform.ErrSecurityConflict):
		return "security-conflict"
	case errors.Is(err, platform.ErrSecurityUnsafe):
		return "security-unsafe"
	case errors.Is(err, platform.ErrSecurityUnavailable):
		return "security-unavailable"
	default:
		return "security-operation-failed"
	}
}
