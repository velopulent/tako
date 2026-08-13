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
	"sort"
	"strings"
)

const maxGroupName = 256

var (
	ErrInvalidGroupOperation         = errors.New("invalid group operation")
	ErrGroupConflict                 = errors.New("group membership conflict")
	ErrGroupNotFound                 = errors.New("group not found")
	ErrGroupReadOnly                 = errors.New("group or identity is read-only")
	ErrGroupUserNotFound             = errors.New("group user not found")
	ErrGroupUnavailable              = errors.New("group service unavailable")
	ErrGroupVerification             = errors.New("group membership verification failed")
	ErrAdministrativeRoleUnavailable = errors.New("administrative role unavailable")
	ErrAdministrativeRoleProtected   = errors.New("administrative role is protected")
)

var administrativeRoleGroups = []string{"sudo", "wheel"}

type GroupMembershipOperation struct {
	Action              string `json:"action"`
	Username            string `json:"username"`
	Group               string `json:"group"`
	ExpectedFingerprint string `json:"expectedFingerprint,omitempty"`
	Preview             bool   `json:"preview,omitempty"`
}

func (operation *GroupMembershipOperation) UnmarshalJSON(payload []byte) error {
	type plain GroupMembershipOperation
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var value plain
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing group membership operation data")
	}
	*operation = GroupMembershipOperation(value)
	return nil
}

type AdministrativeRoleOperation struct {
	Action              string `json:"action"`
	Username            string `json:"username"`
	Role                string `json:"role"`
	ExpectedFingerprint string `json:"expectedFingerprint,omitempty"`
	Confirmation        string `json:"confirmation,omitempty"`
	Preview             bool   `json:"preview,omitempty"`
}

func (operation *AdministrativeRoleOperation) UnmarshalJSON(payload []byte) error {
	type plain AdministrativeRoleOperation
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var value plain
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing administrative role operation data")
	}
	*operation = AdministrativeRoleOperation(value)
	return nil
}

type GroupMembershipState struct {
	Username    string        `json:"username"`
	Group       IdentityGroup `json:"group"`
	Member      bool          `json:"member"`
	Fingerprint string        `json:"fingerprint"`
}

type GroupMembershipPreview struct {
	Action               string               `json:"action"`
	Username             string               `json:"username"`
	Group                string               `json:"group"`
	Current              GroupMembershipState `json:"current"`
	Changes              []string             `json:"changes"`
	Warnings             []string             `json:"warnings"`
	Stale                bool                 `json:"stale"`
	Allowed              bool                 `json:"allowed"`
	Reason               string               `json:"reason,omitempty"`
	RequiresConfirmation bool                 `json:"requiresConfirmation"`
}

type AdministrativeRoleState struct {
	Username    string   `json:"username"`
	Role        string   `json:"role"`
	Group       string   `json:"group"`
	Member      bool     `json:"member"`
	Members     []string `json:"members"`
	Fingerprint string   `json:"fingerprint"`
}

type AdministrativeRolePreview struct {
	Action               string                  `json:"action"`
	Username             string                  `json:"username"`
	Role                 string                  `json:"role"`
	Current              AdministrativeRoleState `json:"current"`
	Changes              []string                `json:"changes"`
	Warnings             []string                `json:"warnings"`
	Stale                bool                    `json:"stale"`
	Allowed              bool                    `json:"allowed"`
	Reason               string                  `json:"reason,omitempty"`
	RequiresConfirmation bool                    `json:"requiresConfirmation"`
}

func ValidateGroupMembershipOperation(operation GroupMembershipOperation) error {
	if (operation.Action != "add" && operation.Action != "remove") || !validAccountUsername(operation.Username) || !validGroupName(operation.Group) {
		return ErrInvalidGroupOperation
	}
	if operation.ExpectedFingerprint != "" {
		if len(operation.ExpectedFingerprint) != sha256.Size*2 {
			return ErrInvalidGroupOperation
		}
		if _, err := hex.DecodeString(operation.ExpectedFingerprint); err != nil {
			return ErrInvalidGroupOperation
		}
	}
	if !operation.Preview && operation.ExpectedFingerprint == "" {
		return ErrInvalidGroupOperation
	}
	return nil
}

