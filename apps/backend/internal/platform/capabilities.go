package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

type CapabilityState string

const (
	StateReady       CapabilityState = "ready"
	StateDegraded    CapabilityState = "degraded"
	StateUnavailable CapabilityState = "unavailable"
	StateConflicted  CapabilityState = "conflicted"
)

type CapabilityScope struct {
	State     CapabilityState `json:"state"`
	Readable  bool            `json:"readable"`
	Mutable   bool            `json:"mutable"`
	Authority string          `json:"authority"`
	Reason    string          `json:"reason,omitempty"`
}

type Capability struct {
	ID                string                     `json:"id"`
	State             CapabilityState            `json:"state"`
	Backend           string                     `json:"backend,omitempty"`
	Version           string                     `json:"version,omitempty"`
	Readable          bool                       `json:"readable"`
	Mutable           bool                       `json:"mutable"`
	Rollback          bool                       `json:"rollback"`
	ReadAuthority     string                     `json:"readAuthority"`
	MutationAuthority string                     `json:"mutationAuthority"`
	Contract          string                     `json:"contract"`
	Reason            string                     `json:"reason,omitempty"`
	MissingDependency string                     `json:"missingDependency,omitempty"`
	SetupGuidance     string                     `json:"setupGuidance,omitempty"`
	Scopes            map[string]CapabilityScope `json:"scopes,omitempty"`
}

type runtimeProbe interface {
	BusNames(context.Context) (map[string]bool, error)
	FileExists(string) bool
	FileContains(string, string) bool
	CommandExists(string) bool
	CommandVersion(context.Context, string, ...string) (string, bool)
	CommandOutput(context.Context, string, ...string) (string, bool)
}

type hostProbe struct{}

var capabilityCache struct {
	sync.Mutex
	value    []Capability
	expires  time.Time
	inFlight chan struct{}
}

var versionCommands = map[string]string{
	"services": "systemctl",
	"logs":     "journalctl",
	"storage":  "udisksctl",
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (hostProbe) FileExists(path string) bool {
	return fileExists(path)
}

func (hostProbe) FileContains(path, expected string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	payload, err := readBounded(file, 64<<10)
	return err == nil && strings.Contains(string(payload), expected)
}

func (hostProbe) CommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (hostProbe) BusNames(ctx context.Context) (map[string]bool, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var names []string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return nil, err
	}
	var activatable []string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err != nil {
		return nil, err
	}
	names = append(names, activatable...)
	result := make(map[string]bool, len(names))
	for _, name := range names {
		result["active:"+name] = true
	}
	for _, name := range activatable {
		result["available:"+name] = true
	}
	return result, nil
}

func (hostProbe) CommandVersion(ctx context.Context, name string, arguments ...string) (string, bool) {
	return (hostProbe{}).CommandOutput(ctx, name, arguments...)
}

func (hostProbe) CommandOutput(ctx context.Context, name string, arguments ...string) (string, bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(versionCtx, path, arguments...)
	stdout, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return "", false
	}
	output, readErr := readBounded(stdout, 4096)
	if readErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return "", false
	}
	waitErr := command.Wait()
	if waitErr != nil {
		return "", false
	}
	line := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	if len(line) > 120 {
		line = line[:120]
	}
	return line, line != ""
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		if closer, ok := reader.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, errors.New("command output limit exceeded")
	}
	return payload, nil
}

func Detect(ctx context.Context, services ...*UpdateService) []Capability {
	for {
		now := time.Now()
		capabilityCache.Lock()
		if now.Before(capabilityCache.expires) {
			result := append([]Capability(nil), capabilityCache.value...)
			capabilityCache.Unlock()
			return result
		}
		if capabilityCache.inFlight != nil {
			inFlight := capabilityCache.inFlight
			capabilityCache.Unlock()
			select {
			case <-inFlight:
				continue
			case <-ctx.Done():
				return []Capability{unavailable("platform", "runtime-probes", "Capability detection was canceled", "Reload the page to retry capability detection.")}
			}
		}
		capabilityCache.inFlight = make(chan struct{})
		capabilityCache.Unlock()

		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		result := detect(probeCtx, hostProbe{}, services...)
		cancel()
		capabilityCache.Lock()
		capabilityCache.value = append([]Capability(nil), result...)
		capabilityCache.expires = now.Add(30 * time.Second)
		close(capabilityCache.inFlight)
		capabilityCache.inFlight = nil
		capabilityCache.Unlock()
		return result
	}
}

