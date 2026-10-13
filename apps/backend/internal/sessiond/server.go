package sessiond

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"go.uber.org/zap"
)

// lane is the credential an operation accepts. sessiond resolves the token to
// a grant before any handler runs, so handlers never see an unauthenticated
// caller.
type lane int

const (
	laneNone       lane = iota // no credentials: login conversations
	laneSession                // an authenticated user session token
	laneAdmin                  // a live administrative grant
	laneEither                 // exactly one of the two above
	laneAdminToken             // an administrative token, possibly expired (revocation)
)

const (
	defaultOperationTimeout = 30 * time.Second
	requestReadTimeout      = 15 * time.Second
	maxRequestBytes         = 8 << 20
)

type operation struct {
	lane     lane
	timeout  time.Duration
	fallback string
	// run returns the result for a request/response operation.
	run func(context.Context, *call) (any, error)
	// takeover owns the connection for streams and terminals.
	takeover func(*call)
}

// call is one authenticated request.
type call struct {
	handler        *handler
	request        auth.Request
	identity       auth.Identity
	administrative bool
	conn           net.Conn
	reader         *bufio.Reader
	encoder        *json.Encoder
}

// handler holds the adapters every operation uses. Tests replace individual
// backends; production uses the system implementations from newHandler.
type handler struct {
	conversations *conversationStore
	grants        *grantStore
	policy        administrativePolicy
	logger        *zap.Logger
	runtime       *hostRuntime

	hostConfig hostConfigBackend
	power      powerBackend
	timers     timerBackend
	overrides  overrideBackend
	passwords  passwordBackend
	sshKeys    sshKeysBackend
	accounts   localAccountBackend
	groups     groupMembershipBackend
	roles      administrativeRoleBackend
	updates    updateBackend
	kpatch     kpatchBackend
	services   serviceBackend
}

func newHandler(service auth.PAMAuthenticator, conversations *conversationStore, grants *grantStore, policy administrativePolicy, logger *zap.Logger, runtime *hostRuntime) *handler {
	h := &handler{
		conversations: conversations,
		grants:        grants,
		policy:        policy,
		logger:        logger,
		runtime:       runtime,
		hostConfig:    systemHostConfigBackend{},
		power:         systemPowerBackend{},
		timers:        systemTimerBackend{},
		overrides:     systemOverrideBackend{},
		passwords:     systemPasswordBackend{service: service},
		sshKeys:       systemSSHKeysBackend{},
		accounts:      systemLocalAccountBackend{},
		groups:        systemGroupMembershipBackend{},
		roles:         systemAdministrativeRoleBackend{},
		updates:       systemUpdateBackend{},
		kpatch:        systemKpatchBackend{},
		services:      systemServiceBackend{},
	}
	if runtime != nil {
		h.updates = systemUpdateBackend{service: runtime.updates}
	}
	return h
}

func (h *handler) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(requestReadTimeout))
	reader := bufio.NewReaderSize(conn, 16<<10)
	encoder := json.NewEncoder(conn)
	var request auth.Request
	if err := decodeRequestLine(reader, &request); err != nil {
		h.logger.Warn("session request rejected", zap.String("reason", "invalid-request"), zap.Error(err))
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	spec, ok := operations[request.Operation]
	if !ok {
		h.logger.Warn("session request rejected", zap.String("reason", "unknown-operation"), zap.String("operation", request.Operation))
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	c := &call{handler: h, request: request, conn: conn, reader: reader, encoder: encoder}
	if code := c.authorize(spec.lane); code != "" {
		h.logger.Info("session request rejected", zap.String("operation", request.Operation), zap.String("error_code", code))
		_ = encoder.Encode(auth.Response{Error: code})
		return
	}
	if spec.takeover != nil {
		spec.takeover(c)
		return
	}
	timeout := spec.timeout
	if timeout == 0 {
		timeout = defaultOperationTimeout
	}
	_ = conn.SetDeadline(time.Now().Add(timeout + requestReadTimeout))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	result, err := spec.run(ctx, c)
	cancel()
	clear(request.Payload)
	if err != nil {
		code := auth.ErrorCode(err, spec.fallback)
		h.logger.Info("operation failed", zap.String("operation", request.Operation), zap.String("username", c.identity.Username), zap.Bool("administrative", c.administrative), zap.String("error_code", code), zap.Error(err))
		_ = encoder.Encode(auth.Response{Error: code})
		return
	}
	payload, err := json.Marshal(result)
	if err != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-result"})
		return
	}
	h.logger.Debug("operation completed", zap.String("operation", request.Operation), zap.String("username", c.identity.Username), zap.Bool("administrative", c.administrative))
	_ = encoder.Encode(auth.Response{Result: payload})
}

