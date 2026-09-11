package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

func confValue(content, key string) string {
	pattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*(\S+)\s*$`)
	matches := pattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

// KpatchStatus reports kernel live-patch state.
type KpatchStatus struct {
	Supported bool     `json:"supported"`
	Loaded    []string `json:"loaded"`
	Installed []string `json:"installed"`
}

var (
	ErrInvalidKpatchOperation = errors.New("invalid kpatch operation")
	ErrKpatchApply            = errors.New("kpatch configuration failed")
)

func kpatchSupported() bool {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			id := strings.Trim(strings.TrimPrefix(line, "ID="), `"`)
			return id == "rhel"
		}
	}
	return false
}

// KpatchStatus inspects loaded/installed kernel live patches via
// `kpatch list` (bounded, read-only).
func InspectKpatchStatus(ctx context.Context) KpatchStatus {
	status := KpatchStatus{Supported: kpatchSupported(), Loaded: []string{}, Installed: []string{}}
	if !status.Supported {
		return status
	}
	result := runUpdateCommand(ctx, "kpatch", "list")
	if result.Err != nil || result.ExitCode != 0 {
		return status
	}
	sections := strings.Split(strings.TrimSpace(result.Output), "\n\n")
	if len(sections) != 2 || !strings.HasPrefix(sections[0], "Loaded patch modules:") || !strings.HasPrefix(sections[1], "Installed patch modules:") {
		return status
	}
	status.Loaded = patchModules(sections[0])
	status.Installed = patchModules(sections[1])
	return status
}

func patchModules(section string) []string {
	lines := strings.Split(section, "\n")
	modules := []string{}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			modules = append(modules, fields[0])
		}
	}
	return modules
}

// ---- settings --------------------------------------------------------------

const (
	kpatchConfigPath   = "/etc/dnf/plugins/kpatch.conf"
	kpatchServiceUnit  = "kpatch.service"
	kpatchManualPolicy = "manual"
	kpatchAutoPolicy   = "auto"
)

// KpatchOperation turns kernel live patching on or off; when enabling, the
// policy covers either the current kernel only or all future kernels.
type KpatchOperation struct {
	Apply       *bool `json:"apply,omitempty"`
	CurrentOnly *bool `json:"currentOnly,omitempty"`
}

func ValidateKpatchOperation(operation KpatchOperation) error {
	if operation.Apply == nil {
		return ErrInvalidKpatchOperation
	}
	return nil
}

// KpatchSettingsStatus describes the kernel live-patch settings state.
type KpatchSettingsStatus struct {
	Supported        bool     `json:"supported"`
	Missing          []string `json:"missing"`
	Unavailable      []string `json:"unavailable"`
	Auto             bool     `json:"auto"`
	ServiceEnabled   bool     `json:"serviceEnabled"`
	Kernel           string   `json:"kernel,omitempty"`
	PatchName        string   `json:"patchName,omitempty"`
	PatchInstalled   bool     `json:"patchInstalled"`
	PatchUnavailable bool     `json:"patchUnavailable"`
}

type kpatchDeps struct {
	commandOutput func(ctx context.Context, name string, args ...string) updateCommandResult
	readFile      func(path string) ([]byte, error)
	// supported overrides the os-release gate for tests; nil means detect.
	supported *bool
}

func defaultKpatchDeps() kpatchDeps {
	return kpatchDeps{
		commandOutput: runUpdateCommand,
		readFile:      os.ReadFile,
	}
}

func (deps kpatchDeps) commandSucceeds(ctx context.Context, name string, args ...string) bool {
	result := deps.commandOutput(ctx, name, args...)
	return result.Err == nil && result.ExitCode == 0
}

// kpatchPackageName derives the live-patch package name for a kernel release
// from `uname -r` output.
func kpatchPackageName(kernelRelease string) string {
	fields := strings.Split(strings.TrimSpace(kernelRelease), "-")
	if len(fields) < 2 || fields[1] == "" {
		return ""
	}
	version := strings.ReplaceAll(fields[0], ".", "_")
	release := strings.Split(fields[1], ".")
	if len(release) < 2 {
		return ""
	}
	release = release[:len(release)-2]
	return strings.Join([]string{"kpatch-patch", version, strings.Join(release, "_")}, "-")
}

// InspectKpatchSettings probes the kpatch setup (bounded, read-only): package
// presence and availability, autoupdate policy, service state, and the patch
// package matching the running kernel.
func InspectKpatchSettings(ctx context.Context) KpatchSettingsStatus {
	return inspectKpatchSettingsWithDeps(ctx, defaultKpatchDeps())
}