func detect(ctx context.Context, probe runtimeProbe, services ...*UpdateService) []Capability {
	busNames, busErr := probe.BusNames(ctx)
	capabilities := []Capability{
		localCapability("dashboard", "built-in", true, false),
		localCapability("metrics", "/proc", probe.FileExists("/proc/stat"), false),
		localCapability("processes", "/proc", probe.FileExists("/proc"), true),
		localCapability("users", "NSS", probe.FileExists("/etc/passwd"), true),
		unavailable("files", "user-bridge", "Authenticated user bridge is not connected", "Sign in through the production PAM service."),
		unavailable("terminal", "user-bridge", "Authenticated user bridge is not connected", "Sign in through the production PAM service."),
	}
	capabilities = append(capabilities,
		dbusCapability("services", "systemd", "org.freedesktop.systemd1", busNames, busErr, true, false),
		dbusCapability("logs", "journald", "org.freedesktop.systemd1", busNames, busErr, false, false),
		dbusCapability("storage", "UDisks2", "org.freedesktop.UDisks2", busNames, busErr, false, false),
	)
	capabilities = append(capabilities, updateCapability(ctx, services...))
	capabilities = append(capabilities, networkCapability(ctx, probe, busNames, busErr))
	capabilities = append(capabilities, firewallCapability(ctx, probe, busNames, busErr))
	capabilities = append(capabilities, policyCapability(ctx, probe, "selinux", "getenforce", "sestatus", "/sys/fs/selinux", "Install SELinux user-space tools and enable SELinux."))
	capabilities = append(capabilities, policyCapability(ctx, probe, "apparmor", "aa-status", "apparmor_parser", "/sys/module/apparmor", "Install AppArmor utilities and enable AppArmor."))
	for index := range capabilities {
		if capabilities[index].Version == "" {
			if command := versionCommands[capabilities[index].ID]; command != "" {
				if version, ok := probe.CommandVersion(ctx, command, "--version"); ok {
					capabilities[index].Version = version
				}
			}
		}
		if capabilities[index].ReadAuthority == "" {
			capabilities[index].ReadAuthority = "none"
			if capabilities[index].Readable {
				capabilities[index].ReadAuthority = "session"
			}
		}
		if capabilities[index].MutationAuthority == "" {
			capabilities[index].MutationAuthority = "none"
			if capabilities[index].Mutable {
				capabilities[index].MutationAuthority = "administrative"
			}
		}
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].ID < capabilities[j].ID })
	return capabilities
}

// localCapability describes kernel/NSS-backed modules. Mutable ones execute
// their privileged operations through sessiond (process signals, account and
// group management), so they advertise administrative write authority.
func localCapability(id, backend string, available, mutable bool) Capability {
	if !available {
		return unavailable(id, backend, "Required kernel interface is unavailable", "Run Tako on a supported Linux host.")
	}
	authority := "none"
	if mutable {
		authority = "administrative"
	}
	return Capability{ID: id, State: StateReady, Backend: backend, Readable: true, Mutable: mutable, ReadAuthority: "session", MutationAuthority: authority, Contract: "native"}
}

func unavailable(id, backend, reason, guidance string) Capability {
	return Capability{ID: id, State: StateUnavailable, Backend: backend, ReadAuthority: "none", MutationAuthority: "none", Contract: "unavailable", Reason: reason, SetupGuidance: guidance}
}

func dbusCapability(id, backend, busName string, names map[string]bool, busErr error, mutable, rollback bool) Capability {
	if busErr != nil {
		return Capability{ID: id, State: StateDegraded, Backend: backend, ReadAuthority: "none", MutationAuthority: "none", Contract: "degraded-read-only", Reason: "System D-Bus is unavailable: " + boundedError(busErr), SetupGuidance: "Start the system D-Bus service and allow Tako session services to access it."}
	}
	if !busAvailable(names, busName) {
		return Capability{ID: id, State: StateUnavailable, Backend: backend, ReadAuthority: "none", MutationAuthority: "none", Contract: "unavailable", Reason: busName + " is not running", MissingDependency: busName, SetupGuidance: "Install and start the " + backend + " service."}
	}
	mutationAuthority := "none"
	if mutable {
		mutationAuthority = "administrative"
	}
	return Capability{ID: id, State: StateReady, Backend: backend, Readable: true, Mutable: mutable, Rollback: rollback, ReadAuthority: "session", MutationAuthority: mutationAuthority, Contract: "dbus"}
}

func updateCapability(ctx context.Context, services ...*UpdateService) Capability {
	if len(services) == 0 || services[0] == nil || services[0].provider == nil {
		return unavailable("updates", "distro-provider", "No compile-time update provider was injected", "Build Tako with exactly one supported distro tag.")
	}
	service := services[0]
	version, err := service.provider.Probe(ctx)
	if err != nil {
		return unavailable("updates", service.ProviderName(), boundedError(err), "Install the native package manager required by this distro build.")
	}
	return Capability{ID: "updates", State: StateReady, Backend: service.ProviderName(), Version: version, Readable: true, Mutable: true, ReadAuthority: "session", MutationAuthority: "administrative", Contract: "native-distro-provider"}
}

