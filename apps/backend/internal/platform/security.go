package platform

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
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
	KernelPresent bool     `json:"kernelPresent"`
	Userspace     bool     `json:"userspace"`
	Profiles      []string `json:"profiles"`
	Denials       []string `json:"denials"`
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

func ReadSecurityStatus(ctx context.Context) (SecurityStatus, error) {
	status := SecurityStatus{Findings: []SecurityFinding{}}
	status.SELinux = readSELinuxStatus(ctx)
	status.AppArmor = readAppArmorStatus(ctx)
	if status.SELinux.Mode != "" && status.SELinux.Mode != "Disabled" {
		status.Active = "SELinux"
	}
	if len(status.AppArmor.Profiles) > 0 {
		if status.Active != "" {
			status.Active = "SELinux+AppArmor"
		} else {
			status.Active = "AppArmor"
		}
	}
	if status.SELinux.KernelPresent && !status.SELinux.Userspace {
		status.Findings = append(status.Findings, SecurityFinding{Framework: "SELinux", Kind: "userspace", Subject: "SELinux tools", Message: "SELinux kernel support is present but userspace inspection tools are unavailable.", Severity: "warning", Guidance: "Install policycoreutils to inspect labels and booleans."})
	}
	if status.AppArmor.KernelPresent && !status.AppArmor.Userspace {
		status.Findings = append(status.Findings, SecurityFinding{Framework: "AppArmor", Kind: "userspace", Subject: "AppArmor tools", Message: "AppArmor kernel support is present but aa-status is unavailable.", Severity: "warning", Guidance: "Install apparmor-utils to inspect profiles."})
	}
	status.Fingerprint = fingerprintBytes([]byte(status.Active + "|" + status.SELinux.Mode + "|" + strings.Join(status.AppArmor.Profiles, "\n")))
	return status, nil
}

func readSELinuxStatus(ctx context.Context) SELinuxStatus {
	status := SELinuxStatus{KernelPresent: fileExists("/sys/fs/selinux"), Booleans: []string{}, Denials: []string{}}
	output, err := securityCommand(ctx, "getenforce")
	if err == nil {
		status.Userspace = true
		status.Mode = strings.TrimSpace(firstLine(output))
	}
	if output, err := securityCommand(ctx, "sestatus"); err == nil {
		for _, line := range strings.Split(output, "\n") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) != 2 {
				continue
			}
			switch strings.TrimSpace(fields[0]) {
			case "SELinux policy":
				status.Policy = strings.TrimSpace(fields[1])
			case "Current mode":
				if status.Mode == "" {
					status.Mode = strings.TrimSpace(fields[1])
				}
			}
		}
	}
	if output, err := securityCommand(ctx, "semanage", "boolean", "-l"); err == nil {
		for _, line := range boundedLines(output, 512) {
			fields := strings.Fields(line)
			if len(fields) >= 2 && (fields[1] == "on" || fields[1] == "off") {
				status.Booleans = append(status.Booleans, fields[0]+"="+fields[1])
			}
		}
	}
	return status
}

func readAppArmorStatus(ctx context.Context) AppArmorStatus {
	status := AppArmorStatus{KernelPresent: fileExists("/sys/module/apparmor"), Profiles: []string{}, Denials: []string{}}
	output, err := securityCommand(ctx, "aa-status", "--profiled")
	if err == nil {
		status.Userspace = true
		for _, line := range boundedLines(output, 2048) {
			line = strings.TrimSpace(line)
			if line != "" {
				status.Profiles = append(status.Profiles, line)
			}
		}
	}
	return status
}

func ValidateSecurityOperation(operation SecurityOperation) error {
	actions := map[string]bool{"inspect": true, "selinux-boolean": true, "selinux-restorecon": true, "apparmor-enforce": true, "apparmor-complain": true, "apparmor-load": true}
	if !actions[operation.Action] || (operation.Framework != "SELinux" && operation.Framework != "AppArmor") || len(operation.Boolean) > 128 || len(operation.Path) > 4096 || len(operation.Profile) > 256 || len(operation.ExpectedFingerprint) > 128 || strings.ContainsAny(operation.Boolean+operation.Path+operation.Profile, "\x00\r\n") {
		return ErrInvalidSecurityOperation
	}
	if operation.Action == "inspect" {
		return nil
	}
	if operation.ExpectedFingerprint == "" || operation.Confirmation != "CONFIRM NARROW SECURITY CHANGE" {
		return ErrInvalidSecurityOperation
	}
	if operation.Action == "selinux-boolean" {
		if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(operation.Boolean) {
			return ErrInvalidSecurityOperation
		}
	}
	if operation.Action == "selinux-restorecon" {
		if operation.Path == "" || operation.Path == "/" || filepath.Clean(operation.Path) != operation.Path {
			return ErrSecurityUnsafe
		}
	}
	if strings.HasPrefix(operation.Action, "apparmor-") && !regexp.MustCompile(`^[a-zA-Z0-9_./-]+$`).MatchString(operation.Profile) {
		return ErrInvalidSecurityOperation
	}
	return nil
}

func PreviewSecurityOperation(ctx context.Context, operation SecurityOperation) (SecurityStatus, error) {
	operation.Action = "inspect"
	if err := ValidateSecurityOperation(operation); err != nil {
		return SecurityStatus{}, err
	}
	return ReadSecurityStatus(ctx)
}

func ApplySecurityOperation(ctx context.Context, operation SecurityOperation) (SecurityStatus, error) {
	if err := ValidateSecurityOperation(operation); err != nil {
		return SecurityStatus{}, err
	}
	current, err := ReadSecurityStatus(ctx)
	if err != nil {
		return SecurityStatus{}, err
	}
	if current.Fingerprint != operation.ExpectedFingerprint {
		return SecurityStatus{}, ErrSecurityConflict
	}
	var command string
	var arguments []string
	switch operation.Action {
	case "selinux-boolean":
		if current.SELinux.Mode == "Disabled" || !current.SELinux.Userspace {
			return SecurityStatus{}, ErrSecurityUnavailable
		}
		command, arguments = "setsebool", []string{"-P", operation.Boolean, map[bool]string{true: "on", false: "off"}[operation.Value]}
	case "selinux-restorecon":
		if !current.SELinux.Userspace {
			return SecurityStatus{}, ErrSecurityUnavailable
		}
		command, arguments = "restorecon", []string{"-v", "--", operation.Path}
	case "apparmor-enforce", "apparmor-complain":
		if !current.AppArmor.Userspace {
			return SecurityStatus{}, ErrSecurityUnavailable
		}
		command, arguments = "aa-"+strings.TrimPrefix(operation.Action, "apparmor-"), []string{"--", operation.Profile}
	case "apparmor-load":
		if !current.AppArmor.Userspace || operation.Path == "" {
			return SecurityStatus{}, ErrSecurityUnavailable
		}
		command, arguments = "apparmor_parser", []string{"-r", "--", operation.Path}
	default:
		return SecurityStatus{}, ErrInvalidSecurityOperation
	}
	if _, err := securityCommand(ctx, command, arguments...); err != nil {
		return SecurityStatus{}, err
	}
	updated, err := ReadSecurityStatus(ctx)
	if err != nil {
		return SecurityStatus{}, err
	}
	return updated, nil
}

func securityCommand(ctx context.Context, name string, arguments ...string) (string, error) {
	payload, err := networkCommand(ctx, name, arguments...)
	return string(payload), err
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
