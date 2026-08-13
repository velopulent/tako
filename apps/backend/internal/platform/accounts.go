package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	maxAccountUsername = 32
	maxAccountName     = 256
	maxAccountHome     = 4096
	maxAccountShell    = 4096
	maxShadowOutput    = 16 << 10
)

var (
	ErrInvalidLocalAccountOperation = errors.New("invalid local account operation")
	ErrLocalAccountConflict         = errors.New("local account conflict")
	ErrLocalAccountNotFound         = errors.New("local account not found")
	ErrLocalAccountReadOnly         = errors.New("local account is read-only")
	ErrLocalAccountProtected        = errors.New("local account is protected")
	ErrLocalAccountUnavailable      = errors.New("local account service unavailable")
	ErrLocalAccountVerification     = errors.New("local account verification failed")
)

// LocalAccountOperation is the small, structured vocabulary accepted by the
// privileged account adapter. It contains no executable or shell fragments.
type LocalAccountOperation struct {
	Action              string `json:"action"`
	Username            string `json:"username"`
	Name                string `json:"name,omitempty"`
	Home                string `json:"home,omitempty"`
	Shell               string `json:"shell,omitempty"`
	ExpectedFingerprint string `json:"expectedFingerprint,omitempty"`
	Confirmation        string `json:"confirmation,omitempty"`
	Preview             bool   `json:"preview,omitempty"`
}

func (operation *LocalAccountOperation) UnmarshalJSON(payload []byte) error {
	type plain LocalAccountOperation
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var value plain
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing local account operation data")
	}
	*operation = LocalAccountOperation(value)
	return nil
}

type LocalAccountState struct {
	Username    string `json:"username"`
	Exists      bool   `json:"exists"`
	User        *User  `json:"user,omitempty"`
	Locked      bool   `json:"locked"`
	Fingerprint string `json:"fingerprint"`
	Source      string `json:"source,omitempty"`
}

type LocalAccountPreview struct {
	Action               string            `json:"action"`
	Username             string            `json:"username"`
	Current              LocalAccountState `json:"current"`
	Changes              []string          `json:"changes"`
	Warnings             []string          `json:"warnings"`
	Stale                bool              `json:"stale"`
	Allowed              bool              `json:"allowed"`
	Reason               string            `json:"reason,omitempty"`
	RequiresConfirmation bool              `json:"requiresConfirmation"`
}

type shadowCommandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

type execShadowCommandRunner struct{}

type boundedShadowOutput struct {
	data     []byte
	limit    int
	overflow bool
}

func (output *boundedShadowOutput) Write(payload []byte) (int, error) {
	if len(output.data) >= output.limit {
		output.overflow = true
		return len(payload), nil
	}
	remaining := output.limit - len(output.data)
	if len(payload) > remaining {
		output.data = append(output.data, payload[:remaining]...)
		output.overflow = true
		return len(payload), nil
	}
	output.data = append(output.data, payload...)
	return len(payload), nil
}

func (output *boundedShadowOutput) Len() int { return len(output.data) }

func (output *boundedShadowOutput) String() string { return string(output.data) }

func (execShadowCommandRunner) Run(ctx context.Context, name string, arguments ...string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrLocalAccountUnavailable, name, err)
	}
	command := exec.CommandContext(ctx, path, arguments...)
	stdout, stderr := &boundedShadowOutput{limit: maxShadowOutput}, &boundedShadowOutput{limit: maxShadowOutput}
	command.Stdout = stdout
	command.Stderr = stderr
	waitErr := command.Run()
	if stdout.overflow || stderr.overflow {
		return "", fmt.Errorf("%w: %s output exceeded limit", ErrLocalAccountUnavailable, name)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if waitErr != nil {
		return strings.TrimSpace(stderr.String()), waitErr
	}
	return strings.TrimSpace(stdout.String()), nil
}

