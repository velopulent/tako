package sessiond

import (
	"errors"

	"github.com/velopulent/tako/internal/platform"
)

func networkErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidNetworkOperation):
		return "invalid-network-operation"
	case errors.Is(err, platform.ErrNetworkConflict):
		return "network-conflict"
	case errors.Is(err, platform.ErrNetworkOwnership):
		return "network-ownership-conflict"
	case errors.Is(err, platform.ErrNetworkUnavailable):
		return "network-unavailable"
	default:
		return "network-operation-failed"
	}
}

func firewallErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidFirewallOperation):
		return "invalid-firewall-operation"
	case errors.Is(err, platform.ErrFirewallConflict):
		return "firewall-conflict"
	case errors.Is(err, platform.ErrFirewallOwnership):
		return "firewall-ownership-conflict"
	case errors.Is(err, platform.ErrFirewallAccessRisk):
		return "firewall-access-risk"
	case errors.Is(err, platform.ErrFirewallUnavailable):
		return "firewall-unavailable"
	default:
		return "firewall-operation-failed"
	}
}

func securityErrorCode(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidSecurityOperation):
		return "invalid-security-operation"
	case errors.Is(err, platform.ErrSecurityConflict):
		return "security-conflict"
	case errors.Is(err, platform.ErrSecurityUnsafe):
		return "security-unsafe"
	case errors.Is(err, platform.ErrSecurityUnavailable):
		return "security-unavailable"
	default:
		return "security-operation-failed"
	}
}
