package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeNetworkManagerCheckpoint struct {
	created   []string
	destroyed []string
	rolled    []string
}

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

