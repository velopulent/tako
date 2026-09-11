package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetworkFileCheckpointPersistsAndRestores(t *testing.T) {
	root := t.TempDir()
	previousConfig, previousCheckpoint := networkConfigRoot, networkCheckpointRoot
	networkConfigRoot = root
	networkCheckpointRoot = filepath.Join(root, "run", "checkpoints")
	defer func() {
		networkConfigRoot = previousConfig
		networkCheckpointRoot = previousCheckpoint
	}()
	restoreRunner := setNetworkCommandRunner(func(_ context.Context, _ string, _ ...string) ([]byte, error) { return nil, nil })
	defer restoreRunner()
	path := networkdManagedPath("eno1")
	if err := writeNetworkFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := beginNetworkCheckpoint(context.Background(), NetworkOperation{Backend: "systemd-networkd", Interface: "eno1", ReconnectToken: "reconnect-test"})
	if err != nil {
		t.Fatal(err)
	}
	if !RecoverNetworkFileCheckpoint("systemd-networkd", checkpoint, "reconnect-test") {
		t.Fatal("persisted checkpoint was not recoverable")
	}
	if err := writeNetworkFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rollbackNetworkCheckpoint(context.Background(), "systemd-networkd", checkpoint, "reconnect-test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old\n" {
		t.Fatalf("restored network file = %q, err=%v", data, err)
	}
	if _, err := os.Stat(networkCheckpointPath(checkpoint)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checkpoint remains: %v", err)
	}
}

func TestNetworkFileCheckpointRequiresReconnectToken(t *testing.T) {
	root := t.TempDir()
	previousConfig, previousCheckpoint := networkConfigRoot, networkCheckpointRoot
	networkConfigRoot = root
	networkCheckpointRoot = filepath.Join(root, "run", "checkpoints")
	defer func() {
		networkConfigRoot = previousConfig
		networkCheckpointRoot = previousCheckpoint
	}()
	if err := writeNetworkFile(networkdManagedPath("eno1"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := beginNetworkCheckpoint(context.Background(), NetworkOperation{Backend: "systemd-networkd", Interface: "eno1", ReconnectToken: "reconnect-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rollbackNetworkCheckpoint(context.Background(), "systemd-networkd", checkpoint, "wrong-token"); !errors.Is(err, ErrNetworkCheckpoint) {
		t.Fatalf("wrong token error = %v", err)
	}
}

func TestNetworkFileAdaptersRejectConflictingOwnership(t *testing.T) {
	root := t.TempDir()
	previous := networkConfigRoot
	networkConfigRoot = root
	defer func() { networkConfigRoot = previous }()
	path := networkConfigPath("/etc/systemd/network/10-other.network")
	if err := writeNetworkFile(path, []byte("[Match]\nName=eno1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !conflictingNetworkdFile("eno1", networkdManagedPath("eno1")) {
		t.Fatal("unmanaged networkd match was not detected")
	}
	if networkBackendOwnsSnapshot("Netplan", NetworkOwnership{ActiveOwner: "systemd-networkd", Detected: []string{"Netplan", "systemd-networkd"}}) {
		t.Fatal("Netplan was allowed to mutate a layered networkd configuration")
	}
}

func TestIfupdownStateRoundTripAndStaticReplacement(t *testing.T) {
	root := t.TempDir()
	previous := networkConfigRoot
	networkConfigRoot = root
	defer func() { networkConfigRoot = previous }()
	path := ifupdownManagedPath("eno1")
	state := desiredNetworkState{IPv4Method: "manual", IPv6Method: "manual", Addresses: []string{"192.0.2.1/24", "2001:db8::1/64"}, DNS: []string{"1.1.1.1"}, Routes: []NetworkRoute{{Destination: "default", Gateway: "192.0.2.1", Metric: 100}}}
	if err := writeIfupdownState(path, "eno1", state); err != nil {
		t.Fatal(err)
	}
	read, err := readIfupdownState(path)
	if err != nil {
		t.Fatal(err)
	}
	if read.IPv4Method != "manual" || read.IPv6Method != "manual" || len(read.Addresses) != 2 || len(read.DNS) != 1 || len(read.Routes) != 1 || read.Routes[0].Metric != 100 {
		t.Fatalf("ifupdown state = %+v", read)
	}
	update := desiredNetworkState{IPv4Method: "auto", Addresses: []string{"192.0.2.10/24", "198.51.100.10/24"}, Gateways: []string{"192.0.2.1"}}
	if err := updateDesiredNetworkState(&update, NetworkOperation{Action: "static", Addresses: []string{"203.0.113.10/24"}, IPv4Gateway: "203.0.113.1"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(update.Addresses, ",") != "203.0.113.10/24" || strings.Join(update.Gateways, ",") != "203.0.113.1" {
		t.Fatalf("static replacement = %+v", update)
	}
	if err := updateDesiredNetworkState(&update, NetworkOperation{Action: "dhcp"}); err != nil {
		t.Fatal(err)
	}
	if len(update.Addresses) != 0 || len(update.Gateways) != 0 {
		t.Fatalf("DHCP retained static state = %+v", update)
	}
}
