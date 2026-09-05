package platform

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	networkManagerService = "org.freedesktop.NetworkManager"
	networkManagerPath    = dbus.ObjectPath("/org/freedesktop/NetworkManager")
	networkManagerFlags   = uint32(0x06) // delete new connections and disconnect new devices on rollback
)

type networkManagerCheckpointClient interface {
	Create(context.Context, string, uint32) (string, error)
	Destroy(context.Context, string) error
	Rollback(context.Context, string) error
	Close()
}

type dbusNetworkManagerCheckpointClient struct {
	connection *dbus.Conn
}

var networkManagerCheckpointFactory = func() (networkManagerCheckpointClient, error) {
	connection, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	return &dbusNetworkManagerCheckpointClient{connection: connection}, nil
}

// setNetworkManagerCheckpointFactory is package-private so tests can model
// NetworkManager's checkpoint lifecycle without a system bus.
func setNetworkManagerCheckpointFactory(factory func() (networkManagerCheckpointClient, error)) func() {
	previous := networkManagerCheckpointFactory
	if factory == nil {
		networkManagerCheckpointFactory = func() (networkManagerCheckpointClient, error) {
			connection, err := dbus.ConnectSystemBus()
			if err != nil {
				return nil, err
			}
			return &dbusNetworkManagerCheckpointClient{connection: connection}, nil
		}
	} else {
		networkManagerCheckpointFactory = factory
	}
	return func() { networkManagerCheckpointFactory = previous }
}

func (client *dbusNetworkManagerCheckpointClient) Close() {
	if client != nil && client.connection != nil {
		_ = client.connection.Close()
	}
}

func (client *dbusNetworkManagerCheckpointClient) Create(ctx context.Context, iface string, timeout uint32) (string, error) {
	if client == nil || client.connection == nil || !validNetworkInterface(iface) {
		return "", ErrInvalidNetworkOperation
	}
	var device dbus.ObjectPath
	if err := client.connection.Object(networkManagerService, networkManagerPath).CallWithContext(ctx, networkManagerService+".GetDeviceByIpIface", 0, iface).Store(&device); err != nil {
		return "", err
	}
	var checkpoint dbus.ObjectPath
	if err := client.connection.Object(networkManagerService, networkManagerPath).CallWithContext(ctx, networkManagerService+".CheckpointCreate", 0, []dbus.ObjectPath{device}, timeout, networkManagerFlags).Store(&checkpoint); err != nil {
		return "", err
	}
	value := string(checkpoint)
	if !validNetworkCheckpoint(value) {
		return "", ErrNetworkCheckpoint
	}
	return value, nil
}

func (client *dbusNetworkManagerCheckpointClient) Destroy(ctx context.Context, checkpoint string) error {
	if client == nil || client.connection == nil || !validNetworkCheckpoint(checkpoint) {
		return ErrNetworkCheckpoint
	}
	return client.connection.Object(networkManagerService, networkManagerPath).CallWithContext(ctx, networkManagerService+".CheckpointDestroy", 0, dbus.ObjectPath(checkpoint)).Err
}

func (client *dbusNetworkManagerCheckpointClient) Rollback(ctx context.Context, checkpoint string) error {
	if client == nil || client.connection == nil || !validNetworkCheckpoint(checkpoint) {
		return ErrNetworkCheckpoint
	}
	result := map[string]uint32{}
	return client.connection.Object(networkManagerService, networkManagerPath).CallWithContext(ctx, networkManagerService+".CheckpointRollback", 0, dbus.ObjectPath(checkpoint)).Store(&result)
}

func networkManagerCheckpointCreate(ctx context.Context, iface string) (string, error) {
	client, err := networkManagerCheckpointFactory()
	if err != nil {
		return "", fmtNetworkManagerUnavailable(err)
	}
	defer client.Close()
	checkpoint, err := client.Create(ctx, iface, uint32(networkCheckpointTTL/time.Second))
	if err != nil {
		return "", fmtNetworkManagerUnavailable(err)
	}
	return checkpoint, nil
}

