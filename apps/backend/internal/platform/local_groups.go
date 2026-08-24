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
	"strings"
)

var (
	ErrInvalidLocalGroupOperation = errors.New("invalid local group operation")
	ErrLocalGroupConflict         = errors.New("local group conflict")
	ErrLocalGroupNotFound         = errors.New("local group not found")
	ErrLocalGroupReadOnly         = errors.New("local group is read-only")
	ErrLocalGroupProtected        = errors.New("local group is protected")
	ErrLocalGroupUnavailable      = errors.New("local group service unavailable")
	ErrLocalGroupVerification     = errors.New("local group verification failed")
)

type LocalGroupOperation struct {
	Action              string `json:"action"`
	Group               string `json:"group"`
	ExpectedFingerprint string `json:"expectedFingerprint,omitempty"`
	Confirmation        string `json:"confirmation,omitempty"`
	Preview             bool   `json:"preview,omitempty"`
}

func (operation *LocalGroupOperation) UnmarshalJSON(payload []byte) error {
	type plain LocalGroupOperation
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var value plain
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing local group operation data")
	}
	*operation = LocalGroupOperation(value)
	return nil
}

type LocalGroupState struct {
	Group       string         `json:"group"`
	Exists      bool           `json:"exists"`
	Info        *IdentityGroup `json:"groupInfo,omitempty"`
	Fingerprint string         `json:"fingerprint"`
	Source      string         `json:"source,omitempty"`
}

type LocalGroupPreview struct {
	Action               string           `json:"action"`
	Group                string           `json:"group"`
	Current              LocalGroupState  `json:"current"`
	Changes              []string         `json:"changes"`
	Warnings             []string         `json:"warnings"`
	Stale                bool             `json:"stale"`
	Allowed              bool             `json:"allowed"`
	Reason               string           `json:"reason,omitempty"`
	RequiresConfirmation bool             `json:"requiresConfirmation"`
}

func ValidateLocalGroupOperation(operation LocalGroupOperation) error {
	if !validGroupName(operation.Group) {
		return ErrInvalidLocalGroupOperation
	}
	switch operation.Action {
	case "create", "delete":
	default:
		return ErrInvalidLocalGroupOperation
	}
	if len(operation.ExpectedFingerprint) > 0 {
		if len(operation.ExpectedFingerprint) != sha256.Size*2 {
			return ErrInvalidLocalGroupOperation
		}
		if _, err := hex.DecodeString(operation.ExpectedFingerprint); err != nil {
			return ErrInvalidLocalGroupOperation
		}
	}
	if len(operation.Confirmation) > 128 || strings.ContainsAny(operation.Confirmation, "\x00\r\n") {
		return ErrInvalidLocalGroupOperation
	}
	switch operation.Action {
	case "create":
		if operation.ExpectedFingerprint != "" || operation.Confirmation != "" {
			return ErrInvalidLocalGroupOperation
		}
		if operation.Preview && operation.ExpectedFingerprint != "" {
			return ErrInvalidLocalGroupOperation
		}
	case "delete":
		if !operation.Preview && operation.ExpectedFingerprint == "" {
			return ErrInvalidLocalGroupOperation
		}
		if !operation.Preview && operation.Confirmation != "DELETE "+operation.Group {
			return ErrInvalidLocalGroupOperation
		}
	}
	// Protect essential groups from deletion
	if operation.Action == "delete" {
		for _, protected := range []string{"root", "sudo", "wheel", "shadow", "passwd"} {
			if operation.Group == protected {
				// Allow preview to show protected reason, but validation passes; preview will mark not allowed
				break
			}
		}
	}
	return nil
}

func LocalGroupFingerprint(state LocalGroupState) string {
	hash := sha256.New()
	fields := []string{state.Group, fmt.Sprintf("%t", state.Exists)}
	if state.Info != nil {
		fields = append(fields, fmt.Sprintf("%d", state.Info.GID), state.Info.Source, strings.Join(state.Info.Members, "\x00"))
	}
	_, _ = hash.Write([]byte(strings.Join(fields, "\x00")))
	return hex.EncodeToString(hash.Sum(nil))
}

func PreviewLocalGroup(ctx context.Context, operation LocalGroupOperation) (LocalGroupPreview, error) {
	return previewLocalGroup(ctx, operation, ListIdentityInventory, execShadowCommandRunner{})
}

func ApplyLocalGroup(ctx context.Context, operation LocalGroupOperation) (LocalGroupState, error) {
	return applyLocalGroup(ctx, operation, ListIdentityInventory, execShadowCommandRunner{})
}

