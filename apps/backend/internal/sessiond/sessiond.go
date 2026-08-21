// Package sessiond implements Tako's narrow, socket-activated PAM boundary.
// Production packaging runs it as root with systemd hardening; it never listens
// on a network socket and never retains credentials.
package sessiond

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

type bridgeGrant struct {
	identity     auth.Identity
	expires      time.Time
	adminToken   string
	adminExpires time.Time
	terminal     *os.File
	bridge       io.Closer
	active       bool
	closePAM     func()
	timer        *time.Timer
}

type grantStore struct {
	mu     sync.Mutex
	values map[string]bridgeGrant
}

type administrativePolicy func(context.Context, auth.Identity, string) error

type serviceBackend interface {
	Run(context.Context, serviceOperation) error
}

type systemServiceBackend struct{}

func (systemServiceBackend) Run(ctx context.Context, operation serviceOperation) error {
	arguments := []string{operation.Action, "--", operation.Unit}
	if operation.Scope == "user" {
		arguments = append([]string{"--user"}, arguments...)
	}
	return exec.CommandContext(ctx, "systemctl", arguments...).Run()
}

var errAdministrativeUnavailable = errors.New("administrative policy unavailable")

// Run starts the privileged local session service. The caller selects this
// process mode explicitly; it never shares a process with the web gateway.
func Run(args []string) error {
	flags := flag.NewFlagSet("sessiond", flag.ContinueOnError)
	socket := flags.String("socket", "/run/tako/session.sock", "Unix socket path")
	if err := flags.Parse(args); err != nil {
		return err
	}

	listener, activated, err := activatedListener()
	if err != nil {
		return err
	}
	if !activated {
		if err := ensureSocketParent(*socket); err != nil {
			return err
		}
		_ = os.Remove(*socket)
		listener, err = net.Listen("unix", *socket)
		if err != nil {
			return err
		}
		if err := os.Chmod(*socket, 0o660); err != nil {
			_ = listener.Close()
			return err
		}
	}
	defer listener.Close()
	logger := zap.L().Named("sessiond")
	logger.Info("session service listening",
		zap.String("network", "unix"),
		zap.String("address", listener.Addr().String()),
		zap.Bool("socket_activated", activated),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	service := auth.PAMAuthenticator{Service: "tako"}
	grants := &grantStore{values: make(map[string]bridgeGrant)}
	policy := newAdministrativePolicy()
	conversations := newConversationStore(service, func(session auth.UserSession) (string, error) {
		return grants.addUserSession(session)
	})
	defer conversations.closeAll()
	defer grants.closeAll()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.Warn("connection accept failed", zap.Error(err))
			continue
		}
		go handle(conn, service, conversations, grants, policy, logger)
	}
}

func ensureSocketParent(socket string) error {
	dir := filepath.Dir(socket)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o750)
}

func activatedListener() (net.Listener, bool, error) {
	if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) || os.Getenv("LISTEN_FDS") != "1" {
		return nil, false, nil
	}
	file := os.NewFile(3, "systemd-session-socket")
	if file == nil {
		return nil, true, errors.New("invalid systemd session socket")
	}
	defer file.Close()
	listener, err := net.FileListener(file)
	return listener, true, err
}

func handle(conn net.Conn, service auth.PAMAuthenticator, conversations *conversationStore, grants *grantStore, policy administrativePolicy, logger *zap.Logger, hostBackends ...hostConfigBackend) {
	backend := hostConfigBackend(systemHostConfigBackend{})
	if len(hostBackends) > 0 && hostBackends[0] != nil {
		backend = hostBackends[0]
	}
	handleWithBackends(conn, service, conversations, grants, policy, logger, backend, systemPowerBackend{}, systemServiceBackend{})
}

func handleWithBackends(conn net.Conn, service auth.PAMAuthenticator, conversations *conversationStore, grants *grantStore, policy administrativePolicy, logger *zap.Logger, backend hostConfigBackend, power powerBackend, serviceBackends ...serviceBackend) {
	handleWithTimerBackends(conn, service, conversations, grants, policy, logger, backend, power, systemTimerBackend{}, systemOverrideBackend{}, serviceBackends...)
}

func handleWithTimerBackends(conn net.Conn, service auth.PAMAuthenticator, conversations *conversationStore, grants *grantStore, policy administrativePolicy, logger *zap.Logger, backend hostConfigBackend, power powerBackend, timer timerBackend, override overrideBackend, serviceBackends ...serviceBackend) {
	handleWithAllBackends(conn, service, conversations, grants, policy, logger, backend, power, timer, override, systemPasswordBackend{service: service}, systemSSHKeysBackend{}, systemLocalAccountBackend{}, systemGroupMembershipBackend{}, systemAdministrativeRoleBackend{}, serviceBackends...)
}

func handleWithLocalAccountBackend(conn net.Conn, service auth.PAMAuthenticator, conversations *conversationStore, grants *grantStore, policy administrativePolicy, logger *zap.Logger, backend hostConfigBackend, power powerBackend, timer timerBackend, override overrideBackend, account localAccountBackend, serviceBackends ...serviceBackend) {
	handleWithAllBackends(conn, service, conversations, grants, policy, logger, backend, power, timer, override, systemPasswordBackend{service: service}, systemSSHKeysBackend{}, account, systemGroupMembershipBackend{}, systemAdministrativeRoleBackend{}, serviceBackends...)
}

func handleWithGroupBackends(conn net.Conn, service auth.PAMAuthenticator, conversations *conversationStore, grants *grantStore, policy administrativePolicy, logger *zap.Logger, backend hostConfigBackend, power powerBackend, timer timerBackend, override overrideBackend, groups groupMembershipBackend, roles administrativeRoleBackend, serviceBackends ...serviceBackend) {
	handleWithAllBackends(conn, service, conversations, grants, policy, logger, backend, power, timer, override, systemPasswordBackend{service: service}, systemSSHKeysBackend{}, systemLocalAccountBackend{}, groups, roles, serviceBackends...)
}

