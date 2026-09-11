package platform

import (
	"context"
	"path/filepath"
	"sort"
)

const (
	networkdBusName = "org.freedesktop.network1"

	networkOwnershipConflictReason = "Multiple active network owners were detected; configuration mutations are disabled."
)

func detectNetworkOwnership(ctx context.Context) NetworkOwnership {
	names, err := (hostProbe{}).BusNames(ctx)
	if err != nil {
		names = nil
	}
	return resolveNetworkOwnership(names, configuredNetworkBackends())
}

func resolveNetworkOwnership(names map[string]bool, configured []string) NetworkOwnership {
	active := activeNetworkBackends(names)
	detected := append([]string(nil), active...)
	detected = append(detected, configured...)
	detected = uniqueSortedStrings(detected)

	ownership := NetworkOwnership{Detected: detected}
	switch len(active) {
	case 0:
		ownership.ActiveOwner = "kernel"
	case 1:
		ownership.ActiveOwner = active[0]
	default:
		ownership.Conflicted = true
		ownership.Reason = networkOwnershipConflictReason
	}
	return ownership
}

func activeNetworkBackends(names map[string]bool) []string {
	active := make([]string, 0, 2)
	if busActive(names, networkManagerService) {
		active = append(active, "NetworkManager")
	}
	if busActive(names, networkdBusName) {
		active = append(active, "systemd-networkd")
	}
	sort.Strings(active)
	return active
}

func configuredNetworkBackends() []string {
	configured := make([]string, 0, 2)
	if matches, err := filepath.Glob("/etc/netplan/*.yaml"); err == nil && len(matches) > 0 {
		configured = append(configured, "Netplan")
	}
	if fileExists("/etc/network/interfaces") {
		configured = append(configured, "ifupdown")
	}
	return uniqueSortedStrings(configured)
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	result := sorted[:0]
	for _, value := range sorted {
		if value == "" || (len(result) > 0 && result[len(result)-1] == value) {
			continue
		}
		result = append(result, value)
	}
	return result
}
