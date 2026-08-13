package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxNetworkOutput = 2 << 20

var (
	ErrInvalidNetworkOperation = errors.New("invalid network operation")
	ErrNetworkConflict         = errors.New("network state changed")
	ErrNetworkOwnership        = errors.New("network ownership is conflicted")
	ErrNetworkUnavailable      = errors.New("network adapter unavailable")
)

type NetworkAddress struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	Family    string `json:"family"`
	Scope     string `json:"scope,omitempty"`
}

type NetworkRoute struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Device      string `json:"device,omitempty"`
	Metric      int    `json:"metric,omitempty"`
}

type NetworkOwnership struct {
	ActiveOwner string   `json:"activeOwner"`
	Detected    []string `json:"detected"`
	Conflicted  bool     `json:"conflicted"`
	Reason      string   `json:"reason,omitempty"`
}

type NetworkSnapshot struct {
	Interfaces  []Interface      `json:"interfaces"`
	Addresses   []NetworkAddress `json:"addresses"`
	Routes      []NetworkRoute   `json:"routes"`
	DNS         []string         `json:"dns"`
	Ownership   NetworkOwnership `json:"ownership"`
	Fingerprint string           `json:"fingerprint"`
}

type NetworkOperation struct {
	Backend             string   `json:"backend"`
	Action              string   `json:"action"`
	Interface           string   `json:"interface,omitempty"`
	Connection          string   `json:"connection,omitempty"`
	Address             string   `json:"address,omitempty"`
	Gateway             string   `json:"gateway,omitempty"`
	DNS                 []string `json:"dns,omitempty"`
	Route               string   `json:"route,omitempty"`
	Metric              int      `json:"metric,omitempty"`
	ExpectedFingerprint string   `json:"expectedFingerprint,omitempty"`
	Confirmation        string   `json:"confirmation,omitempty"`
	ReconnectToken      string   `json:"reconnectToken,omitempty"`
	Checkpoint          string   `json:"checkpoint,omitempty"`
}

type NetworkState struct {
	Snapshot   NetworkSnapshot `json:"snapshot"`
	Action     string          `json:"action"`
	Checkpoint string          `json:"checkpoint,omitempty"`
	Committed  bool            `json:"committed"`
	Rollback   bool            `json:"rollback"`
	Warning    string          `json:"warning,omitempty"`
}

func NetworkSnapshotRead(ctx context.Context) (NetworkSnapshot, error) {
	snapshot := NetworkSnapshot{Interfaces: []Interface{}, Addresses: []NetworkAddress{}, Routes: []NetworkRoute{}, DNS: []string{}}
	interfaces, err := Interfaces()
	if err != nil {
		return snapshot, err
	}
	snapshot.Interfaces = interfaces
	if payload, commandErr := networkCommand(ctx, "ip", "-j", "addr", "show"); commandErr == nil {
		var items []struct {
			IfName   string `json:"ifname"`
			AddrInfo []struct {
				Family    string `json:"family"`
				Local     string `json:"local"`
				Prefixlen int    `json:"prefixlen"`
				Scope     string `json:"scope"`
			} `json:"addr_info"`
		}
		if json.Unmarshal(payload, &items) == nil {
			for _, item := range items {
				for _, address := range item.AddrInfo {
					if address.Local == "" {
						continue
					}
					snapshot.Addresses = append(snapshot.Addresses, NetworkAddress{Interface: item.IfName, Address: fmt.Sprintf("%s/%d", address.Local, address.Prefixlen), Family: address.Family, Scope: address.Scope})
				}
			}
		}
	}
	if payload, commandErr := networkCommand(ctx, "ip", "-j", "route", "show"); commandErr == nil {
		var items []struct {
			Dst     string `json:"dst"`
			Gateway string `json:"gateway"`
			Dev     string `json:"dev"`
			Metric  int    `json:"metric"`
		}
		if json.Unmarshal(payload, &items) == nil {
			for _, route := range items {
				destination := route.Dst
				if destination == "" {
					destination = "default"
				}
				snapshot.Routes = append(snapshot.Routes, NetworkRoute{Destination: destination, Gateway: route.Gateway, Device: route.Dev, Metric: route.Metric})
			}
		}
	}
	snapshot.DNS = readDNSConfiguration()
	snapshot.Ownership = detectNetworkOwnership()
	for index := range snapshot.Interfaces {
		if snapshot.Ownership.ActiveOwner != "" {
			snapshot.Interfaces[index].Manager = snapshot.Ownership.ActiveOwner
		}
	}
	snapshot.Fingerprint = networkFingerprint(snapshot)
	return snapshot, nil
}

