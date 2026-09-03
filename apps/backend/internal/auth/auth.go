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
	"strings"
	"sync"
	"time"

	"github.com/msteinert/pam/v2"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/metrics"
	"github.com/velopulent/tako/internal/platform"
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
	Operation           string                                `json:"operation,omitempty"`
	Username            string                                `json:"username,omitempty"`
	Password            string                                `json:"password,omitempty"`
	ConversationID      string                                `json:"conversationId,omitempty"`
	Responses           []PromptResponse                      `json:"responses,omitempty"`
	Token               string                                `json:"token,omitempty"`
	AdminToken          string                                `json:"adminToken,omitempty"`
	AdminTTL            uint32                                `json:"adminTtlSeconds,omitempty"`
	Columns             uint16                                `json:"columns,omitempty"`
	Rows                uint16                                `json:"rows,omitempty"`
	Action              string                                `json:"action,omitempty"`
	Unit                string                                `json:"unit,omitempty"`
	Scope               string                                `json:"scope,omitempty"`
	Hostname            string                                `json:"hostname,omitempty"`
	Timezone            string                                `json:"timezone,omitempty"`
	NTPEnabled          bool                                  `json:"ntpEnabled,omitempty"`
	ExpectedFingerprint string                                `json:"expectedFingerprint,omitempty"`
	PowerAction         string                                `json:"powerAction,omitempty"`
	PowerConfirmation   string                                `json:"powerConfirmation,omitempty"`
	Timer               *platform.TimerOperation              `json:"timer,omitempty"`
	Override            *platform.OverrideOperation           `json:"override,omitempty"`
	Signal              *platform.SignalOperation             `json:"signal,omitempty"`
	Account             *platform.LocalAccountOperation       `json:"account,omitempty"`
	GroupMembership     *platform.GroupMembershipOperation    `json:"groupMembership,omitempty"`
	AdminRole           *platform.AdministrativeRoleOperation `json:"adminRole,omitempty"`
	PasswordChange      *PasswordChangeOperation              `json:"passwordChange,omitempty"`
	SSHKeys             *platform.SSHKeyOperation             `json:"sshKeys,omitempty"`
	Updates             *platform.UpdateOperation             `json:"updates,omitempty"`
	AutoUpdates         *platform.AutoUpdatesOperation        `json:"autoUpdates,omitempty"`
	Kpatch              *platform.KpatchOperation             `json:"kpatch,omitempty"`
	File                *platform.FileOperation               `json:"file,omitempty"`
	Journal             *platform.JournalQuery                `json:"journal,omitempty"`
	Network             *platform.NetworkOperation            `json:"network,omitempty"`
	Firewall            *platform.FirewallOperation           `json:"firewall,omitempty"`
	Security            *platform.SecurityOperation           `json:"security,omitempty"`
	SupportReport       *platform.SupportReportOperation      `json:"supportReport,omitempty"`
	ServiceAction       *platform.ServiceOperation            `json:"serviceAction,omitempty"`
	ServiceRead         *ServiceReadOperation                 `json:"serviceRead,omitempty"`
	HostInfoRead        *HostInfoReadOperation                `json:"hostInfoRead,omitempty"`
	HostConfigRead      *HostConfigReadOperation              `json:"hostConfigRead,omitempty"`
	CertificateRead     *CertificateReadOperation             `json:"certificateRead,omitempty"`
	PowerRead           *PowerReadOperation                   `json:"powerRead,omitempty"`
	ProcessRead         *ProcessReadOperation                 `json:"processRead,omitempty"`
	IdentityRead        *IdentityReadOperation                `json:"identityRead,omitempty"`
	StorageRead         *StorageReadOperation                 `json:"storageRead,omitempty"`
	NetworkRead         *NetworkReadOperation                 `json:"networkRead,omitempty"`
	UpdateRead          *UpdateReadOperation                  `json:"updateRead,omitempty"`
	UpdateRefresh       *UpdateRefreshOperation               `json:"updateRefresh,omitempty"`
	UpdateHistoryRead   *UpdateHistoryReadOperation           `json:"updateHistoryRead,omitempty"`
	UpdateLiveRead      *UpdateLiveReadOperation              `json:"updateLiveRead,omitempty"`
	UpdateCancel        *UpdateCancelOperation                `json:"updateCancel,omitempty"`
	AutoUpdatesRead     *AutoUpdatesReadOperation             `json:"autoUpdatesRead,omitempty"`
	KpatchRead          *KpatchReadOperation                  `json:"kpatchRead,omitempty"`
	CapabilitiesRead    *CapabilitiesReadOperation            `json:"capabilitiesRead,omitempty"`
	LoginHistoryRead    *LoginHistoryReadOperation            `json:"loginHistoryRead,omitempty"`
	SignalPreview       *platform.SignalOperation             `json:"signalPreview,omitempty"`
	SignalApply         *platform.SignalOperation             `json:"signalApply,omitempty"`
	LocalGroup          *platform.LocalGroupOperation         `json:"localGroup,omitempty"`
	MetricsHistory      *MetricsHistoryOperation              `json:"metricsHistory,omitempty"`
	MetricsFollow       *MetricsFollowOperation               `json:"metricsFollow,omitempty"`
}

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