func networkManagerCheckpointDestroy(ctx context.Context, checkpoint string) error {
	if !strings.HasPrefix(checkpoint, "/org/freedesktop/NetworkManager/Checkpoint/") || !validNetworkCheckpoint(checkpoint) {
		return ErrNetworkCheckpoint
	}
	client, err := networkManagerCheckpointFactory()
	if err != nil {
		return fmtNetworkManagerUnavailable(err)
	}
	defer client.Close()
	if err := client.Destroy(ctx, checkpoint); err != nil {
		return fmtNetworkManagerUnavailable(err)
	}
	return nil
}

func networkManagerCheckpointRollback(ctx context.Context, checkpoint string) error {
	if !strings.HasPrefix(checkpoint, "/org/freedesktop/NetworkManager/Checkpoint/") || !validNetworkCheckpoint(checkpoint) {
		return ErrNetworkCheckpoint
	}
	client, err := networkManagerCheckpointFactory()
	if err != nil {
		return fmtNetworkManagerUnavailable(err)
	}
	defer client.Close()
	if err := client.Rollback(ctx, checkpoint); err != nil {
		return fmtNetworkManagerUnavailable(err)
	}
	return nil
}

func fmtNetworkManagerUnavailable(err error) error {
	if errors.Is(err, ErrInvalidNetworkOperation) || errors.Is(err, ErrNetworkCheckpoint) {
		return err
	}
	return errors.Join(ErrNetworkUnavailable, err)
}

func applyNetworkManagerPersistent(ctx context.Context, operation NetworkOperation) error {
	target := operation.Connection
	if target == "" {
		target = operation.Interface
	}
	arguments, err := networkManagerModifyArguments(operation, target)
	if err != nil {
		return err
	}
	if _, err := networkCommand(ctx, "nmcli", arguments...); err != nil {
		return err
	}
	up := []string{"connection", "up", target}
	if operation.Interface != "" {
		up = append(up, "ifname", operation.Interface)
	}
	_, err = networkCommand(ctx, "nmcli", up...)
	return err
}

func networkManagerModifyArguments(operation NetworkOperation, target string) ([]string, error) {
	arguments := []string{"connection", "modify", target}
	appendProperty := func(name, value string) { arguments = append(arguments, name, value) }
	switch operation.Action {
	case "dhcp":
		ipv4Method, ipv6Method := operation.IPv4Method, operation.IPv6Method
		if ipv4Method == "" {
			ipv4Method = "auto"
		}
		if ipv6Method == "" {
			ipv6Method = "auto"
		}
		appendProperty("ipv4.method", ipv4Method)
		appendProperty("ipv6.method", ipv6Method)
		if ipv4Method != "manual" {
			appendProperty("ipv4.addresses", "")
			appendProperty("ipv4.gateway", "")
			appendProperty("ipv4.dns", "")
		}
		if ipv6Method != "manual" {
			appendProperty("ipv6.addresses", "")
			appendProperty("ipv6.gateway", "")
			appendProperty("ipv6.dns", "")
		}
	case "static":
		addresses := networkOperationAddresses(operation)
		ipv4, ipv6 := splitNetworkAddresses(addresses)
		if len(ipv4) > 0 {
			appendProperty("ipv4.method", nonEmptyNetworkValue(operation.IPv4Method, "manual"))
			appendProperty("ipv4.addresses", strings.Join(ipv4, ","))
			appendProperty("ipv4.gateway", networkGatewayForFamily(operation, false))
		}
		if len(ipv6) > 0 {
			appendProperty("ipv6.method", nonEmptyNetworkValue(operation.IPv6Method, "manual"))
			appendProperty("ipv6.addresses", strings.Join(ipv6, ","))
			appendProperty("ipv6.gateway", networkGatewayForFamily(operation, true))
		}
	case "dns":
		ipv4, ipv6 := splitNetworkIPs(operation.DNS)
		if len(ipv4) > 0 {
			appendProperty("ipv4.dns", strings.Join(ipv4, ","))
		}
		if len(ipv6) > 0 {
			appendProperty("ipv6.dns", strings.Join(ipv6, ","))
		}
	case "route-add", "route-remove":
		ipv6 := strings.Contains(operation.Route, ":") || strings.Contains(networkGatewayForFamily(operation, true), ":")
		family := "ipv4"
		if ipv6 {
			family = "ipv6"
		}
		route := operation.Route
		if route == "default" {
			if ipv6 {
				route = "::/0"
			} else {
				route = "0.0.0.0/0"
			}
		}
		value := route
		if gateway := networkGatewayForFamily(operation, ipv6); gateway != "" {
			value += " " + gateway
		}
		if operation.Metric > 0 {
			value += " " + strconv.Itoa(operation.Metric)
		}
		prefix := "+"
		if operation.Action == "route-remove" {
			prefix = "-"
		}
		appendProperty(prefix+family+".routes", value)
	default:
		return nil, ErrInvalidNetworkOperation
	}
	return arguments, nil
}