func inspectKpatchSettingsWithDeps(ctx context.Context, deps kpatchDeps) KpatchSettingsStatus {
	supported := kpatchSupported()
	if deps.supported != nil {
		supported = *deps.supported
	}
	settings := KpatchSettingsStatus{Supported: supported, Missing: []string{}, Unavailable: []string{}}
	if !settings.Supported {
		return settings
	}
	for _, packageName := range []string{"kpatch", "kpatch-dnf"} {
		switch {
		case deps.commandSucceeds(ctx, "rpm", "-q", packageName):
			// installed
		case deps.commandSucceeds(ctx, "dnf", "--quiet", "list", "available", packageName):
			settings.Missing = append(settings.Missing, packageName)
		default:
			settings.Unavailable = append(settings.Unavailable, packageName)
		}
	}

	if content := deps.readText(kpatchConfigPath); content != "" {
		settings.Auto = strings.EqualFold(confValue(content, "autoupdate"), "true")
	}
	settings.ServiceEnabled = deps.commandSucceeds(ctx, "systemctl", "--quiet", "is-enabled", kpatchServiceUnit)

	releaseResult := deps.commandOutput(ctx, "uname", "-r")
	if releaseResult.Err == nil {
		settings.Kernel = strings.TrimSpace(releaseResult.Output)
		settings.PatchName = kpatchPackageName(settings.Kernel)
	}
	if settings.PatchName != "" {
		settings.PatchInstalled = deps.commandSucceeds(ctx, "rpm", "-q", settings.PatchName)
		settings.PatchUnavailable = !settings.PatchInstalled &&
			!deps.commandSucceeds(ctx, "dnf", "--quiet", "list", "available", settings.PatchName)
	}
	return settings
}

func (deps kpatchDeps) readText(path string) string {
	data, err := deps.readFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// ApplyKpatchSettings applies settings changes with the following semantics:
//
//	off:                 dnf -y kpatch manual; disable+stop kpatch.service
//	on, future kernels:  dnf -y kpatch auto; enable+start kpatch.service
//	on, current kernel:  dnf -y kpatch manual; install current patch if
//	                     possible; enable+start kpatch.service
//
// It runs inside sessiond.
func ApplyKpatchSettings(ctx context.Context, operation KpatchOperation) (KpatchSettingsStatus, error) {
	return ApplyKpatchSettingsForDeps(ctx, defaultKpatchDeps(), operation)
}

func ApplyKpatchSettingsForDeps(ctx context.Context, deps kpatchDeps, operation KpatchOperation) (KpatchSettingsStatus, error) {
	if operation.Apply == nil {
		return KpatchSettingsStatus{}, ErrInvalidKpatchOperation
	}
	if err := applyKpatchSettingsWithDeps(ctx, deps, operation); err != nil {
		return KpatchSettingsStatus{}, err
	}
	return inspectKpatchSettingsWithDeps(ctx, deps), nil
}

func applyKpatchSettingsWithDeps(ctx context.Context, deps kpatchDeps, operation KpatchOperation) error {
	currentOnly := operation.CurrentOnly != nil && *operation.CurrentOnly
	if !*operation.Apply || currentOnly {
		if !deps.commandSucceeds(ctx, "dnf", "-y", "kpatch", kpatchManualPolicy) {
			return fmt.Errorf("%w: dnf kpatch %s failed", ErrKpatchApply, kpatchManualPolicy)
		}
	}
	if !*operation.Apply {
		if !deps.commandSucceeds(ctx, "systemctl", "disable", "--now", kpatchServiceUnit) {
			return fmt.Errorf("%w: could not disable %s", ErrKpatchApply, kpatchServiceUnit)
		}
		return nil
	}
	if !currentOnly {
		if !deps.commandSucceeds(ctx, "dnf", "-y", "kpatch", kpatchAutoPolicy) {
			return fmt.Errorf("%w: dnf kpatch %s failed", ErrKpatchApply, kpatchAutoPolicy)
		}
	} else {
		settings := inspectKpatchSettingsWithDeps(ctx, deps)
		if settings.PatchName != "" && !settings.PatchInstalled && !settings.PatchUnavailable {
			if !deps.commandSucceeds(ctx, "dnf", "-y", "install", settings.PatchName) {
				return fmt.Errorf("%w: installing %s failed", ErrKpatchApply, settings.PatchName)
			}
		}
	}
	if !deps.commandSucceeds(ctx, "systemctl", "enable", "--now", kpatchServiceUnit) {
		return fmt.Errorf("%w: could not enable %s", ErrKpatchApply, kpatchServiceUnit)
	}
	return nil
}