func networkCommand(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, name, arguments...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	payload, readErr := readBounded(stdout, maxNetworkOutput)
	waitErr := command.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		if commandCtx.Err() != nil {
			return nil, commandCtx.Err()
		}
		return nil, waitErr
	}
	return payload, nil
}

func readDNSConfiguration() []string {
	payload, err := os.ReadFile("/etc/resolv.conf")
	if err != nil || len(payload) > 64<<10 {
		return []string{}
	}
	result := []string{}
	for _, line := range strings.Split(string(payload), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" && net.ParseIP(fields[1]) != nil {
			result = append(result, fields[1])
		}
	}
	return result
}

func detectNetworkOwnership() NetworkOwnership {
	detected := []string{}
	if fileExists("/run/NetworkManager") || fileExists("/run/NetworkManager/nm-dhcp-client.action") {
		detected = append(detected, "NetworkManager")
	}
	if fileExists("/run/systemd/netif") || fileExists("/run/systemd/system/systemd-networkd.service") {
		detected = append(detected, "systemd-networkd")
	}
	if fileExists("/etc/netplan") {
		if entries, err := filepath.Glob("/etc/netplan/*.yaml"); err == nil && len(entries) > 0 {
			detected = append(detected, "Netplan")
		}
	}
	if fileExists("/etc/network/interfaces") {
		detected = append(detected, "ifupdown")
	}
	sort.Strings(detected)
	active := append([]string(nil), detected...)
	if len(active) > 1 && contains(active, "Netplan") {
		active = removeString(active, "Netplan")
	}
	ownership := NetworkOwnership{Detected: detected}
	if len(active) == 1 {
		ownership.ActiveOwner = active[0]
	} else if len(active) > 1 {
		ownership.Conflicted = true
		ownership.Reason = "Multiple active network owners were detected; configuration mutations are disabled."
	} else if len(detected) == 1 {
		ownership.ActiveOwner = detected[0]
	} else {
		ownership.ActiveOwner = "kernel"
	}
	return ownership
}

func networkFingerprint(snapshot NetworkSnapshot) string {
	payload, _ := json.Marshal(struct {
		Interfaces []Interface
		Addresses  []NetworkAddress
		Routes     []NetworkRoute
		DNS        []string
		Ownership  NetworkOwnership
	}{snapshot.Interfaces, snapshot.Addresses, snapshot.Routes, snapshot.DNS, snapshot.Ownership})
	return fingerprintBytes(payload)
}

func ValidateNetworkOperation(operation NetworkOperation) error {
	validBackends := map[string]bool{"NetworkManager": true, "Netplan": true, "systemd-networkd": true}
	validActions := map[string]bool{"preview": true, "dhcp": true, "static": true, "dns": true, "route-add": true, "route-remove": true, "checkpoint": true, "commit": true, "rollback": true}
	if !validBackends[operation.Backend] || !validActions[operation.Action] || len(operation.Interface) > 256 || len(operation.Connection) > 256 || len(operation.Address) > 128 || len(operation.Gateway) > 128 || len(operation.Route) > 128 || len(operation.ReconnectToken) > 256 || len(operation.ExpectedFingerprint) > 128 || strings.ContainsAny(operation.Interface+operation.Connection+operation.Address+operation.Gateway+operation.Route, "\x00\r\n") {
		return ErrInvalidNetworkOperation
	}
	if operation.Action != "preview" && operation.Action != "checkpoint" && operation.ExpectedFingerprint == "" {
		return ErrInvalidNetworkOperation
	}
	if operation.Action == "static" {
		if _, _, err := net.ParseCIDR(operation.Address); err != nil {
			return ErrInvalidNetworkOperation
		}
	}
	if operation.Gateway != "" && net.ParseIP(operation.Gateway) == nil {
		return ErrInvalidNetworkOperation
	}
	if operation.Action == "dns" && (len(operation.DNS) == 0 || len(operation.DNS) > 8) {
		return ErrInvalidNetworkOperation
	}
	for _, value := range operation.DNS {
		if net.ParseIP(value) == nil {
			return ErrInvalidNetworkOperation
		}
	}
	if operation.Action == "route-add" || operation.Action == "route-remove" {
		if _, _, err := net.ParseCIDR(operation.Route); err != nil {
			return ErrInvalidNetworkOperation
		}
	}
	if operation.Action != "preview" && operation.Confirmation != "CONFIRM NETWORK CHANGE" {
		return ErrInvalidNetworkOperation
	}
	if (operation.Action == "commit" || operation.Action == "rollback") && operation.Checkpoint == "" {
		return ErrInvalidNetworkOperation
	}
	return nil
}

