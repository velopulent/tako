package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
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
	backend       string
	interfaceName string
	token         string
	expiresAt     time.Time
	files         []networkFileBackup
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

var networkCheckpointRoot = "/run/tako/network-checkpoints"

type networkFileCheckpointDisk struct {
	Backend       string                  `json:"backend"`
	InterfaceName string                  `json:"interface"`
	Token         string                  `json:"token"`
	ExpiresAt     time.Time               `json:"expiresAt"`
	Files         []networkFileBackupDisk `json:"files"`
}

type networkFileBackupDisk struct {
	Path    string      `json:"path"`
	Data    []byte      `json:"data,omitempty"`
	Mode    os.FileMode `json:"mode"`
	Existed bool        `json:"existed"`
}

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
	entry := networkFileCheckpoint{backend: operation.Backend, interfaceName: operation.Interface, token: operation.ReconnectToken, expiresAt: time.Now().UTC().Add(networkCheckpointTTL), files: backups}
	if err := persistNetworkFileCheckpoint(checkpoint, entry); err != nil {
		return "", err
	}
	networkFileCheckpoints.Lock()
	networkFileCheckpoints.values[checkpoint] = entry
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

func commitNetworkCheckpoint(ctx context.Context, backend, checkpoint, token string) error {
	if backend == "NetworkManager" {
		return networkManagerCheckpointDestroy(ctx, checkpoint)
	}
	entry, ok := loadNetworkFileCheckpoint(checkpoint)
	if !ok || entry.backend != backend || entry.token != token {
		return ErrNetworkCheckpoint
	}
	entry, ok = takeNetworkFileCheckpoint(checkpoint)
	if ok {
		if entry.backend != backend {
			return ErrNetworkCheckpoint
		}
		return removeNetworkFileCheckpoint(checkpoint)
	}
	entry, ok = loadNetworkFileCheckpoint(checkpoint)
	if !ok || entry.backend != backend {
		return ErrNetworkCheckpoint
	}
	return removeNetworkFileCheckpoint(checkpoint)
}

func rollbackNetworkCheckpoint(ctx context.Context, backend, checkpoint, token string) error {
	if backend == "NetworkManager" {
		return networkManagerCheckpointRollback(ctx, checkpoint)
	}
	entry, ok := loadNetworkFileCheckpoint(checkpoint)
	if !ok || entry.backend != backend || entry.token != token {
		return ErrNetworkCheckpoint
	}
	entry, ok = takeNetworkFileCheckpoint(checkpoint)
	if ok {
		if entry.backend != backend {
			return ErrNetworkCheckpoint
		}
	}
	if !ok {
		entry, ok = loadNetworkFileCheckpoint(checkpoint)
	}
	if !ok || entry.backend != backend {
		return ErrNetworkCheckpoint
	}
	for _, backup := range entry.files {
		if err := restoreNetworkBackup(backup); err != nil {
			return err
		}
	}
	if err := applyFileNetworkBackend(ctx, backend, entry.interfaceName); err != nil {
		return err
	}
	return removeNetworkFileCheckpoint(checkpoint)
}

