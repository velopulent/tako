package platform

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeProbe struct {
	bus      map[string]bool
	busErr   error
	files    map[string]bool
	contains map[string]bool
	commands map[string]string
	outputs  map[string]string
}

func TestReadBoundedRejectsOversizedCommandOutput(t *testing.T) {
	if _, err := readBounded(bytes.NewReader(bytes.Repeat([]byte("x"), 4097)), 4096); err == nil {
		t.Fatal("oversized command output was accepted")
	}
}

func TestInactiveUFWDoesNotConflictWithFirewalld(t *testing.T) {
	capabilities := detect(context.Background(), fakeProbe{
		bus:      map[string]bool{"org.fedoraproject.FirewallD1": true},
		commands: map[string]string{"ufw": "ufw 0.36"},
		contains: map[string]bool{"/etc/ufw/ufw.conf:ENABLED=yes": false},
	})
	firewall := findCapability(t, capabilities, "firewall")
	if firewall.State != StateReady || firewall.Backend != "firewalld" || !firewall.Mutable {
		t.Fatalf("inactive UFW caused a false conflict or lost mutability: %#v", firewall)
	}
}

func TestActivatableManagersDoNotCreateOwnershipConflict(t *testing.T) {
	capabilities := detect(context.Background(), fakeProbe{
		bus: map[string]bool{
			"available:org.freedesktop.NetworkManager": true,
			"available:org.fedoraproject.FirewallD1":   true,
			"active:org.freedesktop.network1":          true,
		},
		commands: map[string]string{"ufw": "ufw 0.36"},
		contains: map[string]bool{"/etc/ufw/ufw.conf:ENABLED=yes": true},
	})
	if network := findCapability(t, capabilities, "network"); network.State == StateConflicted || network.Backend != "systemd-networkd" {
		t.Fatalf("activatable NetworkManager caused false conflict: %#v", network)
	}
	if firewall := findCapability(t, capabilities, "firewall"); firewall.State == StateConflicted || firewall.Backend != "UFW" {
		t.Fatalf("activatable firewalld caused false conflict: %#v", firewall)
	}
}

func (probe fakeProbe) CommandOutput(_ context.Context, name string, arguments ...string) (string, bool) {
	key := name + " " + strings.Join(arguments, " ")
	if output, ok := probe.outputs[key]; ok {
		return output, true
	}
	return probe.CommandVersion(context.Background(), name, arguments...)
}

func (probe fakeProbe) BusNames(context.Context) (map[string]bool, error) {
	return probe.bus, probe.busErr
}

func (probe fakeProbe) FileExists(path string) bool { return probe.files[path] }

func (probe fakeProbe) FileContains(path, expected string) bool {
	return probe.contains[path+":"+expected]
}

func (probe fakeProbe) CommandExists(name string) bool {
	_, ok := probe.commands[name]
	return ok
}

func (probe fakeProbe) CommandVersion(_ context.Context, name string, _ ...string) (string, bool) {
	version, ok := probe.commands[name]
	return version, ok
}

func TestDetectUsesRuntimeBackendsAndFailsClosedOnConflict(t *testing.T) {
	capabilities := detect(context.Background(), fakeProbe{
		bus: map[string]bool{
			"org.freedesktop.systemd1":       true,
			"org.freedesktop.NetworkManager": true,
			"org.freedesktop.network1":       true,
			"org.fedoraproject.FirewallD1":   true,
		},
		files:    map[string]bool{"/proc": true, "/proc/stat": true},
		commands: map[string]string{"ufw": "ufw 0.36", "nmcli": "nmcli 1.50"},
		contains: map[string]bool{"/etc/ufw/ufw.conf:ENABLED=yes": true},
	})

	network := findCapability(t, capabilities, "network")
	if network.State != StateConflicted || network.Mutable || !network.Readable || network.SetupGuidance == "" {
		t.Fatalf("network conflict did not fail closed: %#v", network)
	}
	firewall := findCapability(t, capabilities, "firewall")
	if firewall.State != StateConflicted || firewall.Mutable || firewall.Backend != "firewalld+UFW" {
		t.Fatalf("firewall conflict did not fail closed: %#v", firewall)
	}
	services := findCapability(t, capabilities, "services")
	if services.State != StateReady || services.Contract != "dbus" {
		t.Fatalf("systemd runtime service was not detected: %#v", services)
	}
}

