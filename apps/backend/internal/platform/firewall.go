package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidFirewallOperation = errors.New("invalid firewall operation")
	ErrFirewallConflict         = errors.New("firewall state changed")
	ErrFirewallOwnership        = errors.New("firewall ownership is conflicted")
	ErrFirewallUnavailable      = errors.New("firewall adapter unavailable")
	ErrFirewallAccessRisk       = errors.New("firewall operation could lock out management access")
	ErrFirewallCheckpoint       = errors.New("firewall rollback checkpoint is invalid or expired")
)

// FirewallSnapshot contains the active firewall state and the two firewalld
// rule stores. Rules is retained as the runtime rule list for compatibility
// with the first beta contract. UFW has one effective store, so its runtime
// and persistent lists are the same view.
type FirewallSnapshot struct {
	Backend               string   `json:"backend"`
	Active                bool     `json:"active"`
	Version               string   `json:"version,omitempty"`
	DefaultZone           string   `json:"defaultZone,omitempty"`
	PersistentDefaultZone string   `json:"persistentDefaultZone,omitempty"`
	Zones                 []string `json:"zones"`
	Rules                 []string `json:"rules"`
	RuntimeRules          []string `json:"runtimeRules,omitempty"`
	PersistentRules       []string `json:"persistentRules,omitempty"`
	Synchronized          bool     `json:"synchronized"`
	Conflicted            bool     `json:"conflicted"`
	ReadOnly              bool     `json:"readOnly"`
	Reason                string   `json:"reason,omitempty"`
	Fingerprint           string   `json:"fingerprint"`
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
	RollbackSeconds     int    `json:"rollbackSeconds,omitempty"`
	Checkpoint          string `json:"checkpoint,omitempty"`
	RollbackToken       string `json:"rollbackToken,omitempty"`
}

type FirewallState struct {
	Snapshot         FirewallSnapshot `json:"snapshot"`
	Action           string           `json:"action"`
	Applied          bool             `json:"applied"`
	Committed        bool             `json:"committed,omitempty"`
	RollbackRequired bool             `json:"rollbackRequired,omitempty"`
	Checkpoint       string           `json:"checkpoint,omitempty"`
	RollbackToken    string           `json:"rollbackToken,omitempty"`
	RollbackDeadline time.Time        `json:"rollbackDeadline,omitempty"`
	Warning          string           `json:"warning,omitempty"`
}

var firewallNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:@+-]{0,127}$`)

// ReadFirewallStatus reads whichever supported manager is active. The
// command executor is kept behind FirewallStrategy so tests can use command
// fixtures and production always uses the bounded networkCommand runner.
func ReadFirewallStatus(ctx context.Context) (FirewallSnapshot, error) {
	return NewFirewallStrategy(nil).Read(ctx)
}

func ValidateFirewallOperation(operation FirewallOperation) error {
	backends := map[string]bool{"auto": true, "firewalld": true, "UFW": true}
	actions := map[string]bool{
		"preview":        true,
		"enable":         true,
		"disable":        true,
		"default-zone":   true,
		"add-service":    true,
		"remove-service": true,
		"add-port":       true,
		"remove-port":    true,
		"add-source":     true,
		"remove-source":  true,
		"reload":         true,
		"commit":         true,
		"rollback":       true,
	}
	if !backends[operation.Backend] || !actions[operation.Action] {
		return ErrInvalidFirewallOperation
	}
	if len(operation.Zone) > 128 || len(operation.Service) > 128 || len(operation.Port) > 32 || len(operation.Source) > 128 || len(operation.DefaultZone) > 128 || len(operation.ExpectedFingerprint) > 128 || len(operation.Confirmation) > 128 || len(operation.Checkpoint) > 256 || len(operation.RollbackToken) > 256 || operation.RollbackSeconds < 0 || operation.RollbackSeconds > 600 || strings.ContainsAny(operation.Zone+operation.Service+operation.Port+operation.Source+operation.DefaultZone+operation.ExpectedFingerprint+operation.Confirmation+operation.Checkpoint+operation.RollbackToken, "\x00\r\n") {
		return ErrInvalidFirewallOperation
	}
	for _, value := range []string{operation.Zone, operation.Service, operation.DefaultZone} {
		if value != "" && !firewallNamePattern.MatchString(value) {
			return ErrInvalidFirewallOperation
		}
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
	if operation.Source != "" {
		if ip := net.ParseIP(operation.Source); ip == nil {
			if _, _, err := net.ParseCIDR(operation.Source); err != nil {
				return ErrInvalidFirewallOperation
			}
		}
	}

	switch operation.Action {
	case "default-zone":
		if operation.DefaultZone == "" || operation.Zone != "" || operation.Service != "" || operation.Port != "" || operation.Source != "" {
			return ErrInvalidFirewallOperation
		}
	case "add-service", "remove-service":
		if operation.Service == "" || operation.Port != "" || operation.Source != "" || operation.DefaultZone != "" {
			return ErrInvalidFirewallOperation
		}
	case "add-port", "remove-port":
		if operation.Port == "" || operation.Service != "" || operation.Source != "" || operation.DefaultZone != "" {
			return ErrInvalidFirewallOperation
		}
	case "add-source", "remove-source":
		if operation.Source == "" || operation.Service != "" || operation.Port != "" || operation.DefaultZone != "" {
			return ErrInvalidFirewallOperation
		}
	case "enable", "disable", "reload":
		if operation.Zone != "" || operation.Service != "" || operation.Port != "" || operation.Source != "" || operation.DefaultZone != "" {
			return ErrInvalidFirewallOperation
		}
	case "commit", "rollback":
		if operation.Checkpoint == "" || operation.RollbackToken == "" || operation.Zone != "" || operation.Service != "" || operation.Port != "" || operation.Source != "" || operation.DefaultZone != "" || operation.ExpectedFingerprint != "" {
			return ErrInvalidFirewallOperation
		}
	}
	if operation.Backend == "UFW" && operation.Action == "default-zone" {
		return ErrInvalidFirewallOperation
	}
	if operation.ExpectedFingerprint == "" && operation.Action != "preview" && operation.Action != "commit" && operation.Action != "rollback" {
		return ErrInvalidFirewallOperation
	}
	if operation.Action != "preview" && operation.Confirmation != "CONFIRM FIREWALL CHANGE" && operation.Confirmation != "CONFIRM FIREWALL ACCESS" {
		return ErrInvalidFirewallOperation
	}
	if firewallAccessRisk(operation) && operation.RollbackSeconds != 0 && operation.RollbackSeconds < 30 {
		return ErrFirewallAccessRisk
	}
	if firewallAccessRisk(operation) && operation.Confirmation != "CONFIRM FIREWALL ACCESS" {
		return ErrFirewallAccessRisk
	}
	return nil
}

func firewallAccessRisk(operation FirewallOperation) bool {
	if operation.Action == "disable" {
		return true
	}
	return operation.Action == "remove-service" || operation.Action == "remove-port" || operation.Action == "remove-source"
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
	return FirewallState{Snapshot: snapshot, Action: "preview", Warning: firewallPreviewWarning(snapshot)}, nil
}

func ApplyFirewallOperation(ctx context.Context, operation FirewallOperation) (FirewallState, error) {
	return NewFirewallStrategy(nil).Apply(ctx, operation)
}

func firewallPreviewWarning(snapshot FirewallSnapshot) string {
	if snapshot.Backend == "firewalld" && !snapshot.Synchronized {
		return "Runtime and persistent firewalld state differ; choose persistence explicitly before applying a change."
	}
	return "Firewall changes are checked against the displayed fingerprint and management access requires explicit confirmation."
}

type firewallFingerprintInput struct {
	Backend               string
	Active                bool
	DefaultZone           string
	PersistentDefaultZone string
	Zones                 []string
	RuntimeRules          []string
	PersistentRules       []string
	Synchronized          bool
	Conflicted            bool
	ReadOnly              bool
}

func firewallFingerprint(snapshot FirewallSnapshot) string {
	zones := sortedUnique(snapshot.Zones)
	runtimeRules := append([]string(nil), snapshot.RuntimeRules...)
	persistentRules := append([]string(nil), snapshot.PersistentRules...)
	if len(runtimeRules) == 0 {
		runtimeRules = append([]string(nil), snapshot.Rules...)
	}
	if snapshot.Backend == "firewalld" {
		sort.Strings(runtimeRules)
		sort.Strings(persistentRules)
	}
	payload, _ := json.Marshal(firewallFingerprintInput{
		Backend: snapshot.Backend, Active: snapshot.Active, DefaultZone: snapshot.DefaultZone,
		PersistentDefaultZone: snapshot.PersistentDefaultZone, Zones: zones,
		RuntimeRules: runtimeRules, PersistentRules: persistentRules,
		Synchronized: snapshot.Synchronized, Conflicted: snapshot.Conflicted, ReadOnly: snapshot.ReadOnly,
	})
	return fingerprintBytes(payload)
}

func sortedUnique(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	if len(result) == 0 {
		return []string{}
	}
	out := result[:1]
	for _, value := range result[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func parseFirewalldZones(active, all string) []string {
	result := []string{}
	for _, line := range strings.Split(active, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(line, " ") || strings.Contains(trimmed, ":") || !firewallNamePattern.MatchString(trimmed) {
			continue
		}
		result = append(result, trimmed)
	}
	for _, value := range strings.Fields(all) {
		if firewallNamePattern.MatchString(value) {
			result = append(result, value)
		}
	}
	return sortedUnique(result)
}

func parseFirewalldRules(zone, output string) []string {
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, zone+" ") || line == zone {
			continue
		}
		if strings.Contains(line, ":") || strings.HasPrefix(line, "rule ") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				result = append(result, zone+" "+strings.Join(fields, " "))
			}
		}
	}
	return sortedUnique(result)
}

func parseUFWRules(output string) []string {
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" || strings.HasPrefix(trimmed, "Status:") || strings.HasPrefix(trimmed, "Logging:") || strings.HasPrefix(trimmed, "Default:") || strings.HasPrefix(trimmed, "New profiles:") || (strings.HasPrefix(lower, "to ") && strings.Contains(lower, "action") && strings.Contains(lower, "from")) || strings.HasPrefix(trimmed, "--") {
			continue
		}
		result = append(result, strings.Join(strings.Fields(trimmed), " "))
	}
	return result
}

// firewallRuntimeMutationChanges reports whether a firewalld runtime command
// would alter a value that was present in the observed snapshot. It prevents
// a failed persistent write from undoing an operator's pre-existing rule.
func firewallRuntimeMutationChanges(snapshot FirewallSnapshot, operation FirewallOperation) bool {
	switch operation.Action {
	case "default-zone":
		return snapshot.DefaultZone != operation.DefaultZone
	case "add-service", "remove-service", "add-port", "remove-port", "add-source", "remove-source":
		zone := operation.Zone
		if zone == "" {
			zone = snapshot.DefaultZone
		}
		present := firewallRuntimeRulePresent(snapshot.RuntimeRules, zone, operation)
		return strings.HasPrefix(operation.Action, "add-") != present
	default:
		return false
	}
}

func firewallRuntimeRulePresent(rules []string, zone string, operation FirewallOperation) bool {
	key, value := "", ""
	switch {
	case strings.HasSuffix(operation.Action, "service"):
		key, value = "services", operation.Service
	case strings.HasSuffix(operation.Action, "port"):
		key, value = "ports", operation.Port
	case strings.HasSuffix(operation.Action, "source"):
		key, value = "sources", operation.Source
	default:
		return false
	}
	prefix := zone + " " + key + ":"
	for _, rule := range rules {
		if !strings.HasPrefix(rule, prefix) {
			continue
		}
		for _, existing := range strings.Fields(strings.TrimSpace(strings.TrimPrefix(rule, prefix))) {
			if existing == value {
				return true
			}
		}
	}
	return false
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
