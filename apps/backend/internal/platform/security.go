package platform

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrInvalidSecurityOperation = errors.New("invalid security operation")
	ErrSecurityConflict         = errors.New("security state changed")
	ErrSecurityUnavailable      = errors.New("security framework unavailable")
	ErrSecurityUnsafe           = errors.New("security remediation is unsafe")
)

type SecurityFinding struct {
	Framework string `json:"framework"`
	Kind      string `json:"kind"`
	Subject   string `json:"subject"`
	Message   string `json:"message"`
	Severity  string `json:"severity"`
	Guidance  string `json:"guidance,omitempty"`
}

type SELinuxStatus struct {
	KernelPresent bool     `json:"kernelPresent"`
	Userspace     bool     `json:"userspace"`
	Mode          string   `json:"mode"`
	Policy        string   `json:"policy,omitempty"`
	Booleans      []string `json:"booleans"`
	Denials       []string `json:"denials"`
}

type AppArmorStatus struct {
	KernelPresent bool              `json:"kernelPresent"`
	Userspace     bool              `json:"userspace"`
	Profiles      []string          `json:"profiles"`
	ProfileModes  map[string]string `json:"profileModes,omitempty"`
	Denials       []string          `json:"denials"`
}

type SecurityStatus struct {
	SELinux     SELinuxStatus     `json:"selinux"`
	AppArmor    AppArmorStatus    `json:"apparmor"`
	Active      string            `json:"active"`
	Findings    []SecurityFinding `json:"findings"`
	Fingerprint string            `json:"fingerprint"`
}

type SecurityOperation struct {
	Action              string `json:"action"`
	Framework           string `json:"framework"`
	Boolean             string `json:"boolean,omitempty"`
	Value               bool   `json:"value,omitempty"`
	Path                string `json:"path,omitempty"`
	Profile             string `json:"profile,omitempty"`
	ExpectedFingerprint string `json:"expectedFingerprint,omitempty"`
	Confirmation        string `json:"confirmation,omitempty"`
}

var (
	securityNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	profileNamePattern  = regexp.MustCompile(`^[A-Za-z0-9_./:@+-]+$`)
	getseboolPattern    = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s+-->\s+(on|off)\s*$`)
	semanageBoolPattern = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s+\(\s*(on|off)\s*,\s*(on|off)\s*\)`)
)

func ReadSecurityStatus(ctx context.Context) (SecurityStatus, error) {
	return NewSecurityStrategy(nil).Read(ctx)
}

func ValidateSecurityOperation(operation SecurityOperation) error {
	actions := map[string]bool{
		"inspect":            true,
		"selinux-boolean":    true,
		"selinux-restorecon": true,
		"apparmor-enforce":   true,
		"apparmor-complain":  true,
		"apparmor-load":      true,
	}
	if !actions[operation.Action] || (operation.Framework != "SELinux" && operation.Framework != "AppArmor") {
		return ErrInvalidSecurityOperation
	}
	if len(operation.Boolean) > 128 || len(operation.Path) > 4096 || len(operation.Profile) > 256 || len(operation.ExpectedFingerprint) > 128 || len(operation.Confirmation) > 128 || strings.ContainsAny(operation.Boolean+operation.Path+operation.Profile+operation.ExpectedFingerprint+operation.Confirmation, "\x00\r\n") {
		return ErrInvalidSecurityOperation
	}
	if operation.Action == "inspect" {
		if operation.Boolean != "" || operation.Path != "" || operation.Profile != "" {
			return ErrInvalidSecurityOperation
		}
		return nil
	}
	if operation.ExpectedFingerprint == "" || operation.Confirmation != "CONFIRM NARROW SECURITY CHANGE" {
		return ErrInvalidSecurityOperation
	}
	switch operation.Action {
	case "selinux-boolean":
		if operation.Framework != "SELinux" || !securityNamePattern.MatchString(operation.Boolean) || operation.Path != "" || operation.Profile != "" {
			return ErrInvalidSecurityOperation
		}
	case "selinux-restorecon":
		if operation.Framework != "SELinux" || !safeRestoreconPath(operation.Path) || operation.Boolean != "" || operation.Profile != "" {
			return ErrSecurityUnsafe
		}
	case "apparmor-enforce", "apparmor-complain":
		if operation.Framework != "AppArmor" || !profileNamePattern.MatchString(operation.Profile) || strings.Contains(operation.Profile, "..") || strings.HasPrefix(operation.Profile, "-") || operation.Path != "" || operation.Boolean != "" {
			return ErrInvalidSecurityOperation
		}
	case "apparmor-load":
		if operation.Framework != "AppArmor" || operation.Boolean != "" || operation.Profile != "" || !trustedAppArmorProfile(operation.Path) {
			return ErrSecurityUnsafe
		}
	default:
		return ErrInvalidSecurityOperation
	}
	return nil
}

