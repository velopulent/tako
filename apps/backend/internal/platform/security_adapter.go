package platform

import (
	"context"
	"golang.org/x/sys/unix"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// SecurityCommandRunner is the command seam for SELinux and AppArmor. Tests
// inject fixture responses; production uses the same bounded executor as the
// other host adapters.
type SecurityCommandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

type SecurityStrategy interface {
	Read(context.Context) (SecurityStatus, error)
	Apply(context.Context, SecurityOperation) (SecurityStatus, error)
}

type securityAdapter struct {
	runner SecurityCommandRunner
}

type systemSecurityCommandRunner struct{}

func (systemSecurityCommandRunner) Run(ctx context.Context, name string, arguments ...string) (string, error) {
	return runSecurityCommand(ctx, nil, name, arguments...)
}

// NewSecurityStrategy returns a security adapter backed by runner. A nil
// runner selects the bounded production command executor.
func NewSecurityStrategy(runner SecurityCommandRunner) SecurityStrategy {
	if runner == nil {
		runner = systemSecurityCommandRunner{}
	}
	return &securityAdapter{runner: runner}
}

func (adapter *securityAdapter) Read(ctx context.Context) (SecurityStatus, error) {
	status := SecurityStatus{Findings: []SecurityFinding{}}
	status.SELinux = readSELinuxStatusWithRunner(ctx, adapter.runner)
	status.AppArmor = readAppArmorStatusWithRunner(ctx, adapter.runner)
	if status.SELinux.Mode == "Enforcing" || status.SELinux.Mode == "Permissive" {
		status.Active = "SELinux"
	}
	if status.AppArmor.Userspace && (status.AppArmor.KernelPresent || len(status.AppArmor.Profiles) > 0) {
		if status.Active != "" {
			status.Active += "+AppArmor"
		} else {
			status.Active = "AppArmor"
		}
	}
	if status.SELinux.KernelPresent && !status.SELinux.Userspace {
		status.Findings = append(status.Findings, SecurityFinding{
			Framework: "SELinux", Kind: "userspace", Subject: "SELinux tools",
			Message:  "SELinux kernel support is present but userspace inspection tools are unavailable.",
			Severity: "warning", Guidance: "Install policycoreutils to inspect labels and booleans.",
		})
	}
	if status.SELinux.Mode == "Permissive" {
		status.Findings = append(status.Findings, SecurityFinding{
			Framework: "SELinux", Kind: "mode", Subject: "SELinux",
			Message:  "SELinux is installed but running in permissive mode.",
			Severity: "warning", Guidance: "Enable enforcing mode through the host's documented policy workflow.",
		})
	}
	if len(status.SELinux.Denials) > 0 {
		status.Findings = append(status.Findings, SecurityFinding{
			Framework: "SELinux", Kind: "denial", Subject: "Recent AVC denials",
			Message:  "Recent SELinux AVC denials require policy review before remediation.",
			Severity: "warning", Guidance: "Review the denial subject and verify the requested access is expected.",
		})
	}
	if status.AppArmor.KernelPresent && !status.AppArmor.Userspace {
		status.Findings = append(status.Findings, SecurityFinding{
			Framework: "AppArmor", Kind: "userspace", Subject: "AppArmor tools",
			Message:  "AppArmor kernel support is present but aa-status is unavailable.",
			Severity: "warning", Guidance: "Install apparmor-utils to inspect profiles.",
		})
	}
	if len(status.AppArmor.Denials) > 0 {
		status.Findings = append(status.Findings, SecurityFinding{
			Framework: "AppArmor", Kind: "denial", Subject: "Recent AppArmor denials",
			Message:  "Recent AppArmor denials require profile review before remediation.",
			Severity: "warning", Guidance: "Review the denied operation and update only the owning profile.",
		})
	}
	status.Fingerprint = securityFingerprint(status)
	return status, nil
}

func (adapter *securityAdapter) Apply(ctx context.Context, operation SecurityOperation) (SecurityStatus, error) {
	if err := ValidateSecurityOperation(operation); err != nil {
		return SecurityStatus{}, err
	}
	current, err := adapter.Read(ctx)
	if err != nil {
		return SecurityStatus{}, err
	}
	if current.Fingerprint != operation.ExpectedFingerprint {
		return SecurityStatus{}, ErrSecurityConflict
	}

	command, arguments, err := adapter.mutationCommand(current, operation)
	if err != nil {
		return SecurityStatus{}, err
	}
	if err := adapter.execute(ctx, operation, command, arguments); err != nil {
		return SecurityStatus{}, err
	}
	updated, err := adapter.Read(ctx)
	if err != nil {
		return SecurityStatus{}, err
	}
	return updated, nil
}

func (adapter *securityAdapter) mutationCommand(current SecurityStatus, operation SecurityOperation) (string, []string, error) {
	switch operation.Action {
	case "selinux-boolean":
		if current.SELinux.Mode == "Disabled" || !current.SELinux.Userspace {
			return "", nil, ErrSecurityUnavailable
		}
		known := false
		for _, boolean := range current.SELinux.Booleans {
			if strings.HasPrefix(boolean, operation.Boolean+"=") {
				known = true
				break
			}
		}
		if !known {
			return "", nil, ErrSecurityUnsafe
		}
		value := "off"
		if operation.Value {
			value = "on"
		}
		return "setsebool", []string{"-P", operation.Boolean, value}, nil
	case "selinux-restorecon":
		if !current.SELinux.Userspace || !safeRestoreconFilesystemPath(operation.Path) {
			return "", nil, ErrSecurityUnavailable
		}
		return "restorecon", []string{"-v", "--", operation.Path}, nil
	case "apparmor-enforce", "apparmor-complain":
		if !current.AppArmor.Userspace {
			return "", nil, ErrSecurityUnavailable
		}
		if !contains(current.AppArmor.Profiles, operation.Profile) {
			return "", nil, ErrSecurityUnsafe
		}
		return "aa-" + strings.TrimPrefix(operation.Action, "apparmor-"), []string{"--", operation.Profile}, nil
	case "apparmor-load":
		if !current.AppArmor.Userspace {
			return "", nil, ErrSecurityUnavailable
		}
		if !trustedAppArmorProfile(operation.Path) {
			return "", nil, ErrSecurityUnsafe
		}
		return "apparmor_parser", []string{"-r", "-I", "/etc/apparmor.d"}, nil
	default:
		return "", nil, ErrInvalidSecurityOperation
	}
}

func safeRestoreconFilesystemPath(path string) bool { return safeRestoreconPath(path) }

func securityCommand(ctx context.Context, name string, arguments ...string) (string, error) {
	return runSecurityCommand(ctx, nil, name, arguments...)
}

func runSecurityCommand(ctx context.Context, input io.Reader, name string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, arguments...)
	command.Stdin = input
	output, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err = command.Start(); err != nil {
		return "", err
	}
	payload, readErr := readBounded(output, 2<<20)
	if readErr != nil {
		_ = command.Process.Kill()
	}
	err = command.Wait()
	if readErr != nil {
		return "", readErr
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return string(payload), err
}

func (adapter *securityAdapter) execute(ctx context.Context, operation SecurityOperation, command string, arguments []string) error {
	if operation.Action != "apparmor-load" && operation.Action != "selinux-restorecon" {
		_, err := adapter.runner.Run(ctx, command, arguments...)
		return err
	}
	root, err := openFileRoot("/")
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.Open(strings.TrimPrefix(operation.Path, "/"))
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if operation.Action == "selinux-restorecon" {
		if !info.Mode().IsRegular() && !info.IsDir() {
			return ErrSecurityUnsafe
		}
		label, err := adapter.runner.Run(ctx, "matchpathcon", "-n", "--", operation.Path)
		if err != nil {
			return err
		}
		label = strings.TrimSpace(label)
		if label == "" || len(label) > 4096 || strings.ContainsAny(label, "\x00\r\n") || label == "<<none>>" {
			return ErrSecurityUnsafe
		}
		return unix.Fsetxattr(int(file.Fd()), "security.selinux", []byte(label+"\x00"), 0)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Size() > 1<<20 || info.Mode().Perm()&0o022 != 0 || !ok || stat.Uid != 0 {
		return ErrSecurityUnsafe
	}
	// Parsing stdin keeps the checked file pinned while policy includes retain their native base.
	_, err = runSecurityCommand(ctx, io.LimitReader(file, (1<<20)+1), command, arguments...)
	return err
}