func TestDetectExplainsMissingAndDegradedDependencies(t *testing.T) {
	capabilities := detect(context.Background(), fakeProbe{
		busErr:   errors.New("permission denied"),
		files:    map[string]bool{"/proc": true, "/proc/stat": true},
		commands: map[string]string{"apt-get": "apt 3.0"},
	})

	updates := findCapability(t, capabilities, "updates")
	if updates.State != StateReady || !updates.Mutable || updates.Version != "apt 3.0" || updates.Contract != "bounded-command" {
		t.Fatalf("apt command path should be ready and writable: %#v", updates)
	}
	if updates.MissingDependency != "" || updates.SetupGuidance == "" {
		t.Fatalf("PackageKit enrichment guidance missing: %#v", updates)
	}
	storage := findCapability(t, capabilities, "storage")
	if storage.State != StateDegraded || storage.Reason == "" || storage.SetupGuidance == "" {
		t.Fatalf("D-Bus failure lacks guidance: %#v", storage)
	}
	selinux := findCapability(t, capabilities, "selinux")
	if selinux.State != StateUnavailable || selinux.MissingDependency == "" {
		t.Fatalf("missing SELinux dependency is unexplained: %#v", selinux)
	}
}

func TestDetectAdvertisesImplementedMutations(t *testing.T) {
	capabilities := detect(context.Background(), fakeProbe{
		bus: map[string]bool{
			"org.freedesktop.systemd1":             true,
			"org.freedesktop.NetworkManager":       true,
			"available:org.freedesktop.PackageKit": true,
		},
		files:    map[string]bool{"/proc": true, "/proc/stat": true, "/etc/passwd": true},
		commands: map[string]string{"nmcli": "nmcli 1.50", "pkcon": "pkcon 1.2.8"},
	})

	// PackageKit present: writable through the sessiond-brokered apply job.
	updates := findCapability(t, capabilities, "updates")
	if !updates.Mutable || updates.MutationAuthority != "administrative" || updates.Contract != "dbus" || updates.Rollback {
		t.Fatalf("PackageKit updates should be administratively writable without rollback: %#v", updates)
	}
	// Service actions and timers mutate systemd state.
	if services := findCapability(t, capabilities, "services"); !services.Mutable || services.Rollback {
		t.Fatalf("services should be mutable: %#v", services)
	}
	// Process signals and account management are privileged mutations.
	for _, id := range []string{"processes", "users"} {
		if capability := findCapability(t, capabilities, id); !capability.Mutable || capability.MutationAuthority != "administrative" {
			t.Fatalf("%s should be administratively mutable: %#v", id, capability)
		}
	}
	// NetworkManager mutations checkpoint and roll back automatically.
	network := findCapability(t, capabilities, "network")
	if !network.Mutable || !network.Rollback || network.Contract != "bounded-command" {
		t.Fatalf("NetworkManager should be mutable with rollback: %#v", network)
	}
	// Genuinely read-only modules stay read-only.
	for _, id := range []string{"dashboard", "metrics", "logs", "storage"} {
		if capability := findCapability(t, capabilities, id); capability.Mutable {
			t.Fatalf("%s must remain read-only: %#v", id, capability)
		}
	}
}

func findCapability(t *testing.T, capabilities []Capability, id string) Capability {
	t.Helper()
	for _, capability := range capabilities {
		if capability.ID == id {
			return capability
		}
	}
	t.Fatalf("capability %q not found", id)
	return Capability{}
}
