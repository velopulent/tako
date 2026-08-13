package platform

import (
	"context"
	"os"

	"github.com/godbus/dbus/v5"
)

type Capability struct {
	ID        string `json:"id"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

var services = map[string]string{
	"services": "org.freedesktop.systemd1",
	"logs":     "org.freedesktop.systemd1",
	"updates":  "org.freedesktop.PackageKit",
	"storage":  "org.freedesktop.UDisks2",
	"network":  "org.freedesktop.NetworkManager",
}

func Detect(ctx context.Context) []Capability {
	capabilities := []Capability{
		{ID: "dashboard", Available: true},
		{ID: "metrics", Available: fileExists("/proc/stat")},
		{ID: "processes", Available: fileExists("/proc")},
		{ID: "users", Available: fileExists("/etc/passwd")},
		{ID: "terminal", Available: false, Reason: "user bridge not connected"},
	}
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		for id := range services {
			capabilities = append(capabilities, Capability{ID: id, Reason: "system D-Bus unavailable"})
		}
		return capabilities
	}
	defer conn.Close()
	var names []string
	err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0).Store(&names)
	if err != nil {
		for id := range services {
			capabilities = append(capabilities, Capability{ID: id, Reason: "cannot list D-Bus services"})
		}
		return capabilities
	}
	present := make(map[string]bool, len(names))
	for _, name := range names {
		present[name] = true
	}
	for id, name := range services {
		capability := Capability{ID: id, Available: present[name]}
		if !capability.Available {
			capability.Reason = name + " is not running"
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