type TimerRequest struct {
	Token      string
	AdminToken string
	Operation  platform.TimerOperation
}

type OverrideRequest struct {
	Token      string
	AdminToken string
	Operation  platform.OverrideOperation
}

type SignalRequest struct {
	Token      string
	AdminToken string
	Operation  platform.SignalOperation
}

type LocalAccountRequest struct {
	AdminToken string
	Operation  platform.LocalAccountOperation
}

type GroupMembershipRequest struct {
	AdminToken string
	Operation  platform.GroupMembershipOperation
}

type AdministrativeRoleRequest struct {
	AdminToken string
	Operation  platform.AdministrativeRoleOperation
}

type PasswordChangeRequest struct {
	Token      string
	AdminToken string
	Operation  PasswordChangeOperation
}

type SSHKeyRequest struct {
	Token          string
	AdminToken     string
	Administrative bool
	Operation      platform.SSHKeyOperation
}

type UpdateRequest struct {
	AdminToken string
	Operation  platform.UpdateOperation
}

type AutoUpdatesRequest struct {
	AdminToken string
	Operation  platform.AutoUpdatesOperation
}

type KpatchRequest struct {
	AdminToken string
	Operation  platform.KpatchOperation
}

type FileRequest struct {
	Token          string
	AdminToken     string
	Administrative bool
	Operation      platform.FileOperation
}

type JournalRequest struct {
	Token      string
	AdminToken string
	Query      platform.JournalQuery
}

type NetworkRequest struct {
	AdminToken string
	Operation  platform.NetworkOperation
}

type FirewallRequest struct {
	AdminToken string
	Operation  platform.FirewallOperation
}

type SecurityRequest struct {
	AdminToken string
	Operation  platform.SecurityOperation
}