func handleWithPasswordBackend(conn net.Conn, service auth.PAMAuthenticator, conversations *conversationStore, grants *grantStore, policy administrativePolicy, logger *zap.Logger, backend hostConfigBackend, power powerBackend, timer timerBackend, override overrideBackend, password passwordBackend, serviceBackends ...serviceBackend) {
	handleWithAllBackends(conn, service, conversations, grants, policy, logger, backend, power, timer, override, password, systemSSHKeysBackend{}, systemLocalAccountBackend{}, systemGroupMembershipBackend{}, systemAdministrativeRoleBackend{}, serviceBackends...)
}

func handleWithSSHKeysBackend(conn net.Conn, service auth.PAMAuthenticator, conversations *conversationStore, grants *grantStore, policy administrativePolicy, logger *zap.Logger, backend hostConfigBackend, power powerBackend, timer timerBackend, override overrideBackend, sshKeys sshKeysBackend, serviceBackends ...serviceBackend) {
	handleWithAllBackends(conn, service, conversations, grants, policy, logger, backend, power, timer, override, systemPasswordBackend{service: service}, sshKeys, systemLocalAccountBackend{}, systemGroupMembershipBackend{}, systemAdministrativeRoleBackend{}, serviceBackends...)
}

func handleWithAllBackends(conn net.Conn, service auth.PAMAuthenticator, conversations *conversationStore, grants *grantStore, policy administrativePolicy, logger *zap.Logger, backend hostConfigBackend, power powerBackend, timer timerBackend, override overrideBackend, password passwordBackend, sshKeys sshKeysBackend, account localAccountBackend, groups groupMembershipBackend, roles administrativeRoleBackend, serviceBackends ...serviceBackend) {
	handleWithAllBackendsAndUpdates(conn, service, conversations, grants, policy, logger, backend, power, timer, override, password, sshKeys, account, groups, roles, systemUpdateBackend{}, serviceBackends...)
}

