package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/user"
	"strconv"
	"sync"
	"time"

	"github.com/msteinert/pam/v2"
)

var (
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrServiceUnavailable   = errors.New("authentication service unavailable")
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
}

func (request ConversationRequest) Valid() bool {
	if len(request.ConversationID) > 64 || len(request.Username) > 256 || len(request.Password) > 4096 {
		return false
	}
	if request.Cancel {
		return request.ConversationID != "" && request.Username == "" && request.Password == "" && len(request.Responses) == 0
	}
	if request.ConversationID == "" {
		return request.Username != "" && len(request.Responses) == 0
	}
	if request.Username != "" || request.Password != "" || len(request.Responses) != 1 {
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

type UserSession struct {
	Identity    Identity
	Environment map[string]string
	Close       func()
}

type Conversation func(PromptStyle, string) (string, error)

type Request struct {
	Operation           string           `json:"operation,omitempty"`
	Username            string           `json:"username,omitempty"`
	Password            string           `json:"password,omitempty"`
	ConversationID      string           `json:"conversationId,omitempty"`
	Responses           []PromptResponse `json:"responses,omitempty"`
	Token               string           `json:"token,omitempty"`
	AdminToken          string           `json:"adminToken,omitempty"`
	AdminTTL            uint32           `json:"adminTtlSeconds,omitempty"`
	Columns             uint16           `json:"columns,omitempty"`
	Rows                uint16           `json:"rows,omitempty"`
	Action              string           `json:"action,omitempty"`
	Unit                string           `json:"unit,omitempty"`
	Scope               string           `json:"scope,omitempty"`
	Hostname            string           `json:"hostname,omitempty"`
	Timezone            string           `json:"timezone,omitempty"`
	NTPEnabled          bool             `json:"ntpEnabled,omitempty"`
	ExpectedFingerprint string           `json:"expectedFingerprint,omitempty"`
}

type HostConfigurationRequest struct {
	AdminToken          string
	Hostname            string
	Timezone            string
	NTPEnabled          bool
	ExpectedFingerprint string
}

func ServiceAction(ctx context.Context, path, token, scope, unit, action string) error {
	return serviceActionRequest(ctx, path, Request{Operation: "service-action", Token: token, Scope: scope, Unit: unit, Action: action})
}

func ServiceActionAsAdmin(ctx context.Context, path, adminToken, scope, unit, action string) error {
	return serviceActionRequest(ctx, path, Request{Operation: "service-action", AdminToken: adminToken, Scope: scope, Unit: unit, Action: action})
}

func ApplyHostConfiguration(ctx context.Context, path string, request HostConfigurationRequest) error {
	return hostConfigurationRequest(ctx, path, Request{
		Operation:           "host-config",
		AdminToken:          request.AdminToken,
		Hostname:            request.Hostname,
		Timezone:            request.Timezone,
		NTPEnabled:          request.NTPEnabled,
		ExpectedFingerprint: request.ExpectedFingerprint,
	})
}

func serviceActionRequest(ctx context.Context, path string, request Request) error {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopCancelWatch := make(chan struct{})
	defer close(stopCancelWatch)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopCancelWatch:
		}
	}()
	deadline := time.Now().Add(15 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return err
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, 16<<10)).Decode(&response); err != nil {
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

func hostConfigurationRequest(ctx context.Context, path string, request Request) error {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return fmt.Errorf("%w: connect to %s: %v", ErrServiceUnavailable, path, err)
	}
	defer conn.Close()
	stopCancelWatch := make(chan struct{})
	defer close(stopCancelWatch)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopCancelWatch:
		}
	}()
	deadline := time.Now().Add(15 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return fmt.Errorf("%w: send host configuration request: %v", ErrServiceUnavailable, err)
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, 16<<10)).Decode(&response); err != nil {
		return fmt.Errorf("%w: read host configuration response: %v", ErrServiceUnavailable, err)
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

func bridgeTokenRequest(ctx context.Context, path, operation, token string) error {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(Request{Operation: operation, Token: token}); err != nil {
		return err
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, 16<<10)).Decode(&response); err != nil {
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

type Response struct {
	Identity       *Identity `json:"identity,omitempty"`
	BridgeToken    string    `json:"bridgeToken,omitempty"`
	ConversationID string    `json:"conversationId,omitempty"`
	Prompts        []Prompt  `json:"prompts,omitempty"`
	Error          string    `json:"error,omitempty"`
	AdminToken     string    `json:"adminToken,omitempty"`
	AdminUntil     time.Time `json:"adminUntil,omitempty"`
}

type Authenticator interface {
	Authenticate(context.Context, string, string) (Identity, error)
}

type ConversationAuthenticator interface {
	AdvanceConversation(context.Context, *ConversationRequest) (ConversationResponse, error)
	CancelConversation(context.Context, string) error
}

type SessionController interface {
	ConfirmUserSession(context.Context, string) error
	CloseUserSession(context.Context, string) error
}

type AdministrativeController interface {
	AuthorizeAdministrative(context.Context, string, string, time.Duration) (AdministrativeAccess, error)
	RevokeAdministrative(context.Context, string) error
}

type PAMAuthenticator struct{ Service string }

func (auth PAMAuthenticator) Authenticate(_ context.Context, username, password string) (Identity, error) {
	secret := password
	defer func() { secret = "" }()
	transaction, err := auth.authenticate(username, func(style PromptStyle, _ string) (string, error) {
		switch style {
		case PromptHidden:
			return secret, nil
		case PromptText:
			return username, nil
		default:
			return "", nil
		}
	})
	if err != nil {
		return Identity{}, err
	}
	defer transaction.End()
	canonical, err := transaction.GetItem(pam.User)
	if err != nil {
		return Identity{}, err
	}
	return lookupIdentity(canonical)
}

// OpenSession authenticates and keeps the PAM session/credentials alive until
// the returned close function is called by the privileged session service.
func (auth PAMAuthenticator) OpenSession(username, password string) (Identity, func(), error) {
	secret := password
	session, err := auth.OpenSessionWithConversation(username, func(style PromptStyle, _ string) (string, error) {
		switch style {
		case PromptHidden:
			return secret, nil
		case PromptText:
			return username, nil
		default:
			return "", nil
		}
	})
	secret = ""
	if err != nil {
		return Identity{}, nil, err
	}
	return session.Identity, session.Close, nil
}

func (auth PAMAuthenticator) OpenSessionWithConversation(username string, conversation Conversation) (UserSession, error) {
	transaction, err := auth.authenticate(username, conversation)
	if err != nil {
		return UserSession{}, err
	}
	if err := transaction.SetCred(pam.EstablishCred); err != nil {
		_ = transaction.End()
		return UserSession{}, err
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

func (auth PAMAuthenticator) authenticate(username string, conversation Conversation) (*pam.Transaction, error) {
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

type SocketAuthenticator struct{ Path string }

func (auth SocketAuthenticator) ConfirmUserSession(ctx context.Context, token string) error {
	return bridgeTokenRequest(ctx, auth.Path, "confirm-session", token)
}

func (auth SocketAuthenticator) CloseUserSession(ctx context.Context, token string) error {
	return bridgeTokenRequest(ctx, auth.Path, "close-session", token)
}

func (auth SocketAuthenticator) AuthorizeAdministrative(ctx context.Context, bridgeToken, password string, ttl time.Duration) (AdministrativeAccess, error) {
	seconds := ttl / time.Second
	if seconds < 1 {
		seconds = 1
	}
	if seconds > 24*time.Hour/time.Second {
		seconds = 24 * time.Hour / time.Second
	}
	request := &Request{
		Operation: "authorize-admin",
		Token:     bridgeToken,
		Password:  password,
		AdminTTL:  uint32(seconds),
	}
	response, err := auth.conversationRequest(ctx, request)
	request.Password = ""
	if err != nil {
		return AdministrativeAccess{}, err
	}
	if response.Error != "" {
		return AdministrativeAccess{}, errors.New(response.Error)
	}
	if response.AdminToken == "" || response.AdminUntil.IsZero() {
		return AdministrativeAccess{}, ErrAuthenticationFailed
	}
	return AdministrativeAccess{Token: response.AdminToken, Until: response.AdminUntil}, nil
}

func (auth SocketAuthenticator) RevokeAdministrative(ctx context.Context, token string) error {
	return adminTokenRequest(ctx, auth.Path, "revoke-admin", token)
}

func (auth SocketAuthenticator) AdvanceConversation(ctx context.Context, request *ConversationRequest) (ConversationResponse, error) {
	if request == nil {
		return ConversationResponse{}, errConversationRequest
	}
	wireRequest := &Request{
		Operation:      "conversation",
		Username:       request.Username,
		Password:       request.Password,
		ConversationID: request.ConversationID,
		Responses:      request.Responses,
	}
	request.Password = ""
	for index := range request.Responses {
		request.Responses[index].Value = ""
	}
	response, err := auth.conversationRequest(ctx, wireRequest)
	if err != nil {
		return ConversationResponse{}, err
	}
	return ConversationResponse{
		ConversationID: response.ConversationID,
		Prompts:        response.Prompts,
		Identity:       response.Identity,
		BridgeToken:    response.BridgeToken,
		Error:          response.Error,
	}, nil
}

func (auth SocketAuthenticator) CancelConversation(ctx context.Context, id string) error {
	response, err := auth.conversationRequest(ctx, &Request{Operation: "cancel-conversation", ConversationID: id})
	if err != nil {
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

var errConversationRequest = errors.New("invalid conversation request")

func (auth SocketAuthenticator) conversationRequest(ctx context.Context, request *Request) (Response, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", auth.Path)
	if err != nil {
		return Response{}, fmt.Errorf("%w: connect to %s: %v", ErrServiceUnavailable, auth.Path, err)
	}
	defer conn.Close()
	stopCancelWatch := make(chan struct{})
	defer close(stopCancelWatch)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopCancelWatch:
		}
	}()
	deadline := time.Now().Add(time.Minute)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return Response{}, fmt.Errorf("%w: send conversation request: %v", ErrServiceUnavailable, err)
	}
	request.Password = ""
	for index := range request.Responses {
		request.Responses[index].Value = ""
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, 16<<10)).Decode(&response); err != nil {
		return Response{}, fmt.Errorf("%w: read conversation response: %v", ErrServiceUnavailable, err)
	}
	return response, nil
}

func adminTokenRequest(ctx context.Context, path, operation, token string) error {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return fmt.Errorf("%w: connect to %s: %v", ErrServiceUnavailable, path, err)
	}
	defer conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(Request{Operation: operation, AdminToken: token}); err != nil {
		return fmt.Errorf("%w: send request: %v", ErrServiceUnavailable, err)
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, 16<<10)).Decode(&response); err != nil {
		return fmt.Errorf("%w: read response: %v", ErrServiceUnavailable, err)
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

func (auth SocketAuthenticator) Authenticate(ctx context.Context, username, password string) (Identity, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", auth.Path)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: connect to %s: %v", ErrServiceUnavailable, auth.Path, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := json.NewEncoder(conn).Encode(Request{Operation: "authenticate", Username: username, Password: password}); err != nil {
		return Identity{}, fmt.Errorf("%w: send request: %v", ErrServiceUnavailable, err)
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, 16<<10)).Decode(&response); err != nil {
		return Identity{}, fmt.Errorf("%w: read response: %v", ErrServiceUnavailable, err)
	}
	if response.Identity == nil {
		if response.Error == "authentication-failed" {
			return Identity{}, ErrAuthenticationFailed
		}
		if response.Error == "" {
			response.Error = "empty response"
		}
		return Identity{}, fmt.Errorf("%w: %s", ErrServiceUnavailable, response.Error)
	}
	response.Identity.BridgeToken = response.BridgeToken
	return *response.Identity, nil
}

func OpenTerminal(ctx context.Context, path, token string, columns, rows uint16) (net.Conn, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(Request{Operation: "terminal", Token: token, Columns: columns, Rows: rows}); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func ResizeTerminal(ctx context.Context, path, token string, columns, rows uint16) error {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(conn).Encode(Request{Operation: "resize-terminal", Token: token, Columns: columns, Rows: rows}); err != nil {
		return err
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, 16<<10)).Decode(&response); err != nil {
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

type DevelopmentAuthenticator struct{}

func (DevelopmentAuthenticator) Authenticate(_ context.Context, username, _ string) (Identity, error) {
	if username == "" {
		current, err := user.Current()
		if err != nil {
			return Identity{}, err
		}
		username = current.Username
	}
	return lookupIdentity(username)
}

func (DevelopmentAuthenticator) AuthorizeAdministrative(_ context.Context, _ string, _ string, ttl time.Duration) (AdministrativeAccess, error) {
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
