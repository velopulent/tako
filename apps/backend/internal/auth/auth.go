package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/msteinert/pam/v2"
)

var (
	ErrAuthenticationFailed         = errors.New("authentication failed")
	ErrServiceUnavailable           = errors.New("authentication service unavailable")
	ErrPasswordInvalid              = errors.New("invalid password operation")
	ErrPasswordAuthenticationFailed = errors.New("password authentication failed")
	ErrPasswordPolicy               = errors.New("password policy rejected")
	ErrPasswordExpired              = errors.New("password expired")
	ErrPasswordUnavailable          = errors.New("password service unavailable")
)

type Identity struct {
	Username    string `json:"username"`
	Name        string `json:"name"`
	UID         int    `json:"uid"`
	GID         int    `json:"gid"`
	BridgeToken string `json:"-"`
	AdminToken  string `json:"-"`
}

type AdministrativeAccess struct {
	Token string
	Until time.Time
}

type PromptStyle string

const (
	PromptHidden PromptStyle = "hidden"
	PromptText   PromptStyle = "text"
	PromptInfo   PromptStyle = "info"
	PromptError  PromptStyle = "error"
)

type Prompt struct {
	ID      string      `json:"id"`
	Style   PromptStyle `json:"style"`
	Message string      `json:"message"`
}

type PromptResponse struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

type ConversationRequest struct {
	ConversationID string           `json:"conversationId,omitempty"`
	Username       string           `json:"username,omitempty"`
	Password       string           `json:"password,omitempty"`
	Responses      []PromptResponse `json:"responses,omitempty"`
	Cancel         bool             `json:"cancel,omitempty"`
	// RemoteAddress is the client IP the gateway attributes the sign-in to.
	// It becomes PAM_RHOST, so wtmp, lastlog, and audit name the source.
	RemoteAddress string `json:"remoteAddress,omitempty"`
}

func (request ConversationRequest) Valid() bool {
	if len(request.ConversationID) > 64 || len(request.Username) > 256 || len(request.Password) > 4096 || !validRemoteAddress(request.RemoteAddress) {
		return false
	}
	if request.Cancel {
		return request.ConversationID != "" && request.Username == "" && request.Password == "" && request.RemoteAddress == "" && len(request.Responses) == 0
	}
	if request.ConversationID == "" {
		return request.Username != "" && len(request.Responses) == 0
	}
	if request.Username != "" || request.Password != "" || request.RemoteAddress != "" || len(request.Responses) != 1 {
		return false
	}
	response := request.Responses[0]
	return response.ID != "" && len(response.ID) <= 64 && len(response.Value) <= 4096
}

type ConversationResponse struct {
	ConversationID string    `json:"conversationId,omitempty"`
	Prompts        []Prompt  `json:"prompts,omitempty"`
	Identity       *Identity `json:"identity,omitempty"`
	BridgeToken    string    `json:"bridgeToken,omitempty"`
	Error          string    `json:"error,omitempty"`
}

// validRemoteAddress accepts an empty value or an IP literal.
func validRemoteAddress(value string) bool {
	if len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') && (character < 'A' || character > 'F') && character != '.' && character != ':' {
			return false
		}
	}
	return true
}

type UserSession struct {
	Identity    Identity
	Environment map[string]string
	Close       func()
	// Bridge is the session's running user bridge when the opener started
	// one; Done closes when the session ends on its own.
	Bridge io.Closer
	Done   <-chan struct{}
}

type Conversation func(PromptStyle, string) (string, error)

// PasswordChangeOperation is deliberately wire-only. Secret fields are sent
// over the protected session socket and are cleared immediately after the PAM
// transaction; they are never logged or persisted
// by the gateway.
type PasswordChangeOperation struct {
	Action          string `json:"action"`
	Username        string `json:"username,omitempty"`
	CurrentPassword string `json:"currentPassword,omitempty"`
	NewPassword     string `json:"newPassword"`
	Confirmation    string `json:"confirmation"`
}

func (operation *PasswordChangeOperation) Clear() {
	if operation == nil {
		return
	}
	operation.Username = ""
	operation.CurrentPassword = ""
	operation.NewPassword = ""
	operation.Confirmation = ""
}

func ValidatePasswordChangeOperation(operation PasswordChangeOperation) error {
	if operation.Action != "change" && operation.Action != "reset" {
		return ErrPasswordInvalid
	}
	if operation.NewPassword == "" || operation.NewPassword != operation.Confirmation || len(operation.NewPassword) > 4096 || len(operation.Confirmation) > 4096 || len(operation.CurrentPassword) > 4096 || len(operation.Username) > 32 || strings.ContainsAny(operation.CurrentPassword+operation.NewPassword+operation.Confirmation, "\x00") {
		return ErrPasswordInvalid
	}
	if operation.Action == "change" {
		if operation.Username != "" || operation.CurrentPassword == "" {
			return ErrPasswordInvalid
		}
	} else if operation.Username == "" || operation.CurrentPassword != "" {
		return ErrPasswordInvalid
	}
	if !validPasswordUsername(operation.Username) && operation.Username != "" {
		return ErrPasswordInvalid
	}
	return nil
}

