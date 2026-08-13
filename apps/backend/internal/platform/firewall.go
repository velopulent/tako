package platform

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
)

var (
	ErrInvalidFirewallOperation = errors.New("invalid firewall operation")
	ErrFirewallConflict         = errors.New("firewall state changed")
	ErrFirewallOwnership        = errors.New("firewall ownership is conflicted")
	ErrFirewallUnavailable      = errors.New("firewall adapter unavailable")
	ErrFirewallAccessRisk       = errors.New("firewall operation could lock out management access")
)

type FirewallSnapshot struct {
	Backend     string   `json:"backend"`
	Active      bool     `json:"active"`
	Version     string   `json:"version,omitempty"`
	DefaultZone string   `json:"defaultZone,omitempty"`
	Zones       []string `json:"zones"`
	Rules       []string `json:"rules"`
	Conflicted  bool     `json:"conflicted"`
	ReadOnly    bool     `json:"readOnly"`
	Reason      string   `json:"reason,omitempty"`
	Fingerprint string   `json:"fingerprint"`
}

type FirewallOperation struct {
	Backend             string `json:"backend"`
	Action              string `json:"action"`
	Zone                string `json:"zone,omitempty"`
	Service             string `json:"service,omitempty"`
	Port                string `json:"port,omitempty"`
	Source              string `json:"source,omitempty"`
	DefaultZone         string `json:"defaultZone,omitempty"`
	ExpectedFingerprint string `json:"expectedFingerprint,omitempty"`
	Confirmation        string `json:"confirmation,omitempty"`
	Persist             bool   `json:"persist,omitempty"`
}

type FirewallState struct {
	Snapshot FirewallSnapshot `json:"snapshot"`
	Action   string           `json:"action"`
	Applied  bool             `json:"applied"`
	Warning  string           `json:"warning,omitempty"`
}

func ReadFirewallStatus(ctx context.Context) (FirewallSnapshot, error) {
	snapshot := FirewallSnapshot{Zones: []string{}, Rules: []string{}}
	firewalld, firewalldErr := firewallCommand(ctx, "firewall-cmd", "--state")
	ufw, ufwErr := firewallCommand(ctx, "ufw", "status", "verbose")
	if firewalldErr == nil && strings.Contains(strings.ToLower(firewalld), "running") {
		snapshot.Backend = "firewalld"
		snapshot.Active = true
		version, _ := firewallCommand(ctx, "firewall-cmd", "--version")
		snapshot.Version = strings.TrimSpace(version)
		zones, _ := firewallCommand(ctx, "firewall-cmd", "--get-active-zones")
		snapshot.Rules = boundedLines(zones, 256)
		defaultZone, _ := firewallCommand(ctx, "firewall-cmd", "--get-default-zone")
		snapshot.DefaultZone = strings.TrimSpace(defaultZone)
	} else if ufwErr == nil && strings.HasPrefix(strings.TrimSpace(ufw), "Status: active") {
		snapshot.Backend = "UFW"
		snapshot.Active = true
		version, _ := firewallCommand(ctx, "ufw", "--version")
		snapshot.Version = firstLine(version)
		snapshot.Rules = boundedLines(ufw, 256)
	} else if firewalldErr == nil || ufwErr == nil {
		if firewalldErr == nil && ufwErr == nil {
			snapshot.Conflicted = true
			snapshot.Reason = "firewalld and UFW were both detected; active state could not be established."
		} else {
			snapshot.ReadOnly = true
			snapshot.Reason = "A firewall command responded, but no active firewall state was reported."
		}
	} else {
		return snapshot, ErrFirewallUnavailable
	}
	snapshot.Fingerprint = fingerprintBytes([]byte(snapshot.Backend + "|" + strconv.FormatBool(snapshot.Active) + "|" + strings.Join(snapshot.Rules, "\n")))
	return snapshot, nil
}

func ValidateFirewallOperation(operation FirewallOperation) error {
	backends := map[string]bool{"auto": true, "firewalld": true, "UFW": true}
	actions := map[string]bool{"preview": true, "enable": true, "disable": true, "default-zone": true, "add-service": true, "remove-service": true, "add-port": true, "remove-port": true, "add-source": true, "remove-source": true, "reload": true}
	if !backends[operation.Backend] || !actions[operation.Action] || len(operation.Zone) > 128 || len(operation.Service) > 128 || len(operation.Port) > 32 || len(operation.Source) > 128 || len(operation.DefaultZone) > 128 || len(operation.ExpectedFingerprint) > 128 || strings.ContainsAny(operation.Zone+operation.Service+operation.Port+operation.Source+operation.DefaultZone, "\x00\r\n") {
		return ErrInvalidFirewallOperation
	}
	if operation.Port != "" {
		parts := strings.Split(operation.Port, "/")
		if len(parts) != 2 {
			return ErrInvalidFirewallOperation
		}
		port, err := strconv.Atoi(parts[0])
		if err != nil || port < 1 || port > 65535 || (parts[1] != "tcp" && parts[1] != "udp") {
			return ErrInvalidFirewallOperation
		}
	}
	if operation.Source != "" && net.ParseIP(operation.Source) == nil {
		if _, _, err := net.ParseCIDR(operation.Source); err != nil {
			return ErrInvalidFirewallOperation
		}
	}
	if operation.ExpectedFingerprint == "" && operation.Action != "preview" {
		return ErrInvalidFirewallOperation
	}
	if operation.Action != "preview" && operation.Confirmation != "CONFIRM FIREWALL CHANGE" && operation.Confirmation != "CONFIRM FIREWALL ACCESS" {
		return ErrInvalidFirewallOperation
	}
	if (operation.Action == "disable" || operation.Action == "remove-service" || operation.Action == "remove-port") && operation.Confirmation != "CONFIRM FIREWALL ACCESS" {
		return ErrFirewallAccessRisk
	}
	return nil
}