// authorize resolves the request credentials for the operation's lane and
// returns a stable error code when they do not fit.
func (c *call) authorize(required lane) string {
	token, admin := c.request.Token, c.request.AdminToken
	switch required {
	case laneNone:
		if token != "" || admin != "" {
			return "invalid-request"
		}
		return ""
	case laneAdminToken:
		if token != "" || admin == "" {
			return "invalid-request"
		}
		return ""
	case laneSession:
		if token == "" || admin != "" {
			return "invalid-request"
		}
	case laneAdmin:
		if admin == "" || token != "" {
			return "invalid-request"
		}
	case laneEither:
		if (token == "") == (admin == "") {
			return "invalid-request"
		}
	}
	if admin != "" {
		identity, ok := c.handler.grants.adminIdentity(admin)
		if !ok {
			return "invalid-admin-token"
		}
		c.identity, c.administrative = identity, true
		return ""
	}
	identity, ok := c.handler.grants.get(token)
	if !ok {
		return "invalid-bridge-token"
	}
	c.identity = identity
	return ""
}

// decode strictly decodes the request payload into destination.
func (c *call) decode(destination any) error {
	payload := c.request.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return auth.Code("invalid-request")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return auth.Code("invalid-request")
	}
	return nil
}

// bridge returns the caller's per-login user bridge.
func (c *call) bridge() (bridgeCaller, error) {
	closer, ok := c.handler.grants.bridgeFor(c.request.Token)
	if !ok {
		return nil, auth.Code("user-bridge-unavailable")
	}
	caller, ok := closer.(bridgeCaller)
	if !ok {
		return nil, auth.Code("user-bridge-unavailable")
	}
	return caller, nil
}

// viaBridge runs a read in the user lane and returns the bridge's JSON result
// unchanged.
func (c *call) viaBridge(ctx context.Context, method string, payload any) (any, error) {
	caller, err := c.bridge()
	if err != nil {
		return nil, err
	}
	return caller.call(ctx, method, method, payload)
}

// bridgeInto runs a user-lane call and decodes its result.
func bridgeInto[T any](ctx context.Context, c *call, method string, payload any) (T, error) {
	caller, err := c.bridge()
	if err != nil {
		var zero T
		return zero, err
	}
	return callBridge[T](ctx, caller, method, payload)
}

func (c *call) requireRuntime() (*hostRuntime, error) {
	if c.handler.runtime == nil {
		return nil, auth.Code("system-backend-unavailable")
	}
	return c.handler.runtime, nil
}

// stream writes one result per event until the caller stops.
func (c *call) emit(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.encoder.Encode(auth.Response{Result: payload})
}

func (c *call) fail(code string) {
	_ = c.encoder.Encode(auth.Response{Error: code})
}

func decodeRequestLine(reader *bufio.Reader, request *auth.Request) error {
	var payload []byte
	for {
		part, err := reader.ReadSlice('\n')
		payload = append(payload, part...)
		if len(payload) > maxRequestBytes {
			return errors.New("request exceeds limit")
		}
		if err == nil || errors.Is(err, io.EOF) {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return err
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(request); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing request data")
	}
	return nil
}