func validPasswordUsername(username string) bool {
	if username == "" || username == "." || username == ".." {
		return false
	}
	for index, character := range username {
		if index == 0 {
			if (character < 'a' || character > 'z') && character != '_' {
				return false
			}
			continue
		}
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' && character != '$' {
			return false
		}
	}
	return true
}

// Authenticator drives a PAM conversation that ends in an authenticated user
// session. Prompts are relayed to the browser one at a time.
type Authenticator interface {
	AdvanceConversation(context.Context, *ConversationRequest) (ConversationResponse, error)
	CancelConversation(context.Context, string) error
}

// SessionController confirms and closes the user session behind a login.
type SessionController interface {
	ConfirmUserSession(context.Context, string) error
	CloseUserSession(context.Context, string) error
}

// AdministrativeController grants and revokes time-limited administrative
// access for an authenticated user session.
type AdministrativeController interface {
	AuthorizeAdministrative(context.Context, string, string, time.Duration) (AdministrativeAccess, error)
	RevokeAdministrative(context.Context, string) error
}

type PAMAuthenticator struct {
	Service         string
	PasswordService string
}

func (auth PAMAuthenticator) passwordService() string {
	if auth.PasswordService != "" {
		return auth.PasswordService
	}
	return "passwd"
}

type passwordConversation struct {
	current string
	new     string
	index   int
}

func (conversation *passwordConversation) respond(style PromptStyle, _ string) (string, error) {
	switch style {
	case PromptHidden:
		if conversation.index >= 6 {
			return "", ErrPasswordInvalid
		}
		if conversation.current != "" && conversation.index == 0 {
			conversation.index++
			return conversation.current, nil
		}
		if conversation.new == "" {
			return "", ErrPasswordInvalid
		}
		conversation.index++
		return conversation.new, nil
	case PromptText:
		return "", nil
	case PromptInfo, PromptError:
		return "", nil
	default:
		return "", ErrPasswordInvalid
	}
}

func passwordError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, pam.ErrNewAuthtokReqd), errors.Is(err, pam.ErrAuthtokExpired), errors.Is(err, pam.ErrAcctExpired):
		return ErrPasswordExpired
	case errors.Is(err, pam.ErrAuth), errors.Is(err, pam.ErrPermDenied), errors.Is(err, pam.ErrUserUnknown), errors.Is(err, pam.ErrMaxtries):
		return ErrPasswordAuthenticationFailed
	case errors.Is(err, pam.ErrAuthtok), errors.Is(err, pam.ErrAuthtokRecovery), errors.Is(err, pam.ErrAuthtokLockBusy), errors.Is(err, pam.ErrAuthtokDisableAging), errors.Is(err, pam.ErrTryAgain), errors.Is(err, pam.ErrConv):
		return ErrPasswordPolicy
	default:
		return ErrPasswordUnavailable
	}
}

