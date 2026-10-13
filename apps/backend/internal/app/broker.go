package app

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"github.com/velopulent/tako/internal/auth"
)

// HostBroker is the gateway's only host-access boundary. Production talks to
// sessiond over its Unix socket; development uses fixtures. Operation names
// and their credential lanes are defined by sessiond.
type HostBroker interface {
	Call(ctx context.Context, operation string, credentials auth.Credentials, payload, result any) error
	Stream(ctx context.Context, operation string, credentials auth.Credentials, payload any, emit func(json.RawMessage) error) error
	Open(ctx context.Context, operation string, credentials auth.Credentials, payload any) (net.Conn, error)
}

type socketHostBroker struct{ path string }

// callTimeouts outlast the matching sessiond deadlines so sessiond reports its
// own timeout instead of the gateway cutting the connection first.
var callTimeouts = map[string]time.Duration{
	"updates.apply":   31 * time.Minute,
	"updates.refresh": 5*time.Minute + 15*time.Second,
	"updates.preview": 2*time.Minute + 15*time.Second,
	"kpatch.apply":    5*time.Minute + 15*time.Second,
	"files.apply":     2*time.Minute + 15*time.Second,
	"storage.apply":   2*time.Minute + 15*time.Second,
	"network.apply":   2*time.Minute + 15*time.Second,
	"firewall.apply":  2*time.Minute + 15*time.Second,
	"security.apply":  2*time.Minute + 15*time.Second,
	"groups.local":    45 * time.Second,
}

func (broker socketHostBroker) Call(ctx context.Context, operation string, credentials auth.Credentials, payload, result any) error {
	timeout, ok := callTimeouts[operation]
	if !ok {
		timeout = 45 * time.Second
	}
	return auth.Call(ctx, broker.path, operation, credentials, payload, result, auth.CallOptions{Timeout: timeout})
}

func (broker socketHostBroker) Stream(ctx context.Context, operation string, credentials auth.Credentials, payload any, emit func(json.RawMessage) error) error {
	return auth.Stream(ctx, broker.path, operation, credentials, payload, emit)
}

func (broker socketHostBroker) Open(ctx context.Context, operation string, credentials auth.Credentials, payload any) (net.Conn, error) {
	return auth.Open(ctx, broker.path, operation, credentials, payload)
}

// streamAs decodes each streamed result into T before calling emit.
func streamAs[T any](ctx context.Context, broker HostBroker, operation string, credentials auth.Credentials, payload any, emit func(T) error) error {
	return broker.Stream(ctx, operation, credentials, payload, func(raw json.RawMessage) error {
		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		return emit(value)
	})
}