func networkCapability(ctx context.Context, probe runtimeProbe, names map[string]bool, busErr error) Capability {
	networkManager := busErr == nil && busActive(names, "org.freedesktop.NetworkManager")
	networkd := (busErr == nil && busActive(names, "org.freedesktop.network1")) || probe.FileExists("/run/systemd/netif")
	if networkManager && networkd {
		return Capability{ID: "network", State: StateConflicted, Backend: "NetworkManager+networkd", Readable: true, Contract: "conflicted-read-only", Reason: "Multiple network managers are active; mutations fail closed", SetupGuidance: "Choose one network manager for each interface before editing in Tako."}
	}
	if networkManager {
		version, _ := probe.CommandVersion(ctx, "nmcli", "--version")
		// Mutations run through bounded nmcli commands with device
		// checkpoints; failures roll back automatically (network.go).
		return Capability{ID: "network", State: StateReady, Backend: "NetworkManager", Version: version, Readable: true, Mutable: true, Rollback: true, ReadAuthority: "session", MutationAuthority: "administrative", Contract: "bounded-command"}
	}
	if networkd {
		return Capability{ID: "network", State: StateDegraded, Backend: "systemd-networkd", Readable: true, Contract: "degraded-read-only", Reason: "networkd mutation adapter is not available in this release", SetupGuidance: "Use the terminal for changes; Tako will continue read-only inspection."}
	}
	return unavailable("network", "none", "No supported network manager detected", "Install and start NetworkManager.")
}

func firewallCapability(ctx context.Context, probe runtimeProbe, names map[string]bool, busErr error) Capability {
	firewalld := busErr == nil && busActive(names, "org.fedoraproject.FirewallD1")
	ufwVersion, ufwInstalled := probe.CommandVersion(ctx, "ufw", "--version")
	ufw := ufwInstalled && probe.FileContains("/etc/ufw/ufw.conf", "ENABLED=yes")
	if firewalld && ufw {
		return Capability{ID: "firewall", State: StateConflicted, Backend: "firewalld+UFW", Readable: true, Contract: "conflicted-read-only", Reason: "firewalld and UFW are both present; mutations fail closed", SetupGuidance: "Select and enable one firewall manager."}
	}
	if firewalld {
		version, _ := probe.CommandVersion(ctx, "firewall-cmd", "--version")
		// Zone and service mutations run as bounded firewall-cmd commands.
		return Capability{ID: "firewall", State: StateReady, Backend: "firewalld", Version: version, Readable: true, Mutable: true, ReadAuthority: "session", MutationAuthority: "administrative", Contract: "bounded-command"}
	}
	if ufw {
		return Capability{ID: "firewall", State: StateReady, Backend: "UFW", Version: ufwVersion, Readable: true, Mutable: true, ReadAuthority: "session", MutationAuthority: "administrative", Contract: "bounded-command"}
	}
	return unavailable("firewall", "none", "No supported firewall manager detected", "Install firewalld or UFW.")
}

func busActive(names map[string]bool, name string) bool {
	return names["active:"+name] || names[name]
}

func busAvailable(names map[string]bool, name string) bool {
	return busActive(names, name) || names["available:"+name]
}

func policyCapability(ctx context.Context, probe runtimeProbe, id, statusCommand, versionCommand, kernelPath, guidance string) Capability {
	commandFound := probe.CommandExists(statusCommand)
	kernelFound := probe.FileExists(kernelPath)
	if kernelFound && commandFound {
		status, statusOK := probe.CommandOutput(ctx, statusCommand)
		if !statusOK {
			return Capability{ID: id, State: StateDegraded, Backend: id, Readable: false, Contract: "kernel+bounded-command", Reason: "The policy status command is installed but did not return a trustworthy result.", SetupGuidance: guidance}
		}
		version, _ := probe.CommandVersion(ctx, versionCommand, "--version")
		if strings.EqualFold(strings.TrimSpace(status), "disabled") {
			return Capability{ID: id, State: StateDegraded, Backend: id, Version: version, Readable: true, Mutable: true, ReadAuthority: "session", MutationAuthority: "administrative", Contract: "kernel+bounded-command", Reason: id + " is installed but not enforcing", SetupGuidance: guidance}
		}
		// Booleans (setsebool), file contexts (restorecon), and AppArmor
		// profile modes are implemented mutations.
		return Capability{ID: id, State: StateReady, Backend: id, Version: version, Readable: true, Mutable: true, ReadAuthority: "session", MutationAuthority: "administrative", Contract: "kernel+bounded-command"}
	}
	missing := statusCommand
	if !kernelFound {
		missing = kernelPath
	}
	return Capability{ID: id, State: StateUnavailable, Backend: id, Contract: "unavailable", Reason: "Required kernel interface or user-space tool is unavailable", MissingDependency: missing, SetupGuidance: guidance}
}

func boundedError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 160 {
		message = message[:160]
	}
	if message == "" {
		return "unknown error"
	}
	return message
}