// ChangePassword authenticates the current password and asks the host PAM
// password stack to set a replacement. It intentionally uses PAM's
// conversation, never argv, environment, files, or a child process stdin.
func (auth PAMAuthenticator) ChangePassword(ctx context.Context, username, current, replacement string) error {
	if ctx == nil || ctx.Err() != nil || username == "" || current == "" || replacement == "" {
		return ErrPasswordInvalid
	}
	conversation := &passwordConversation{current: current, new: replacement}
	defer func() {
		conversation.current = ""
		conversation.new = ""
	}()
	transaction, err := pam.StartFunc(auth.passwordService(), username, func(style pam.Style, message string) (string, error) {
		switch style {
		case pam.PromptEchoOff:
			return conversation.respond(PromptHidden, message)
		case pam.PromptEchoOn:
			return conversation.respond(PromptText, message)
		case pam.TextInfo:
			return conversation.respond(PromptInfo, message)
		case pam.ErrorMsg:
			return conversation.respond(PromptError, message)
		default:
			return "", ErrPasswordInvalid
		}
	})
	if err != nil {
		return passwordError(err)
	}
	defer transaction.End()
	if err := transaction.Authenticate(0); err != nil {
		return passwordError(err)
	}
	flags := pam.Flags(0)
	if err := transaction.AcctMgmt(0); err != nil {
		if errors.Is(err, pam.ErrNewAuthtokReqd) || errors.Is(err, pam.ErrAuthtokExpired) {
			flags = pam.ChangeExpiredAuthtok
		} else {
			return passwordError(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return passwordError(transaction.ChangeAuthTok(flags))
}

// ResetPassword performs a root-owned PAM password reset. No old password is
// accepted, and the target is selected by the authenticated administrative
// grant in sessiond rather than by a gateway-supplied command line.
func (auth PAMAuthenticator) ResetPassword(ctx context.Context, username, replacement string) error {
	if ctx == nil || ctx.Err() != nil || username == "" || replacement == "" {
		return ErrPasswordInvalid
	}
	conversation := &passwordConversation{new: replacement}
	defer func() {
		conversation.current = ""
		conversation.new = ""
	}()
	transaction, err := pam.StartFunc(auth.passwordService(), username, func(style pam.Style, message string) (string, error) {
		switch style {
		case pam.PromptEchoOff:
			return conversation.respond(PromptHidden, message)
		case pam.PromptEchoOn:
			return conversation.respond(PromptText, message)
		case pam.TextInfo:
			return conversation.respond(PromptInfo, message)
		case pam.ErrorMsg:
			return conversation.respond(PromptError, message)
		default:
			return "", ErrPasswordInvalid
		}
	})
	if err != nil {
		return passwordError(err)
	}
	defer transaction.End()
	if err := ctx.Err(); err != nil {
		return err
	}
	return passwordError(transaction.ChangeAuthTok(pam.ChangeExpiredAuthtok))
}

// OpenSessionWithConversation authenticates username and opens a PAM session
// in the calling process. PAM modules such as pam_loginuid, pam_selinux, and
// pam_keyinit change the calling thread, so callers run this in a dedicated
// process on a locked OS thread and start the user's processes from it.
func (auth PAMAuthenticator) OpenSessionWithConversation(username, remoteHost string, conversation Conversation) (UserSession, error) {
	transaction, err := auth.authenticate(username, remoteHost, conversation)
	if err != nil {
		return UserSession{}, err
	}
	if err := transaction.SetCred(pam.EstablishCred); err != nil {
		_ = transaction.End()
		return UserSession{}, err
	}
	// pam_systemd registers a web session: logind lists it, and the class
	// starts the user's service manager like any other login.
	for _, variable := range []string{"XDG_SESSION_TYPE=web", "XDG_SESSION_CLASS=user"} {
		if err := transaction.PutEnv(variable); err != nil {
			_ = transaction.SetCred(pam.DeleteCred)
			_ = transaction.End()
			return UserSession{}, err
		}
	}
	if err := transaction.OpenSession(0); err != nil {
		_ = transaction.SetCred(pam.DeleteCred)
		_ = transaction.End()
		return UserSession{}, err
	}
	canonical, err := transaction.GetItem(pam.User)
	if err != nil {
		_ = transaction.CloseSession(0)
		_ = transaction.SetCred(pam.DeleteCred)
		_ = transaction.End()
		return UserSession{}, err
	}
	identity, err := lookupIdentity(canonical)
	if err != nil {
		_ = transaction.CloseSession(0)
		_ = transaction.SetCred(pam.DeleteCred)
		_ = transaction.End()
		return UserSession{}, err
	}
	environment, err := transaction.GetEnvList()
	if err != nil {
		environment = make(map[string]string)
	}
	var once sync.Once
	closeSession := func() {
		once.Do(func() {
			_ = transaction.CloseSession(0)
			_ = transaction.SetCred(pam.DeleteCred)
			_ = transaction.End()
		})
	}
	return UserSession{Identity: identity, Environment: environment, Close: closeSession}, nil
}

func (auth PAMAuthenticator) authenticate(username, remoteHost string, conversation Conversation) (*pam.Transaction, error) {
	if username == "" || conversation == nil {
		return nil, errors.New("missing credentials")
	}
	transaction, err := pam.StartFunc(auth.Service, username, func(style pam.Style, message string) (string, error) {
		switch style {
		case pam.PromptEchoOff:
			return conversation(PromptHidden, message)
		case pam.PromptEchoOn:
			return conversation(PromptText, message)
		case pam.TextInfo:
			return conversation(PromptInfo, message)
		case pam.ErrorMsg:
			return conversation(PromptError, message)
		}
		return "", errors.New("unsupported PAM conversation style")
	})
	if err != nil {
		return nil, err
	}
	if remoteHost != "" {
		if err := transaction.SetItem(pam.Rhost, remoteHost); err != nil {
			_ = transaction.End()
			return nil, err
		}
	}
	if err := transaction.Authenticate(0); err != nil {
		_ = transaction.End()
		return nil, err
	}
	if err := transaction.AcctMgmt(0); err != nil {
		_ = transaction.End()
		return nil, err
	}
	return transaction, nil
}

// SocketAuthenticator authenticates through the privileged session service.
type SocketAuthenticator struct{ Path string }

func (auth SocketAuthenticator) call(ctx context.Context, operation string, credentials Credentials, payload, result any) error {
	return Call(ctx, auth.Path, operation, credentials, payload, result, CallOptions{Timeout: time.Minute, Limit: 64 << 10})
}

func (auth SocketAuthenticator) ConfirmUserSession(ctx context.Context, token string) error {
	return auth.call(ctx, "session.confirm", Credentials{Token: token}, nil, nil)
}

func (auth SocketAuthenticator) CloseUserSession(ctx context.Context, token string) error {
	return auth.call(ctx, "session.close", Credentials{Token: token}, nil, nil)
}

func (auth SocketAuthenticator) AuthorizeAdministrative(ctx context.Context, bridgeToken, password string, ttl time.Duration) (AdministrativeAccess, error) {
	seconds := min(max(ttl/time.Second, 1), time.Hour/time.Second)
	payload := AdminAuthorization{Password: password, TTLSeconds: uint32(seconds)}
	var grant AdminGrant
	err := auth.call(ctx, "admin.authorize", Credentials{Token: bridgeToken}, &payload, &grant)
	payload.Password = ""
	if err != nil {
		return AdministrativeAccess{}, err
	}
	if grant.Token == "" || grant.Until.IsZero() {
		return AdministrativeAccess{}, ErrAuthenticationFailed
	}
	return AdministrativeAccess{Token: grant.Token, Until: grant.Until}, nil
}

func (auth SocketAuthenticator) RevokeAdministrative(ctx context.Context, token string) error {
	return auth.call(ctx, "admin.revoke", Credentials{AdminToken: token}, nil, nil)
}

func (auth SocketAuthenticator) AdvanceConversation(ctx context.Context, request *ConversationRequest) (ConversationResponse, error) {
	payload := *request
	request.Password = ""
	for index := range request.Responses {
		request.Responses[index].Value = ""
	}
	var response ConversationResponse
	err := auth.call(ctx, "session.conversation", Credentials{}, &payload, &response)
	payload.Password = ""
	return response, err
}

func (auth SocketAuthenticator) CancelConversation(ctx context.Context, id string) error {
	return auth.call(ctx, "session.cancel", Credentials{}, ConversationCancel{ConversationID: id}, nil)
}

// DevelopmentAuthenticator backs `tako serve --dev`, which only listens on
// loopback. It signs in the account running the gateway when the request
// carries the development password printed at startup.
type DevelopmentAuthenticator struct{ Password string }

func (authenticator DevelopmentAuthenticator) AdvanceConversation(_ context.Context, request *ConversationRequest) (ConversationResponse, error) {
	password := request.Password
	request.Password = ""
	current, err := user.Current()
	if err != nil {
		return ConversationResponse{}, err
	}
	if authenticator.Password == "" || subtle.ConstantTimeCompare([]byte(password), []byte(authenticator.Password)) != 1 {
		return ConversationResponse{}, ErrAuthenticationFailed
	}
	if request.Username != "" && request.Username != current.Username {
		return ConversationResponse{}, ErrAuthenticationFailed
	}
	identity, err := lookupIdentity(current.Username)
	if err != nil {
		return ConversationResponse{}, ErrAuthenticationFailed
	}
	// The local development broker ignores tokens; a fixed one lets the
	// gateway treat this as a full session with a terminal and files.
	return ConversationResponse{Identity: &identity, BridgeToken: "development"}, nil
}

func (DevelopmentAuthenticator) CancelConversation(context.Context, string) error { return nil }

// AuthorizeAdministrative checks the development password, so the UI's
// rejected-password path behaves as it does in production. Only the first line
// is compared; later lines carry answers to extra PAM prompts.
func (authenticator DevelopmentAuthenticator) AuthorizeAdministrative(_ context.Context, _ string, secret string, ttl time.Duration) (AdministrativeAccess, error) {
	password, _, _ := strings.Cut(secret, "\n")
	if authenticator.Password == "" || subtle.ConstantTimeCompare([]byte(password), []byte(authenticator.Password)) != 1 {
		return AdministrativeAccess{}, ErrAuthenticationFailed
	}
	token, err := opaqueToken(32)
	if err != nil {
		return AdministrativeAccess{}, err
	}
	return AdministrativeAccess{Token: token, Until: time.Now().Add(ttl)}, nil
}

func (DevelopmentAuthenticator) RevokeAdministrative(context.Context, string) error { return nil }

func opaqueToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func lookupIdentity(username string) (Identity, error) {
	account, err := user.Lookup(username)
	if err != nil {
		return Identity{}, err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return Identity{}, err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return Identity{}, err
	}
	name := account.Name
	if name == "" {
		name = account.Username
	}
	return Identity{Username: account.Username, Name: name, UID: uid, GID: gid}, nil
}
