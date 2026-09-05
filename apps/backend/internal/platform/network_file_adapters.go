package platform

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// networkConfigRoot is empty on a host and may be set by package tests to a
// temporary root. All paths below are validated before they reach the file
// system; no user supplied path is ever joined directly.
var networkConfigRoot string

const maxNetworkConfigOutput = 1 << 20

type networkFileBackup struct {
	path    string
	data    []byte
	mode    os.FileMode
	existed bool
}

type networkFileCheckpoint struct {
	backend string
	files   []networkFileBackup
}

type desiredNetworkState struct {
	IPv4Method string
	IPv6Method string
	Addresses  []string
	Gateways   []string
	DNS        []string
	Routes     []NetworkRoute
}

var networkFileCheckpoints = struct {
	sync.Mutex
	values map[string]networkFileCheckpoint
}{values: make(map[string]networkFileCheckpoint)}

func beginNetworkCheckpoint(ctx context.Context, operation NetworkOperation) (string, error) {
	if operation.Interface == "" || !validNetworkInterface(operation.Interface) {
		return "", ErrInvalidNetworkOperation
	}
	if operation.Backend == "NetworkManager" {
		return networkManagerCheckpointCreate(ctx, operation.Interface)
	}
	paths := []string{}
	switch operation.Backend {
	case "Netplan":
		matches, err := filepath.Glob(networkConfigPath("/etc/netplan/*.yaml"))
		if err != nil {
			return "", err
		}
		paths = append(paths, matches...)
		if len(paths) == 0 {
			paths = append(paths, netplanFallbackPath())
		}
	case "systemd-networkd":
		paths = []string{networkdManagedPath(operation.Interface)}
	case "ifupdown":
		paths = []string{ifupdownManagedPath(operation.Interface)}
	default:
		return "", ErrNetworkUnavailable
	}
	checkpoint := newNetworkToken("tako-network")
	backups := make([]networkFileBackup, 0, len(paths))
	for _, path := range paths {
		backup, err := readNetworkBackup(path)
		if err != nil {
			return "", err
		}
		backups = append(backups, backup)
	}
	networkFileCheckpoints.Lock()
	networkFileCheckpoints.values[checkpoint] = networkFileCheckpoint{backend: operation.Backend, files: backups}
	networkFileCheckpoints.Unlock()
	return checkpoint, nil
}

func applyNetworkMutation(ctx context.Context, operation NetworkOperation) error {
	switch operation.Backend {
	case "NetworkManager":
		return applyNetworkManagerPersistent(ctx, operation)
	case "Netplan":
		return applyNetplanPersistent(ctx, operation)
	case "systemd-networkd":
		return applyNetworkdPersistent(ctx, operation)
	case "ifupdown":
		return applyIfupdownPersistent(ctx, operation)
	default:
		return ErrNetworkUnavailable
	}
}

func commitNetworkCheckpoint(ctx context.Context, backend, checkpoint string) error {
	if backend == "NetworkManager" {
		return networkManagerCheckpointDestroy(ctx, checkpoint)
	}
	networkFileCheckpoints.Lock()
	entry, ok := networkFileCheckpoints.values[checkpoint]
	if ok {
		delete(networkFileCheckpoints.values, checkpoint)
	}
	networkFileCheckpoints.Unlock()
	if !ok || entry.backend != backend {
		return ErrNetworkCheckpoint
	}
	return nil
}

func rollbackNetworkCheckpoint(ctx context.Context, backend, checkpoint string) error {
	if backend == "NetworkManager" {
		return networkManagerCheckpointRollback(ctx, checkpoint)
	}
	networkFileCheckpoints.Lock()
	entry, ok := networkFileCheckpoints.values[checkpoint]
	if ok {
		delete(networkFileCheckpoints.values, checkpoint)
	}
	networkFileCheckpoints.Unlock()
	if !ok || entry.backend != backend {
		return ErrNetworkCheckpoint
	}
	for _, backup := range entry.files {
		if err := restoreNetworkBackup(backup); err != nil {
			return err
		}
	}
	return applyFileNetworkBackend(ctx, backend, "")
}