func ValidateAdministrativeRoleOperation(operation AdministrativeRoleOperation) error {
	if (operation.Action != "grant" && operation.Action != "revoke") || !validAccountUsername(operation.Username) || operation.Role != "administrator" {
		return ErrInvalidGroupOperation
	}
	if operation.ExpectedFingerprint != "" {
		if len(operation.ExpectedFingerprint) != sha256.Size*2 {
			return ErrInvalidGroupOperation
		}
		if _, err := hex.DecodeString(operation.ExpectedFingerprint); err != nil {
			return ErrInvalidGroupOperation
		}
	}
	if !operation.Preview && operation.ExpectedFingerprint == "" {
		return ErrInvalidGroupOperation
	}
	if len(operation.Confirmation) > 128 || strings.ContainsAny(operation.Confirmation, "\x00\r\n") {
		return ErrInvalidGroupOperation
	}
	if !operation.Preview && operation.Confirmation != strings.ToUpper(operation.Action)+" ADMIN "+operation.Username {
		return ErrInvalidGroupOperation
	}
	return nil
}

func validGroupName(name string) bool {
	if name == "" || len(name) > maxGroupName || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\r\n:") {
		return false
	}
	for _, character := range name {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '_' && character != '-' && character != '.' && character != '$' {
			return false
		}
	}
	return true
}

