package platform

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// FirewallCommandRunner is the small command seam used by the firewall
// adapter. Production uses networkCommand, while tests provide deterministic
// command fixtures and therefore never mutate the host firewall.
type FirewallCommandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

// FirewallStrategy is the module-owned read/apply boundary. Keeping it
// separate from sessiond makes command planning and parser behavior testable.
type FirewallStrategy interface {
	Read(context.Context) (FirewallSnapshot, error)
	Apply(context.Context, FirewallOperation) (FirewallState, error)
}

type firewallAdapter struct {
	runner FirewallCommandRunner
}

type systemFirewallCommandRunner struct{}

func (systemFirewallCommandRunner) Run(ctx context.Context, name string, arguments ...string) (string, error) {
	payload, err := networkCommand(ctx, name, arguments...)
	return string(payload), err
}

// NewFirewallStrategy returns a firewall adapter backed by runner. A nil
// runner selects the bounded production command executor.
func NewFirewallStrategy(runner FirewallCommandRunner) FirewallStrategy {
	if runner == nil {
		runner = systemFirewallCommandRunner{}
	}
	return &firewallAdapter{runner: runner}
}

func (adapter *firewallAdapter) Read(ctx context.Context) (FirewallSnapshot, error) {
	snapshot := FirewallSnapshot{Zones: []string{}, Rules: []string{}, RuntimeRules: []string{}, PersistentRules: []string{}}
	firewalldState, firewalldErr := adapter.runner.Run(ctx, "firewall-cmd", "--state")
	ufwOutput, ufwErr := adapter.runner.Run(ctx, "ufw", "status", "verbose")
	firewalldReportedRunning := strings.EqualFold(strings.TrimSpace(firstLine(firewalldState)), "running")
	firewalldActive := firewalldErr == nil && firewalldReportedRunning
	ufwActive := ufwErr == nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(ufwOutput)), "status: active")
	if firewalldReportedRunning && firewalldErr != nil {
		if !ufwActive {
			return snapshot, ErrFirewallUnavailable
		}
		snapshot.Backend = "firewalld+UFW"
		snapshot.Active = true
		snapshot.Conflicted = true
		snapshot.ReadOnly = true
		snapshot.Reason = "firewalld state could not be verified while UFW reported active; mutations are disabled."
		snapshot.Rules = prefixRules("UFW", parseUFWRules(ufwOutput))
		snapshot.RuntimeRules = append([]string(nil), snapshot.Rules...)
		snapshot.PersistentRules = append([]string(nil), snapshot.Rules...)
		snapshot.Fingerprint = firewallFingerprint(snapshot)
		return snapshot, nil
	}

	switch {
	case firewalldActive && ufwActive:
		var readErr error
		snapshot, readErr = adapter.readFirewalld(ctx, snapshot)
		if readErr != nil {
			return snapshot, readErr
		}
		ufwRules := parseUFWRules(ufwOutput)
		snapshot.Backend = "firewalld+UFW"
		snapshot.Conflicted = true
		snapshot.ReadOnly = true
		snapshot.Reason = "firewalld and UFW are both active; mutations are disabled until one manager is stopped."
		snapshot.Rules = append(snapshot.Rules, prefixRules("UFW", ufwRules)...)
		snapshot.RuntimeRules = append(snapshot.RuntimeRules, prefixRules("UFW", ufwRules)...)
		snapshot.PersistentRules = append(snapshot.PersistentRules, prefixRules("UFW", ufwRules)...)
		snapshot.Synchronized = false
	case firewalldActive:
		var readErr error
		snapshot, readErr = adapter.readFirewalld(ctx, snapshot)
		if readErr != nil {
			return snapshot, readErr
		}
	case ufwActive:
		snapshot = adapter.readUFW(ctx, snapshot, ufwOutput)
	case firewalldErr == nil || ufwErr == nil:
		snapshot.ReadOnly = true
		snapshot.Synchronized = false
		// A successful inactive status probe means the manager is installed,
		// not that it owns the host. Only two active managers are a conflict;
		// an inactive UFW must not prevent explicitly enabling firewalld.
		if firewalldErr == nil {
			snapshot.Backend = "firewalld"
			snapshot.Reason = "firewalld is installed but did not report a running state."
		} else {
			snapshot.Backend = "UFW"
			snapshot.Reason = "UFW is installed but did not report an active state."
		}
	default:
		return snapshot, ErrFirewallUnavailable
	}

	snapshot.Fingerprint = firewallFingerprint(snapshot)
	return snapshot, nil
}