func safeRestoreconPath(path string) bool {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsAny(path, "*?[]{}") {
		return false
	}
	for _, blocked := range []string{"/proc", "/sys", "/dev", "/run"} {
		if filePathWithin(blocked, path) {
			return false
		}
	}
	return true
}

func PreviewSecurityOperation(ctx context.Context, operation SecurityOperation) (SecurityStatus, error) {
	operation = SecurityOperation{Action: "inspect", Framework: operation.Framework}
	if err := ValidateSecurityOperation(operation); err != nil {
		return SecurityStatus{}, err
	}
	return ReadSecurityStatus(ctx)
}

func ApplySecurityOperation(ctx context.Context, operation SecurityOperation) (SecurityStatus, error) {
	return NewSecurityStrategy(nil).Apply(ctx, operation)
}

type securityFingerprintInput struct {
	SELinux  SELinuxStatus
	AppArmor AppArmorStatus
	Active   string
}

func securityFingerprint(status SecurityStatus) string {
	status.SELinux.Booleans = sortedUnique(status.SELinux.Booleans)
	status.SELinux.Denials = nil
	status.AppArmor.Profiles = sortedUnique(status.AppArmor.Profiles)
	status.AppArmor.Denials = nil
	payload, _ := json.Marshal(securityFingerprintInput{SELinux: status.SELinux, AppArmor: status.AppArmor, Active: status.Active})
	return fingerprintBytes(payload)
}

func normalizeSELinuxMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "enforcing":
		return "Enforcing"
	case "permissive":
		return "Permissive"
	case "disabled":
		return "Disabled"
	default:
		return strings.TrimSpace(value)
	}
}

func parseSELinuxBooleans(getsebool, semanage string) []string {
	values := map[string]string{}
	for _, line := range strings.Split(getsebool, "\n") {
		match := getseboolPattern.FindStringSubmatch(line)
		if len(match) == 3 {
			values[match[1]] = match[2]
		}
	}
	for _, line := range strings.Split(semanage, "\n") {
		match := semanageBoolPattern.FindStringSubmatch(line)
		if len(match) == 4 {
			if _, exists := values[match[1]]; !exists {
				values[match[1]] = match[2]
			}
		}
	}
	result := make([]string, 0, len(values))
	for name, value := range values {
		result = append(result, name+"="+value)
	}
	sort.Strings(result)
	return result
}

func parsePolicyDenials(output string, framework string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		matched := false
		if framework == "SELinux" {
			matched = strings.Contains(lower, "type=avc") || strings.Contains(lower, "avc:") && strings.Contains(lower, "denied")
		} else {
			matched = strings.Contains(lower, "apparmor=") && strings.Contains(lower, "denied") || strings.Contains(lower, "apparmor.*denied")
		}
		if !matched {
			continue
		}
		line = strings.Join(strings.Fields(line), " ")
		if len(line) > 1024 {
			line = line[:1024]
		}
		if !seen[line] {
			seen[line] = true
			result = append(result, line)
		}
		if len(result) >= 256 {
			break
		}
	}
	return result
}

func readSELinuxStatus(ctx context.Context) SELinuxStatus {
	return readSELinuxStatusWithRunner(ctx, systemSecurityCommandRunner{})
}

