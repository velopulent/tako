package sessiond

import (
	"context"
	"errors"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

type localAccountBackend interface {
	Preview(context.Context, platform.LocalAccountOperation, auth.Identity) (platform.LocalAccountPreview, error)
	Apply(context.Context, platform.LocalAccountOperation, auth.Identity) (platform.LocalAccountState, error)
}

type systemLocalAccountBackend struct{}

func (systemLocalAccountBackend) Preview(ctx context.Context, operation platform.LocalAccountOperation, identity auth.Identity) (platform.LocalAccountPreview, error) {
	return platform.PreviewLocalAccount(ctx, operation, identity.Username)
}

type groupMembershipBackend interface {
	Preview(context.Context, platform.GroupMembershipOperation, auth.Identity) (platform.GroupMembershipPreview, error)
	Apply(context.Context, platform.GroupMembershipOperation, auth.Identity) (platform.GroupMembershipState, error)
}

type administrativeRoleBackend interface {
	Preview(context.Context, platform.AdministrativeRoleOperation, auth.Identity) (platform.AdministrativeRolePreview, error)
	Apply(context.Context, platform.AdministrativeRoleOperation, auth.Identity) (platform.AdministrativeRoleState, error)
}

type systemGroupMembershipBackend struct{}

func (systemGroupMembershipBackend) Preview(ctx context.Context, operation platform.GroupMembershipOperation, identity auth.Identity) (platform.GroupMembershipPreview, error) {
	return platform.PreviewGroupMembership(ctx, operation, identity.Username)
}

func (systemGroupMembershipBackend) Apply(ctx context.Context, operation platform.GroupMembershipOperation, identity auth.Identity) (platform.GroupMembershipState, error) {
	return platform.ApplyGroupMembership(ctx, operation, identity.Username)
}

type systemAdministrativeRoleBackend struct{}

func (systemAdministrativeRoleBackend) Preview(ctx context.Context, operation platform.AdministrativeRoleOperation, identity auth.Identity) (platform.AdministrativeRolePreview, error) {
	return platform.PreviewAdministrativeRole(ctx, operation, identity.Username)
}

func (systemAdministrativeRoleBackend) Apply(ctx context.Context, operation platform.AdministrativeRoleOperation, identity auth.Identity) (platform.AdministrativeRoleState, error) {
	return platform.ApplyAdministrativeRole(ctx, operation, identity.Username)
}

func (systemLocalAccountBackend) Apply(ctx context.Context, operation platform.LocalAccountOperation, identity auth.Identity) (platform.LocalAccountState, error) {
	return platform.ApplyLocalAccount(ctx, operation, identity.Username)
}

func localAccountErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidLocalAccountOperation):
		return "invalid-local-account-operation"
	case errors.Is(err, platform.ErrLocalAccountConflict):
		return "local-account-conflict"
	case errors.Is(err, platform.ErrLocalAccountNotFound):
		return "local-account-not-found"
	case errors.Is(err, platform.ErrLocalAccountReadOnly):
		return "local-account-read-only"
	case errors.Is(err, platform.ErrLocalAccountProtected):
		return "local-account-protected"
	case errors.Is(err, platform.ErrLocalAccountVerification):
		return "local-account-verification-failed"
	default:
		return "local-account-unavailable"
	}
}

func groupErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidGroupOperation):
		return "invalid-group-operation"
	case errors.Is(err, platform.ErrGroupConflict):
		return "group-conflict"
	case errors.Is(err, platform.ErrGroupNotFound):
		return "group-not-found"
	case errors.Is(err, platform.ErrGroupUserNotFound):
		return "group-user-not-found"
	case errors.Is(err, platform.ErrGroupReadOnly):
		return "group-read-only"
	case errors.Is(err, platform.ErrGroupVerification):
		return "group-verification-failed"
	case errors.Is(err, platform.ErrAdministrativeRoleUnavailable):
		return "administrative-role-unavailable"
	case errors.Is(err, platform.ErrAdministrativeRoleProtected):
		return "administrative-role-protected"
	default:
		return "group-unavailable"
	}
}
