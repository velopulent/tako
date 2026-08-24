package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	ErrInvalidAutoUpdatesOperation = errors.New("invalid automatic updates operation")
	ErrAutoUpdatesUnavailable      = errors.New("automatic updates backend unavailable")
	ErrAutoUpdatesApply            = errors.New("automatic updates configuration failed")
)

// Automatic-update schedule vocabulary shared by every provider.
const (
	autoTypeAll      = "all"
	autoTypeSecurity = "security"
)

var autoDays = map[string]bool{"": true, "mon": true, "tue": true, "wed": true, "thu": true, "fri": true, "sat": true, "sun": true}

var autoTimePattern = regexp.MustCompile(`^([01]?[0-9]|2[0-3]):[0-5][0-9]$`)

// AutoUpdatesConfig reports the host's automatic-update setup, shaped so a
// future unattended-upgrades (apt) or zypper provider slots in unchanged.
type AutoUpdatesConfig struct {
	Available   bool   `json:"available"`
	Supported   bool   `json:"supported"`
	Installed   bool   `json:"installed"`
	Enabled     bool   `json:"enabled"`
	Type        string `json:"type"`
	Day         string `json:"day"`
	Time        string `json:"time"`
	Provider    string `json:"provider,omitempty"`
	PackageName string `json:"packageName,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// AutoUpdatesOperation changes automatic-update configuration; nil fields are
// left untouched (sent as null on the wire).
type AutoUpdatesOperation struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Type    *string `json:"type,omitempty"`
	Day     *string `json:"day,omitempty"`
	Time    *string `json:"time,omitempty"`
}

func ValidateAutoUpdatesOperation(operation AutoUpdatesOperation) error {
	if operation.Type != nil && *operation.Type != autoTypeAll && *operation.Type != autoTypeSecurity {
		return ErrInvalidAutoUpdatesOperation
	}
	if operation.Day != nil && !autoDays[*operation.Day] {
		return ErrInvalidAutoUpdatesOperation
	}
	if operation.Time != nil && *operation.Time != "" && !autoTimePattern.MatchString(*operation.Time) {
		return ErrInvalidAutoUpdatesOperation
	}
	if operation.Time != nil && operation.Day == nil {
		// A time only makes sense together with a day choice ("every day" is
		// expressed as an explicit empty day), keeping operations unambiguous.
		return ErrInvalidAutoUpdatesOperation
	}
	return nil
}

// autoUpdatesProvider abstracts one distro family's automatic-update
// machinery. Providers are tried in order; the first whose Applies() holds
// owns inspection and mutation.
type autoUpdatesProvider interface {
	Name() string
	Applies(ctx context.Context, deps autoUpdatesDeps) bool
	Inspect(ctx context.Context, deps autoUpdatesDeps) AutoUpdatesConfig
	Apply(ctx context.Context, deps autoUpdatesDeps, operation AutoUpdatesOperation) error
}

func autoUpdatesProviders() []autoUpdatesProvider {
	return []autoUpdatesProvider{
		dnf5AutomaticProvider{},
		dnf4AutomaticProvider{},
	}
}

// autoUpdatesDeps isolates the process/filesystem surface so tests can drive
// every provider without a real host.
type autoUpdatesDeps struct {
	commandOutput func(ctx context.Context, name string, args ...string) updateCommandResult
	commandExists func(string) bool
	readFile      func(string) ([]byte, error)
	writeFile     func(string, []byte, os.FileMode) error
	removeFile    func(string) error
	mkdirAll      func(string, os.FileMode) error
}

func defaultAutoUpdatesDeps() autoUpdatesDeps {
	return autoUpdatesDeps{
		commandOutput: func(ctx context.Context, name string, args ...string) updateCommandResult {
			return runUpdateCommand(ctx, name, args...)
		},
		commandExists: commandExists,
		readFile:      os.ReadFile,
		writeFile: func(path string, data []byte, mode os.FileMode) error {
			return os.WriteFile(path, data, mode)
		},
		removeFile: os.Remove,
		mkdirAll:   os.MkdirAll,
	}
}

func (deps autoUpdatesDeps) commandSucceeds(ctx context.Context, name string, args ...string) bool {
	result := deps.commandOutput(ctx, name, args...)
	return result.Err == nil && result.ExitCode == 0
}

func (deps autoUpdatesDeps) readText(path string) string {
	data, err := deps.readFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// confValue returns the last value for key in path, honouring nothing beyond
// simple ini-style lines (dnf config files are flat sections; the keys we
// manage are unique in practice).
func confValue(content, key string) string {
	pattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*(\S+)\s*$`)
	matches := pattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