func completeFileNetworkCheckpoint(ctx context.Context, operation NetworkOperation, snapshot NetworkSnapshot) (NetworkState, error) {
	var err error
	if operation.Action == "commit" {
		err = commitNetworkCheckpoint(ctx, operation.Backend, operation.Checkpoint)
	} else {
		err = rollbackNetworkCheckpoint(ctx, operation.Backend, operation.Checkpoint)
	}
	if err != nil {
		return NetworkState{}, err
	}
	updated, err := NetworkSnapshotRead(ctx)
	if err != nil {
		return NetworkState{}, err
	}
	warning := "The network change was rolled back."
	if operation.Action == "commit" {
		warning = "The network change was committed after reconnect confirmation."
	}
	return NetworkState{Snapshot: updated, Action: operation.Action, Checkpoint: operation.Checkpoint, Committed: operation.Action == "commit", Rollback: operation.Action == "rollback", ReconnectToken: operation.ReconnectToken, Warning: warning}, nil
}

func applyNetplanPersistent(ctx context.Context, operation NetworkOperation) error {
	if !validNetworkInterface(operation.Interface) {
		return ErrInvalidNetworkOperation
	}
	key := "ethernets." + operation.Interface
	arguments := []string{"set"}
	switch operation.Action {
	case "dhcp":
		ipv4Method, ipv6Method := operation.IPv4Method, operation.IPv6Method
		if ipv4Method == "" {
			ipv4Method = "true"
		} else {
			ipv4Method = strconv.FormatBool(ipv4Method == "auto")
		}
		if ipv6Method == "" {
			ipv6Method = "true"
		} else {
			ipv6Method = strconv.FormatBool(ipv6Method == "auto")
		}
		arguments = append(arguments, key+".dhcp4="+ipv4Method, key+".dhcp6="+ipv6Method)
	case "static":
		addresses := networkOperationAddresses(operation)
		ipv4, ipv6 := splitNetworkAddresses(addresses)
		if len(ipv4) > 0 {
			arguments = append(arguments, key+".dhcp4=false", key+".addresses=["+strings.Join(ipv4, ",")+"]")
			if gateway := networkGatewayForFamily(operation, false); gateway != "" {
				arguments = append(arguments, key+".routes=[{to=default,via="+gateway+"}]")
			}
		}
		if len(ipv6) > 0 {
			arguments = append(arguments, key+".dhcp6=false", key+".addresses=["+strings.Join(ipv6, ",")+"]")
			if gateway := networkGatewayForFamily(operation, true); gateway != "" {
				arguments = append(arguments, key+".routes=[{to=::/0,via="+gateway+"}]")
			}
		}
	case "dns":
		arguments = append(arguments, key+".nameservers.addresses=["+strings.Join(operation.DNS, ",")+"]")
	case "route-add":
		route := operation.Route
		if route == "default" {
			if strings.Contains(networkGatewayForFamily(operation, true), ":") {
				route = "::/0"
			} else {
				route = "0.0.0.0/0"
			}
		}
		value := "{to=" + route
		if gateway := networkGatewayForFamily(operation, strings.Contains(route, ":")); gateway != "" {
			value += ",via=" + gateway
		}
		if operation.Metric > 0 {
			value += ",metric=" + strconv.Itoa(operation.Metric)
		}
		arguments = append(arguments, key+".routes=["+value+"}]")
	case "route-remove":
		// Netplan's CLI cannot remove one arbitrary route from a merged YAML
		// list without replacing the complete list. Refuse this action rather
		// than deleting routes owned by another configuration file.
		return ErrNetworkUnavailable
	default:
		return ErrInvalidNetworkOperation
	}
	if _, err := networkCommand(ctx, "netplan", arguments...); err != nil {
		return err
	}
	_, err := networkCommand(ctx, "netplan", "apply")
	return err
}

func applyNetworkdPersistent(ctx context.Context, operation NetworkOperation) error {
	path := networkdManagedPath(operation.Interface)
	if conflictingNetworkdFile(operation.Interface, path) {
		return ErrNetworkOwnership
	}
	state, err := readNetworkdState(path)
	if err != nil {
		return err
	}
	if err := updateDesiredNetworkState(&state, operation); err != nil {
		return err
	}
	if err := writeNetworkdState(path, operation.Interface, state); err != nil {
		return err
	}
	return applyFileNetworkBackend(ctx, "systemd-networkd", operation.Interface)
}

