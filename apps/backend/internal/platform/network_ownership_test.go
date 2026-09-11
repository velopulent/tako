package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestResolveNetworkOwnershipUsesOnlyActiveBusOwners(t *testing.T) {
	ownership := resolveNetworkOwnership(map[string]bool{
		"active:" + networkManagerService: true,
		"available:" + networkdBusName:    true,
	}, []string{"Netplan"})

	if ownership.Conflicted || ownership.ActiveOwner != "NetworkManager" {
		t.Fatalf("activatable backend changed ownership: %#v", ownership)
	}
	if !reflect.DeepEqual(ownership.Detected, []string{"Netplan", "NetworkManager"}) {
		t.Fatalf("detected backends = %#v", ownership.Detected)
	}
}

func TestResolveNetworkOwnershipDoesNotInferRuntimeFromConfiguration(t *testing.T) {
	ownership := resolveNetworkOwnership(nil, []string{"Netplan", "ifupdown"})

	if ownership.Conflicted || ownership.ActiveOwner != "kernel" {
		t.Fatalf("configuration files were treated as active owners: %#v", ownership)
	}
	if !reflect.DeepEqual(ownership.Detected, []string{"Netplan", "ifupdown"}) {
		t.Fatalf("detected backends = %#v", ownership.Detected)
	}
}

func TestResolveNetworkOwnershipFailsClosedForMultipleActiveManagers(t *testing.T) {
	ownership := resolveNetworkOwnership(map[string]bool{
		"active:" + networkManagerService: true,
		"active:" + networkdBusName:       true,
	}, nil)

	if !ownership.Conflicted || ownership.ActiveOwner != "" {
		t.Fatalf("multiple active owners were not rejected: %#v", ownership)
	}
	if ownership.Reason != networkOwnershipConflictReason {
		t.Fatalf("conflict reason = %q", ownership.Reason)
	}
}

func TestNetworkMutationsRequireNetworkManager(t *testing.T) {
	_, err := ApplyNetworkOperation(context.Background(), NetworkOperation{
		Backend:             "systemd-networkd",
		Action:              "dhcp",
		Interface:           "eno1",
		ExpectedFingerprint: "fingerprint",
		Confirmation:        "CONFIRM NETWORK CHANGE",
	})
	if !errors.Is(err, ErrNetworkUnavailable) {
		t.Fatalf("non-NetworkManager mutation error = %v", err)
	}
}

func TestNetworkManagerOwnershipIsNotConfusedByConfigurationLayer(t *testing.T) {
	if !networkBackendOwnsSnapshot("NetworkManager", NetworkOwnership{
		ActiveOwner: "NetworkManager",
		Detected:    []string{"Netplan", "NetworkManager"},
	}) {
		t.Fatal("active NetworkManager was blocked by a non-runtime configuration layer")
	}
}
