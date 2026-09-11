package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

type fakeNetworkManagerCheckpoint struct {
	created   []string
	destroyed []string
	rolled    []string
}

type fakeNetworkManagerConnection struct {
	target string
	err    error
}

func (fake *fakeNetworkManagerConnection) ConnectionForInterface(context.Context, string) (string, error) {
	return fake.target, fake.err
}

func (*fakeNetworkManagerConnection) Close() {}

func (fake *fakeNetworkManagerCheckpoint) Create(_ context.Context, iface string, timeout uint32) (string, error) {
	fake.created = append(fake.created, iface+":"+string(rune(timeout)))
	return "/org/freedesktop/NetworkManager/Checkpoint/7", nil
}

func (fake *fakeNetworkManagerCheckpoint) Destroy(_ context.Context, checkpoint string) error {
	fake.destroyed = append(fake.destroyed, checkpoint)
	return nil
}

func (fake *fakeNetworkManagerCheckpoint) Rollback(_ context.Context, checkpoint string) error {
	fake.rolled = append(fake.rolled, checkpoint)
	return nil
}

func (*fakeNetworkManagerCheckpoint) Close() {}

func TestNetworkFingerprintExcludesInterfaceCounters(t *testing.T) {
	left := NetworkSnapshot{
		Interfaces: []Interface{{Name: "eno1", Index: 2, MTU: 1500, Hardware: "00:11:22:33:44:55", Manager: "NetworkManager", RX: 10, TX: 20}},
		Addresses:  []NetworkAddress{{Interface: "eno1", Address: "192.0.2.10/24", Family: "inet"}},
		Routes:     []NetworkRoute{{Destination: "default", Gateway: "192.0.2.1", Device: "eno1"}},
		DNS:        []string{"192.0.2.53"},
		Ownership:  NetworkOwnership{ActiveOwner: "NetworkManager", Detected: []string{"NetworkManager"}},
	}
	right := left
	right.Interfaces = []Interface{{Name: "eno1", Index: 2, MTU: 1500, Hardware: "00:11:22:33:44:55", Manager: "NetworkManager", RX: 99999, TX: 88888}}
	if networkFingerprint(left) != networkFingerprint(right) {
		t.Fatal("traffic counters changed the network mutation fingerprint")
	}
}

func TestNetworkManagerModifyIsPersistentAndSupportsBothFamilies(t *testing.T) {
	arguments, err := networkManagerModifyArguments(NetworkOperation{
		Action:      "static",
		Interface:   "eno1",
		Connection:  "Wired connection 1",
		IPv4Address: "192.0.2.10/24",
		IPv4Gateway: "192.0.2.1",
		IPv6Address: "2001:db8::10/64",
		IPv6Gateway: "2001:db8::1",
	}, "Wired connection 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(arguments) < 3 || !reflect.DeepEqual(arguments[:3], []string{"connection", "modify", "Wired connection 1"}) {
		t.Fatalf("unexpected persistent command: %#v", arguments)
	}
	joined := strings.Join(arguments, " ")
	for _, value := range []string{"ipv4.method", "ipv4.addresses", "ipv6.method", "ipv6.addresses"} {
		if !strings.Contains(joined, value) {
			t.Fatalf("%q missing from persistent command: %s", value, joined)
		}
	}
	if strings.Contains(joined, "device modify") {
		t.Fatalf("runtime-only device modify was used: %s", joined)
	}
}