func GroupMembershipFingerprint(group IdentityGroup) string {
	members := append([]string(nil), group.Members...)
	sort.Strings(members)
	hash := sha256.New()
	_, _ = hash.Write([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s", group.Name, group.GID, group.Source, strings.Join(members, "\x00"))))
	return hex.EncodeToString(hash.Sum(nil))
}

func PreviewGroupMembership(ctx context.Context, operation GroupMembershipOperation, actor string) (GroupMembershipPreview, error) {
	return previewGroupMembership(ctx, operation, actor, ListIdentityInventory)
}

func ApplyGroupMembership(ctx context.Context, operation GroupMembershipOperation, actor string) (GroupMembershipState, error) {
	return applyGroupMembership(ctx, operation, actor, ListIdentityInventory, execShadowCommandRunner{})
}

func PreviewAdministrativeRole(ctx context.Context, operation AdministrativeRoleOperation, actor string) (AdministrativeRolePreview, error) {
	return previewAdministrativeRole(ctx, operation, actor, ListIdentityInventory)
}

func ApplyAdministrativeRole(ctx context.Context, operation AdministrativeRoleOperation, actor string) (AdministrativeRoleState, error) {
	return applyAdministrativeRole(ctx, operation, actor, ListIdentityInventory, execShadowCommandRunner{})
}

func previewGroupMembership(ctx context.Context, operation GroupMembershipOperation, actor string, inventoryReader func(context.Context) (IdentityInventory, error)) (GroupMembershipPreview, error) {
	if err := ValidateGroupMembershipOperation(operation); err != nil {
		return GroupMembershipPreview{}, err
	}
	state, user, err := readGroupMembershipState(ctx, operation.Username, operation.Group, inventoryReader)
	if err != nil {
		return GroupMembershipPreview{}, err
	}
	preview := GroupMembershipPreview{Action: operation.Action, Username: operation.Username, Group: operation.Group, Current: state, Changes: []string{}, Warnings: []string{}, Allowed: true, RequiresConfirmation: true}
	if user == nil {
		preview.Allowed = false
		preview.Reason = ErrGroupUserNotFound.Error()
		return preview, nil
	}
	if !user.Local || !user.Mutable || state.Group.Source != "local" || !state.Group.Mutable {
		preview.Allowed = false
		preview.Reason = "Remote/NSS identities and groups are read-only in Tako."
		return preview, nil
	}
	if operation.ExpectedFingerprint != "" && state.Fingerprint != operation.ExpectedFingerprint {
		preview.Allowed = false
		preview.Stale = true
		preview.Reason = "Group membership changed; refresh before applying this operation."
		return preview, nil
	}
	if operation.Action == "add" {
		if state.Member {
			preview.Changes = append(preview.Changes, "user is already a member")
		} else {
			preview.Changes = append(preview.Changes, "add user to local group")
		}
	} else if state.Member {
		preview.Changes = append(preview.Changes, "remove user from local group")
	} else {
		preview.Changes = append(preview.Changes, "user is not a member")
	}
	if actor == operation.Username && operation.Action == "remove" {
		preview.Warnings = append(preview.Warnings, "Removing your own group membership may revoke access to workloads.")
	}
	return preview, nil
}

func applyGroupMembership(ctx context.Context, operation GroupMembershipOperation, actor string, inventoryReader func(context.Context) (IdentityInventory, error), runner shadowCommandRunner) (GroupMembershipState, error) {
	if err := ValidateGroupMembershipOperation(operation); err != nil {
		return GroupMembershipState{}, err
	}
	state, user, err := readGroupMembershipState(ctx, operation.Username, operation.Group, inventoryReader)
	if err != nil {
		return GroupMembershipState{}, err
	}
	if user == nil {
		return GroupMembershipState{}, ErrGroupUserNotFound
	}
	if !user.Local || !user.Mutable || state.Group.Source != "local" || !state.Group.Mutable {
		return GroupMembershipState{}, ErrGroupReadOnly
	}
	if state.Fingerprint != operation.ExpectedFingerprint {
		return GroupMembershipState{}, ErrGroupConflict
	}
	arguments := []string{"gpasswd", "--delete", operation.Username, operation.Group}
	if operation.Action == "add" {
		arguments = []string{"gpasswd", "--add", operation.Username, operation.Group}
	}
	if _, err := runner.Run(ctx, arguments[0], arguments[1:]...); err != nil {
		if ctx.Err() != nil {
			return GroupMembershipState{}, ctx.Err()
		}
		return GroupMembershipState{}, fmt.Errorf("%w: %v", ErrGroupUnavailable, err)
	}
	updated, _, err := readGroupMembershipState(ctx, operation.Username, operation.Group, inventoryReader)
	if err != nil {
		return GroupMembershipState{}, err
	}
	if updated.Member != (operation.Action == "add") {
		return GroupMembershipState{}, ErrGroupVerification
	}
	return updated, nil
}

func readGroupMembershipState(ctx context.Context, username, groupName string, inventoryReader func(context.Context) (IdentityInventory, error)) (GroupMembershipState, *User, error) {
	inventory, err := inventoryReader(ctx)
	if err != nil {
		return GroupMembershipState{}, nil, fmt.Errorf("%w: read NSS inventory: %v", ErrGroupUnavailable, err)
	}
	var user *User
	for index := range inventory.Users {
		if inventory.Users[index].Username == username {
			candidate := inventory.Users[index]
			user = &candidate
			break
		}
	}
	var group *IdentityGroup
	for index := range inventory.Groups {
		if inventory.Groups[index].Name == groupName {
			candidate := inventory.Groups[index]
			group = &candidate
			break
		}
	}
	if group == nil {
		return GroupMembershipState{}, user, ErrGroupNotFound
	}
	state := GroupMembershipState{Username: username, Group: *group, Fingerprint: GroupMembershipFingerprint(*group)}
	for _, member := range group.Members {
		if member == username {
			state.Member = true
			break
		}
	}
	return state, user, nil
}

func previewAdministrativeRole(ctx context.Context, operation AdministrativeRoleOperation, actor string, inventoryReader func(context.Context) (IdentityInventory, error)) (AdministrativeRolePreview, error) {
	if err := ValidateAdministrativeRoleOperation(operation); err != nil {
		return AdministrativeRolePreview{}, err
	}
	state, user, err := readAdministrativeRoleState(ctx, operation.Username, inventoryReader)
	if err != nil {
		return AdministrativeRolePreview{}, err
	}
	preview := AdministrativeRolePreview{Action: operation.Action, Username: operation.Username, Role: operation.Role, Current: state, Changes: []string{}, Warnings: []string{}, Allowed: true, RequiresConfirmation: true}
	if user == nil {
		preview.Allowed = false
		preview.Reason = ErrGroupUserNotFound.Error()
		return preview, nil
	}
	if !user.Local || !user.Mutable {
		preview.Allowed = false
		preview.Reason = "Remote/NSS identities are read-only in Tako."
		return preview, nil
	}
	if operation.ExpectedFingerprint != "" && state.Fingerprint != operation.ExpectedFingerprint {
		preview.Allowed = false
		preview.Stale = true
		preview.Reason = "Administrator membership changed; refresh before applying this operation."
		return preview, nil
	}
	if operation.Action == "grant" {
		if state.Member {
			preview.Changes = append(preview.Changes, "user already has the administrator role")
		} else {
			preview.Changes = append(preview.Changes, "grant administrator role through "+state.Group)
		}
	} else if state.Member {
		preview.Changes = append(preview.Changes, "revoke administrator role through "+state.Group)
		if actor == operation.Username || len(state.Members) <= 1 {
			preview.Allowed = false
			preview.Reason = "The last/current administrator cannot be removed."
		}
	} else {
		preview.Changes = append(preview.Changes, "user does not have the administrator role")
	}
	return preview, nil
}

func applyAdministrativeRole(ctx context.Context, operation AdministrativeRoleOperation, actor string, inventoryReader func(context.Context) (IdentityInventory, error), runner shadowCommandRunner) (AdministrativeRoleState, error) {
	if err := ValidateAdministrativeRoleOperation(operation); err != nil {
		return AdministrativeRoleState{}, err
	}
	state, user, err := readAdministrativeRoleState(ctx, operation.Username, inventoryReader)
	if err != nil {
		return AdministrativeRoleState{}, err
	}
	if user == nil {
		return AdministrativeRoleState{}, ErrGroupUserNotFound
	}
	if !user.Local || !user.Mutable {
		return AdministrativeRoleState{}, ErrGroupReadOnly
	}
	if state.Fingerprint != operation.ExpectedFingerprint {
		return AdministrativeRoleState{}, ErrGroupConflict
	}
	if operation.Action == "revoke" && (actor == operation.Username || (state.Member && len(state.Members) <= 1)) {
		return AdministrativeRoleState{}, ErrAdministrativeRoleProtected
	}
	membership := GroupMembershipOperation{Action: map[string]string{"grant": "add", "revoke": "remove"}[operation.Action], Username: operation.Username, Group: state.Group, ExpectedFingerprint: operation.ExpectedFingerprint}
	if _, err := applyGroupMembership(ctx, membership, actor, inventoryReader, runner); err != nil {
		return AdministrativeRoleState{}, err
	}
	updated, _, err := readAdministrativeRoleState(ctx, operation.Username, inventoryReader)
	if err != nil {
		return AdministrativeRoleState{}, err
	}
	if updated.Member != (operation.Action == "grant") {
		return AdministrativeRoleState{}, ErrGroupVerification
	}
	return updated, nil
}

func readAdministrativeRoleState(ctx context.Context, username string, inventoryReader func(context.Context) (IdentityInventory, error)) (AdministrativeRoleState, *User, error) {
	inventory, err := inventoryReader(ctx)
	if err != nil {
		return AdministrativeRoleState{}, nil, fmt.Errorf("%w: read NSS inventory: %v", ErrGroupUnavailable, err)
	}
	var user *User
	for index := range inventory.Users {
		if inventory.Users[index].Username == username {
			candidate := inventory.Users[index]
			user = &candidate
			break
		}
	}
	var selected *IdentityGroup
	var fallback *IdentityGroup
	for _, candidateName := range administrativeRoleGroups {
		for index := range inventory.Groups {
			candidate := inventory.Groups[index]
			if candidate.Name != candidateName || !candidate.Local || !candidate.Mutable {
				continue
			}
			if fallback == nil {
				fallback = &candidate
			}
			for _, member := range candidate.Members {
				if member == username {
					selected = &candidate
					break
				}
			}
		}
		if selected != nil {
			break
		}
	}
	if selected == nil {
		selected = fallback
	}
	if selected == nil {
		return AdministrativeRoleState{}, user, ErrAdministrativeRoleUnavailable
	}
	state := AdministrativeRoleState{Username: username, Role: "administrator", Group: selected.Name, Members: append([]string(nil), selected.Members...), Fingerprint: GroupMembershipFingerprint(*selected)}
	for _, member := range selected.Members {
		if member == username {
			state.Member = true
			break
		}
	}
	sort.Strings(state.Members)
	return state, user, nil
}