func hasConfKey(content, key string) bool {
	pattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	return pattern.MatchString(content)
}

// setConfKey replaces or appends key=value under section, creating the file
// with the given section header when absent. It returns the new content.
func setConfKey(content, section, key, value string) string {
	pattern := regexp.MustCompile(`(?m)^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*).*$`)
	if pattern.MatchString(content) {
		return pattern.ReplaceAllString(content, "${1}"+value)
	}
	if strings.TrimSpace(content) == "" {
		return "[" + section + "]\n" + key + " = " + value + "\n"
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + key + " = " + value + "\n"
}

// parseAutoCalendar validates an OnCalendar spec: only specifications we
// would have written count as supported ("mon 06:00", "*-*-* 06:00", "06:00",
// or the daily OnUnitInactiveSec=1d default).
func parseAutoCalendar(spec string, inactiveDaily bool) (day, timeOfDay string, supported bool) {
	spec = strings.TrimSpace(strings.ToLower(spec))
	if spec == "" {
		if inactiveDaily {
			return "", "", true
		}
		return "", "", false
	}
	daysOfWeek := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	words := strings.Fields(spec)
	day = ""
	switch {
	case containsString(daysOfWeek, words[0]):
		day = words[0]
		words = words[1:]
	case words[0] == "*-*-*":
		day = ""
		words = words[1:]
	default:
		day = ""
	}
	if len(words) != 1 || !autoTimePattern.MatchString(words[0]) {
		return "", "", false
	}
	timeOfDay = strings.TrimLeft(words[0], "0")
	if strings.HasPrefix(timeOfDay, ":") {
		timeOfDay = "0" + timeOfDay
	}
	return day, timeOfDay, true
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// timerSchedule extracts OnCalendar=/OnUnitInactiveSec= from `systemctl cat`
// output, preferring the last OnCalendar line.
func timerSchedule(unitOutput string) (calendar, inactiveSec string) {
	for _, line := range strings.Split(unitOutput, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "OnCalendar="):
			calendar = strings.TrimSpace(strings.TrimPrefix(line, "OnCalendar="))
		case strings.HasPrefix(line, "OnUnitInactiveSec="):
			inactiveSec = strings.TrimSpace(strings.TrimPrefix(line, "OnUnitInactiveSec="))
		}
	}
	return calendar, inactiveSec
}

// ---- dnf5 ------------------------------------------------------------------

type dnf5AutomaticProvider struct{}

func (dnf5AutomaticProvider) Name() string { return "dnf5-automatic" }

const (
	dnf5ConfigPath     = "/etc/dnf/dnf5-plugins/automatic.conf"
	dnf5TimerUnit      = "dnf5-automatic.timer"
	dnf5TimerDropInDir = "/etc/systemd/system/dnf5-automatic.timer.d"
	dnf5TimerDropIn    = dnf5TimerDropInDir + "/time.conf"
	dnf5PackageName    = "dnf5-plugin-automatic"
)

func (p dnf5AutomaticProvider) Applies(ctx context.Context, deps autoUpdatesDeps) bool {
	if !deps.commandExists("dnf") {
		return false
	}
	version, _ := runVersionProbe(ctx, deps)
	return version.containsDNF5
}

func (p dnf5AutomaticProvider) Inspect(ctx context.Context, deps autoUpdatesDeps) AutoUpdatesConfig {
	config := AutoUpdatesConfig{Available: true, Provider: p.Name(), PackageName: dnf5PackageName, Type: autoTypeAll}
	if !deps.commandSucceeds(ctx, "rpm", "-q", dnf5PackageName) {
		config.Reason = "Install " + dnf5PackageName + " to configure automatic updates."
		return config
	}
	config.Installed = true
	confContent := deps.readText(dnf5ConfigPath)
	if confValue(confContent, "upgrade_type") == autoTypeSecurity {
		config.Type = autoTypeSecurity
	}
	timerEnabled := deps.commandSucceeds(ctx, "systemctl", "--quiet", "is-enabled", dnf5TimerUnit)
	if timerEnabled && confValue(confContent, "apply_updates") == "yes" {
		config.Enabled = true
	}
	calendar, inactive := unitSchedule(ctx, deps, dnf5TimerUnit)
	day, timeOfDay, supported := parseAutoCalendar(calendar, strings.HasPrefix(inactive, "1d"))
	config.Supported = supported
	config.Day, config.Time = day, timeOfDay
	return config
}