func applyIfupdownPersistent(ctx context.Context, operation NetworkOperation) error {
	path := ifupdownManagedPath(operation.Interface)
	if conflictingIfupdownConfig(operation.Interface, path) {
		return ErrNetworkOwnership
	}
	state, err := readIfupdownState(path)
	if err != nil {
		return err
	}
	if err := updateDesiredNetworkState(&state, operation); err != nil {
		return err
	}
	if err := writeIfupdownState(path, operation.Interface, state); err != nil {
		return err
	}
	return applyFileNetworkBackend(ctx, "ifupdown", operation.Interface)
}

func applyFileNetworkBackend(ctx context.Context, backend, iface string) error {
	switch backend {
	case "Netplan":
		if _, err := networkCommand(ctx, "netplan", "generate"); err != nil {
			return err
		}
		_, err := networkCommand(ctx, "netplan", "apply")
		return err
	case "systemd-networkd":
		if _, err := networkCommand(ctx, "networkctl", "reload"); err != nil {
			return err
		}
		_, err := networkCommand(ctx, "networkctl", "reconfigure", iface)
		return err
	case "ifupdown":
		// An already-down device makes ifdown fail; the subsequent ifup is the
		// operation that establishes the new persistent state.
		_, _ = networkCommand(ctx, "ifdown", "--force", iface)
		_, err := networkCommand(ctx, "ifup", "--force", iface)
		return err
	default:
		return ErrNetworkUnavailable
	}
}

func networkConfigPath(path string) string {
	if networkConfigRoot == "" {
		return path
	}
	return filepath.Join(networkConfigRoot, strings.TrimPrefix(path, string(filepath.Separator)))
}

func netplanFallbackPath() string {
	return networkConfigPath("/etc/netplan/99-tako.yaml")
}

func networkdManagedPath(iface string) string {
	return networkConfigPath("/etc/systemd/network/99-tako-" + iface + ".network")
}

func ifupdownManagedPath(iface string) string {
	return networkConfigPath("/etc/network/interfaces.d/99-tako-" + iface)
}