func ValidateLocalAccountOperation(operation LocalAccountOperation) error {
	if !validAccountUsername(operation.Username) {
		return ErrInvalidLocalAccountOperation
	}
	switch operation.Action {
	case "create", "update", "lock", "unlock", "delete":
	default:
		return ErrInvalidLocalAccountOperation
	}
	if len(operation.Name) > maxAccountName || strings.ContainsAny(operation.Name, "\x00\r\n:") {
		return ErrInvalidLocalAccountOperation
	}
	if operation.Home != "" && !validAccountPath(operation.Home, maxAccountHome) {
		return ErrInvalidLocalAccountOperation
	}
	if operation.Shell != "" && !validAccountPath(operation.Shell, maxAccountShell) {
		return ErrInvalidLocalAccountOperation
	}
	if len(operation.ExpectedFingerprint) > 0 {
		if len(operation.ExpectedFingerprint) != sha256.Size*2 {
			return ErrInvalidLocalAccountOperation
		}
		if _, err := hex.DecodeString(operation.ExpectedFingerprint); err != nil {
			return ErrInvalidLocalAccountOperation
		}
	}
	if len(operation.Confirmation) > 64 || strings.ContainsAny(operation.Confirmation, "\x00\r\n") {
		return ErrInvalidLocalAccountOperation
	}
	switch operation.Action {
	case "create":
		if operation.ExpectedFingerprint != "" || operation.Confirmation != "" || operation.Preview && operation.ExpectedFingerprint != "" {
			return ErrInvalidLocalAccountOperation
		}
	case "update":
		if (!operation.Preview && operation.ExpectedFingerprint == "") || (operation.Name == "" && operation.Home == "" && operation.Shell == "") {
			return ErrInvalidLocalAccountOperation
		}
	case "lock", "unlock", "delete":
		if (!operation.Preview && operation.ExpectedFingerprint == "") || operation.Name != "" || operation.Home != "" || operation.Shell != "" {
			return ErrInvalidLocalAccountOperation
		}
	}
	if operation.Action == "delete" && !operation.Preview && operation.Confirmation != "DELETE "+operation.Username {
		return ErrInvalidLocalAccountOperation
	}
	if operation.Action == "lock" && !operation.Preview && operation.Confirmation != "LOCK "+operation.Username {
		return ErrInvalidLocalAccountOperation
	}
	return nil
}