func (dnf5AutomaticProvider) Apply(ctx context.Context, deps autoUpdatesDeps, operation AutoUpdatesOperation) error {
	settings := [][2]string{}
	if operation.Type != nil {
		value := "default"
		if *operation.Type == autoTypeSecurity {
			value = autoTypeSecurity
		}
		settings = append(settings, [2]string{"upgrade_type", value})
	}
	scheduleChanged := operation.Day != nil || operation.Time != nil
	var day, timeOfDay string
	if scheduleChanged {
		current := dnf5AutomaticProvider{}.Inspect(ctx, deps)
		day, timeOfDay = current.Day, current.Time
		if operation.Day != nil {
			day = *operation.Day
		}
		if operation.Time != nil {
			timeOfDay = *operation.Time
		}
		if day == "" && timeOfDay == "" {
			// Restore packaged defaults by dropping our override.
			if err := deps.removeFile(dnf5TimerDropIn); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w: %v", ErrAutoUpdatesApply, err)
			}
		} else {
			if timeOfDay == "" {
				timeOfDay = "6:00"
			}
			if err := writeTimerDropIn(deps, dnf5TimerDropInDir, dnf5TimerDropIn, day, timeOfDay); err != nil {
				return err
			}
		}
	}
	if operation.Enabled != nil {
		action := "disable"
		if *operation.Enabled {
			action = "enable"
		}
		if !deps.commandSucceeds(ctx, "systemctl", action, "--now", dnf5TimerUnit) {
			return fmt.Errorf("%w: systemctl %s %s failed", ErrAutoUpdatesApply, action, dnf5TimerUnit)
		}
		if *operation.Enabled {
			settings = append(settings, [2]string{"apply_updates", "yes"}, [2]string{"reboot", "when-needed"})
		}
	}
	if len(settings) > 0 {
		content := deps.readText(dnf5ConfigPath)
		for _, setting := range settings {
			content = setConfKey(content, "commands", setting[0], setting[1])
		}
		if err := deps.writeFile(dnf5ConfigPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("%w: %v", ErrAutoUpdatesApply, err)
		}
	}
	return daemonReload(ctx, deps)
}

// ---- dnf4 ------------------------------------------------------------------

type dnf4AutomaticProvider struct{}

func (dnf4AutomaticProvider) Name() string { return "dnf4-automatic" }

const (
	dnf4ConfigPath     = "/etc/dnf/automatic.conf"
	dnf4InstallTimer   = "dnf-automatic-install.timer"
	dnf4LegacyTimer    = "dnf-automatic.timer"
	dnf4TimerDropInDir = "/etc/systemd/system/dnf-automatic-install.timer.d"
	dnf4TimerDropIn    = dnf4TimerDropInDir + "/time.conf"
	dnf4RebootDropInD  = "/etc/systemd/system/dnf-automatic-install.service.d"
	dnf4RebootDropIn   = dnf4RebootDropInD + "/autoreboot.conf"
	dnf4PackageName    = "dnf-automatic"
)

func (dnf4AutomaticProvider) Applies(ctx context.Context, deps autoUpdatesDeps) bool {
	if !deps.commandExists("dnf") {
		return false
	}
	version, _ := runVersionProbe(ctx, deps)
	return !version.containsDNF5
}