func readSELinuxStatusWithRunner(ctx context.Context, runner SecurityCommandRunner) SELinuxStatus {
	status := SELinuxStatus{KernelPresent: fileExists("/sys/fs/selinux"), Booleans: []string{}, Denials: []string{}}
	getenforceOutput, getenforceErr := runner.Run(ctx, "getenforce")
	if getenforceErr == nil {
		mode := normalizeSELinuxMode(firstLine(getenforceOutput))
		if mode != "" {
			status.Userspace = true
			status.Mode = mode
		}
	}
	if output, err := runner.Run(ctx, "sestatus"); err == nil {
		status.Userspace = true
		for _, line := range strings.Split(output, "\n") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) != 2 {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(fields[0]))
			value := strings.TrimSpace(fields[1])
			switch key {
			case "selinux status":
				if strings.EqualFold(value, "disabled") {
					status.Mode = "Disabled"
				}
			case "current mode":
				status.Mode = normalizeSELinuxMode(value)
			case "loaded policy name", "selinux policy":
				status.Policy = value
			}
		}
	}
	getsebool, _ := runner.Run(ctx, "getsebool", "-a")
	semanage, _ := runner.Run(ctx, "semanage", "boolean", "-l")
	status.Booleans = parseSELinuxBooleans(getsebool, semanage)
	status.Denials = readSecurityDenials(ctx, runner, "SELinux")
	return status
}

func readAppArmorStatus(ctx context.Context) AppArmorStatus {
	return readAppArmorStatusWithRunner(ctx, systemSecurityCommandRunner{})
}

func readAppArmorStatusWithRunner(ctx context.Context, runner SecurityCommandRunner) AppArmorStatus {
	status := AppArmorStatus{KernelPresent: fileExists("/sys/module/apparmor"), Profiles: []string{}, ProfileModes: map[string]string{}, Denials: []string{}}
	if output, err := runner.Run(ctx, "aa-status", "--json"); err == nil {
		var document struct {
			Profiles map[string]string `json:"profiles"`
		}
		if json.Unmarshal([]byte(output), &document) == nil && document.Profiles != nil {
			status.Userspace = true
			for profile, mode := range document.Profiles {
				status.Profiles = append(status.Profiles, profile)
				status.ProfileModes[profile] = mode
			}
		}
	}
	sort.Strings(status.Profiles)
	status.Denials = readSecurityDenials(ctx, runner, "AppArmor")
	return status
}

func parseAppArmorProfiles(output string) []string {
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "profiles are ") || strings.Contains(line, "processes are ") {
			continue
		}
		if strings.HasSuffix(line, ":") {
			continue
		}
		result = append(result, line)
	}
	return sortedUnique(result)
}

func readSecurityDenials(ctx context.Context, runner SecurityCommandRunner, framework string) []string {
	if framework == "SELinux" {
		if output, err := runner.Run(ctx, "ausearch", "-m", "avc", "-ts", "recent", "-i"); err == nil {
			if denials := parsePolicyDenials(output, framework); len(denials) > 0 {
				return denials
			}
		}
		output, _ := runner.Run(ctx, "journalctl", "-k", "--no-pager", "-g", "avc:.*denied", "-n", "256")
		return parsePolicyDenials(output, framework)
	}
	output, _ := runner.Run(ctx, "journalctl", "-k", "--no-pager", "-g", `apparmor="DENIED"`, "-n", "256")
	return parsePolicyDenials(output, framework)
}

func trustedAppArmorProfile(path string) bool {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
		return false
	}
	for _, root := range []string{"/etc/apparmor.d", "/usr/lib/apparmor.d"} {
		if path != root && filePathWithin(root, path) {
			return true
		}
	}
	return false
}

func securityErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidSecurityOperation):
		return "invalid-security-operation"
	case errors.Is(err, ErrSecurityConflict):
		return "security-conflict"
	case errors.Is(err, ErrSecurityUnsafe):
		return "security-unsafe"
	case errors.Is(err, ErrSecurityUnavailable):
		return "security-unavailable"
	default:
		return "security-operation-failed"
	}
}