type SupportReportRequest struct {
	AdminToken string
	Operation  platform.SupportReportOperation
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

type PowerRequest struct {
	AdminToken          string
	Action              string
	Confirmation        string
	ExpectedFingerprint string
}

func RequestPower(ctx context.Context, path string, request PowerRequest) error {
	return serviceActionRequest(ctx, path, Request{
		Operation:           "power",
		AdminToken:          request.AdminToken,
		PowerAction:         request.Action,
		PowerConfirmation:   request.Confirmation,
		ExpectedFingerprint: request.ExpectedFingerprint,
	})
}

func ApplyTimer(ctx context.Context, path string, request TimerRequest) (platform.TimerState, error) {
	response, err := socketRequest(ctx, path, Request{
		Operation:  "timer",
		Token:      request.Token,
		AdminToken: request.AdminToken,
		Timer:      &request.Operation,
	})
	if err != nil {
		return platform.TimerState{}, err
	}
	if response.Error != "" {
		return platform.TimerState{}, errors.New(response.Error)
	}
	if response.TimerState == nil {
		return platform.TimerState{}, ErrServiceUnavailable
	}
	return *response.TimerState, nil
}

func ApplyOverride(ctx context.Context, path string, request OverrideRequest) (platform.OverrideState, error) {
	response, err := socketRequest(ctx, path, Request{
		Operation:  "service-override",
		Token:      request.Token,
		AdminToken: request.AdminToken,
		Override:   &request.Operation,
	})
	if err != nil {
		return platform.OverrideState{}, err
	}
	if response.Error != "" {
		return platform.OverrideState{}, errors.New(response.Error)
	}
	if response.OverrideState == nil {
		return platform.OverrideState{}, ErrServiceUnavailable
	}
	return *response.OverrideState, nil
}

func SignalProcesses(ctx context.Context, path string, request SignalRequest) (platform.SignalResult, error) {
	response, err := socketRequest(ctx, path, Request{
		Operation:  "signal-process",
		Token:      request.Token,
		AdminToken: request.AdminToken,
		Signal:     &request.Operation,
	})
	if err != nil {
		return platform.SignalResult{}, err
	}
	if response.Error != "" {
		return platform.SignalResult{}, errors.New(response.Error)
	}
	if response.SignalResult == nil {
		return platform.SignalResult{}, ErrServiceUnavailable
	}
	return *response.SignalResult, nil
}

func PreviewLocalAccount(ctx context.Context, path string, request LocalAccountRequest) (platform.LocalAccountPreview, error) {
	operation := request.Operation
	operation.Preview = true
	response, err := socketRequest(ctx, path, Request{Operation: "local-account", AdminToken: request.AdminToken, Account: &operation})
	if err != nil {
		return platform.LocalAccountPreview{}, err
	}
	if response.Error != "" {
		return platform.LocalAccountPreview{}, errors.New(response.Error)
	}
	if response.AccountPreview == nil {
		return platform.LocalAccountPreview{}, ErrServiceUnavailable
	}
	return *response.AccountPreview, nil
}

func ApplyLocalAccount(ctx context.Context, path string, request LocalAccountRequest) (platform.LocalAccountState, error) {
	operation := request.Operation
	operation.Preview = false
	response, err := socketRequest(ctx, path, Request{Operation: "local-account", AdminToken: request.AdminToken, Account: &operation})
	if err != nil {
		return platform.LocalAccountState{}, err
	}
	if response.Error != "" {
		return platform.LocalAccountState{}, errors.New(response.Error)
	}
	if response.AccountState == nil {
		return platform.LocalAccountState{}, ErrServiceUnavailable
	}
	return *response.AccountState, nil
}

func PreviewGroupMembership(ctx context.Context, path string, request GroupMembershipRequest) (platform.GroupMembershipPreview, error) {
	operation := request.Operation
	operation.Preview = true
	response, err := socketRequest(ctx, path, Request{Operation: "group-membership", AdminToken: request.AdminToken, GroupMembership: &operation})
	if err != nil {
		return platform.GroupMembershipPreview{}, err
	}
	if response.Error != "" {
		return platform.GroupMembershipPreview{}, errors.New(response.Error)
	}
	if response.GroupMembershipPreview == nil {
		return platform.GroupMembershipPreview{}, ErrServiceUnavailable
	}
	return *response.GroupMembershipPreview, nil
}

func ApplyGroupMembership(ctx context.Context, path string, request GroupMembershipRequest) (platform.GroupMembershipState, error) {
	operation := request.Operation
	operation.Preview = false
	response, err := socketRequest(ctx, path, Request{Operation: "group-membership", AdminToken: request.AdminToken, GroupMembership: &operation})
	if err != nil {
		return platform.GroupMembershipState{}, err
	}
	if response.Error != "" {
		return platform.GroupMembershipState{}, errors.New(response.Error)
	}
	if response.GroupMembershipState == nil {
		return platform.GroupMembershipState{}, ErrServiceUnavailable
	}
	return *response.GroupMembershipState, nil
}

func PreviewAdministrativeRole(ctx context.Context, path string, request AdministrativeRoleRequest) (platform.AdministrativeRolePreview, error) {
	operation := request.Operation
	operation.Preview = true
	response, err := socketRequest(ctx, path, Request{Operation: "admin-role", AdminToken: request.AdminToken, AdminRole: &operation})
	if err != nil {
		return platform.AdministrativeRolePreview{}, err
	}
	if response.Error != "" {
		return platform.AdministrativeRolePreview{}, errors.New(response.Error)
	}
	if response.AdminRolePreview == nil {
		return platform.AdministrativeRolePreview{}, ErrServiceUnavailable
	}
	return *response.AdminRolePreview, nil
}

func ApplyAdministrativeRole(ctx context.Context, path string, request AdministrativeRoleRequest) (platform.AdministrativeRoleState, error) {
	operation := request.Operation
	operation.Preview = false
	response, err := socketRequest(ctx, path, Request{Operation: "admin-role", AdminToken: request.AdminToken, AdminRole: &operation})
	if err != nil {
		return platform.AdministrativeRoleState{}, err
	}
	if response.Error != "" {
		return platform.AdministrativeRoleState{}, errors.New(response.Error)
	}
	if response.AdminRoleState == nil {
		return platform.AdministrativeRoleState{}, ErrServiceUnavailable
	}
	return *response.AdminRoleState, nil
}

func ChangePassword(ctx context.Context, path string, request PasswordChangeRequest) error {
	operation := request.Operation
	defer operation.Clear()
	response, err := socketRequest(ctx, path, Request{
		Operation:      "password-change",
		Token:          request.Token,
		AdminToken:     request.AdminToken,
		PasswordChange: &operation,
	})
	if err != nil {
		return err
	}
	if response.Error != "" {
		switch response.Error {
		case "password-authentication-failed":
			return ErrPasswordAuthenticationFailed
		case "password-policy-failed":
			return ErrPasswordPolicy
		case "password-expired":
			return ErrPasswordExpired
		case "password-unavailable":
			return ErrPasswordUnavailable
		case "invalid-password-operation":
			return ErrPasswordInvalid
		default:
			return errors.New(response.Error)
		}
	}
	return nil
}

func PreviewSSHKeys(ctx context.Context, path string, request SSHKeyRequest) (platform.SSHKeyPreview, error) {
	operation := request.Operation
	operation.Preview = true
	response, err := socketRequest(ctx, path, Request{Operation: "ssh-keys", Token: request.Token, AdminToken: request.AdminToken, SSHKeys: &operation})
	if err != nil {
		return platform.SSHKeyPreview{}, err
	}
	if response.Error != "" {
		return platform.SSHKeyPreview{}, sshKeyResponseError(response.Error)
	}
	if response.SSHKeyPreview == nil {
		return platform.SSHKeyPreview{}, ErrServiceUnavailable
	}
	return *response.SSHKeyPreview, nil
}

func ApplySSHKeys(ctx context.Context, path string, request SSHKeyRequest) (platform.SSHKeyState, error) {
	operation := request.Operation
	operation.Preview = false
	response, err := socketRequest(ctx, path, Request{Operation: "ssh-keys", Token: request.Token, AdminToken: request.AdminToken, SSHKeys: &operation})
	if err != nil {
		return platform.SSHKeyState{}, err
	}
	if response.Error != "" {
		return platform.SSHKeyState{}, sshKeyResponseError(response.Error)
	}
	if response.SSHKeyState == nil {
		return platform.SSHKeyState{}, ErrServiceUnavailable
	}
	return *response.SSHKeyState, nil
}

func ApplyUpdates(ctx context.Context, path string, request UpdateRequest) (platform.UpdateResult, error) {
	operation := request.Operation
	response, err := socketRequestWithLimit(ctx, path, Request{Operation: "updates", AdminToken: request.AdminToken, Updates: &operation}, 30*time.Minute, 512<<10)
	if err != nil {
		return platform.UpdateResult{}, err
	}
	if response.Error != "" {
		switch response.Error {
		case "invalid-update-operation":
			return platform.UpdateResult{}, platform.ErrInvalidUpdateOperation
		case "update-conflict":
			return platform.UpdateResult{}, platform.ErrUpdateConflict
		case "update-locked":
			return platform.UpdateResult{}, platform.ErrUpdateLocked
		case "update-unavailable":
			return platform.UpdateResult{}, platform.ErrUpdateUnavailable
		case "update-verification-failed":
			return platform.UpdateResult{}, platform.ErrUpdateVerification
		case "update-apply-failed":
			return platform.UpdateResult{}, platform.ErrUpdateApply
		default:
			return platform.UpdateResult{}, errors.New(response.Error)
		}
	}
	if response.UpdateResult == nil {
		return platform.UpdateResult{}, ErrServiceUnavailable
	}
	return *response.UpdateResult, nil
}

// ApplyAutoUpdatesConfig mutates automatic-update configuration through
// sessiond; the gateway never edits package-manager configuration itself.
func ApplyAutoUpdatesConfig(ctx context.Context, path string, request AutoUpdatesRequest) (platform.AutoUpdatesConfig, error) {
	operation := request.Operation
	response, err := socketRequest(ctx, path, Request{Operation: "updates-auto", AdminToken: request.AdminToken, AutoUpdates: &operation})
	if err != nil {
		return platform.AutoUpdatesConfig{}, err
	}
	if response.Error != "" {
		switch response.Error {
		case "invalid-auto-updates-operation":
			return platform.AutoUpdatesConfig{}, platform.ErrInvalidAutoUpdatesOperation
		case "auto-updates-unavailable":
			return platform.AutoUpdatesConfig{}, platform.ErrAutoUpdatesUnavailable
		case "auto-updates-apply-failed":
			return platform.AutoUpdatesConfig{}, platform.ErrAutoUpdatesApply
		default:
			return platform.AutoUpdatesConfig{}, errors.New(response.Error)
		}
	}
	if response.AutoUpdatesConfig == nil {
		return platform.AutoUpdatesConfig{}, ErrServiceUnavailable
	}
	return *response.AutoUpdatesConfig, nil
}

// ApplyKpatchSettings turns kernel live patching on or off through sessiond.
func ApplyKpatchSettings(ctx context.Context, path string, request KpatchRequest) (platform.KpatchSettingsStatus, error) {
	operation := request.Operation
	response, err := socketRequest(ctx, path, Request{Operation: "kpatch", AdminToken: request.AdminToken, Kpatch: &operation})
	if err != nil {
		return platform.KpatchSettingsStatus{}, err
	}
	if response.Error != "" {
		switch response.Error {
		case "invalid-kpatch-operation":
			return platform.KpatchSettingsStatus{}, platform.ErrInvalidKpatchOperation
		case "kpatch-apply-failed":
			return platform.KpatchSettingsStatus{}, platform.ErrKpatchApply
		default:
			return platform.KpatchSettingsStatus{}, errors.New(response.Error)
		}
	}
	if response.KpatchSettings == nil {
		return platform.KpatchSettingsStatus{}, ErrServiceUnavailable
	}
	return *response.KpatchSettings, nil
}

func QueryJournal(ctx context.Context, path string, request JournalRequest) (platform.JournalPage, error) {
	query := request.Query
	response, err := socketRequestWithLimit(ctx, path, Request{
		Operation:  "journal-query",
		Token:      request.Token,
		AdminToken: request.AdminToken,
		Journal:    &query,
	}, 30*time.Second, 8<<20)
	if err != nil {
		return platform.JournalPage{}, err
	}
	if response.Error != "" {
		return platform.JournalPage{}, journalResponseError(response.Error)
	}
	if response.JournalPage == nil {
		return platform.JournalPage{}, ErrServiceUnavailable
	}
	if response.JournalPage.Items == nil {
		response.JournalPage.Items = []platform.LogEntry{}
	}
	return *response.JournalPage, nil
}

func FollowJournal(ctx context.Context, path string, request JournalRequest, emit func(platform.LogEntry) error) error {
	query := request.Query
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
	_ = conn.SetDeadline(time.Time{})
	if err := json.NewEncoder(conn).Encode(Request{
		Operation:  "journal-follow",
		Token:      request.Token,
		AdminToken: request.AdminToken,
		Journal:    &query,
	}); err != nil {
		return fmt.Errorf("%w: send request: %v", ErrServiceUnavailable, err)
	}
	decoder := json.NewDecoder(conn)
	for {
		var response Response
		if err := decoder.Decode(&response); err != nil {
			if ctx.Err() != nil || err == io.EOF {
				return nil
			}
			return fmt.Errorf("%w: read response: %v", ErrServiceUnavailable, err)
		}
		if response.Error != "" {
			return journalResponseError(response.Error)
		}
		if response.LogEntry == nil {
			continue
		}
		if err := emit(*response.LogEntry); err != nil {
			return nil
		}
	}
}

func journalResponseError(code string) error {
	switch code {
	case "invalid-journal-query":
		return platform.ErrInvalidJournalQuery
	case "invalid-bridge-token":
		return errors.New("invalid-bridge-token")
	case "invalid-admin-token":
		return errors.New("invalid-admin-token")
	default:
		return errors.New(code)
	}
}

func ApplyFileOperation(ctx context.Context, path string, request FileRequest) (platform.FileResult, error) {
	operation := request.Operation
	response, err := socketRequestWithLimit(ctx, path, Request{
		Operation:  "file",
		Token:      request.Token,
		AdminToken: request.AdminToken,
		File:       &operation,
	}, 2*time.Minute, 8<<20)
	if err != nil {
		return platform.FileResult{}, err
	}
	if response.Error != "" {
		return platform.FileResult{}, fileResponseError(response.Error)
	}
	if response.FileResult == nil {
		return platform.FileResult{}, ErrServiceUnavailable
	}
	return *response.FileResult, nil
}

func PreviewNetwork(ctx context.Context, path string, request NetworkRequest) (platform.NetworkState, error) {
	operation := request.Operation
	operation.Action = "preview"
	response, err := socketRequest(ctx, path, Request{Operation: "network", AdminToken: request.AdminToken, Network: &operation})
	if err != nil {
		return platform.NetworkState{}, err
	}
	if response.Error != "" {
		return platform.NetworkState{}, networkResponseError(response.Error)
	}
	if response.NetworkState == nil {
		return platform.NetworkState{}, ErrServiceUnavailable
	}
	return *response.NetworkState, nil
}

func ApplyNetwork(ctx context.Context, path string, request NetworkRequest) (platform.NetworkState, error) {
	response, err := socketRequestWithLimit(ctx, path, Request{Operation: "network", AdminToken: request.AdminToken, Network: &request.Operation}, 2*time.Minute, 512<<10)
	if err != nil {
		return platform.NetworkState{}, err
	}
	if response.Error != "" {
		return platform.NetworkState{}, networkResponseError(response.Error)
	}
	if response.NetworkState == nil {
		return platform.NetworkState{}, ErrServiceUnavailable
	}
	return *response.NetworkState, nil
}

func PreviewFirewall(ctx context.Context, path string, request FirewallRequest) (platform.FirewallState, error) {
	operation := request.Operation
	operation.Action = "preview"
	response, err := socketRequest(ctx, path, Request{Operation: "firewall", AdminToken: request.AdminToken, Firewall: &operation})
	if err != nil {
		return platform.FirewallState{}, err
	}
	if response.Error != "" {
		return platform.FirewallState{}, firewallResponseError(response.Error)
	}
	if response.FirewallState == nil {
		return platform.FirewallState{}, ErrServiceUnavailable
	}
	return *response.FirewallState, nil
}

func ApplyFirewall(ctx context.Context, path string, request FirewallRequest) (platform.FirewallState, error) {
	response, err := socketRequestWithLimit(ctx, path, Request{Operation: "firewall", AdminToken: request.AdminToken, Firewall: &request.Operation}, 2*time.Minute, 512<<10)
	if err != nil {
		return platform.FirewallState{}, err
	}
	if response.Error != "" {
		return platform.FirewallState{}, firewallResponseError(response.Error)
	}
	if response.FirewallState == nil {
		return platform.FirewallState{}, ErrServiceUnavailable
	}
	return *response.FirewallState, nil
}

func PreviewSecurity(ctx context.Context, path string, request SecurityRequest) (platform.SecurityStatus, error) {
	operation := request.Operation
	operation.Action = "inspect"
	response, err := socketRequestWithLimit(ctx, path, Request{Operation: "security", AdminToken: request.AdminToken, Security: &operation}, 30*time.Second, 512<<10)
	if err != nil {
		return platform.SecurityStatus{}, err
	}
	if response.Error != "" {
		return platform.SecurityStatus{}, securityResponseError(response.Error)
	}
	if response.SecurityStatus == nil {
		return platform.SecurityStatus{}, ErrServiceUnavailable
	}
	return *response.SecurityStatus, nil
}

func ApplySecurity(ctx context.Context, path string, request SecurityRequest) (platform.SecurityStatus, error) {
	response, err := socketRequestWithLimit(ctx, path, Request{Operation: "security", AdminToken: request.AdminToken, Security: &request.Operation}, 2*time.Minute, 512<<10)
	if err != nil {
		return platform.SecurityStatus{}, err
	}
	if response.Error != "" {
		return platform.SecurityStatus{}, securityResponseError(response.Error)
	}
	if response.SecurityStatus == nil {
		return platform.SecurityStatus{}, ErrServiceUnavailable
	}
	return *response.SecurityStatus, nil
}

func CollectSupportReport(ctx context.Context, path string, request SupportReportRequest) (platform.SupportReport, error) {
	response, err := socketRequestWithLimit(ctx, path, Request{Operation: "support-report", AdminToken: request.AdminToken, SupportReport: &request.Operation}, 5*time.Minute, 64<<10)
	if err != nil {
		return platform.SupportReport{}, err
	}
	if response.Error != "" {
		if response.Error == "invalid-support-report" {
			return platform.SupportReport{}, platform.ErrInvalidSupportReport
		}
		return platform.SupportReport{}, errors.New(response.Error)
	}
	if response.SupportReport == nil {
		return platform.SupportReport{}, ErrServiceUnavailable
	}
	return *response.SupportReport, nil
}

func securityResponseError(code string) error {
	switch code {
	case "invalid-security-operation":
		return platform.ErrInvalidSecurityOperation
	case "security-conflict":
		return platform.ErrSecurityConflict
	case "security-unsafe":
		return platform.ErrSecurityUnsafe
	case "security-unavailable":
		return platform.ErrSecurityUnavailable
	default:
		return errors.New(code)
	}
}

func firewallResponseError(code string) error {
	switch code {
	case "invalid-firewall-operation":
		return platform.ErrInvalidFirewallOperation
	case "firewall-conflict":
		return platform.ErrFirewallConflict
	case "firewall-ownership-conflict":
		return platform.ErrFirewallOwnership
	case "firewall-access-risk":
		return platform.ErrFirewallAccessRisk
	case "firewall-unavailable":
		return platform.ErrFirewallUnavailable
	default:
		return errors.New(code)
	}
}

func networkResponseError(code string) error {
	switch code {
	case "invalid-network-operation":
		return platform.ErrInvalidNetworkOperation
	case "network-conflict":
		return platform.ErrNetworkConflict
	case "network-ownership-conflict":
		return platform.ErrNetworkOwnership
	case "network-unavailable":
		return platform.ErrNetworkUnavailable
	default:
		return errors.New(code)
	}
}

func fileResponseError(code string) error {
	switch code {
	case "invalid-file-operation":
		return platform.ErrInvalidFileOperation
	case "file-not-found":
		return platform.ErrFileNotFound
	case "file-permission-denied":
		return platform.ErrFilePermission
	case "file-conflict":
		return platform.ErrFileConflict
	case "file-too-large":
		return platform.ErrFileTooLarge
	case "unsafe-archive":
		return platform.ErrUnsafeArchive
	case "archive-limit":
		return platform.ErrArchiveLimit
	default:
		return errors.New(code)
	}
}

func sshKeyResponseError(code string) error {
	switch code {
	case "invalid-ssh-key-operation":
		return platform.ErrInvalidSSHKeyOperation
	case "ssh-key-conflict":
		return platform.ErrSSHKeyConflict
	case "ssh-key-not-found":
		return platform.ErrSSHKeyNotFound
	case "ssh-key-read-only":
		return platform.ErrSSHKeyReadOnly
	case "ssh-key-unauthorized":
		return platform.ErrSSHKeyUnauthorized
	case "ssh-key-protected":
		return platform.ErrSSHKeyProtected
	case "ssh-key-verification-failed":
		return platform.ErrSSHKeyVerification
	case "ssh-key-unavailable":
		return platform.ErrSSHKeyUnavailable
	default:
		return errors.New(code)
	}
}

func socketRequest(ctx context.Context, path string, request Request) (Response, error) {
	return socketRequestWithLimit(ctx, path, request, 20*time.Second, 16<<10)
}

func socketRequestWithLimit(ctx context.Context, path string, request Request, maximum time.Duration, responseLimit int64) (Response, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return Response{}, fmt.Errorf("%w: connect to %s: %v", ErrServiceUnavailable, path, err)
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
	deadline := time.Now().Add(maximum)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return Response{}, fmt.Errorf("%w: send request: %v", ErrServiceUnavailable, err)
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, responseLimit)).Decode(&response); err != nil {
		return Response{}, fmt.Errorf("%w: read response: %v", ErrServiceUnavailable, err)
	}
	return response, nil
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
	Identity               *Identity                           `json:"identity,omitempty"`
	BridgeToken            string                              `json:"bridgeToken,omitempty"`
	ConversationID         string                              `json:"conversationId,omitempty"`
	Prompts                []Prompt                            `json:"prompts,omitempty"`
	Error                  string                              `json:"error,omitempty"`
	AdminToken             string                              `json:"adminToken,omitempty"`
	AdminUntil             time.Time                           `json:"adminUntil,omitempty"`
	TimerState             *platform.TimerState                `json:"timerState,omitempty"`
	OverrideState          *platform.OverrideState             `json:"overrideState,omitempty"`
	SignalResult           *platform.SignalResult              `json:"signalResult,omitempty"`
	AccountState           *platform.LocalAccountState         `json:"accountState,omitempty"`
	AccountPreview         *platform.LocalAccountPreview       `json:"accountPreview,omitempty"`
	GroupMembershipState   *platform.GroupMembershipState      `json:"groupMembershipState,omitempty"`
	GroupMembershipPreview *platform.GroupMembershipPreview    `json:"groupMembershipPreview,omitempty"`
	AdminRoleState         *platform.AdministrativeRoleState   `json:"adminRoleState,omitempty"`
	AdminRolePreview       *platform.AdministrativeRolePreview `json:"adminRolePreview,omitempty"`
	SSHKeyState            *platform.SSHKeyState               `json:"sshKeyState,omitempty"`
	SSHKeyPreview          *platform.SSHKeyPreview             `json:"sshKeyPreview,omitempty"`
	UpdateResult           *platform.UpdateResult              `json:"updateResult,omitempty"`
	AutoUpdatesConfig      *platform.AutoUpdatesConfig         `json:"autoUpdatesConfig,omitempty"`
	KpatchSettings         *platform.KpatchSettingsStatus      `json:"kpatchSettings,omitempty"`
	FileResult             *platform.FileResult                `json:"fileResult,omitempty"`
	JournalPage            *platform.JournalPage               `json:"journalPage,omitempty"`
	LogEntry               *platform.LogEntry                  `json:"logEntry,omitempty"`
	NetworkState           *platform.NetworkState              `json:"networkState,omitempty"`
	FirewallState          *platform.FirewallState             `json:"firewallState,omitempty"`
	SecurityStatus         *platform.SecurityStatus            `json:"securityStatus,omitempty"`
	SupportReport          *platform.SupportReport             `json:"supportReport,omitempty"`
	Units                  []platform.Unit                     `json:"units,omitempty"`
	UnitDetail             *platform.UnitDetail                `json:"unitDetail,omitempty"`
	UnitConfiguration      *platform.UnitConfiguration         `json:"unitConfiguration,omitempty"`
	HostInfo               *host.Info                          `json:"hostInfo,omitempty"`
	HostConfiguration      *platform.HostConfiguration         `json:"hostConfiguration,omitempty"`
	CertificateStatus      *platform.CertificateStatus         `json:"certificateStatus,omitempty"`
	PowerStatus            *platform.PowerStatus               `json:"powerStatus,omitempty"`
	Processes              []platform.Process                  `json:"processes,omitempty"`
	ProcessDetails         *platform.ProcessDetails            `json:"processDetails,omitempty"`
	SignalPreview          *platform.SignalPreview             `json:"signalPreview,omitempty"`
	IdentityInventory      *platform.IdentityInventory         `json:"identityInventory,omitempty"`
	Filesystems            []platform.Filesystem               `json:"filesystems,omitempty"`
	NetworkSnapshot        *platform.NetworkSnapshot           `json:"networkSnapshot,omitempty"`
	UpdateStatus           *platform.UpdateStatus              `json:"updateStatus,omitempty"`
	UpdateHistory          []platform.UpdateHistoryEntry       `json:"updateHistory,omitempty"`
	UpdateObservation      *UpdateObservation                  `json:"updateObservation,omitempty"`
	UpdateCanceled         *bool                               `json:"updateCanceled,omitempty"`
	KpatchStatus           *platform.KpatchStatus              `json:"kpatchStatus,omitempty"`
	Capabilities           []platform.Capability               `json:"capabilities,omitempty"`
	LoginHistoryPage       *platform.LoginHistoryPage          `json:"loginHistoryPage,omitempty"`
	LocalGroupPreview      *platform.LocalGroupPreview         `json:"localGroupPreview,omitempty"`
	LocalGroupState        *platform.LocalGroupState           `json:"localGroupState,omitempty"`
	MetricSamples          []metrics.Sample                    `json:"metricSamples,omitempty"`
	MetricSample           *metrics.Sample                     `json:"metricSample,omitempty"`
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
