package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestValidateAutoUpdatesOperationRejectsUnsafeValues(t *testing.T) {
	enabled := true
	badType := "sometimes"
	badDay := "funday"
	badTime := "25:99"
	timeWithoutDay := "06:00"
	if err := ValidateAutoUpdatesOperation(AutoUpdatesOperation{Enabled: &enabled, Type: &badType}); !errors.Is(err, ErrInvalidAutoUpdatesOperation) {
		t.Fatalf("bad type accepted: %v", err)
	}
	if err := ValidateAutoUpdatesOperation(AutoUpdatesOperation{Enabled: &enabled, Day: &badDay}); !errors.Is(err, ErrInvalidAutoUpdatesOperation) {
		t.Fatalf("bad day accepted: %v", err)
	}
	if err := ValidateAutoUpdatesOperation(AutoUpdatesOperation{Enabled: &enabled, Time: &badTime}); !errors.Is(err, ErrInvalidAutoUpdatesOperation) {
		t.Fatalf("bad time accepted: %v", err)
	}
	if err := ValidateAutoUpdatesOperation(AutoUpdatesOperation{Time: &timeWithoutDay}); !errors.Is(err, ErrInvalidAutoUpdatesOperation) {
		t.Fatalf("time without day accepted: %v", err)
	}
	day := ""
	goodTime := "06:00"
	if err := ValidateAutoUpdatesOperation(AutoUpdatesOperation{Enabled: &enabled, Day: &day, Time: &goodTime}); err != nil {
		t.Fatalf("valid operation rejected: %v", err)
	}
}

func TestParseAutoCalendarAcceptedSpecs(t *testing.T) {
	cases := []struct {
		spec          string
		inactiveDaily bool
		day           string
		timeOfDay     string
		supported     bool
	}{
		{"", true, "", "", true},
		{"", false, "", "", false},
		{"mon 06:00", false, "mon", "6:00", true},
		{"*-*-* 06:30", false, "", "6:30", true},
		{"06:05", false, "", "6:05", true},
		{"*-*-01 00:00", false, "", "", false},
		{"fri 6:*", false, "", "", false},
	}
	for _, testCase := range cases {
		day, timeOfDay, supported := parseAutoCalendar(testCase.spec, testCase.inactiveDaily)
		if day != testCase.day || timeOfDay != testCase.timeOfDay || supported != testCase.supported {
			t.Fatalf("parseAutoCalendar(%q, %v) = (%q, %q, %v)", testCase.spec, testCase.inactiveDaily, day, timeOfDay, supported)
		}
	}
}

func TestConfValueAndSetConfKeyRoundTrip(t *testing.T) {
	content := "[commands]\nupgrade_type = default\napply_updates = no\n"
	if value := confValue(content, "upgrade_type"); value != "default" {
		t.Fatalf("confValue = %q", value)
	}
	updated := setConfKey(content, "commands", "upgrade_type", "security")
	if value := confValue(updated, "upgrade_type"); value != "security" || strings.Count(updated, "upgrade_type") != 1 {
		t.Fatalf("setConfKey replace failed: %q", updated)
	}
	updated = setConfKey(updated, "commands", "reboot", "when-needed")
	if value := confValue(updated, "reboot"); value != "when-needed" {
		t.Fatalf("setConfKey append failed: %q", updated)
	}
	fresh := setConfKey("", "commands", "apply_updates", "yes")
	if fresh != "[commands]\napply_updates = yes\n" {
		t.Fatalf("setConfKey create failed: %q", fresh)
	}
	if hasConfKey(content, "reboot") {
		t.Fatal("hasConfKey reported absent key")
	}
}

func TestTimerScheduleParsesDropInConcatenation(t *testing.T) {
	output := "# /usr/lib/systemd/system/dnf-automatic.timer\n[Unit]\nDescription=dnf automatic timer\n\n[Timer]\nOnUnitInactiveSec=1d\n\n# drop-in\n[Timer]\nOnCalendar=fri 06:00\n"
	calendar, inactive := timerSchedule(output)
	if calendar != "fri 06:00" || inactive != "1d" {
		t.Fatalf("timerSchedule = (%q, %q)", calendar, inactive)
	}
}