func validAccountUsername(username string) bool {
	if username == "" || len(username) > maxAccountUsername || username == "." || username == ".." {
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

func validAccountPath(value string, limit int) bool {
	return len(value) <= limit && filepath.IsAbs(value) && filepath.Clean(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

func LocalAccountFingerprint(state LocalAccountState) string {
	hash := sha256.New()
	fields := []string{state.Username, fmt.Sprintf("%t", state.Exists), fmt.Sprintf("%t", state.Locked)}
	if state.User != nil {
		fields = append(fields, fmt.Sprintf("%d", state.User.UID), fmt.Sprintf("%d", state.User.GID), state.User.Name, state.User.Home, state.User.Shell, state.User.Source)
	}
	_, _ = hash.Write([]byte(strings.Join(fields, "\x00")))
	return hex.EncodeToString(hash.Sum(nil))
}

func PreviewLocalAccount(ctx context.Context, operation LocalAccountOperation, actor string) (LocalAccountPreview, error) {
	return previewLocalAccount(ctx, operation, actor, ListIdentityInventory, execShadowCommandRunner{})
}

func ApplyLocalAccount(ctx context.Context, operation LocalAccountOperation, actor string) (LocalAccountState, error) {
	return applyLocalAccount(ctx, operation, actor, ListIdentityInventory, execShadowCommandRunner{})
}

func previewLocalAccount(ctx context.Context, operation LocalAccountOperation, actor string, inventoryReader func(context.Context) (IdentityInventory, error), runner shadowCommandRunner) (LocalAccountPreview, error) {
	if err := ValidateLocalAccountOperation(operation); err != nil {
		return LocalAccountPreview{}, err
	}
	current, err := readLocalAccountState(ctx, operation.Username, inventoryReader, runner)
	if err != nil {
		return LocalAccountPreview{}, err
	}
	preview := LocalAccountPreview{
		Action:               operation.Action,
		Username:             operation.Username,
		Current:              current,
		Changes:              []string{},
		Warnings:             []string{},
		Allowed:              true,
		RequiresConfirmation: operation.Action == "delete" || operation.Action == "lock" || operation.Action == "unlock" || operation.Action == "create" || operation.Action == "update",
	}
	if current.Exists && current.Source != "local" {
		preview.Allowed = false
		preview.Reason = "Remote/NSS identities are read-only in Tako."
		return preview, nil
	}
	if operation.Action == "create" {
		if current.Exists {
			preview.Allowed = false
			preview.Stale = true
			preview.Reason = "An identity with this username already exists."
			return preview, nil
		}
		preview.Changes = append(preview.Changes, "create local user", "create home directory")
		preview.Warnings = append(preview.Warnings, "The new account starts without a password; set one through the terminal before login.")
		return preview, nil
	}
	if !current.Exists {
		preview.Allowed = false
		preview.Reason = ErrLocalAccountNotFound.Error()
		return preview, nil
	}
	if operation.ExpectedFingerprint != "" && current.Fingerprint != operation.ExpectedFingerprint {
		preview.Stale = true
		preview.Allowed = false
		preview.Reason = "The account changed; refresh before applying this operation."
		return preview, nil
	}
	if (operation.Action == "lock" || operation.Action == "delete") && actor == operation.Username {
		preview.Allowed = false
		preview.Reason = "The current operator cannot lock or delete their own account."
		return preview, nil
	}
	switch operation.Action {
	case "update":
		if operation.Name != "" && operation.Name != current.User.Name {
			preview.Changes = append(preview.Changes, "display name")
		}
		if operation.Home != "" && operation.Home != current.User.Home {
			preview.Changes = append(preview.Changes, "home directory path")
		}
		if operation.Shell != "" && operation.Shell != current.User.Shell {
			preview.Changes = append(preview.Changes, "login shell")
		}
	case "lock":
		if current.Locked {
			preview.Changes = append(preview.Changes, "account is already locked")
		} else {
			preview.Changes = append(preview.Changes, "lock password authentication")
		}
	case "unlock":
		if current.Locked {
			preview.Changes = append(preview.Changes, "unlock password authentication")
		} else {
			preview.Changes = append(preview.Changes, "account is already unlocked")
		}
	case "delete":
		preview.Changes = append(preview.Changes, "delete local user", "remove home directory")
		preview.Warnings = append(preview.Warnings, "This permanently removes the local account and its home directory.")
	}
	return preview, nil
}

func applyLocalAccount(ctx context.Context, operation LocalAccountOperation, actor string, inventoryReader func(context.Context) (IdentityInventory, error), runner shadowCommandRunner) (LocalAccountState, error) {
	if err := ValidateLocalAccountOperation(operation); err != nil {
		return LocalAccountState{}, err
	}
	if operation.Preview {
		return LocalAccountState{}, errors.New("preview must use PreviewLocalAccount")
	}
	current, err := readLocalAccountState(ctx, operation.Username, inventoryReader, runner)
	if err != nil {
		return LocalAccountState{}, err
	}
	if current.Exists && current.Source != "local" {
		return LocalAccountState{}, ErrLocalAccountReadOnly
	}
	if operation.Action == "create" {
		if current.Exists {
			return LocalAccountState{}, ErrLocalAccountConflict
		}
	} else {
		if !current.Exists {
			return LocalAccountState{}, ErrLocalAccountNotFound
		}
		if current.Fingerprint != operation.ExpectedFingerprint {
			return LocalAccountState{}, ErrLocalAccountConflict
		}
	}
	if (operation.Action == "lock" || operation.Action == "delete") && actor == operation.Username {
		return LocalAccountState{}, ErrLocalAccountProtected
	}
	arguments, err := localAccountArguments(operation, current)
	if err != nil {
		return LocalAccountState{}, err
	}
	commandName := arguments[0]
	if _, err := runner.Run(ctx, commandName, arguments[1:]...); err != nil {
		if ctx.Err() != nil {
			return LocalAccountState{}, ctx.Err()
		}
		return LocalAccountState{}, fmt.Errorf("%w: %v", ErrLocalAccountUnavailable, err)
	}
	updated, err := readLocalAccountState(ctx, operation.Username, inventoryReader, runner)
	if err != nil {
		return LocalAccountState{}, err
	}
	if !localAccountPostcondition(operation, updated) {
		return LocalAccountState{}, ErrLocalAccountVerification
	}
	return updated, nil
}

func readLocalAccountState(ctx context.Context, username string, inventoryReader func(context.Context) (IdentityInventory, error), runner shadowCommandRunner) (LocalAccountState, error) {
	inventory, err := inventoryReader(ctx)
	if err != nil {
		return LocalAccountState{}, fmt.Errorf("%w: read NSS inventory: %v", ErrLocalAccountUnavailable, err)
	}
	state := LocalAccountState{Username: username}
	for index := range inventory.Users {
		if inventory.Users[index].Username != username {
			continue
		}
		user := inventory.Users[index]
		state.Exists = true
		state.User = &user
		state.Source = user.Source
		break
	}
	if !state.Exists {
		state.Fingerprint = LocalAccountFingerprint(state)
		return state, nil
	}
	if state.Source != "local" {
		state.Fingerprint = LocalAccountFingerprint(state)
		return state, nil
	}
	status, err := runner.Run(ctx, "passwd", "-S", username)
	if err != nil {
		return LocalAccountState{}, fmt.Errorf("%w: read lock state: %v", ErrLocalAccountUnavailable, err)
	}
	state.Locked = shadowStatusLocked(status)
	state.Fingerprint = LocalAccountFingerprint(state)
	return state, nil
}

func shadowStatusLocked(status string) bool {
	fields := strings.Fields(status)
	return len(fields) >= 2 && strings.HasPrefix(strings.ToUpper(fields[1]), "L")
}

func localAccountArguments(operation LocalAccountOperation, current LocalAccountState) ([]string, error) {
	switch operation.Action {
	case "create":
		home := operation.Home
		if home == "" {
			home = "/home/" + operation.Username
		}
		shell := operation.Shell
		if shell == "" {
			shell = "/bin/bash"
		}
		name := operation.Name
		if name == "" {
			name = operation.Username
		}
		return []string{"useradd", "--create-home", "--home-dir", home, "--shell", shell, "--comment", name, "--", operation.Username}, nil
	case "update":
		arguments := []string{"usermod"}
		if operation.Name != "" {
			arguments = append(arguments, "--comment", operation.Name)
		}
		if operation.Home != "" {
			arguments = append(arguments, "--home-dir", operation.Home)
		}
		if operation.Shell != "" {
			arguments = append(arguments, "--shell", operation.Shell)
		}
		return append(arguments, "--", operation.Username), nil
	case "lock":
		return []string{"usermod", "--lock", "--", operation.Username}, nil
	case "unlock":
		return []string{"usermod", "--unlock", "--", operation.Username}, nil
	case "delete":
		return []string{"userdel", "--remove", "--", operation.Username}, nil
	default:
		return nil, ErrInvalidLocalAccountOperation
	}
}

func localAccountPostcondition(operation LocalAccountOperation, state LocalAccountState) bool {
	switch operation.Action {
	case "create":
		return state.Exists && state.Source == "local" && state.User != nil
	case "update":
		if !state.Exists || state.User == nil || state.Source != "local" {
			return false
		}
		if operation.Name != "" && state.User.Name != operation.Name {
			return false
		}
		if operation.Home != "" && state.User.Home != operation.Home {
			return false
		}
		if operation.Shell != "" && state.User.Shell != operation.Shell {
			return false
		}
		return true
	case "lock":
		return state.Exists && state.Locked
	case "unlock":
		return state.Exists && !state.Locked
	case "delete":
		return !state.Exists
	default:
		return false
	}
}
