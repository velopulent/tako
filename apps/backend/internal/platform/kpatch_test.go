package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestValidateKpatchOperationRequiresApply(t *testing.T) {
	if err := ValidateKpatchOperation(KpatchOperation{}); !errors.Is(err, ErrInvalidKpatchOperation) {
		t.Fatalf("nil apply accepted: %v", err)
	}
	apply := true
	if err := ValidateKpatchOperation(KpatchOperation{Apply: &apply}); err != nil {
		t.Fatalf("valid operation rejected: %v", err)
	}
}

func TestKpatchPackageNameDerivation(t *testing.T) {
	cases := map[string]string{
		"5.14.0-427.40.1.el9_4.x86_64": "kpatch-patch-5_14_0-427_40_1",
		"4.18.0-553.el8_10.x86_64":     "kpatch-patch-4_18_0-553",
		"6.1.0-generic":                "",
	}
	for kernelRelease, want := range cases {
		if got := kpatchPackageName(kernelRelease); got != want {
			t.Fatalf("kpatchPackageName(%q) = %q, want %q", kernelRelease, got, want)
		}
	}
}

func fakeKpatchDeps(installed []string, available []string, conf string, enabledUnits map[string]bool) kpatchDeps {
	supported := true
	deps := kpatchDeps{supported: &supported}
	contains := func(list []string, want string) bool {
		for _, item := range list {
			if item == want {
				return true
			}
		}
		return false
	}
	deps.commandOutput = func(_ context.Context, name string, args ...string) updateCommandResult {
		switch name {
		case "uname":
			return updateCommandResult{ExitCode: 0, Output: "5.14.0-427.40.1.el9_4.x86_64\n"}
		case "rpm":
			pkg := args[len(args)-1]
			if contains(installed, pkg) {
				return updateCommandResult{ExitCode: 0}
			}
			return updateCommandResult{ExitCode: 1}
		case "dnf":
			joined := strings.Join(args, " ")
			switch {
			case strings.Contains(joined, "list"):
				pkg := args[len(args)-1]
				if contains(available, pkg) {
					return updateCommandResult{ExitCode: 0}
				}
				return updateCommandResult{ExitCode: 1}
			default:
				// dnf -y kpatch auto|manual / dnf -y install <pkg>
				return updateCommandResult{ExitCode: 0}
			}
		case "systemctl":
			joined := strings.Join(args, " ")
			switch {
			case strings.HasPrefix(joined, "--quiet is-enabled"):
				if enabledUnits[kpatchServiceUnit] {
					return updateCommandResult{ExitCode: 0}
				}
				return updateCommandResult{ExitCode: 1}
			case strings.HasPrefix(joined, "enable"):
				enabledUnits[kpatchServiceUnit] = true
			case strings.HasPrefix(joined, "disable"):
				delete(enabledUnits, kpatchServiceUnit)
			}
			return updateCommandResult{ExitCode: 0}
		}
		return updateCommandResult{ExitCode: -1, Err: errors.New("unexpected command")}
	}
	deps.readFile = func(path string) ([]byte, error) {
		if path == kpatchConfigPath && conf != "" {
			return []byte(conf), nil
		}
		return nil, os.ErrNotExist
	}
	return deps
}

func TestInspectKpatchSettingsWithDepsProbesPackagesAndPolicy(t *testing.T) {
	deps := fakeKpatchDeps(
		[]string{"kpatch"},
		[]string{"kpatch-dnf", "kpatch-patch-5_14_0-427_40_1"},
		"[main]\nautoupdate=True\n",
		map[string]bool{},
	)
	settings := inspectKpatchSettingsWithDeps(context.Background(), deps)
	if len(settings.Missing) != 1 || settings.Missing[0] != "kpatch-dnf" {
		t.Fatalf("missing = %v", settings.Missing)
	}
	if !settings.Auto || settings.ServiceEnabled {
		t.Fatalf("policy/state wrong: %#v", settings)
	}
	if settings.PatchName != "kpatch-patch-5_14_0-427_40_1" || settings.PatchInstalled || settings.PatchUnavailable {
		t.Fatalf("patch probe wrong: %#v", settings)
	}
}

func TestApplyKpatchSettingsEnablesAutoAndService(t *testing.T) {
	units := map[string]bool{}
	deps := fakeKpatchDeps([]string{"kpatch", "kpatch-dnf"}, []string{}, "[main]\nautoupdate=False\n", units)
	apply := true
	settings, err := ApplyKpatchSettingsForDeps(context.Background(), deps, KpatchOperation{Apply: &apply})
	if err != nil {
		t.Fatal(err)
	}
	if !units[kpatchServiceUnit] {
		t.Fatal("kpatch.service was not enabled")
	}
	if !settings.Supported {
		t.Fatalf("unexpected settings: %#v", settings)
	}
}

func TestApplyKpatchSettingsOffDisablesService(t *testing.T) {
	units := map[string]bool{kpatchServiceUnit: true}
	deps := fakeKpatchDeps([]string{"kpatch", "kpatch-dnf"}, []string{}, "", units)
	off := false
	if _, err := ApplyKpatchSettingsForDeps(context.Background(), deps, KpatchOperation{Apply: &off}); err != nil {
		t.Fatal(err)
	}
	if units[kpatchServiceUnit] {
		t.Fatal("kpatch.service still enabled")
	}
}