func TestNetworkManagerMutationResolvesActiveProfileWhenConnectionIsOmitted(t *testing.T) {
	commands := make([][]string, 0, 2)
	restoreRunner := setNetworkCommandRunner(func(_ context.Context, _ string, arguments ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), arguments...))
		return nil, nil
	})
	defer restoreRunner()
	restoreResolver := setNetworkManagerConnectionFactory(func() (networkManagerConnectionClient, error) {
		return &fakeNetworkManagerConnection{target: "9d3e3c2a-6f14-4a56-9f9e-2cc2a0a8d4f5"}, nil
	})
	defer restoreResolver()

	err := applyNetworkManagerPersistent(context.Background(), NetworkOperation{
		Action:    "dhcp",
		Interface: "eno1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 {
		t.Fatalf("NetworkManager commands = %#v", commands)
	}
	if len(commands[0]) < 3 || commands[0][0] != "connection" || commands[0][1] != "modify" || commands[0][2] != "9d3e3c2a-6f14-4a56-9f9e-2cc2a0a8d4f5" {
		t.Fatalf("profile UUID was not used for modification: %#v", commands[0])
	}
	if len(commands[1]) < 5 || commands[1][0] != "connection" || commands[1][1] != "up" || commands[1][2] != "9d3e3c2a-6f14-4a56-9f9e-2cc2a0a8d4f5" || commands[1][3] != "ifname" || commands[1][4] != "eno1" {
		t.Fatalf("profile UUID was not used for activation: %#v", commands[1])
	}
}

func TestNetworkManagerMutationRejectsMissingActiveProfile(t *testing.T) {
	restoreResolver := setNetworkManagerConnectionFactory(func() (networkManagerConnectionClient, error) {
		return &fakeNetworkManagerConnection{err: errNetworkManagerConnectionMissing}, nil
	})
	defer restoreResolver()

	err := applyNetworkManagerPersistent(context.Background(), NetworkOperation{Action: "dhcp", Interface: "eno1"})
	if !errors.Is(err, ErrNetworkUnavailable) {
		t.Fatalf("missing active profile error = %v", err)
	}
}

func TestNetworkManagerConnectionUUIDRequiresConnectionSettings(t *testing.T) {
	if _, err := networkManagerConnectionUUID(nil); err == nil {
		t.Fatal("missing connection settings were accepted")
	}
	if _, err := networkManagerConnectionUUID(map[string]map[string]dbus.Variant{
		"connection": {"uuid": dbus.MakeVariant("not a uuid")},
	}); err == nil {
		t.Fatal("invalid connection UUID was accepted")
	}
	uuid := "9d3e3c2a-6f14-4a56-9f9e-2cc2a0a8d4f5"
	got, err := networkManagerConnectionUUID(map[string]map[string]dbus.Variant{
		"connection": {"uuid": dbus.MakeVariant(uuid)},
	})
	if err != nil || got != uuid {
		t.Fatalf("connection UUID = %q, err=%v", got, err)
	}
}

func TestNetworkManagerCheckpointUsesDBusLifecycle(t *testing.T) {
	fake := &fakeNetworkManagerCheckpoint{}
	restore := setNetworkManagerCheckpointFactory(func() (networkManagerCheckpointClient, error) { return fake, nil })
	defer restore()
	checkpoint, err := networkManagerCheckpointCreate(context.Background(), "eno1")
	if err != nil {
		t.Fatal(err)
	}
	if err := networkManagerCheckpointDestroy(context.Background(), checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := networkManagerCheckpointRollback(context.Background(), checkpoint); err != nil {
		t.Fatal(err)
	}
	if len(fake.created) != 1 || fake.created[0][:4] != "eno1" || len(fake.destroyed) != 1 || len(fake.rolled) != 1 {
		t.Fatalf("checkpoint lifecycle = %#v", fake)
	}
}

func TestNetworkManagerCheckpointRejectsUntrustedPath(t *testing.T) {
	fake := &fakeNetworkManagerCheckpoint{}
	restore := setNetworkManagerCheckpointFactory(func() (networkManagerCheckpointClient, error) { return fake, nil })
	defer restore()
	if err := networkManagerCheckpointDestroy(context.Background(), "/tmp/other"); !errors.Is(err, ErrNetworkCheckpoint) {
		t.Fatalf("untrusted checkpoint error = %v", err)
	}
}