func handleWithAllBackendsAndUpdates(conn net.Conn, service auth.PAMAuthenticator, conversations *conversationStore, grants *grantStore, policy administrativePolicy, logger *zap.Logger, backend hostConfigBackend, power powerBackend, timer timerBackend, override overrideBackend, password passwordBackend, sshKeys sshKeysBackend, account localAccountBackend, groups groupMembershipBackend, roles administrativeRoleBackend, updates updateBackend, serviceBackends ...serviceBackend) {
	defer conn.Close()
	if backend == nil {
		backend = systemHostConfigBackend{}
	}
	if power == nil {
		power = systemPowerBackend{}
	}
	if timer == nil {
		timer = systemTimerBackend{}
	}
	if override == nil {
		override = systemOverrideBackend{}
	}
	if password == nil {
		password = systemPasswordBackend{service: service}
	}
	if sshKeys == nil {
		sshKeys = systemSSHKeysBackend{}
	}
	if account == nil {
		account = systemLocalAccountBackend{}
	}
	if groups == nil {
		groups = systemGroupMembershipBackend{}
	}
	if roles == nil {
		roles = systemAdministrativeRoleBackend{}
	}
	if updates == nil {
		updates = systemUpdateBackend{}
	}
	serviceBackend := serviceBackend(systemServiceBackend{})
	if len(serviceBackends) > 0 && serviceBackends[0] != nil {
		serviceBackend = serviceBackends[0]
	}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReaderSize(conn, 16<<10)
	encoder := json.NewEncoder(conn)
	var request auth.Request
	if err := decodeRequestLine(reader, &request); err != nil {
		logger.Warn("session request rejected", zap.String("reason", "invalid-request"), zap.Error(err))
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "host-config" && request.Operation != "power" && hasHostConfigurationFields(request) {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "power" && hasPowerFields(request) {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "timer" && request.Timer != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "service-override" && request.Override != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "signal-process" && request.Signal != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "local-account" && request.Account != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "group-membership" && request.GroupMembership != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "admin-role" && request.AdminRole != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "password-change" && request.PasswordChange != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "ssh-keys" && request.SSHKeys != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "updates" && request.Updates != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "file" && request.File != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "journal-query" && request.Operation != "journal-follow" && request.Journal != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "network" && request.Network != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "firewall" && request.Firewall != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "security" && request.Security != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "support-report" && request.SupportReport != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation == "conversation" {
		if request.Token != "" || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" {
			_ = encoder.Encode(auth.Response{Error: "invalid-conversation"})
			return
		}
		conversationRequest := auth.ConversationRequest{
			ConversationID: request.ConversationID,
			Username:       request.Username,
			Password:       request.Password,
			Responses:      request.Responses,
		}
		if !conversationRequest.Valid() || conversationRequest.Cancel {
			_ = encoder.Encode(auth.Response{Error: "invalid-conversation"})
			return
		}
		_ = conn.SetDeadline(time.Now().Add(time.Minute))
		conversationCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		response, err := conversations.advance(conversationCtx, conversationRequest)
		request.Password = ""
		conversationRequest.Password = ""
		for index := range request.Responses {
			request.Responses[index].Value = ""
		}
		for index := range conversationRequest.Responses {
			conversationRequest.Responses[index].Value = ""
		}
		if err != nil {
			code := "authentication-failed"
			if errors.Is(err, errConversationBusy) {
				code = "authentication-busy"
			}
			if errors.Is(err, errConversationInvalid) || errors.Is(err, errConversationBounds) {
				code = "invalid-conversation"
			}
			if errors.Is(err, errSessionFailed) {
				code = "session-failed"
			}
			// Keep the client response generic, but retain the underlying PAM or
			// user-bridge failure in the privileged service journal. Secret fields
			// have been cleared before this point.
			logger.Warn("authentication conversation failed",
				zap.String("username", conversationRequest.Username),
				zap.String("reason", code),
				zap.Error(err),
			)
			_ = encoder.Encode(auth.Response{Error: code})
			return
		}
		if err := encoder.Encode(auth.Response{
			Identity:       response.Identity,
			BridgeToken:    response.BridgeToken,
			ConversationID: response.ConversationID,
			Prompts:        response.Prompts,
		}); err != nil && response.ConversationID != "" {
			conversations.cancel(response.ConversationID)
		}
		return
	}
	if request.Operation == "cancel-conversation" {
		if request.Username != "" || request.Password != "" || len(request.Responses) != 0 || request.Token != "" || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" {
			_ = encoder.Encode(auth.Response{Error: "invalid-conversation"})
			return
		}
		if !(auth.ConversationRequest{ConversationID: request.ConversationID, Cancel: true}).Valid() {
			_ = encoder.Encode(auth.Response{Error: "invalid-conversation"})
			return
		}
		conversations.cancel(request.ConversationID)
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation == "authorize-admin" {
		if request.Token == "" || request.AdminToken != "" || request.Username != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.AdminTTL > 3600 || len(request.Password) == 0 || len(request.Password) > 16<<10 {
			_ = encoder.Encode(auth.Response{Error: "invalid-administrative-request"})
			return
		}
		if policy == nil {
			request.Password = ""
			_ = encoder.Encode(auth.Response{Error: "administrative-policy-unavailable"})
			return
		}
		adminCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		adminToken, until, err := grants.authorize(adminCtx, request.Token, request.Password, request.AdminTTL, policy)
		cancel()
		request.Password = ""
		if err != nil {
			code := "administrative-access-denied"
			if errors.Is(err, errAdministrativeUnavailable) {
				code = "administrative-policy-unavailable"
			}
			_ = encoder.Encode(auth.Response{Error: code})
			return
		}
		_ = encoder.Encode(auth.Response{AdminToken: adminToken, AdminUntil: until})
		return
	}
	if request.Operation == "revoke-admin" {
		if request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.AdminTTL != 0 {
			_ = encoder.Encode(auth.Response{Error: "invalid-administrative-request"})
			return
		}
		if !grants.revokeAdmin(request.AdminToken) {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation == "close-session" {
		if request.Token == "" || request.AdminToken != "" || !grants.close(request.Token) {
			_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
			return
		}
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation == "confirm-session" {
		if request.Token == "" || request.AdminToken != "" || !grants.confirm(request.Token) {
			_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
			return
		}
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation == "terminal" {
		if request.Token == "" || request.AdminToken != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" {
			_ = encoder.Encode(auth.Response{Error: "invalid-terminal-request"})
			return
		}
		_ = conn.SetDeadline(time.Time{})
		identity, ok := grants.claim(request.Token)
		if !ok {
			logger.Warn("terminal request rejected", zap.String("reason", "invalid-bridge-token"))
			_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
			return
		}
		logger.Info("terminal starting", zap.String("username", identity.Username), zap.Int("uid", identity.UID))
		runTerminal(conn, reader, identity, request.Columns, request.Rows, grants, request.Token, logger)
		return
	}
	if request.Operation == "resize-terminal" {
		if request.Token == "" || request.AdminToken != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" {
			_ = encoder.Encode(auth.Response{Error: "invalid-terminal-request"})
			return
		}
		responseError := grants.resize(request.Token, request.Columns, request.Rows)
		if responseError != "" {
			logger.Warn("terminal resize rejected", zap.String("reason", responseError))
		}
		_ = encoder.Encode(auth.Response{Error: responseError})
		return
	}
	if request.Operation == "service-action" {
		if request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 {
			_ = encoder.Encode(auth.Response{Error: "invalid-service-request"})
			return
		}
		var identity auth.Identity
		var ok bool
		if request.Scope == "system" {
			if request.Token != "" || request.AdminToken == "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-administrative-request"})
				return
			}
			identity, ok = grants.adminIdentity(request.AdminToken)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
				return
			}
		} else {
			if request.Token == "" || request.AdminToken != "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-service-request"})
				return
			}
			identity, ok = grants.get(request.Token)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
				return
			}
		}
		operation, operationErr := parseServiceOperation(request.Scope, request.Unit, request.Action)
		if operationErr != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-service-operation"})
			return
		}
		errorCode := runServiceAction(serviceBackend, operation)
		logger.Info("service action", zap.String("username", identity.Username), zap.String("scope", request.Scope), zap.String("unit", request.Unit), zap.String("action", request.Action), zap.String("result", errorCode))
		_ = encoder.Encode(auth.Response{Error: errorCode})
		return
	}
	if request.Operation == "timer" {
		if request.Timer == nil || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 {
			_ = encoder.Encode(auth.Response{Error: "invalid-timer-request"})
			return
		}
		operation := *request.Timer
		if err := platform.ValidateTimerOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-timer-operation"})
			return
		}
		var bridge io.Closer
		var identity auth.Identity
		var ok bool
		if operation.Scope == "system" {
			if request.Token != "" || request.AdminToken == "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-administrative-request"})
				return
			}
			identity, ok = grants.adminIdentity(request.AdminToken)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
				return
			}
			bridge, _ = grants.adminBridge(request.AdminToken)
		} else {
			if request.Token == "" || request.AdminToken != "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-timer-request"})
				return
			}
			identity, ok = grants.get(request.Token)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
				return
			}
			bridge, ok = grants.bridgeFor(request.Token)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "user-bridge-unavailable"})
				return
			}
		}
		timerCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		state, applyErr := timer.Apply(timerCtx, operation, bridge)
		cancel()
		if applyErr != nil {
			code := timerErrorCode(applyErr)
			logger.Warn("timer operation failed", zap.String("username", identity.Username), zap.String("scope", operation.Scope), zap.String("name", operation.Name), zap.String("error", code))
			_ = encoder.Encode(auth.Response{Error: code})
			return
		}
		logger.Info("timer operation", zap.String("username", identity.Username), zap.String("scope", operation.Scope), zap.String("name", operation.Name), zap.String("action", operation.Action))
		_ = encoder.Encode(auth.Response{TimerState: &state})
		return
	}
	if request.Operation == "service-override" {
		if request.Override == nil || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 {
			_ = encoder.Encode(auth.Response{Error: "invalid-service-override-request"})
			return
		}
		operation := *request.Override
		if err := platform.ValidateOverrideOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-service-override"})
			return
		}
		var bridge io.Closer
		var identity auth.Identity
		var ok bool
		if operation.Scope == "system" {
			if request.Token != "" || request.AdminToken == "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-administrative-request"})
				return
			}
			identity, ok = grants.adminIdentity(request.AdminToken)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
				return
			}
		} else {
			if request.Token == "" || request.AdminToken != "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-service-override-request"})
				return
			}
			identity, ok = grants.get(request.Token)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
				return
			}
			bridge, ok = grants.bridgeFor(request.Token)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "user-bridge-unavailable"})
				return
			}
		}
		overrideCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		state, applyErr := override.Apply(overrideCtx, operation, bridge)
		cancel()
		if applyErr != nil {
			code := overrideErrorCode(applyErr)
			logger.Warn("service override failed", zap.String("username", identity.Username), zap.String("scope", operation.Scope), zap.String("unit", operation.Unit), zap.String("error", code))
			_ = encoder.Encode(auth.Response{Error: code})
			return
		}
		_ = encoder.Encode(auth.Response{OverrideState: &state})
		return
	}
	if request.Operation == "signal-process" {
		if request.Signal == nil || request.Signal.Action != "apply" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 {
			_ = encoder.Encode(auth.Response{Error: "invalid-signal-request"})
			return
		}
		operation := *request.Signal
		if err := platform.ValidateSignalOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-signal-operation"})
			return
		}
		var identity auth.Identity
		var administrative bool
		if request.AdminToken != "" {
			if request.Token != "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-administrative-request"})
				return
			}
			var ok bool
			identity, ok = grants.adminIdentity(request.AdminToken)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
				return
			}
			administrative = true
		} else {
			if request.Token == "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
				return
			}
			var ok bool
			identity, ok = grants.get(request.Token)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
				return
			}
		}
		signalCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		result, signalErr := executeProcessSignal(signalCtx, identity, administrative, operation)
		cancel()
		if signalErr != nil {
			logger.Warn("process signal rejected", zap.String("username", identity.Username), zap.String("signal", operation.Signal), zap.Error(signalErr))
			_ = encoder.Encode(auth.Response{Error: signalErrorCode(signalErr)})
			return
		}
		logger.Info("process signal", zap.String("username", identity.Username), zap.String("signal", operation.Signal), zap.Bool("tree", operation.Tree), zap.Int("targets", len(result.Targets)), zap.Bool("administrative", administrative))
		_ = encoder.Encode(auth.Response{SignalResult: &result})
		return
	}
	if request.Operation == "local-account" {
		if request.Account == nil || request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-local-account-request"})
			return
		}
		identity, ok := grants.adminIdentity(request.AdminToken)
		if !ok {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		operation := *request.Account
		if err := platform.ValidateLocalAccountOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-local-account-operation"})
			return
		}
		accountCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if operation.Preview {
			preview, previewErr := account.Preview(accountCtx, operation, identity)
			cancel()
			if previewErr != nil {
				_ = encoder.Encode(auth.Response{Error: localAccountErrorCode(previewErr)})
				return
			}
			_ = encoder.Encode(auth.Response{AccountPreview: &preview})
			return
		}
		state, applyErr := account.Apply(accountCtx, operation, identity)
		cancel()
		if applyErr != nil {
			_ = encoder.Encode(auth.Response{Error: localAccountErrorCode(applyErr)})
			return
		}
		_ = encoder.Encode(auth.Response{AccountState: &state})
		return
	}
	if request.Operation == "group-membership" {
		if request.GroupMembership == nil || request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil || request.Account != nil || request.AdminRole != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-group-request"})
			return
		}
		identity, ok := grants.adminIdentity(request.AdminToken)
		if !ok {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		operation := *request.GroupMembership
		if err := platform.ValidateGroupMembershipOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-group-operation"})
			return
		}
		groupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if operation.Preview {
			preview, previewErr := groups.Preview(groupCtx, operation, identity)
			cancel()
			if previewErr != nil {
				_ = encoder.Encode(auth.Response{Error: groupErrorCode(previewErr)})
				return
			}
			_ = encoder.Encode(auth.Response{GroupMembershipPreview: &preview})
			return
		}
		state, applyErr := groups.Apply(groupCtx, operation, identity)
		cancel()
		if applyErr != nil {
			_ = encoder.Encode(auth.Response{Error: groupErrorCode(applyErr)})
			return
		}
		_ = encoder.Encode(auth.Response{GroupMembershipState: &state})
		return
	}
	if request.Operation == "admin-role" {
		if request.AdminRole == nil || request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil || request.Account != nil || request.GroupMembership != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-role-request"})
			return
		}
		identity, ok := grants.adminIdentity(request.AdminToken)
		if !ok {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		operation := *request.AdminRole
		if err := platform.ValidateAdministrativeRoleOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-role-operation"})
			return
		}
		roleCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if operation.Preview {
			preview, previewErr := roles.Preview(roleCtx, operation, identity)
			cancel()
			if previewErr != nil {
				_ = encoder.Encode(auth.Response{Error: groupErrorCode(previewErr)})
				return
			}
			_ = encoder.Encode(auth.Response{AdminRolePreview: &preview})
			return
		}
		state, applyErr := roles.Apply(roleCtx, operation, identity)
		cancel()
		if applyErr != nil {
			_ = encoder.Encode(auth.Response{Error: groupErrorCode(applyErr)})
			return
		}
		_ = encoder.Encode(auth.Response{AdminRoleState: &state})
		return
	}
	if request.Operation == "password-change" {
		if request.PasswordChange == nil || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil || request.Account != nil || request.GroupMembership != nil || request.AdminRole != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-password-operation"})
			return
		}
		operation := *request.PasswordChange
		request.PasswordChange.Clear()
		request.PasswordChange = nil
		defer operation.Clear()
		if err := auth.ValidatePasswordChangeOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-password-operation"})
			return
		}
		var identity auth.Identity
		var ok bool
		if operation.Action == "change" {
			if request.Token == "" || request.AdminToken != "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-password-operation"})
				return
			}
			identity, ok = grants.get(request.Token)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
				return
			}
		} else {
			if request.AdminToken == "" || request.Token != "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-password-operation"})
				return
			}
			identity, ok = grants.adminIdentity(request.AdminToken)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
				return
			}
		}
		passwordCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		var passwordErr error
		if operation.Action == "change" {
			passwordErr = password.Change(passwordCtx, identity.Username, operation.CurrentPassword, operation.NewPassword)
		} else {
			passwordErr = password.Reset(passwordCtx, operation.Username, operation.NewPassword)
		}
		cancel()
		if passwordErr != nil {
			code := passwordErrorCode(passwordErr)
			logger.Warn("password operation failed", zap.String("username", identity.Username), zap.String("action", operation.Action), zap.String("result", code))
			_ = encoder.Encode(auth.Response{Error: code})
			return
		}
		logger.Info("password operation succeeded", zap.String("username", identity.Username), zap.String("action", operation.Action), zap.Bool("administrative", operation.Action == "reset"))
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation == "ssh-keys" {
		if request.SSHKeys == nil || (request.Token == "" && request.AdminToken == "") || (request.Token != "" && request.AdminToken != "") || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil || request.Account != nil || request.GroupMembership != nil || request.AdminRole != nil || request.PasswordChange != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-ssh-key-operation"})
			return
		}
		operation := *request.SSHKeys
		request.SSHKeys = nil
		var identity auth.Identity
		var ok bool
		administrative := request.AdminToken != ""
		if administrative {
			identity, ok = grants.adminIdentity(request.AdminToken)
		} else {
			identity, ok = grants.get(request.Token)
		}
		if !ok {
			if administrative {
				_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			} else {
				_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
			}
			return
		}
		if !administrative && operation.Username != identity.Username {
			_ = encoder.Encode(auth.Response{Error: "ssh-key-unauthorized"})
			return
		}
		if err := platform.ValidateSSHKeyOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-ssh-key-operation"})
			return
		}
		sshCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if operation.Preview {
			preview, previewErr := sshKeys.Preview(sshCtx, operation, identity, administrative)
			cancel()
			if previewErr != nil {
				_ = encoder.Encode(auth.Response{Error: sshKeyErrorCode(previewErr)})
				return
			}
			_ = encoder.Encode(auth.Response{SSHKeyPreview: &preview})
			return
		}
		state, applyErr := sshKeys.Apply(sshCtx, operation, identity, administrative)
		cancel()
		if applyErr != nil {
			_ = encoder.Encode(auth.Response{Error: sshKeyErrorCode(applyErr)})
			return
		}
		_ = encoder.Encode(auth.Response{SSHKeyState: &state})
		return
	}
	if request.Operation == "updates" {
		if request.Updates == nil || request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil || request.Account != nil || request.GroupMembership != nil || request.AdminRole != nil || request.PasswordChange != nil || request.SSHKeys != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-update-operation"})
			return
		}
		identity, ok := grants.adminIdentity(request.AdminToken)
		if !ok {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		operation := *request.Updates
		request.Updates = nil
		if err := platform.ValidateUpdateOperation(operation); err != nil || operation.Preview {
			_ = encoder.Encode(auth.Response{Error: "invalid-update-operation"})
			return
		}
		_ = conn.SetDeadline(time.Now().Add(30 * time.Minute))
		updateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		result, applyErr := updates.Apply(updateCtx, operation, identity)
		cancel()
		if applyErr != nil {
			_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
			_ = encoder.Encode(auth.Response{Error: updateErrorCode(applyErr)})
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
		_ = encoder.Encode(auth.Response{UpdateResult: &result})
		return
	}
	if request.Operation == "file" {
		if request.File == nil || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil || request.Account != nil || request.GroupMembership != nil || request.AdminRole != nil || request.PasswordChange != nil || request.SSHKeys != nil || request.Updates != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-file-operation"})
			return
		}
		operation := *request.File
		request.File = nil
		if err := platform.ValidateFileOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: fileErrorCode(err)})
			return
		}
		var identity auth.Identity
		var bridge io.Closer
		var administrative bool
		var ok bool
		if request.AdminToken != "" {
			if request.Token != "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-file-operation"})
				return
			}
			identity, ok = grants.adminIdentity(request.AdminToken)
			administrative = true
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
				return
			}
		} else {
			if request.Token == "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-file-operation"})
				return
			}
			identity, ok = grants.get(request.Token)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
				return
			}
			bridge, ok = grants.bridgeFor(request.Token)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "user-bridge-unavailable"})
				return
			}
		}
		fileCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		result, applyErr := (systemFileBackend{}).Apply(fileCtx, operation, identity, administrative, bridge)
		cancel()
		if applyErr != nil {
			_ = encoder.Encode(auth.Response{Error: fileErrorCode(applyErr)})
			return
		}
		_ = encoder.Encode(auth.Response{FileResult: &result})
		return
	}
	if request.Operation == "journal-query" || request.Operation == "journal-follow" {
		handleJournal(conn, encoder, request, grants, request.Operation == "journal-follow")
		return
	}
	if request.Operation == "network" {
		if request.Network == nil || request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil || request.Account != nil || request.GroupMembership != nil || request.AdminRole != nil || request.PasswordChange != nil || request.SSHKeys != nil || request.Updates != nil || request.File != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-network-operation"})
			return
		}
		identity, ok := grants.adminIdentity(request.AdminToken)
		if !ok {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		operation := *request.Network
		request.Network = nil
		if err := platform.ValidateNetworkOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: networkErrorCode(err)})
			return
		}
		networkCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		var state platform.NetworkState
		var operationErr error
		if operation.Action == "preview" {
			state, operationErr = platform.PreviewNetworkOperation(networkCtx, operation)
		} else {
			state, operationErr = platform.ApplyNetworkOperation(networkCtx, operation)
		}
		cancel()
		if operationErr != nil {
			_ = encoder.Encode(auth.Response{Error: networkErrorCode(operationErr)})
			return
		}
		_ = encoder.Encode(auth.Response{NetworkState: &state})
		_ = identity
		return
	}
	if request.Operation == "firewall" {
		if request.Firewall == nil || request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil || request.Account != nil || request.GroupMembership != nil || request.AdminRole != nil || request.PasswordChange != nil || request.SSHKeys != nil || request.Updates != nil || request.File != nil || request.Network != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-firewall-operation"})
			return
		}
		if _, ok := grants.adminIdentity(request.AdminToken); !ok {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		operation := *request.Firewall
		request.Firewall = nil
		if err := platform.ValidateFirewallOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: firewallErrorCode(err)})
			return
		}
		firewallCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		var state platform.FirewallState
		var operationErr error
		if operation.Action == "preview" {
			state, operationErr = platform.PreviewFirewallOperation(firewallCtx, operation)
		} else {
			state, operationErr = platform.ApplyFirewallOperation(firewallCtx, operation)
		}
		cancel()
		if operationErr != nil {
			_ = encoder.Encode(auth.Response{Error: firewallErrorCode(operationErr)})
			return
		}
		_ = encoder.Encode(auth.Response{FirewallState: &state})
		return
	}
	if request.Operation == "security" {
		if request.Security == nil || request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil || request.Account != nil || request.GroupMembership != nil || request.AdminRole != nil || request.PasswordChange != nil || request.SSHKeys != nil || request.Updates != nil || request.File != nil || request.Network != nil || request.Firewall != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-security-operation"})
			return
		}
		if _, ok := grants.adminIdentity(request.AdminToken); !ok {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		operation := *request.Security
		request.Security = nil
		if err := platform.ValidateSecurityOperation(operation); err != nil {
			_ = encoder.Encode(auth.Response{Error: securityErrorCode(err)})
			return
		}
		securityCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		var status platform.SecurityStatus
		var operationErr error
		if operation.Action == "inspect" {
			status, operationErr = platform.PreviewSecurityOperation(securityCtx, operation)
		} else {
			status, operationErr = platform.ApplySecurityOperation(securityCtx, operation)
		}
		cancel()
		if operationErr != nil {
			_ = encoder.Encode(auth.Response{Error: securityErrorCode(operationErr)})
			return
		}
		_ = encoder.Encode(auth.Response{SecurityStatus: &status})
		return
	}
	if request.Operation == "support-report" {
		if request.SupportReport == nil || request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil || request.Account != nil || request.GroupMembership != nil || request.AdminRole != nil || request.PasswordChange != nil || request.SSHKeys != nil || request.Updates != nil || request.File != nil || request.Network != nil || request.Firewall != nil || request.Security != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-support-report"})
			return
		}
		if _, ok := grants.adminIdentity(request.AdminToken); !ok {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		operation := *request.SupportReport
		request.SupportReport = nil
		reportCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		report, reportErr := platform.CollectSupportReport(reportCtx, operation)
		cancel()
		if reportErr != nil {
			if errors.Is(reportErr, platform.ErrInvalidSupportReport) {
				_ = encoder.Encode(auth.Response{Error: "invalid-support-report"})
			} else {
				_ = encoder.Encode(auth.Response{Error: "support-report-failed"})
			}
			return
		}
		_ = encoder.Encode(auth.Response{SupportReport: &report})
		return
	}
	if request.Operation == "host-config" {
		if request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" {
			_ = encoder.Encode(auth.Response{Error: "invalid-administrative-request"})
			return
		}
		operation, operationErr := parseHostConfigurationOperation(request.Hostname, request.Timezone, request.NTPEnabled, request.ExpectedFingerprint)
		if operationErr != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-host-configuration"})
			return
		}
		identity, ok := grants.adminIdentity(request.AdminToken)
		if !ok {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		configurationCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		current, readErr := backend.Read(configurationCtx)
		if readErr != nil {
			cancel()
			_ = encoder.Encode(auth.Response{Error: "host-config-unavailable"})
			return
		}
		if current.Fingerprint != operation.ExpectedFingerprint {
			cancel()
			_ = encoder.Encode(auth.Response{Error: "host-config-conflict"})
			return
		}
		desired := platform.NewHostConfiguration(operation.Hostname, operation.Timezone, operation.NTPEnabled)
		applyErr := backend.Apply(configurationCtx, current, desired)
		cancel()
		if applyErr != nil {
			_ = encoder.Encode(auth.Response{Error: "host-config-failed"})
			return
		}
		logger.Info("host configuration changed", zap.String("username", identity.Username))
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation == "power" {
		if request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.ExpectedFingerprint == "" {
			_ = encoder.Encode(auth.Response{Error: "invalid-power-request"})
			return
		}
		if err := parsePowerOperation(request.PowerAction, request.PowerConfirmation, request.ExpectedFingerprint); err != nil {
			_ = encoder.Encode(auth.Response{Error: err.Error()})
			return
		}
		identity, ok := grants.adminIdentity(request.AdminToken)
		if !ok {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		powerCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		status, readErr := power.Read(powerCtx)
		if readErr != nil {
			cancel()
			_ = encoder.Encode(auth.Response{Error: "power-unavailable"})
			return
		}
		if status.Fingerprint != request.ExpectedFingerprint {
			cancel()
			_ = encoder.Encode(auth.Response{Error: "power-conflict"})
			return
		}
		selected := status.Reboot
		if request.PowerAction == "shutdown" {
			selected = status.Shutdown
		}
		if !selected.Available {
			cancel()
			_ = encoder.Encode(auth.Response{Error: "power-" + selected.State})
			return
		}
		if err := power.Request(powerCtx, request.PowerAction); err != nil {
			cancel()
			_ = encoder.Encode(auth.Response{Error: "power-request-failed"})
			return
		}
		cancel()
		logger.Info("host power request", zap.String("username", identity.Username), zap.String("action", request.PowerAction))
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation != "authenticate" || request.Token != "" || request.AdminToken != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Username == "" || request.Password == "" {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	identity, closePAM, err := service.OpenSession(request.Username, request.Password)
	request.Password = ""
	if err != nil {
		logger.Warn("PAM authentication failed", zap.String("username", request.Username), zap.Error(err))
		_ = encoder.Encode(auth.Response{Error: "authentication-failed"})
		return
	}
	token, err := grants.add(identity, closePAM)
	if err != nil {
		closePAM()
		logger.Error("bridge grant creation failed", zap.String("username", identity.Username), zap.Error(err))
		_ = encoder.Encode(auth.Response{Error: "session-failed"})
		return
	}
	logger.Info("PAM authentication succeeded", zap.String("username", identity.Username), zap.Int("uid", identity.UID))
	_ = encoder.Encode(auth.Response{Identity: &identity, BridgeToken: token})
}

func hasHostConfigurationFields(request auth.Request) bool {
	return request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != ""
}

func hasPowerFields(request auth.Request) bool {
	return request.PowerAction != "" || request.PowerConfirmation != ""
}

func runServiceAction(backend serviceBackend, operation serviceOperation) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := backend.Run(ctx, operation); err != nil {
		if ctx.Err() != nil {
			return "service-job-timeout"
		}
		return "service-action-failed"
	}
	return ""
}

func (store *grantStore) resize(token string, columns, rows uint16) string {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || time.Now().After(grant.expires) {
		if ok {
			delete(store.values, token)
		}
		store.mu.Unlock()
		if ok {
			cleanupGrant(grant)
		}
		return "invalid-bridge-token"
	}
	if grant.terminal == nil {
		store.mu.Unlock()
		return "terminal-not-ready"
	}
	if columns == 0 || rows == 0 {
		store.mu.Unlock()
		return "invalid-terminal-size"
	}
	err := pty.Setsize(grant.terminal, &pty.Winsize{Cols: columns, Rows: rows})
	store.mu.Unlock()
	if err != nil {
		return "terminal-resize-failed"
	}
	return ""
}

func (store *grantStore) setTerminal(token string, terminal *os.File) bool {
	store.mu.Lock()
	grant, ok := store.values[token]
	if ok {
		grant.terminal = terminal
		store.values[token] = grant
	}
	store.mu.Unlock()
	return ok
}

func (store *grantStore) add(identity auth.Identity, closePAM ...func()) (string, error) {
	var closeSession func()
	if len(closePAM) > 0 {
		closeSession = closePAM[0]
	}
	return store.addGrant(identity, nil, closeSession)
}

func (store *grantStore) addGrant(identity auth.Identity, bridge io.Closer, closeSession func()) (string, error) {
	var once sync.Once
	closeResources := func() {
		once.Do(func() {
			if bridge != nil {
				_ = bridge.Close()
			}
			if closeSession != nil {
				closeSession()
			}
		})
	}
	token, err := randomToken(32)
	if err != nil {
		closeResources()
		return "", err
	}
	store.mu.Lock()
	grant := bridgeGrant{identity: identity, expires: time.Now().Add(30 * time.Second), bridge: bridge, closePAM: closeResources}
	grant.timer = time.AfterFunc(30*time.Second, func() { store.expire(token) })
	store.values[token] = grant
	store.mu.Unlock()
	return token, nil
}

func (store *grantStore) expire(token string) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok {
		store.mu.Unlock()
		return
	}
	remaining := time.Until(grant.expires)
	if remaining > 0 {
		grant.timer = time.AfterFunc(remaining, func() { store.expire(token) })
		store.values[token] = grant
		store.mu.Unlock()
		return
	}
	delete(store.values, token)
	store.mu.Unlock()
	cleanupGrant(grant)
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func (store *grantStore) get(token string) (auth.Identity, bool) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || time.Now().After(grant.expires) {
		delete(store.values, token)
		store.mu.Unlock()
		if ok {
			cleanupGrant(grant)
		}
		return auth.Identity{}, false
	}
	store.mu.Unlock()
	return grant.identity, true
}

func (store *grantStore) bridgeFor(token string) (io.Closer, bool) {
	store.mu.Lock()
	grant, ok := store.values[token]
	expired := ok && time.Now().After(grant.expires)
	if !ok || expired || grant.bridge == nil {
		store.mu.Unlock()
		if expired {
			store.close(token)
		}
		return nil, false
	}
	bridge := grant.bridge
	store.mu.Unlock()
	return bridge, true
}

func (store *grantStore) authorize(ctx context.Context, token, password string, ttl uint32, policy administrativePolicy) (string, time.Time, error) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || time.Now().After(grant.expires) {
		store.mu.Unlock()
		if ok {
			store.close(token)
		}
		return "", time.Time{}, errors.New("invalid-bridge-token")
	}
	identity := grant.identity
	store.mu.Unlock()
	if err := policy(ctx, identity, password); err != nil {
		return "", time.Time{}, err
	}
	adminToken, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, errAdministrativeUnavailable
	}
	duration := time.Duration(ttl) * time.Second
	if duration < time.Minute {
		duration = 5 * time.Minute
	}
	if duration > time.Hour {
		duration = time.Hour
	}
	until := time.Now().Add(duration)
	store.mu.Lock()
	grant, ok = store.values[token]
	if !ok || time.Now().After(grant.expires) {
		store.mu.Unlock()
		return "", time.Time{}, errors.New("invalid-bridge-token")
	}
	grant.adminToken = adminToken
	grant.adminExpires = until
	store.values[token] = grant
	store.mu.Unlock()
	return adminToken, until, nil
}