func (adapter *firewallAdapter) readFirewalld(ctx context.Context, snapshot FirewallSnapshot) (FirewallSnapshot, error) {
	snapshot.Backend = "firewalld"
	snapshot.Active = true
	if version, err := adapter.runner.Run(ctx, "firewall-cmd", "--version"); err == nil {
		snapshot.Version = firstLine(version)
	}
	activeZones, err := adapter.runner.Run(ctx, "firewall-cmd", "--get-active-zones")
	if err != nil {
		return snapshot, fmt.Errorf("%w: active-zone inventory: %v", ErrFirewallUnavailable, err)
	}
	allZones, err := adapter.runner.Run(ctx, "firewall-cmd", "--get-zones")
	if err != nil {
		return snapshot, fmt.Errorf("%w: zone inventory: %v", ErrFirewallUnavailable, err)
	}
	snapshot.Zones = parseFirewalldZones(activeZones, allZones)
	value, err := adapter.runner.Run(ctx, "firewall-cmd", "--get-default-zone")
	if err != nil {
		return snapshot, fmt.Errorf("%w: default zone: %v", ErrFirewallUnavailable, err)
	}
	snapshot.DefaultZone = strings.TrimSpace(firstLine(value))
	// firewalld documents --set-default-zone as a runtime and permanent
	// operation. There is no separate persistent default-zone read to merge;
	// recording the observed value in both views avoids a false drift signal.
	snapshot.PersistentDefaultZone = snapshot.DefaultZone
	for _, zone := range snapshot.Zones {
		output, err := adapter.runner.Run(ctx, "firewall-cmd", "--zone="+zone, "--list-all")
		if err != nil {
			return snapshot, fmt.Errorf("%w: runtime zone %s: %v", ErrFirewallUnavailable, zone, err)
		}
		snapshot.RuntimeRules = append(snapshot.RuntimeRules, parseFirewalldRules(zone, output)...)
		output, err = adapter.runner.Run(ctx, "firewall-cmd", "--permanent", "--zone="+zone, "--list-all")
		if err != nil {
			return snapshot, fmt.Errorf("%w: persistent zone %s: %v", ErrFirewallUnavailable, zone, err)
		}
		snapshot.PersistentRules = append(snapshot.PersistentRules, parseFirewalldRules(zone, output)...)
	}
	snapshot.RuntimeRules = sortedUnique(snapshot.RuntimeRules)
	snapshot.PersistentRules = sortedUnique(snapshot.PersistentRules)
	snapshot.Rules = append([]string(nil), snapshot.RuntimeRules...)
	snapshot.Synchronized = snapshot.DefaultZone == snapshot.PersistentDefaultZone && equalStringSlices(snapshot.RuntimeRules, snapshot.PersistentRules)
	return snapshot, nil
}

func (adapter *firewallAdapter) readUFW(ctx context.Context, snapshot FirewallSnapshot, status string) FirewallSnapshot {
	snapshot.Backend = "UFW"
	snapshot.Active = true
	if version, err := adapter.runner.Run(ctx, "ufw", "--version"); err == nil {
		snapshot.Version = firstLine(version)
	}
	rules := parseUFWRules(status)
	snapshot.Rules = append([]string(nil), rules...)
	snapshot.RuntimeRules = append([]string(nil), rules...)
	snapshot.PersistentRules = append([]string(nil), rules...)
	snapshot.Synchronized = true
	return snapshot
}