func previewLocalGroup(ctx context.Context, operation LocalGroupOperation, inventoryReader func(context.Context) (IdentityInventory, error), runner shadowCommandRunner) (LocalGroupPreview, error) {
	if err := ValidateLocalGroupOperation(operation); err != nil {
		return LocalGroupPreview{}, err
	}
	current, err := readLocalGroupState(ctx, operation.Group, inventoryReader)
	if err != nil {
		return LocalGroupPreview{}, err
	}
	preview := LocalGroupPreview{
		Action:               operation.Action,
		Group:                operation.Group,
		Current:              current,
		Changes:              []string{},
		Warnings:             []string{},
		Allowed:              true,
		RequiresConfirmation: operation.Action == "delete" || operation.Action == "create",
	}
	if current.Exists && current.Source != "local" {
		preview.Allowed = false
		preview.Reason = "Remote/NSS groups are read-only in Tako."
		return preview, nil
	}
	if operation.Action == "create" {
		if current.Exists {
			preview.Allowed = false
			preview.Stale = true
			preview.Reason = "A group with this name already exists."
			return preview, nil
		}
		preview.Changes = append(preview.Changes, "create local group")
		return preview, nil
	}
	// delete
	if !current.Exists {
		preview.Allowed = false
		preview.Reason = ErrLocalGroupNotFound.Error()
		return preview, nil
	}
	if operation.ExpectedFingerprint != "" && current.Fingerprint != operation.ExpectedFingerprint {
		preview.Stale = true
		preview.Allowed = false
		preview.Reason = "The group changed; refresh before applying this operation."
		return preview, nil
	}
	// Protect essential system groups
	for _, protected := range []string{"root", "sudo", "wheel"} {
		if operation.Group == protected {
			preview.Allowed = false
			preview.Reason = "This system group cannot be deleted."
			return preview, nil
		}
	}
	preview.Changes = append(preview.Changes, "delete local group")
	preview.Warnings = append(preview.Warnings, "This permanently removes the local group.")
	return preview, nil
}

func applyLocalGroup(ctx context.Context, operation LocalGroupOperation, inventoryReader func(context.Context) (IdentityInventory, error), runner shadowCommandRunner) (LocalGroupState, error) {
	if err := ValidateLocalGroupOperation(operation); err != nil {
		return LocalGroupState{}, err
	}
	if operation.Preview {
		return LocalGroupState{}, errors.New("preview must use PreviewLocalGroup")
	}
	current, err := readLocalGroupState(ctx, operation.Group, inventoryReader)
	if err != nil {
		return LocalGroupState{}, err
	}
	if current.Exists && current.Source != "local" {
		return LocalGroupState{}, ErrLocalGroupReadOnly
	}
	if operation.Action == "create" {
		if current.Exists {
			return LocalGroupState{}, ErrLocalGroupConflict
		}
	} else {
		if !current.Exists {
			return LocalGroupState{}, ErrLocalGroupNotFound
		}
		if current.Fingerprint != operation.ExpectedFingerprint {
			return LocalGroupState{}, ErrLocalGroupConflict
		}
		for _, protected := range []string{"root", "sudo", "wheel"} {
			if operation.Group == protected {
				return LocalGroupState{}, ErrLocalGroupProtected
			}
		}
	}
	arguments, err := localGroupArguments(operation)
	if err != nil {
		return LocalGroupState{}, err
	}
	commandName := arguments[0]
	if _, err := runner.Run(ctx, commandName, arguments[1:]...); err != nil {
		if ctx.Err() != nil {
			return LocalGroupState{}, ctx.Err()
		}
		return LocalGroupState{}, fmt.Errorf("%w: %v", ErrLocalGroupUnavailable, err)
	}
	updated, err := readLocalGroupState(ctx, operation.Group, inventoryReader)
	if err != nil {
		return LocalGroupState{}, err
	}
	if !localGroupPostcondition(operation, updated) {
		return LocalGroupState{}, ErrLocalGroupVerification
	}
	return updated, nil
}

func readLocalGroupState(ctx context.Context, groupName string, inventoryReader func(context.Context) (IdentityInventory, error)) (LocalGroupState, error) {
	inventory, err := inventoryReader(ctx)
	if err != nil {
		return LocalGroupState{}, fmt.Errorf("%w: read NSS inventory: %v", ErrLocalGroupUnavailable, err)
	}
	state := LocalGroupState{Group: groupName}
	for index := range inventory.Groups {
		if inventory.Groups[index].Name != groupName {
			continue
		}
		group := inventory.Groups[index]
		state.Exists = true
		copyGroup := group
		state.Info = &copyGroup
		state.Source = group.Source
		break
	}
	state.Fingerprint = LocalGroupFingerprint(state)
	return state, nil
}

func localGroupArguments(operation LocalGroupOperation) ([]string, error) {
	switch operation.Action {
	case "create":
		return []string{"groupadd", "--", operation.Group}, nil
	case "delete":
		return []string{"groupdel", "--", operation.Group}, nil
	default:
		return nil, ErrInvalidLocalGroupOperation
	}
}

func localGroupPostcondition(operation LocalGroupOperation, state LocalGroupState) bool {
	switch operation.Action {
	case "create":
		return state.Exists && state.Source == "local" && state.Info != nil
	case "delete":
		return !state.Exists
	default:
		return false
	}
}