func completeFileNetworkCheckpoint(ctx context.Context, operation NetworkOperation, snapshot NetworkSnapshot) (NetworkState, error) {
	var err error
	if operation.Action == "commit" {
		err = commitNetworkCheckpoint(ctx, operation.Backend, operation.Checkpoint, operation.ReconnectToken)
	} else {
		err = rollbackNetworkCheckpoint(ctx, operation.Backend, operation.Checkpoint, operation.ReconnectToken)
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
	return NetworkState{Snapshot: updated, Action: operation.Action, Checkpoint: operation.Checkpoint, Committed: operation.Action == "commit", Rollback: operation.Action == "rollback", ReconnectToken: operation.ReconnectToken, RollbackDeadline: time.Now().UTC().Add(networkCheckpointTTL), Warning: warning}, nil
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

func networkCheckpointPath(checkpoint string) string {
	if !validNetworkToken(checkpoint) {
		return ""
	}
	return filepath.Join(networkCheckpointRoot, checkpoint+".json")
}

func persistNetworkFileCheckpoint(checkpoint string, entry networkFileCheckpoint) error {
	path := networkCheckpointPath(checkpoint)
	if path == "" {
		return ErrNetworkCheckpoint
	}
	if err := os.MkdirAll(networkCheckpointRoot, 0o700); err != nil {
		return err
	}
	disk := networkFileCheckpointDisk{Backend: entry.backend, InterfaceName: entry.interfaceName, Token: entry.token, ExpiresAt: entry.expiresAt, Files: make([]networkFileBackupDisk, 0, len(entry.files))}
	for _, backup := range entry.files {
		disk.Files = append(disk.Files, networkFileBackupDisk{Path: backup.path, Data: backup.data, Mode: backup.mode, Existed: backup.existed})
	}
	data, err := json.Marshal(disk)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(networkCheckpointRoot, ".tako-checkpoint-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func loadNetworkFileCheckpoint(checkpoint string) (networkFileCheckpoint, bool) {
	path := networkCheckpointPath(checkpoint)
	if path == "" {
		return networkFileCheckpoint{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 2<<20 {
		return networkFileCheckpoint{}, false
	}
	var disk networkFileCheckpointDisk
	if json.Unmarshal(data, &disk) != nil || disk.Backend == "" || !validNetworkToken(disk.Token) || disk.ExpiresAt.IsZero() || !time.Now().UTC().Before(disk.ExpiresAt) || len(disk.Files) == 0 {
		return networkFileCheckpoint{}, false
	}
	entry := networkFileCheckpoint{backend: disk.Backend, interfaceName: disk.InterfaceName, token: disk.Token, expiresAt: disk.ExpiresAt, files: make([]networkFileBackup, 0, len(disk.Files))}
	for _, backup := range disk.Files {
		if !validNetworkConfigPath(backup.Path) || len(backup.Data) > maxNetworkConfigOutput {
			return networkFileCheckpoint{}, false
		}
		entry.files = append(entry.files, networkFileBackup{path: backup.Path, data: backup.Data, mode: backup.Mode, existed: backup.Existed})
	}
	return entry, true
}

func RecoverNetworkFileCheckpoint(backend, checkpoint, token string) bool {
	entry, ok := loadNetworkFileCheckpoint(checkpoint)
	return ok && entry.backend == backend && entry.token == token
}

func takeNetworkFileCheckpoint(checkpoint string) (networkFileCheckpoint, bool) {
	networkFileCheckpoints.Lock()
	entry, ok := networkFileCheckpoints.values[checkpoint]
	if ok {
		delete(networkFileCheckpoints.values, checkpoint)
	}
	networkFileCheckpoints.Unlock()
	return entry, ok
}

func removeNetworkFileCheckpoint(checkpoint string) error {
	path := networkCheckpointPath(checkpoint)
	if path == "" {
		return ErrNetworkCheckpoint
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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
	if !validNetworkConfigPath(path) {
		return networkFileBackup{}, ErrNetworkUnavailable
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return networkFileBackup{path: path}, nil
	}
	if err != nil {
		return networkFileBackup{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxNetworkConfigOutput {
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
	if !validNetworkConfigPath(path) || len(data) > maxNetworkConfigOutput {
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

func validNetworkConfigPath(path string) bool {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean != path || strings.ContainsAny(path, "\x00\r\n") {
		return false
	}
	allowed := []string{
		networkConfigPath("/etc/netplan"),
		networkConfigPath("/etc/systemd/network"),
		networkConfigPath("/etc/network"),
	}
	within := false
	for _, root := range allowed {
		if filePathWithin(root, clean) {
			within = true
			break
		}
	}
	if !within {
		return false
	}
	current := string(filepath.Separator)
	if networkConfigRoot != "" {
		current = filepath.Clean(networkConfigRoot)
	}
	for _, component := range strings.Split(strings.TrimPrefix(clean, current), string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

func conflictingNetworkdFile(iface, managed string) bool {
	matches, _ := filepath.Glob(networkConfigPath("/etc/systemd/network/*.network"))
	for _, path := range matches {
		if path == managed {
			continue
		}
		if !validNetworkConfigPath(path) {
			return true
		}
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return true
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
		if len(fields) >= 4 && fields[0] == "iface" && fields[2] == "inet" {
			if fields[3] == "dhcp" {
				state.IPv4Method = "auto"
			} else if fields[3] == "static" {
				state.IPv4Method = "manual"
			}
		}
		if len(fields) >= 4 && fields[0] == "iface" && fields[2] == "inet6" {
			if fields[3] == "auto" {
				state.IPv6Method = "auto"
			} else if fields[3] == "static" {
				state.IPv6Method = "manual"
			}
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
		if len(fields) >= 4 && fields[0] == "up" && fields[1] == "ip" && fields[2] == "route" && fields[3] == "add" {
			if route, ok := parseIfupdownRoute(fields[4:]); ok {
				state.Routes = append(state.Routes, route)
			}
		}
	}
	return state, nil
}

func parseIfupdownRoute(fields []string) (NetworkRoute, bool) {
	if len(fields) == 0 {
		return NetworkRoute{}, false
	}
	route := NetworkRoute{Destination: fields[0]}
	for index := 1; index < len(fields); index++ {
		switch fields[index] {
		case "via":
			if index+1 >= len(fields) {
				return NetworkRoute{}, false
			}
			route.Gateway = fields[index+1]
			index++
		case "metric":
			if index+1 >= len(fields) {
				return NetworkRoute{}, false
			}
			metric, err := strconv.Atoi(fields[index+1])
			if err != nil || metric < 0 || metric > 65535 {
				return NetworkRoute{}, false
			}
			route.Metric = metric
			index++
		default:
			return NetworkRoute{}, false
		}
	}
	return route, route.Destination != ""
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
		if route.Metric > 0 {
			builder.WriteString(" metric ")
			builder.WriteString(strconv.Itoa(route.Metric))
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
		state.Addresses = nil
		state.Gateways = nil
	case "static":
		addresses := networkOperationAddresses(operation)
		if len(addresses) == 0 {
			return ErrInvalidNetworkOperation
		}
		state.Addresses = append([]string(nil), addresses...)
		state.Gateways = nil
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
