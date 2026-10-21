package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/velopulent/tako/internal/platform"
)

// Request is one call to the privileged session service. The operation names
// the handler; at most one of Token (an authenticated user session) or
// AdminToken (an administrative grant) selects the execution lane. Payload is
// decoded strictly by the handler, so unknown fields are rejected.
type Request struct {
	Operation  string          `json:"operation"`
	Token      string          `json:"token,omitempty"`
	AdminToken string          `json:"adminToken,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// Response carries either a stable error code or the operation result.
// Streaming operations write one Response per event.
type Response struct {
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// Credentials select the already-authenticated execution lane. Session
// credentials run through the per-login user bridge; administrative
// credentials run as root inside sessiond.
type Credentials struct {
	Token      string
	AdminToken string
}

// Payload and result types shared by the gateway and sessiond.
type (
	ServiceReadOperation struct {
		Scope string `json:"scope"`
		Type  string `json:"type,omitempty"`
		Unit  string `json:"unit,omitempty"`
	}
	ProcessReadOperation struct {
		PID     int    `json:"pid"`
		Started uint64 `json:"started,omitempty"`
	}
	UpdateRefreshOperation struct {
		Force bool `json:"force"`
	}
	MetricsHistoryOperation struct {
		Since   time.Time `json:"since"`
		Maximum int       `json:"maximum"`
	}
	MetricsFollowOperation struct {
		Interval time.Duration `json:"interval"`
	}
	TerminalSize struct {
		Columns uint16 `json:"columns"`
		Rows    uint16 `json:"rows"`
	}
	AdminAuthorization struct {
		Password   string `json:"password"`
		TTLSeconds uint32 `json:"ttlSeconds"`
	}
	AdminGrant struct {
		Token string    `json:"token"`
		Until time.Time `json:"until"`
	}
	ConversationCancel struct {
		ConversationID string `json:"conversationId"`
	}
	HostConfigurationOperation struct {
		Hostname            string `json:"hostname"`
		Timezone            string `json:"timezone"`
		NTPEnabled          bool   `json:"ntpEnabled"`
		ExpectedFingerprint string `json:"expectedFingerprint"`
	}
	PowerOperation struct {
		Action              string `json:"action"`
		Confirmation        string `json:"confirmation"`
		ExpectedFingerprint string `json:"expectedFingerprint"`
	}
	KpatchReadResult struct {
		Status   platform.KpatchStatus         `json:"status"`
		Settings platform.KpatchSettingsStatus `json:"settings"`
	}
	// CertificateMaterial is the configured TLS pair in PEM form. Sessiond
	// reads it as root; the gateway holds it only in memory.
	CertificateMaterial struct {
		Certificate string `json:"certificate"`
		Key         string `json:"key"`
	}
)

type UpdateObservation = platform.UpdateObservation

// RemoteError is a stable error code returned by sessiond. It matches the
// platform sentinel registered for the same code, so gateway handlers can use
// errors.Is regardless of which side produced the failure.
type RemoteError struct{ Code string }

func (err *RemoteError) Error() string { return err.Code }

func (err *RemoteError) Is(target error) bool {
	for _, entry := range errorCodes {
		if entry.code == err.Code && entry.err == target {
			return true
		}
	}
	return false
}

// errorCodes is the single table of stable wire codes. Order matters for
// ErrorCode: the first sentinel that matches wins.
var errorCodes = []struct {
	code string
	err  error
}{
	{"invalid-file-operation", platform.ErrInvalidFileOperation},
	{"file-not-found", platform.ErrFileNotFound},
	{"file-permission-denied", platform.ErrFilePermission},
	{"file-conflict", platform.ErrFileConflict},
	{"file-too-large", platform.ErrFileTooLarge},
	{"unsafe-archive", platform.ErrUnsafeArchive},
	{"archive-limit", platform.ErrArchiveLimit},
	{"invalid-network-operation", platform.ErrInvalidNetworkOperation},
	{"network-conflict", platform.ErrNetworkConflict},
	{"network-ownership-conflict", platform.ErrNetworkOwnership},
	{"network-checkpoint-invalid", platform.ErrNetworkCheckpoint},
	{"network-unavailable", platform.ErrNetworkUnavailable},
	{"invalid-storage-operation", platform.ErrInvalidStorageOperation},
	{"storage-conflict", platform.ErrStorageConflict},
	{"storage-unsafe", platform.ErrStorageUnsafe},
	{"storage-busy", platform.ErrStorageBusy},
	{"storage-unavailable", platform.ErrStorageUnavailable},
	{"invalid-firewall-operation", platform.ErrInvalidFirewallOperation},
	{"firewall-conflict", platform.ErrFirewallConflict},
	{"firewall-ownership-conflict", platform.ErrFirewallOwnership},
	{"firewall-access-risk", platform.ErrFirewallAccessRisk},
	{"firewall-checkpoint-invalid", platform.ErrFirewallCheckpoint},
	{"firewall-unavailable", platform.ErrFirewallUnavailable},
	{"invalid-security-operation", platform.ErrInvalidSecurityOperation},
	{"security-conflict", platform.ErrSecurityConflict},
	{"security-unsafe", platform.ErrSecurityUnsafe},
	{"security-unavailable", platform.ErrSecurityUnavailable},
	{"invalid-ssh-key-operation", platform.ErrInvalidSSHKeyOperation},
	{"ssh-key-conflict", platform.ErrSSHKeyConflict},
	{"ssh-key-not-found", platform.ErrSSHKeyNotFound},
	{"ssh-key-read-only", platform.ErrSSHKeyReadOnly},
	{"ssh-key-unauthorized", platform.ErrSSHKeyUnauthorized},
	{"ssh-key-protected", platform.ErrSSHKeyProtected},
	{"ssh-key-verification-failed", platform.ErrSSHKeyVerification},
	{"ssh-key-unavailable", platform.ErrSSHKeyUnavailable},
	{"invalid-local-account-operation", platform.ErrInvalidLocalAccountOperation},
	{"local-account-conflict", platform.ErrLocalAccountConflict},
	{"local-account-not-found", platform.ErrLocalAccountNotFound},
	{"local-account-read-only", platform.ErrLocalAccountReadOnly},
	{"local-account-protected", platform.ErrLocalAccountProtected},
	{"local-account-verification-failed", platform.ErrLocalAccountVerification},
	{"local-account-unavailable", platform.ErrLocalAccountUnavailable},
	{"invalid-group-operation", platform.ErrInvalidGroupOperation},
	{"group-conflict", platform.ErrGroupConflict},
	{"group-not-found", platform.ErrGroupNotFound},
	{"group-user-not-found", platform.ErrGroupUserNotFound},
	{"group-read-only", platform.ErrGroupReadOnly},
	{"group-verification-failed", platform.ErrGroupVerification},
	{"group-unavailable", platform.ErrGroupUnavailable},
	{"administrative-role-unavailable", platform.ErrAdministrativeRoleUnavailable},
	{"administrative-role-protected", platform.ErrAdministrativeRoleProtected},
	{"invalid-local-group-operation", platform.ErrInvalidLocalGroupOperation},
	{"local-group-conflict", platform.ErrLocalGroupConflict},
	{"local-group-not-found", platform.ErrLocalGroupNotFound},
	{"local-group-read-only", platform.ErrLocalGroupReadOnly},
	{"local-group-protected", platform.ErrLocalGroupProtected},
	{"local-group-verification-failed", platform.ErrLocalGroupVerification},
	{"local-group-unavailable", platform.ErrLocalGroupUnavailable},
	{"invalid-password-operation", ErrPasswordInvalid},
	{"password-authentication-failed", ErrPasswordAuthenticationFailed},
	{"password-expired", ErrPasswordExpired},
	{"password-policy-failed", ErrPasswordPolicy},
	{"password-unavailable", ErrPasswordUnavailable},
	{"invalid-update-operation", platform.ErrInvalidUpdateOperation},
	{"update-conflict", platform.ErrUpdateConflict},
	{"update-locked", platform.ErrUpdateLocked},
	{"update-risk-not-accepted", platform.ErrUpdateRiskNotAccepted},
	{"update-verification-failed", platform.ErrUpdateVerification},
	{"update-apply-failed", platform.ErrUpdateApply},
	{"updates-unavailable", platform.ErrUpdateUnavailable},
	{"invalid-kpatch-operation", platform.ErrInvalidKpatchOperation},
	{"kpatch-apply-failed", platform.ErrKpatchApply},
	{"invalid-timer-operation", platform.ErrInvalidTimerOperation},
	{"timer-conflict", platform.ErrTimerConflict},
	{"timer-not-found", platform.ErrTimerNotFound},
	{"timer-pair-incomplete", platform.ErrTimerPairIncomplete},
	{"invalid-service-override", platform.ErrInvalidOverrideOperation},
	{"service-override-conflict", platform.ErrOverrideConflict},
	{"service-override-not-found", platform.ErrOverrideNotFound},
	{"service-override-unmanaged", platform.ErrOverrideUnmanaged},
	{"invalid-service-operation", platform.ErrInvalidServiceScope},
	{"invalid-service-operation", platform.ErrInvalidServiceUnit},
	{"invalid-service-operation", platform.ErrInvalidServiceAction},
	{"invalid-signal-operation", platform.ErrInvalidSignalOperation},
	{"process-not-found", platform.ErrProcessNotFound},
	{"process-reused", platform.ErrProcessReused},
	{"process-signal-conflict", platform.ErrSignalConflict},
	{"process-signal-unauthorized", platform.ErrSignalUnauthorized},
	{"invalid-journal-query", platform.ErrInvalidJournalQuery},
	{"invalid-login-history-query", platform.ErrInvalidLoginHistoryQuery},
	{"authentication-failed", ErrAuthenticationFailed},
	{"service-unavailable", ErrServiceUnavailable},
}

// ErrorCode returns the stable wire code for err, or fallback when err is not
// a registered sentinel. Unregistered errors never leak their text.
func ErrorCode(err error, fallback string) string {
	var remote *RemoteError
	if errors.As(err, &remote) {
		return remote.Code
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	for _, entry := range errorCodes {
		if errors.Is(err, entry.err) {
			return entry.code
		}
	}
	return fallback
}

// Code reports a stable code chosen by sessiond itself, such as
// "invalid-admin-token" or "permission-denied".
func Code(code string) error { return &RemoteError{Code: code} }

const (
	defaultCallTimeout = 30 * time.Second
	defaultResultLimit = 8 << 20
	dialTimeout        = 3 * time.Second
)

// CallOptions bound one request. Zero values use the defaults.
type CallOptions struct {
	Timeout time.Duration
	Limit   int64
}

// Call performs one request/response exchange with sessiond and decodes the
// result into result (which may be nil). Transport failures wrap
// ErrServiceUnavailable; sessiond failures are *RemoteError.
func Call(ctx context.Context, path, operation string, credentials Credentials, payload, result any, options CallOptions) error {
	timeout, limit := options.Timeout, options.Limit
	if timeout <= 0 {
		timeout = defaultCallTimeout
	}
	if limit <= 0 {
		limit = defaultResultLimit
	}
	conn, err := dial(ctx, path, operation, credentials, payload, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, limit)).Decode(&response); err != nil {
		return transportError(ctx, "read response", err)
	}
	return decodeResult(response, result)
}

// Stream sends one request and delivers each result until sessiond closes the
// stream, emit returns an error, or ctx ends.
func Stream(ctx context.Context, path, operation string, credentials Credentials, payload any, emit func(json.RawMessage) error) error {
	conn, err := dial(ctx, path, operation, credentials, payload, 0)
	if err != nil {
		return err
	}
	defer conn.Close()
	decoder := json.NewDecoder(conn)
	for {
		var response Response
		if err := decoder.Decode(&response); err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) {
				return nil
			}
			return transportError(ctx, "read stream", err)
		}
		if response.Error != "" {
			return &RemoteError{Code: response.Error}
		}
		if err := emit(response.Result); err != nil {
			return nil
		}
	}
}

// Open sends one request and returns the connection for a raw byte stream,
// such as a terminal. The caller owns and must close the connection.
func Open(ctx context.Context, path, operation string, credentials Credentials, payload any) (net.Conn, error) {
	return dial(ctx, path, operation, credentials, payload, 0)
}

// dial connects, sends the request, and ties the connection to ctx. A zero
// timeout leaves the connection open for streaming.
func dial(ctx context.Context, path, operation string, credentials Credentials, payload any, timeout time.Duration) (net.Conn, error) {
	request := Request{Operation: operation, Token: credentials.Token, AdminToken: credentials.AdminToken}
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode %s payload: %w", operation, err)
		}
		request.Payload = encoded
	}
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, transportError(ctx, "connect", err)
	}
	if timeout > 0 {
		deadline := time.Now().Add(timeout)
		if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
			deadline = contextDeadline
		}
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	encoded, err := json.Marshal(request)
	clear(request.Payload)
	if err == nil {
		_, err = conn.Write(append(encoded, '\n'))
		clear(encoded)
	}
	if err != nil {
		stop()
		_ = conn.Close()
		return nil, transportError(ctx, "send request", err)
	}
	return &contextConn{Conn: conn, stop: stop}, nil
}

// contextConn releases the context watcher when the caller closes it.
type contextConn struct {
	net.Conn
	stop func() bool
}

func (conn *contextConn) Close() error {
	conn.stop()
	return conn.Conn.Close()
}

func decodeResult(response Response, result any) error {
	if response.Error != "" {
		return &RemoteError{Code: response.Error}
	}
	if result == nil || len(response.Result) == 0 || bytes.Equal(response.Result, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("%w: decode result: %v", ErrServiceUnavailable, err)
	}
	return nil
}

func transportError(ctx context.Context, step string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return fmt.Errorf("%w: %s: %v", ErrServiceUnavailable, step, err)
}