func readNetworkBackup(path string) (networkFileBackup, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return networkFileBackup{path: path}, nil
	}
	if err != nil {
		return networkFileBackup{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxNetworkConfigOutput {
		return networkFileBackup{}, ErrNetworkUnavailable
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return networkFileBackup{}, err
	}
	return networkFileBackup{path: path, data: data, mode: info.Mode().Perm(), existed: true}, nil
}

func restoreNetworkBackup(backup networkFileBackup) error {
	if !backup.existed {
		if err := os.Remove(backup.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeNetworkFile(backup.path, backup.data, backup.mode)
}

func writeNetworkFile(path string, data []byte, mode os.FileMode) error {
	if len(data) > maxNetworkConfigOutput {
		return ErrNetworkUnavailable
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tako-network-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func conflictingNetworkdFile(iface, managed string) bool {
	matches, _ := filepath.Glob(networkConfigPath("/etc/systemd/network/*.network"))
	for _, path := range matches {
		if path == managed {
			continue
		}
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "Name="+iface) {
			return true
		}
	}
	return false
}

func conflictingIfupdownConfig(iface, managed string) bool {
	paths := []string{networkConfigPath("/etc/network/interfaces")}
	matches, _ := filepath.Glob(networkConfigPath("/etc/network/interfaces.d/*"))
	paths = append(paths, matches...)
	for _, path := range paths {
		if path == managed {
			continue
		}
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "iface "+iface+" ") {
			return true
		}
	}
	return false
}

func (operation NetworkOperation) String() string {
	return fmt.Sprintf("%s/%s/%s", operation.Backend, operation.Action, operation.Interface)
}

func readNetworkdState(path string) (desiredNetworkState, error) {
	state := desiredNetworkState{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if len(data) > maxNetworkConfigOutput {
		return state, ErrNetworkUnavailable
	}
	var route *NetworkRoute
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "DHCP="):
			value := strings.TrimPrefix(line, "DHCP=")
			if value == "ipv4" || value == "yes" {
				state.IPv4Method = "auto"
			}
			if value == "ipv6" || value == "yes" {
				state.IPv6Method = "auto"
			}
		case strings.HasPrefix(line, "Address="):
			state.Addresses = append(state.Addresses, strings.TrimPrefix(line, "Address="))
		case strings.HasPrefix(line, "Gateway="):
			state.Gateways = append(state.Gateways, strings.TrimPrefix(line, "Gateway="))
		case strings.HasPrefix(line, "DNS="):
			state.DNS = append(state.DNS, strings.TrimPrefix(line, "DNS="))
		case line == "[Route]":
			state.Routes = append(state.Routes, NetworkRoute{})
			route = &state.Routes[len(state.Routes)-1]
		case route != nil && strings.HasPrefix(line, "Destination="):
			route.Destination = strings.TrimPrefix(line, "Destination=")
		case route != nil && strings.HasPrefix(line, "Gateway="):
			route.Gateway = strings.TrimPrefix(line, "Gateway=")
		case route != nil && strings.HasPrefix(line, "Metric="):
			route.Metric, _ = strconv.Atoi(strings.TrimPrefix(line, "Metric="))
		}
	}
	return state, nil
}

func writeNetworkdState(path, iface string, state desiredNetworkState) error {
	var builder strings.Builder
	builder.WriteString("[Match]\nName=")
	builder.WriteString(iface)
	builder.WriteString("\n\n[Network]\n")
	if state.IPv4Method == "auto" && state.IPv6Method == "auto" {
		builder.WriteString("DHCP=yes\n")
	} else {
		if state.IPv4Method == "auto" {
			builder.WriteString("DHCP=ipv4\n")
		}
		if state.IPv6Method == "auto" {
			builder.WriteString("DHCP=ipv6\n")
		}
	}
	for _, address := range sortedNetworkStrings(state.Addresses) {
		builder.WriteString("Address=")
		builder.WriteString(address)
		builder.WriteByte('\n')
	}
	for _, gateway := range sortedNetworkStrings(state.Gateways) {
		builder.WriteString("Gateway=")
		builder.WriteString(gateway)
		builder.WriteByte('\n')
	}
	for _, dns := range sortedNetworkStrings(state.DNS) {
		builder.WriteString("DNS=")
		builder.WriteString(dns)
		builder.WriteByte('\n')
	}
	for _, route := range state.Routes {
		builder.WriteString("\n[Route]\nDestination=")
		builder.WriteString(route.Destination)
		builder.WriteByte('\n')
		if route.Gateway != "" {
			builder.WriteString("Gateway=")
			builder.WriteString(route.Gateway)
			builder.WriteByte('\n')
		}
		if route.Metric > 0 {
			builder.WriteString("Metric=")
			builder.WriteString(strconv.Itoa(route.Metric))
			builder.WriteByte('\n')
		}
	}
	return writeNetworkFile(path, []byte(builder.String()), 0o644)
}

func readIfupdownState(path string) (desiredNetworkState, error) {
	state := desiredNetworkState{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if len(data) > maxNetworkConfigOutput {
		return state, ErrNetworkUnavailable
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "iface" && fields[2] == "dhcp" {
			if fields[1] == "iface" {
				state.IPv4Method = "auto"
			} else if fields[1] == "iface6" {
				state.IPv6Method = "auto"
			}
		}
		if len(fields) >= 4 && fields[0] == "iface" && fields[2] == "inet6" && fields[3] == "auto" {
			state.IPv6Method = "auto"
		}
		if len(fields) >= 2 && fields[0] == "address" {
			state.Addresses = append(state.Addresses, fields[1])
		}
		if len(fields) >= 2 && fields[0] == "gateway" {
			state.Gateways = append(state.Gateways, fields[1])
		}
		if len(fields) >= 2 && fields[0] == "dns-nameservers" {
			state.DNS = append(state.DNS, fields[1:]...)
		}
	}
	return state, nil
}

func writeIfupdownState(path, iface string, state desiredNetworkState) error {
	var builder strings.Builder
	builder.WriteString("auto ")
	builder.WriteString(iface)
	builder.WriteByte('\n')
	ipv4, ipv6 := splitNetworkAddresses(state.Addresses)
	if state.IPv4Method == "auto" || len(ipv4) == 0 {
		builder.WriteString("iface ")
		builder.WriteString(iface)
		builder.WriteString(" inet dhcp\n")
	} else {
		builder.WriteString("iface ")
		builder.WriteString(iface)
		builder.WriteString(" inet static\n")
		builder.WriteString("  address ")
		builder.WriteString(ipv4[0])
		builder.WriteByte('\n')
	}
	if state.IPv6Method == "auto" || len(ipv6) == 0 {
		builder.WriteString("iface ")
		builder.WriteString(iface)
		builder.WriteString(" inet6 auto\n")
	} else {
		builder.WriteString("iface ")
		builder.WriteString(iface)
		builder.WriteString(" inet6 static\n")
		builder.WriteString("  address ")
		builder.WriteString(ipv6[0])
		builder.WriteByte('\n')
	}
	for _, gateway := range sortedNetworkStrings(state.Gateways) {
		builder.WriteString("  gateway ")
		builder.WriteString(gateway)
		builder.WriteByte('\n')
	}
	if len(state.DNS) > 0 {
		builder.WriteString("  dns-nameservers ")
		builder.WriteString(strings.Join(sortedNetworkStrings(state.DNS), " "))
		builder.WriteByte('\n')
	}
	for _, route := range state.Routes {
		builder.WriteString("  up ip route add ")
		builder.WriteString(route.Destination)
		if route.Gateway != "" {
			builder.WriteString(" via ")
			builder.WriteString(route.Gateway)
		}
		builder.WriteByte('\n')
		builder.WriteString("  down ip route del ")
		builder.WriteString(route.Destination)
		if route.Gateway != "" {
			builder.WriteString(" via ")
			builder.WriteString(route.Gateway)
		}
		builder.WriteByte('\n')
	}
	return writeNetworkFile(path, []byte(builder.String()), 0o644)
}

func updateDesiredNetworkState(state *desiredNetworkState, operation NetworkOperation) error {
	switch operation.Action {
	case "dhcp":
		state.IPv4Method = nonEmptyNetworkValue(operation.IPv4Method, "auto")
		state.IPv6Method = nonEmptyNetworkValue(operation.IPv6Method, "auto")
	case "static":
		addresses := networkOperationAddresses(operation)
		if len(addresses) == 0 {
			return ErrInvalidNetworkOperation
		}
		state.Addresses = append(state.Addresses, addresses...)
		for _, address := range addresses {
			ip, _, _ := net.ParseCIDR(address)
			if ip != nil && ip.To4() == nil {
				state.IPv6Method = nonEmptyNetworkValue(operation.IPv6Method, "manual")
			} else {
				state.IPv4Method = nonEmptyNetworkValue(operation.IPv4Method, "manual")
			}
		}
		if gateway := networkGatewayForFamily(operation, false); gateway != "" {
			state.Gateways = append(state.Gateways, gateway)
		}
		if gateway := networkGatewayForFamily(operation, true); gateway != "" {
			state.Gateways = append(state.Gateways, gateway)
		}
	case "dns":
		state.DNS = sortedNetworkStrings(operation.DNS)
	case "route-add":
		route := NetworkRoute{Destination: operation.Route, Gateway: operation.Gateway, Metric: operation.Metric}
		for _, existing := range state.Routes {
			if existing == route {
				return nil
			}
		}
		state.Routes = append(state.Routes, route)
	case "route-remove":
		kept := state.Routes[:0]
		for _, existing := range state.Routes {
			if existing.Destination == operation.Route && (operation.Gateway == "" || existing.Gateway == operation.Gateway) {
				continue
			}
			kept = append(kept, existing)
		}
		state.Routes = kept
	default:
		return ErrInvalidNetworkOperation
	}
	state.Addresses = sortedNetworkStrings(state.Addresses)
	state.Gateways = sortedNetworkStrings(state.Gateways)
	state.DNS = sortedNetworkStrings(state.DNS)
	return nil
}