func (p dnf4AutomaticProvider) Inspect(ctx context.Context, deps autoUpdatesDeps) AutoUpdatesConfig {
	config := AutoUpdatesConfig{Available: true, Provider: p.Name(), PackageName: dnf4PackageName, Type: autoTypeAll}
	if !deps.commandSucceeds(ctx, "rpm", "-q", dnf4PackageName) {
		config.Reason = "Install " + dnf4PackageName + " to configure automatic updates."
		return config
	}
	config.Installed = true
	confContent := deps.readText(dnf4ConfigPath)
	if confValue(confContent, "upgrade_type") == autoTypeSecurity {
		config.Type = autoTypeSecurity
	}
	timer := ""
	if deps.commandSucceeds(ctx, "systemctl", "--quiet", "is-enabled", dnf4InstallTimer) {
		config.Enabled = true
		timer = dnf4InstallTimer
	} else if deps.commandSucceeds(ctx, "systemctl", "--quiet", "is-enabled", dnf4LegacyTimer) && confValue(confContent, "apply_updates") == "yes" {
		config.Enabled = true
		timer = dnf4LegacyTimer
	}
	if timer != "" {
		calendar, inactive := unitSchedule(ctx, deps, timer)
		day, timeOfDay, supported := parseAutoCalendar(calendar, strings.HasPrefix(inactive, "1d"))
		config.Supported = supported
		config.Day, config.Time = day, timeOfDay
	} else if config.Enabled {
		config.Supported = true
	} else {
		// Disabled: the packaged default is a daily run; report it as the
		// schedule that enabling would keep unless edited.
		config.Supported = true
	}
	return config
}

func (dnf4AutomaticProvider) Apply(ctx context.Context, deps autoUpdatesDeps, operation AutoUpdatesOperation) error {
	if operation.Type != nil {
		content := deps.readText(dnf4ConfigPath)
		value := "default"
		if *operation.Type == autoTypeSecurity {
			value = autoTypeSecurity
		}
		content = setConfKey(content, "commands", "upgrade_type", value)
		if err := deps.writeFile(dnf4ConfigPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("%w: %v", ErrAutoUpdatesApply, err)
		}
	}
	scheduleChanged := operation.Day != nil || operation.Time != nil
	if scheduleChanged {
		current := dnf4AutomaticProvider{}.Inspect(ctx, deps)
		day, timeOfDay := current.Day, current.Time
		if operation.Day != nil {
			day = *operation.Day
		}
		if operation.Time != nil {
			timeOfDay = *operation.Time
		}
		if day == "" && timeOfDay == "" {
			if err := deps.removeFile(dnf4TimerDropIn); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w: %v", ErrAutoUpdatesApply, err)
			}
		} else {
			// Pin a 6:00 start when first scheduling so enabling the
			// timer does not fire immediately via the packaged OnBootSec=1h.
			if timeOfDay == "" {
				timeOfDay = "6:00"
			}
			if err := writeTimerDropIn(deps, dnf4TimerDropInDir, dnf4TimerDropIn, day, timeOfDay); err != nil {
				return err
			}
		}
	}
	if operation.Enabled != nil {
		if *operation.Enabled {
			if !deps.commandSucceeds(ctx, "systemctl", "enable", "--now", dnf4InstallTimer) {
				return fmt.Errorf("%w: systemctl enable %s failed", ErrAutoUpdatesApply, dnf4InstallTimer)
			}
			if err := dnf4ApplyRebootPolicy(ctx, deps); err != nil {
				return err
			}
		} else {
			if !deps.commandSucceeds(ctx, "systemctl", "disable", "--now", dnf4InstallTimer) {
				return fmt.Errorf("%w: systemctl disable %s failed", ErrAutoUpdatesApply, dnf4InstallTimer)
			}
			// Legacy unit may not exist; ignore failure.
			_ = deps.commandSucceeds(ctx, "systemctl", "disable", "--now", dnf4LegacyTimer)
			if err := deps.removeFile(dnf4RebootDropIn); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w: %v", ErrAutoUpdatesApply, err)
			}
		}
	}
	return daemonReload(ctx, deps)
}

// dnf4ApplyRebootPolicy handles the version split: dnf >= 4.15
// understands `reboot = when-needed`; older setups get the ExecStartPost
// journal-grep hack drop-in.
func dnf4ApplyRebootPolicy(ctx context.Context, deps autoUpdatesDeps) error {
	content := deps.readText(dnf4ConfigPath)
	if hasConfKey(content, "reboot") {
		if confValue(content, "reboot") == "never" {
			content = setConfKey(content, "commands", "reboot", "when-needed")
			if err := deps.writeFile(dnf4ConfigPath, []byte(content), 0o644); err != nil {
				return fmt.Errorf("%w: %v", ErrAutoUpdatesApply, err)
			}
		}
		if err := deps.removeFile(dnf4RebootDropIn); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %v", ErrAutoUpdatesApply, err)
		}
		return nil
	}
	hack := "[Service]\nExecStartPost=/bin/sh -ec \"if systemctl status --no-pager --lines=100 dnf-automatic-install.service| grep -q ===========$$; then shutdown -r +5 rebooting after applying package updates; fi\"\n"
	if err := deps.mkdirAll(dnf4RebootDropInD, 0o755); err != nil {
		return fmt.Errorf("%w: %v", ErrAutoUpdatesApply, err)
	}
	if err := deps.writeFile(dnf4RebootDropIn, []byte(hack), 0o644); err != nil {
		return fmt.Errorf("%w: %v", ErrAutoUpdatesApply, err)
	}
	return daemonReload(ctx, deps)
}