func PreviewNetworkOperation(ctx context.Context, operation NetworkOperation) (NetworkState, error) {
	operation.Action = "preview"
	if err := ValidateNetworkOperation(operation); err != nil {
		return NetworkState{}, err
	}
	snapshot, err := NetworkSnapshotRead(ctx)
	if err != nil {
		return NetworkState{}, err
	}
	return NetworkState{Snapshot: snapshot, Action: "preview", Rollback: snapshot.Ownership.ActiveOwner == "NetworkManager" && !snapshot.Ownership.Conflicted}, nil
}

func ApplyNetworkOperation(ctx context.Context, operation NetworkOperation) (NetworkState, error) {
	if err := ValidateNetworkOperation(operation); err != nil {
		return NetworkState{}, err
	}
	snapshot, err := NetworkSnapshotRead(ctx)
	if err != nil {
		return NetworkState{}, err
	}
	if snapshot.Ownership.Conflicted || snapshot.Ownership.ActiveOwner != operation.Backend {
		return NetworkState{}, ErrNetworkOwnership
	}
	if operation.ExpectedFingerprint != "" && operation.ExpectedFingerprint != snapshot.Fingerprint {
		return NetworkState{}, ErrNetworkConflict
	}
	if operation.Backend == "Netplan" {
		return applyNetplanOperation(ctx, operation, snapshot)
	}
	if operation.Backend != "NetworkManager" {
		return NetworkState{}, ErrNetworkUnavailable
	}
	if operation.Action == "checkpoint" {
		payload, checkpointErr := networkCommand(ctx, "nmcli", "device", "checkpoint", operation.Interface, "--timeout", "120", "--persist", "no")
		if checkpointErr != nil {
			return NetworkState{}, checkpointErr
		}
		checkpoint := firstLine(string(payload))
		return NetworkState{Snapshot: snapshot, Action: "checkpoint", Checkpoint: checkpoint, Rollback: checkpoint != "", Warning: "Checkpoint expires automatically; commit only after a fresh connection is verified."}, nil
	}
	if operation.Action == "commit" || operation.Action == "rollback" {
		argument := "--" + operation.Action
		if _, commandErr := networkCommand(ctx, "nmcli", "device", "checkpoint", operation.Checkpoint, argument); commandErr != nil {
			return NetworkState{}, commandErr
		}
		updated, readErr := NetworkSnapshotRead(ctx)
		if readErr != nil {
			return NetworkState{}, readErr
		}
		return NetworkState{Snapshot: updated, Action: operation.Action, Committed: operation.Action == "commit", Rollback: operation.Action == "rollback", Checkpoint: operation.Checkpoint}, nil
	}
	checkpointPayload, checkpointErr := networkCommand(ctx, "nmcli", "device", "checkpoint", operation.Interface, "--timeout", "120", "--persist", "no")
	if checkpointErr != nil {
		return NetworkState{}, checkpointErr
	}
	checkpoint := firstLine(string(checkpointPayload))
	arguments := []string{"device"}
	switch operation.Action {
	case "dhcp":
		arguments = append(arguments, "modify", operation.Interface, "ipv4.method", "auto", "ipv4.addresses", "", "ipv4.gateway", "", "ipv4.dns", "")
	case "static":
		arguments = append(arguments, "modify", operation.Interface, "ipv4.method", "manual", "ipv4.addresses", operation.Address, "ipv4.gateway", operation.Gateway)
	case "dns":
		arguments = append(arguments, "modify", operation.Interface, "ipv4.dns", strings.Join(operation.DNS, ","))
	case "route-add":
		arguments = append(arguments, "modify", operation.Interface, "+ipv4.routes", operation.Route)
	case "route-remove":
		arguments = append(arguments, "modify", operation.Interface, "-ipv4.routes", operation.Route)
	case "commit":
		arguments = append(arguments, "connect", operation.Interface)
	case "rollback", "checkpoint":
		return NetworkState{}, ErrNetworkUnavailable
	default:
		return NetworkState{}, ErrInvalidNetworkOperation
	}
	if _, err := networkCommand(ctx, "nmcli", arguments...); err != nil {
		if checkpoint != "" {
			_, _ = networkCommand(ctx, "nmcli", "device", "checkpoint", checkpoint, "--rollback")
		}
		return NetworkState{}, err
	}
	if operation.Action == "commit" || operation.Action == "dhcp" || operation.Action == "static" || operation.Action == "dns" || strings.HasPrefix(operation.Action, "route-") {
		if _, err := networkCommand(ctx, "nmcli", "device", "connect", operation.Interface); err != nil {
			if checkpoint != "" {
				_, _ = networkCommand(ctx, "nmcli", "device", "checkpoint", checkpoint, "--rollback")
			}
			return NetworkState{}, err
		}
	}
	updated, err := NetworkSnapshotRead(ctx)
	if err != nil {
		return NetworkState{}, err
	}
	if checkpoint != "" {
		if _, commitErr := networkCommand(ctx, "nmcli", "device", "checkpoint", checkpoint, "--commit"); commitErr != nil {
			return NetworkState{Snapshot: updated, Action: operation.Action, Checkpoint: checkpoint, Committed: false, Rollback: true, Warning: "The change applied but checkpoint commit needs explicit operator verification."}, commitErr
		}
	}
	return NetworkState{Snapshot: updated, Action: operation.Action, Checkpoint: checkpoint, Committed: true, Rollback: false, Warning: "Reconnect verification completed before checkpoint commit."}, nil
}

