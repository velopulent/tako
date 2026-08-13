package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUpdateParsersStayBoundedAndExposeVersions(t *testing.T) {
	apt := parseAPTUpdates("" +
		"WARNING: apt does not have a stable CLI interface. Use with caution in scripts.\n" +
		"Listing...\n" +
		"openssl/bookworm-security 3.0.14-1~deb12u2 amd64 [upgradable from: 3.0.11-1~deb12u1]\n")
	if len(apt) != 1 || apt[0].Name != "openssl" || apt[0].CurrentVersion == "" || apt[0].CandidateVersion == "" || apt[0].Architecture != "amd64" {
		t.Fatalf("unexpected apt inventory: %#v", apt)
	}
	dnf := parseDNFUpdates("Last metadata expiration check: 0:01:00 ago on Tue.\nPackage     Arch     Version       Repository\nopenssl     x86_64   3.0.14-1      updates\n")
	if len(dnf) != 1 || dnf[0].Name != "openssl" || dnf[0].CandidateVersion != "3.0.14-1" {
		t.Fatalf("unexpected dnf inventory: %#v", dnf)
	}
	packageKit := parsePackageKitUpdates("Available packages\nopenssl.x86_64 3.0.14-1 Security update\n")
	if len(packageKit) != 1 || packageKit[0].Name != "openssl" || packageKit[0].Architecture != "x86_64" || packageKit[0].Summary == "" {
		t.Fatalf("unexpected PackageKit inventory: %#v", packageKit)
	}
	packageKitID := parsePackageKitUpdates("Available openssl;3.0.14-1;x86_64;updates Security update\n")
	if len(packageKitID) != 1 || packageKitID[0].Name != "openssl" || packageKitID[0].CandidateVersion != "3.0.14-1" || packageKitID[0].Architecture != "x86_64" {
		t.Fatalf("PackageKit package id was not decoded: %#v", packageKitID)
	}
	oversized := strings.Repeat("pkg/updates 1.0 amd64\n", MaxUpdatePackages+10)
	if got := len(parseAPTUpdates(oversized)); got != MaxUpdatePackages {
		t.Fatalf("parser exceeded package bound: %d", got)
	}
}

func TestUpdatesPreferPackageKitAndRemainReadOnly(t *testing.T) {
	var calls []string
	status := updatesWithDependencies(context.Background(), updateDependencies{
		packageKitAvailable: func(context.Context) bool { return true },
		commandExists:       func(name string) bool { return name == "pkcon" },
		commandVersion: func(_ context.Context, name string, _ ...string) (string, bool) {
			if name == "pkcon" {
				return "pkcon 1.2", true
			}
			return "", false
		},
		commandOutput: func(_ context.Context, name string, args ...string) updateCommandResult {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return updateCommandResult{Output: "Available packages\nvim.x86_64 9.0 editor\n", ExitCode: 0}
		},
	})
	if !status.Available || status.Backend != "PackageKit" || status.Contract != "dbus-read-only" || len(status.Packages) != 1 || status.Packages[0].Name != "vim" {
		t.Fatalf("unexpected PackageKit status: %#v", status)
	}
	if len(calls) != 1 || calls[0] != "pkcon --noninteractive get-updates" {
		t.Fatalf("unexpected PackageKit command: %#v", calls)
	}
}

func TestUpdatesUseVersionGatedAPTAndReportHeldLock(t *testing.T) {
	var calls []string
	status := updatesWithDependencies(context.Background(), updateDependencies{
		packageKitAvailable: func(context.Context) bool { return false },
		commandExists:       func(name string) bool { return name == "apt" },
		commandVersion: func(_ context.Context, name string, _ ...string) (string, bool) {
			if name == "apt-get" {
				return "apt 3.0", true
			}
			return "", false
		},
		commandOutput: func(_ context.Context, name string, args ...string) updateCommandResult {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return updateCommandResult{Output: "Listing...\nvim/bookworm 9.0 amd64 [upgradable from: 8.2]\n", ExitCode: 0}
		},
		lockHeld: func(path string) bool { return path == "/var/lib/dpkg/lock" },
	})
	if !status.Available || status.Backend != "apt-get" || !status.ExternalLock || status.LockReason == "" || len(status.Packages) != 1 {
		t.Fatalf("unexpected APT status: %#v", status)
	}
	if len(calls) != 1 || calls[0] != "apt list --upgradable" {
		t.Fatalf("APT did not use read-only inventory command: %#v", calls)
	}
}

func TestUpdatesAcceptDNFCheckUpdateExitCodeAndFailClosed(t *testing.T) {
	status := updatesWithDependencies(context.Background(), updateDependencies{
		packageKitAvailable: func(context.Context) bool { return false },
		commandExists:       func(string) bool { return false },
		commandVersion: func(_ context.Context, name string, _ ...string) (string, bool) {
			if name == "apt-get" {
				return "apt 0.9", true
			}
			if name == "dnf" {
				return "dnf 4.18", true
			}
			return "", false
		},
		commandOutput: func(_ context.Context, _ string, _ ...string) updateCommandResult {
			return updateCommandResult{Output: "Package Arch Version Repository\nvim x86_64 9.0 updates\n", ExitCode: 100, Err: errors.New("updates available")}
		},
	})
	if status.Backend != "dnf" || len(status.Packages) != 1 || status.Packages[0].CandidateVersion != "9.0" {
		t.Fatalf("DNF exit 100 was not treated as inventory: %#v", status)
	}
	unavailable := updatesWithDependencies(context.Background(), updateDependencies{
		packageKitAvailable: func(context.Context) bool { return false },
		commandVersion:      func(context.Context, string, ...string) (string, bool) { return "", false },
	})
	if unavailable.Available || unavailable.Contract != "unavailable" || unavailable.Backend != "none" {
		t.Fatalf("unsupported backend did not fail closed: %#v", unavailable)
	}
}

func TestVersionGate(t *testing.T) {
	if !versionAtLeast("apt 3.0", 1) || versionAtLeast("dnf 3.0", 4) || versionAtLeast("unknown", 1) {
		t.Fatal("version gate misclassified backend versions")
	}
}
