package platform

import (
	"context"
	"errors"

	"github.com/godbus/dbus/v5"
)

var errNetworkManagerConnectionMissing = errors.New("interface has no active network manager connection")

type networkManagerConnectionClient interface {
	ConnectionForInterface(context.Context, string) (string, error)
	Close()
}

type dbusNetworkManagerConnectionClient struct {
	connection *dbus.Conn
}

func newDBusNetworkManagerConnectionClient() (networkManagerConnectionClient, error) {
	connection, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	return &dbusNetworkManagerConnectionClient{connection: connection}, nil
}

var networkManagerConnectionFactory = newDBusNetworkManagerConnectionClient

func setNetworkManagerConnectionFactory(factory func() (networkManagerConnectionClient, error)) func() {
	previous := networkManagerConnectionFactory
	if factory == nil {
		networkManagerConnectionFactory = newDBusNetworkManagerConnectionClient
	} else {
		networkManagerConnectionFactory = factory
	}
	return func() { networkManagerConnectionFactory = previous }
}

func networkManagerConnectionForInterface(ctx context.Context, iface string) (string, error) {
	if !validNetworkInterface(iface) {
		return "", ErrInvalidNetworkOperation
	}
	client, err := networkManagerConnectionFactory()
	if err != nil {
		return "", err
	}
	if client == nil {
		return "", errors.New("network manager connection client is unavailable")
	}
	defer client.Close()
	return client.ConnectionForInterface(ctx, iface)
}

func (client *dbusNetworkManagerConnectionClient) Close() {
	if client != nil && client.connection != nil {
		_ = client.connection.Close()
	}
}

func (client *dbusNetworkManagerConnectionClient) ConnectionForInterface(ctx context.Context, iface string) (string, error) {
	if client == nil || client.connection == nil || !validNetworkInterface(iface) {
		return "", ErrInvalidNetworkOperation
	}

	device, err := client.deviceForInterface(ctx, iface)
	if err != nil {
		return "", err
	}
	active, err := client.objectProperty(ctx, device, "org.freedesktop.NetworkManager.Device", "ActiveConnection")
	if err != nil {
		return "", err
	}
	activePath, ok := active.Value().(dbus.ObjectPath)
	if !ok || activePath == "/" {
		return "", errNetworkManagerConnectionMissing
	}
	connection, err := client.objectProperty(ctx, activePath, "org.freedesktop.NetworkManager.Connection.Active", "Connection")
	if err != nil {
		return "", err
	}
	connectionPath, ok := connection.Value().(dbus.ObjectPath)
	if !ok || connectionPath == "/" {
		return "", errNetworkManagerConnectionMissing
	}

	settings := map[string]map[string]dbus.Variant{}
	if err := client.connection.Object(networkManagerService, connectionPath).CallWithContext(ctx, "org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0).Store(&settings); err != nil {
		return "", err
	}
	return networkManagerConnectionUUID(settings)
}

func networkManagerConnectionUUID(settings map[string]map[string]dbus.Variant) (string, error) {
	connection, ok := settings["connection"]
	if !ok {
		return "", errors.New("network manager connection settings are incomplete")
	}
	uuid, ok := connection["uuid"].Value().(string)
	if !ok || !validNetworkManagerUUID(uuid) {
		return "", errors.New("network manager connection UUID is invalid")
	}
	return uuid, nil
}

func validNetworkManagerUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') && (character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}

func (client *dbusNetworkManagerConnectionClient) deviceForInterface(ctx context.Context, iface string) (dbus.ObjectPath, error) {
	var device dbus.ObjectPath
	if err := client.connection.Object(networkManagerService, networkManagerPath).CallWithContext(ctx, networkManagerService+".GetDeviceByIpIface", 0, iface).Store(&device); err != nil {
		return "", err
	}
	if device == "/" {
		return "", errors.New("network manager device was not found")
	}
	return device, nil
}

func (client *dbusNetworkManagerConnectionClient) objectProperty(ctx context.Context, object dbus.ObjectPath, interfaceName, property string) (dbus.Variant, error) {
	var value dbus.Variant
	if err := client.connection.Object(networkManagerService, object).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, interfaceName, property).Store(&value); err != nil {
		return dbus.Variant{}, err
	}
	return value, nil
}