func applyNetplanOperation(ctx context.Context, operation NetworkOperation, snapshot NetworkSnapshot) (NetworkState, error) {
	if operation.Interface == "" || strings.ContainsAny(operation.Interface, "./\\") {
		return NetworkState{}, ErrInvalidNetworkOperation
	}
	key := "ethernets." + operation.Interface
	arguments := []string{"set"}
	switch operation.Action {
	case "dhcp":
		arguments = append(arguments, key+".dhcp4=true")
	case "static":
		arguments = append(arguments, key+".dhcp4=false", key+".addresses=["+operation.Address+"]")
		if operation.Gateway != "" {
			arguments = append(arguments, key+".routes=[{to=default,via="+operation.Gateway+"}]")
		}
	case "dns":
		arguments = append(arguments, key+".nameservers.addresses=["+strings.Join(operation.DNS, ",")+"]")
	default:
		return NetworkState{}, ErrNetworkUnavailable
	}
	if _, err := networkCommand(ctx, "netplan", arguments...); err != nil {
		return NetworkState{}, err
	}
	if _, err := networkCommand(ctx, "netplan", "try", "--timeout", "30"); err != nil {
		_, _ = networkCommand(ctx, "netplan", "rollback")
		return NetworkState{}, err
	}
	if _, err := networkCommand(ctx, "netplan", "apply"); err != nil {
		_, _ = networkCommand(ctx, "netplan", "rollback")
		return NetworkState{}, err
	}
	updated, err := NetworkSnapshotRead(ctx)
	if err != nil {
		return NetworkState{}, err
	}
	return NetworkState{Snapshot: updated, Action: operation.Action, Committed: true, Rollback: false, Warning: "Netplan try completed and runtime state was re-read before reporting success."}, nil
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func removeString(values []string, value string) []string {
	result := values[:0]
	for _, item := range values {
		if item != value {
			result = append(result, item)
		}
	}
	return result
}

func networkErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidNetworkOperation):
		return "invalid-network-operation"
	case errors.Is(err, ErrNetworkConflict):
		return "network-conflict"
	case errors.Is(err, ErrNetworkOwnership):
		return "network-ownership-conflict"
	case errors.Is(err, ErrNetworkUnavailable):
		return "network-unavailable"
	default:
		return "network-operation-failed"
	}
}

func fingerprintBytes(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