func fakeAutoDeps(rpmInstalled bool, dnfVersion string, configFiles map[string]string, enabledUnits map[string]bool) autoUpdatesDeps {
	written := map[string][]byte{}
	return autoUpdatesDeps{
		commandOutput: func(_ context.Context, name string, args ...string) updateCommandResult {
			switch {
			case name == "rpm" && args[0] == "-q":
				if rpmInstalled {
					return updateCommandResult{ExitCode: 0}
				}
				return updateCommandResult{ExitCode: 1}
			case name == "dnf" && len(args) > 0 && args[0] == "--version":
				return updateCommandResult{ExitCode: 0, Output: dnfVersion + "\n"}
			case name == "systemctl" && args[0] == "--quiet":
				unit := args[len(args)-1]
				if enabledUnits[unit] {
					return updateCommandResult{ExitCode: 0}
				}
				return updateCommandResult{ExitCode: 1}
			case name == "systemctl" && args[0] == "enable":
				for _, unit := range args {
					enabledUnits[unit] = true
				}
				return updateCommandResult{ExitCode: 0}
			case name == "systemctl" && args[0] == "disable":
				for _, unit := range args {
					delete(enabledUnits, unit)
				}
				return updateCommandResult{ExitCode: 0}
			case name == "systemctl" && args[0] == "daemon-reload":
				return updateCommandResult{ExitCode: 0}
			case name == "systemctl" && args[0] == "cat":
				if content, ok := written[args[len(args)-1]+".cat"]; ok {
					return updateCommandResult{ExitCode: 0, Output: string(content)}
				}
				return updateCommandResult{ExitCode: 1}
			}
			return updateCommandResult{ExitCode: -1, Err: errors.New("unexpected command")}
		},
		commandExists: func(name string) bool { return name == "dnf" || name == "rpm" },
		readFile: func(path string) ([]byte, error) {
			if content, ok := configFiles[path]; ok {
				return []byte(content), nil
			}
			if content, ok := written[path]; ok {
				return content, nil
			}
			return nil, os.ErrNotExist
		},
		writeFile: func(path string, data []byte, _ os.FileMode) error {
			written[path] = data
			return nil
		},
		removeFile: func(path string) error {
			delete(written, path)
			return nil
		},
		mkdirAll: func(string, os.FileMode) error { return nil },
	}
}

func TestProviderSelectionFollowsDnfMajorVersion(t *testing.T) {
	deps4 := fakeAutoDeps(false, "4.19.2", nil, nil)
	deps5 := fakeAutoDeps(false, "dnf5 5.2.6", nil, nil)
	for _, provider := range autoUpdatesProviders() {
		if provider.Applies(context.Background(), deps5) != (provider.Name() == "dnf5-automatic") {
			t.Fatalf("%s mis-selected for dnf5 host", provider.Name())
		}
		if provider.Applies(context.Background(), deps4) != (provider.Name() == "dnf4-automatic") {
			t.Fatalf("%s mis-selected for dnf4 host", provider.Name())
		}
	}
}

func TestAutoUpdatesStatusWithDepsReportsUninstalledPackage(t *testing.T) {
	deps := fakeAutoDeps(false, "dnf5 5.2.6", map[string]string{}, map[string]bool{})
	config := AutoUpdatesStatusWithDeps(context.Background(), deps)
	if !config.Available || config.Installed || config.Enabled {
		t.Fatalf("unexpected status for missing package: %#v", config)
	}
	if config.PackageName != "dnf5-plugin-automatic" || config.Reason == "" {
		t.Fatalf("missing package guidance: %#v", config)
	}
}

func TestApplyAutoUpdatesEnablesSecurityScheduleOnDnf5(t *testing.T) {
	deps := fakeAutoDeps(true, "dnf5 5.2.6", map[string]string{}, map[string]bool{})
	enabled := true
	security := "security"
	everyday := ""
	timeOfDay := "06:00"
	operation := AutoUpdatesOperation{Enabled: &enabled, Type: &security, Day: &everyday, Time: &timeOfDay}
	config, err := ApplyAutoUpdatesWithDeps(context.Background(), deps, operation)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Enabled || config.Type != "security" || config.Provider != "dnf5-automatic" {
		t.Fatalf("post-apply status wrong: %#v", config)
	}
	conf := deps.readText(dnf5ConfigPath)
	if confValue(conf, "apply_updates") != "yes" || confValue(conf, "upgrade_type") != "security" || confValue(conf, "reboot") != "when-needed" {
		t.Fatalf("automatic.conf settings wrong: %q", conf)
	}
}

func TestApplyAutoUpdatesRejectsWhenNoProvider(t *testing.T) {
	deps := autoUpdatesDeps{
		commandOutput: func(context.Context, string, ...string) updateCommandResult {
			return updateCommandResult{ExitCode: -1, Err: errors.New("no dnf")}
		},
		commandExists: func(string) bool { return false },
		readFile:      func(string) ([]byte, error) { return nil, os.ErrNotExist },
		writeFile:     func(string, []byte, os.FileMode) error { return nil },
		removeFile:    func(string) error { return nil },
		mkdirAll:      func(string, os.FileMode) error { return nil },
	}
	if _, err := ApplyAutoUpdatesWithDeps(context.Background(), deps, AutoUpdatesOperation{}); !errors.Is(err, ErrAutoUpdatesUnavailable) {
		t.Fatalf("expected unavailable, got %v", err)
	}
}