func PreviewFirewallOperation(ctx context.Context, operation FirewallOperation) (FirewallState, error) {
	operation.Action = "preview"
	if err := ValidateFirewallOperation(operation); err != nil {
		return FirewallState{}, err
	}
	snapshot, err := ReadFirewallStatus(ctx)
	if err != nil {
		return FirewallState{}, err
	}
	if operation.Backend == "auto" {
		operation.Backend = snapshot.Backend
	}
	return FirewallState{Snapshot: snapshot, Action: "preview", Warning: "Runtime and persistent firewall state remain separate; persistence requires an explicit commit."}, nil
}

func ApplyFirewallOperation(ctx context.Context, operation FirewallOperation) (FirewallState, error) {
	if err := ValidateFirewallOperation(operation); err != nil {
		return FirewallState{}, err
	}
	snapshot, err := ReadFirewallStatus(ctx)
	if err != nil {
		return FirewallState{}, err
	}
	if operation.Backend == "auto" {
		operation.Backend = snapshot.Backend
	}
	if snapshot.Conflicted || snapshot.Backend != operation.Backend {
		return FirewallState{}, ErrFirewallOwnership
	}
	if operation.ExpectedFingerprint != snapshot.Fingerprint {
		return FirewallState{}, ErrFirewallConflict
	}
	arguments := []string{}
	if operation.Backend == "firewalld" {
		zone := operation.Zone
		if zone == "" {
			zone = snapshot.DefaultZone
		}
		if operation.Action == "default-zone" {
			arguments = []string{"--set-default-zone=" + operation.DefaultZone}
		} else if operation.Action == "reload" {
			arguments = []string{"--reload"}
		} else if operation.Action == "enable" {
			arguments = []string{"--set-log-denied=all"}
		} else if operation.Action == "add-service" || operation.Action == "remove-service" {
			arguments = []string{"--zone=" + zone, "--" + strings.TrimSuffix(operation.Action, "-service") + "-service=" + operation.Service}
		} else if operation.Action == "add-port" || operation.Action == "remove-port" {
			arguments = []string{"--zone=" + zone, "--" + strings.TrimSuffix(operation.Action, "-port") + "-port=" + operation.Port}
		} else if operation.Action == "add-source" || operation.Action == "remove-source" {
			arguments = []string{"--zone=" + zone, "--" + strings.TrimSuffix(operation.Action, "-source") + "-source=" + operation.Source}
		} else {
			return FirewallState{}, ErrInvalidFirewallOperation
		}
		if operation.Persist && operation.Action != "reload" {
			arguments = append(arguments, "--permanent")
		}
		if _, err := firewallCommand(ctx, "firewall-cmd", arguments...); err != nil {
			return FirewallState{}, err
		}
	} else {
		if operation.Action == "reload" {
			if _, err := firewallCommand(ctx, "ufw", "reload"); err != nil {
				return FirewallState{}, err
			}
		} else if operation.Action == "enable" || operation.Action == "disable" {
			if _, err := firewallCommand(ctx, "ufw", operation.Action); err != nil {
				return FirewallState{}, err
			}
		} else if operation.Action == "add-port" || operation.Action == "remove-port" {
			action := "allow"
			if operation.Action == "remove-port" {
				action = "delete allow"
			}
			if _, err := firewallCommand(ctx, "ufw", strings.Fields(action+" "+operation.Port)...); err != nil {
				return FirewallState{}, err
			}
		} else {
			return FirewallState{}, ErrFirewallUnavailable
		}
	}
	updated, err := ReadFirewallStatus(ctx)
	if err != nil {
		return FirewallState{}, err
	}
	return FirewallState{Snapshot: updated, Action: operation.Action, Applied: true, Warning: "Management access protection was evaluated before applying this change; verify reconnection from a fresh session."}, nil
}

func firewallCommand(ctx context.Context, name string, arguments ...string) (string, error) {
	payload, err := networkCommand(ctx, name, arguments...)
	return string(payload), err
}

func boundedLines(value string, max int) []string {
	result := []string{}
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 512 {
			line = line[:512]
		}
		result = append(result, line)
		if len(result) >= max {
			break
		}
	}
	return result
}

func firstLine(value string) string {
	lines := boundedLines(value, 1)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func firewallErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidFirewallOperation):
		return "invalid-firewall-operation"
	case errors.Is(err, ErrFirewallConflict):
		return "firewall-conflict"
	case errors.Is(err, ErrFirewallOwnership):
		return "firewall-ownership-conflict"
	case errors.Is(err, ErrFirewallAccessRisk):
		return "firewall-access-risk"
	case errors.Is(err, ErrFirewallUnavailable):
		return "firewall-unavailable"
	default:
		return "firewall-operation-failed"
	}
}