func networkOperationAddresses(operation NetworkOperation) []string {
	values := append([]string(nil), operation.Addresses...)
	if operation.Address != "" {
		values = append(values, operation.Address)
	}
	if operation.IPv4Address != "" {
		values = append(values, operation.IPv4Address)
	}
	if operation.IPv6Address != "" {
		values = append(values, operation.IPv6Address)
	}
	return sortedNetworkStrings(values)
}

func networkGatewayForFamily(operation NetworkOperation, ipv6 bool) string {
	if ipv6 && operation.IPv6Gateway != "" {
		return operation.IPv6Gateway
	}
	if !ipv6 && operation.IPv4Gateway != "" {
		return operation.IPv4Gateway
	}
	if operation.Gateway == "" {
		return ""
	}
	ip := net.ParseIP(operation.Gateway)
	if ip == nil || (ip.To4() == nil) != ipv6 {
		return ""
	}
	return operation.Gateway
}

func splitNetworkAddresses(values []string) (ipv4, ipv6 []string) {
	for _, value := range sortedNetworkStrings(values) {
		ip, _, err := net.ParseCIDR(value)
		if err != nil {
			continue
		}
		if ip.To4() == nil {
			ipv6 = append(ipv6, value)
		} else {
			ipv4 = append(ipv4, value)
		}
	}
	return ipv4, ipv6
}

func splitNetworkIPs(values []string) (ipv4, ipv6 []string) {
	for _, value := range sortedNetworkStrings(values) {
		ip := net.ParseIP(value)
		if ip == nil {
			continue
		}
		if ip.To4() == nil {
			ipv6 = append(ipv6, value)
		} else {
			ipv4 = append(ipv4, value)
		}
	}
	return ipv4, ipv6
}

func sortedNetworkStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	write := 0
	for _, value := range result {
		if value == "" || (write > 0 && result[write-1] == value) {
			continue
		}
		result[write] = value
		write++
	}
	return result[:write]
}

func nonEmptyNetworkValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func validNetworkMethod(method string) bool {
	return method == "auto" || method == "manual" || method == "disabled" || method == "ignore"
}

func validNetworkInterface(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > 15 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && !strings.ContainsRune("._:@-", character) {
			return false
		}
	}
	return true
}

func validNetworkConnection(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func validNetworkCheckpoint(value string) bool {
	if strings.HasPrefix(value, "/org/freedesktop/NetworkManager/Checkpoint/") {
		name := strings.TrimPrefix(value, "/org/freedesktop/NetworkManager/Checkpoint/")
		if name == "" {
			return false
		}
		for _, character := range name {
			if character < '0' || character > '9' {
				return false
			}
		}
		return true
	}
	return validNetworkConnection(value)
}

func validNetworkToken(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n /\\")
}

func networkBackendOwnsSnapshot(backend string, ownership NetworkOwnership) bool {
	if backend == "systemd-networkd" && contains(ownership.Detected, "Netplan") {
		return false
	}
	if ownership.ActiveOwner == backend {
		return true
	}
	return backend == "Netplan" && ownership.ActiveOwner == "systemd-networkd" && contains(ownership.Detected, "Netplan")
}

func newNetworkToken(prefix string) string {
	var value [18]byte
	if _, err := rand.Read(value[:]); err != nil {
		return prefix + "-" + fingerprintBytes([]byte(time.Now().UTC().String()))[:24]
	}
	return prefix + "-" + hexNetwork(value[:])
}

func hexNetwork(value []byte) string {
	const digits = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, item := range value {
		result[index*2] = digits[item>>4]
		result[index*2+1] = digits[item&0x0f]
	}
	return string(result)
}