func (store *grantStore) adminIdentity(token string) (auth.Identity, bool) {
	if token == "" {
		return auth.Identity{}, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	now := time.Now()
	for _, grant := range store.values {
		if grant.adminToken == token && !grant.adminExpires.IsZero() && now.Before(grant.adminExpires) && now.Before(grant.expires) {
			return grant.identity, true
		}
	}
	return auth.Identity{}, false
}

func (store *grantStore) adminBridge(token string) (io.Closer, bool) {
	if token == "" {
		return nil, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	now := time.Now()
	for _, grant := range store.values {
		if grant.adminToken == token && !grant.adminExpires.IsZero() && now.Before(grant.adminExpires) && now.Before(grant.expires) && grant.bridge != nil {
			return grant.bridge, true
		}
	}
	return nil, false
}

func (store *grantStore) revokeAdmin(token string) bool {
	if token == "" {
		return false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for key, grant := range store.values {
		if grant.adminToken == token {
			grant.adminToken = ""
			grant.adminExpires = time.Time{}
			store.values[key] = grant
			return true
		}
	}
	return false
}

func (store *grantStore) claim(token string) (auth.Identity, bool) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || grant.active || time.Now().After(grant.expires) {
		expired := ok && time.Now().After(grant.expires)
		if expired {
			delete(store.values, token)
		}
		store.mu.Unlock()
		if expired {
			cleanupGrant(grant)
		}
		return auth.Identity{}, false
	}
	grant.active = true
	store.values[token] = grant
	store.mu.Unlock()
	return grant.identity, true
}

func (store *grantStore) closeAll() {
	store.mu.Lock()
	values := store.values
	store.values = make(map[string]bridgeGrant)
	store.mu.Unlock()
	for _, grant := range values {
		cleanupGrant(grant)
	}
}

func (store *grantStore) close(token string) bool {
	store.mu.Lock()
	grant, ok := store.values[token]
	if ok {
		delete(store.values, token)
	}
	store.mu.Unlock()
	if !ok {
		return false
	}
	cleanupGrant(grant)
	return true
}

func cleanupGrant(grant bridgeGrant) {
	if grant.timer != nil {
		grant.timer.Stop()
	}
	if grant.terminal != nil {
		_ = grant.terminal.Close()
	}
	if grant.closePAM != nil {
		grant.closePAM()
	}
}

func (store *grantStore) confirm(token string) bool {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || time.Now().After(grant.expires) {
		store.mu.Unlock()
		if ok {
			store.close(token)
		}
		return false
	}
	grant.expires = time.Now().Add(12 * time.Hour)
	store.values[token] = grant
	store.mu.Unlock()
	return true
}

func (store *grantStore) addUserSession(session auth.UserSession) (string, error) {
	userBridge, err := startUserBridge(session)
	if err != nil {
		return "", err
	}
	token, err := store.addGrant(session.Identity, userBridge, session.Close)
	if err != nil {
		return "", err
	}
	go func() {
		<-userBridge.exited
		store.close(token)
	}()
	return token, nil
}

func (store *grantStore) release(token string) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if ok {
		grant.active = false
		grant.terminal = nil
		store.values[token] = grant
	}
	store.mu.Unlock()
}

func decodeRequestLine(reader *bufio.Reader, request *auth.Request) error {
	const maxRequestBytes = 8 << 20
	var payload []byte
	for {
		part, err := reader.ReadSlice('\n')
		payload = append(payload, part...)
		if len(payload) > maxRequestBytes {
			return errors.New("request exceeds limit")
		}
		if err == nil {
			break
		}
		if errors.Is(err, io.EOF) {
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

func runTerminal(conn net.Conn, reader io.Reader, identity auth.Identity, columns, rows uint16, grants *grantStore, token string, logger *zap.Logger) {
	defer grants.release(token)
	account, err := user.LookupId(strconv.Itoa(identity.UID))
	if err != nil {
		logger.Error("terminal user lookup failed", zap.Int("uid", identity.UID), zap.Error(err))
		return
	}
	shell := "/bin/sh"
	if payload, readErr := os.ReadFile("/etc/passwd"); readErr == nil {
		for _, line := range strings.Split(string(payload), "\n") {
			fields := strings.Split(line, ":")
			if len(fields) == 7 && fields[2] == strconv.Itoa(identity.UID) {
				shell = fields[6]
				break
			}
		}
	}
	command := exec.Command(shell, "-l")
	command.Dir = account.HomeDir
	command.Env = []string{"HOME=" + account.HomeDir, "USER=" + account.Username, "LOGNAME=" + account.Username, "SHELL=" + shell, "TERM=xterm-256color", "PATH=/usr/local/sbin:/usr/local/bin:/usr/bin:/bin"}
	credential := &syscall.Credential{Uid: uint32(identity.UID), Gid: uint32(identity.GID)}
	if groupIDs, groupErr := account.GroupIds(); groupErr == nil {
		for _, groupID := range groupIDs {
			parsed, parseErr := strconv.ParseUint(groupID, 10, 32)
			if parseErr == nil {
				credential.Groups = append(credential.Groups, uint32(parsed))
			}
		}
	}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: credential, Setsid: true}
	if columns == 0 {
		columns = 120
	}
	if rows == 0 {
		rows = 30
	}
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Cols: columns, Rows: rows})
	if err != nil {
		logger.Error("terminal launch failed", zap.String("username", identity.Username), zap.Error(err))
		return
	}
	if !grants.setTerminal(token, terminal) {
		_ = terminal.Close()
		_ = command.Process.Kill()
		_ = command.Wait()
		return
	}
	defer terminal.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(terminal, reader); done <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, terminal); done <- struct{}{} }()
	<-done
	_ = terminal.Close()
	_ = command.Process.Kill()
	_ = command.Wait()
	logger.Info("terminal stopped", zap.String("username", identity.Username))
}