func prefixRules(prefix string, rules []string) []string {
	result := make([]string, 0, len(rules))
	for _, rule := range rules {
		result = append(result, prefix+" "+rule)
	}
	return result
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (adapter *firewallAdapter) Apply(ctx context.Context, operation FirewallOperation) (FirewallState, error) {
	if err := ValidateFirewallOperation(operation); err != nil {
		return FirewallState{}, err
	}
	snapshot, err := adapter.Read(ctx)
	if err != nil {
		return FirewallState{}, err
	}
	if operation.Backend == "auto" {
		operation.Backend = snapshot.Backend
	}
	managerStart := operation.Action == "enable"
	if snapshot.Conflicted || snapshot.Backend != operation.Backend || (!managerStart && (snapshot.ReadOnly || !snapshot.Active)) {
		return FirewallState{}, ErrFirewallOwnership
	}
	if operation.ExpectedFingerprint != snapshot.Fingerprint {
		return FirewallState{}, ErrFirewallConflict
	}

	var warning string
	switch operation.Backend {
	case "firewalld":
		warning, err = adapter.applyFirewalld(ctx, operation, snapshot)
	case "UFW":
		warning, err = adapter.applyUFW(ctx, operation)
	default:
		err = ErrFirewallUnavailable
	}
	if err != nil {
		return FirewallState{}, err
	}
	updated, readErr := adapter.Read(ctx)
	if readErr != nil {
		return FirewallState{}, readErr
	}
	if updated.Backend != operation.Backend || updated.Conflicted || (managerStart && !updated.Active) {
		return FirewallState{}, ErrFirewallOwnership
	}
	if err := verifyFirewallMutation(snapshot, updated, operation); err != nil {
		return FirewallState{}, err
	}
	return FirewallState{Snapshot: updated, Action: operation.Action, Applied: true, Warning: warning}, nil
}

func verifyFirewallMutation(before, after FirewallSnapshot, operation FirewallOperation) error {
	switch operation.Action {
	case "enable":
		if !after.Active {
			return ErrFirewallUnavailable
		}
	case "disable":
		if after.Active {
			return ErrFirewallUnavailable
		}
	case "default-zone":
		if after.DefaultZone != operation.DefaultZone {
			return ErrFirewallConflict
		}
	case "add-service", "remove-service", "add-port", "remove-port", "add-source", "remove-source":
		wantPresent := strings.HasPrefix(operation.Action, "add-")
		if operation.Backend == "firewalld" {
			runtimePresent := firewallRuntimeRulePresent(after.RuntimeRules, operation.zoneOrDefault(after), operation)
			beforeRuntimePresent := firewallRuntimeRulePresent(before.RuntimeRules, operation.zoneOrDefault(before), operation)
			if beforeRuntimePresent != wantPresent && runtimePresent != wantPresent {
				return ErrFirewallConflict
			}
			if operation.Persist {
				persistentPresent := firewallRuntimeRulePresent(after.PersistentRules, operation.zoneOrDefault(after), operation)
				if persistentPresent != wantPresent {
					return ErrFirewallConflict
				}
			}
		} else {
			present := ufwRulePresent(after.Rules, operation)
			if beforePresent := ufwRulePresent(before.Rules, operation); beforePresent != wantPresent && present != wantPresent {
				return ErrFirewallConflict
			}
		}
	}
	return nil
}

func (operation FirewallOperation) zoneOrDefault(snapshot FirewallSnapshot) string {
	if operation.Zone != "" {
		return operation.Zone
	}
	return snapshot.DefaultZone
}

func ufwRulePresent(rules []string, operation FirewallOperation) bool {
	needle := ""
	switch {
	case strings.HasSuffix(operation.Action, "service"):
		needle = operation.Service
	case strings.HasSuffix(operation.Action, "port"):
		needle = operation.Port
	case strings.HasSuffix(operation.Action, "source"):
		needle = operation.Source
	}
	for _, rule := range rules {
		fields := strings.Fields(rule)
		for _, field := range fields {
			if field == needle || ufwFieldMatchesService(field, needle) {
				return true
			}
		}
	}
	return false
}

func ufwFieldMatchesService(field, service string) bool {
	if service == "" {
		return false
	}
	for _, protocol := range []string{"tcp", "udp"} {
		port, err := net.LookupPort(protocol, service)
		if err == nil && field == strconv.Itoa(port)+"/"+protocol {
			return true
		}
	}
	return false
}

func (adapter *firewallAdapter) applyFirewalld(ctx context.Context, operation FirewallOperation, snapshot FirewallSnapshot) (string, error) {
	if operation.Action == "enable" || operation.Action == "disable" {
		verb := "enable"
		if operation.Action == "disable" {
			verb = "disable"
		}
		if _, err := adapter.runner.Run(ctx, "systemctl", verb, "--now", "firewalld.service"); err != nil {
			return "", err
		}
		return "Service state changed through systemd. Verify management access from a fresh session before continuing.", nil
	}

	arguments, inverse, err := firewalldMutationArguments(operation, snapshot)
	if err != nil {
		return "", err
	}
	runtimeChanged := firewallRuntimeMutationChanges(snapshot, operation)
	if _, err := adapter.runner.Run(ctx, "firewall-cmd", arguments...); err != nil {
		return "", err
	}
	if operation.Action == "default-zone" {
		return "firewalld updated the default zone in runtime and persistent configuration.", nil
	}
	if operation.Persist && operation.Action != "reload" && operation.Action != "default-zone" {
		permanent := append([]string{"--permanent"}, arguments...)
		if _, err := adapter.runner.Run(ctx, "firewall-cmd", permanent...); err != nil {
			// The runtime mutation is ours and is therefore safe to undo. This
			// targeted inverse preserves all unrelated operator rules.
			if runtimeChanged {
				_, _ = adapter.runner.Run(ctx, "firewall-cmd", inverse...)
			}
			return "", err
		}
		return "Runtime and persistent firewalld state were updated with targeted rules; unmanaged rules were preserved.", nil
	}
	if operation.Action == "reload" {
		return "firewalld reloaded its configured policy; verify management access from a fresh session.", nil
	}
	return "Runtime firewalld state changed. Persistent state remains unchanged until persistence is explicitly requested.", nil
}

func firewalldMutationArguments(operation FirewallOperation, snapshot FirewallSnapshot) ([]string, []string, error) {
	zone := operation.Zone
	if zone == "" {
		zone = snapshot.DefaultZone
	}
	if operation.Action == "reload" {
		return []string{"--reload"}, nil, nil
	}
	if operation.Action == "default-zone" {
		return []string{"--set-default-zone=" + operation.DefaultZone}, []string{"--set-default-zone=" + snapshot.DefaultZone}, nil
	}
	if zone == "" {
		return nil, nil, ErrFirewallUnavailable
	}
	var kind, value string
	switch {
	case strings.HasSuffix(operation.Action, "service"):
		kind, value = "service", operation.Service
	case strings.HasSuffix(operation.Action, "port"):
		kind, value = "port", operation.Port
	case strings.HasSuffix(operation.Action, "source"):
		kind, value = "source", operation.Source
	default:
		return nil, nil, ErrInvalidFirewallOperation
	}
	verb := "add-" + kind
	if strings.HasPrefix(operation.Action, "remove-") {
		verb = "remove-" + kind
	}
	arguments := []string{"--zone=" + zone, "--" + verb + "=" + value}
	inverseVerb := "add-" + kind
	if strings.HasPrefix(operation.Action, "add-") {
		inverseVerb = "remove-" + kind
	}
	inverse := []string{"--zone=" + zone, "--" + inverseVerb + "=" + value}
	return arguments, inverse, nil
}

func (adapter *firewallAdapter) applyUFW(ctx context.Context, operation FirewallOperation) (string, error) {
	switch operation.Action {
	case "enable", "disable", "reload":
		if _, err := adapter.runner.Run(ctx, "ufw", operation.Action); err != nil {
			return "", err
		}
		if operation.Action == "disable" {
			return "UFW was disabled. Verify management access from a fresh session before continuing.", nil
		}
		return "UFW changed its active policy; verify management access from a fresh session.", nil
	case "add-port":
		if _, err := adapter.runner.Run(ctx, "ufw", "allow", operation.Port); err != nil {
			return "", err
		}
		return "UFW stores this rule in its persistent policy and applied it to the active firewall.", nil
	case "remove-port":
		if _, err := adapter.runner.Run(ctx, "ufw", "delete", "allow", operation.Port); err != nil {
			return "", err
		}
		return "UFW removed this rule from its persistent policy and active firewall.", nil
	case "add-service":
		if _, err := adapter.runner.Run(ctx, "ufw", "allow", operation.Service); err != nil {
			return "", err
		}
		return "UFW stores this service rule in its persistent policy and applied it to the active firewall.", nil
	case "remove-service":
		if _, err := adapter.runner.Run(ctx, "ufw", "delete", "allow", operation.Service); err != nil {
			return "", err
		}
		return "UFW removed this service rule from its persistent policy and active firewall.", nil
	case "add-source":
		if _, err := adapter.runner.Run(ctx, "ufw", "allow", "from", operation.Source); err != nil {
			return "", err
		}
		return "UFW stores this source rule in its persistent policy and applied it to the active firewall.", nil
	case "remove-source":
		if _, err := adapter.runner.Run(ctx, "ufw", "delete", "allow", "from", operation.Source); err != nil {
			return "", err
		}
		return "UFW removed this source rule from its persistent policy and active firewall.", nil
	default:
		return "", ErrFirewallUnavailable
	}
}

func firewallCommand(ctx context.Context, name string, arguments ...string) (string, error) {
	payload, err := networkCommand(ctx, name, arguments...)
	return string(payload), err
}