// ---- shared helpers --------------------------------------------------------

type dnfVersionProbe struct {
	containsDNF5 bool
	raw          string
}

func runVersionProbe(ctx context.Context, deps autoUpdatesDeps) (dnfVersionProbe, bool) {
	result := deps.commandOutput(ctx, "dnf", "--version")
	if result.Err != nil || result.ExitCode != 0 {
		return dnfVersionProbe{}, false
	}
	raw := strings.TrimSpace(result.Output)
	return dnfVersionProbe{containsDNF5: strings.Contains(raw, "dnf5"), raw: raw}, true
}

func writeTimerDropIn(deps autoUpdatesDeps, dir, path, day, timeOfDay string) error {
	if !autoTimePattern.MatchString(timeOfDay) {
		return fmt.Errorf("%w: invalid schedule time %q", ErrInvalidAutoUpdatesOperation, timeOfDay)
	}
	if !autoDays[day] {
		return fmt.Errorf("%w: invalid schedule day %q", ErrInvalidAutoUpdatesOperation, day)
	}
	content := "[Timer]\nOnBootSec=\nOnCalendar=" + day + " " + timeOfDay + "\n"
	if err := deps.mkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%w: %v", ErrAutoUpdatesApply, err)
	}
	if err := deps.writeFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("%w: %v", ErrAutoUpdatesApply, err)
	}
	return nil
}

func daemonReload(ctx context.Context, deps autoUpdatesDeps) error {
	if !deps.commandSucceeds(ctx, "systemctl", "daemon-reload") {
		return fmt.Errorf("%w: systemctl daemon-reload failed", ErrAutoUpdatesApply)
	}
	return nil
}

// unitSchedule reads a timer unit plus its drop-ins through `systemctl cat`
// (bounded, side-effect free) and extracts OnCalendar=/OnUnitInactiveSec=,
// preferring the last OnCalendar line.
func unitSchedule(ctx context.Context, deps autoUpdatesDeps, unit string) (calendar, inactiveSec string) {
	result := deps.commandOutput(ctx, "systemctl", "cat", unit)
	if result.Err != nil || result.ExitCode != 0 {
		return "", ""
	}
	return timerSchedule(result.Output)
}

// ---- entry points ----------------------------------------------------------

// AutoUpdatesStatus inspects the host's automatic-update configuration.
func AutoUpdatesStatus(ctx context.Context) AutoUpdatesConfig {
	deps := defaultAutoUpdatesDeps()
	return AutoUpdatesStatusWithDeps(ctx, deps)
}

func AutoUpdatesStatusWithDeps(ctx context.Context, deps autoUpdatesDeps) AutoUpdatesConfig {
	for _, provider := range autoUpdatesProviders() {
		if provider.Applies(ctx, deps) {
			return provider.Inspect(ctx, deps)
		}
	}
	return AutoUpdatesConfig{
		Available: false,
		Reason:    "No supported automatic-update backend detected (dnf-automatic or dnf5-plugin-automatic).",
	}
}

// ApplyAutoUpdates mutates automatic-update configuration through the first
// applicable provider. It runs inside sessiond.
func ApplyAutoUpdates(ctx context.Context, operation AutoUpdatesOperation) (AutoUpdatesConfig, error) {
	return ApplyAutoUpdatesWithDeps(ctx, defaultAutoUpdatesDeps(), operation)
}

func ApplyAutoUpdatesWithDeps(ctx context.Context, deps autoUpdatesDeps, operation AutoUpdatesOperation) (AutoUpdatesConfig, error) {
	for _, provider := range autoUpdatesProviders() {
		if provider.Applies(ctx, deps) {
			if err := provider.Apply(ctx, deps, operation); err != nil {
				return AutoUpdatesConfig{}, err
			}
			return provider.Inspect(ctx, deps), nil
		}
	}
	return AutoUpdatesConfig{}, ErrAutoUpdatesUnavailable
}
