package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

type HostConfiguration struct {
	Hostname    string `json:"hostname"`
	Timezone    string `json:"timezone"`
	NTPEnabled  bool   `json:"ntpEnabled"`
	Fingerprint string `json:"fingerprint"`
}

func ReadHostConfiguration(ctx context.Context) (HostConfiguration, error) {
	connection, err := dbus.ConnectSystemBus()
	if err != nil {
		return HostConfiguration{}, err
	}
	defer connection.Close()
	hostname, err := dbusStringProperty(ctx, connection, "org.freedesktop.hostname1", "/org/freedesktop/hostname1", "org.freedesktop.hostname1.StaticHostname")
	if err != nil {
		return HostConfiguration{}, fmt.Errorf("read hostname: %w", err)
	}
	timezone, err := dbusStringProperty(ctx, connection, "org.freedesktop.timedate1", "/org/freedesktop/timedate1", "org.freedesktop.timedate1.Timezone")
	if err != nil {
		return HostConfiguration{}, fmt.Errorf("read timezone: %w", err)
	}
	ntp, err := dbusBoolProperty(ctx, connection, "org.freedesktop.timedate1", "/org/freedesktop/timedate1", "org.freedesktop.timedate1.NTP")
	if err != nil {
		return HostConfiguration{}, fmt.Errorf("read NTP state: %w", err)
	}
	return NewHostConfiguration(hostname, timezone, ntp), nil
}

func ApplyHostConfiguration(ctx context.Context, current, desired HostConfiguration) error {
	connection, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	defer connection.Close()
	if current.Hostname != desired.Hostname {
		if err := connection.Object("org.freedesktop.hostname1", "/org/freedesktop/hostname1").CallWithContext(ctx, "org.freedesktop.hostname1.SetStaticHostname", 0, desired.Hostname, true).Err; err != nil {
			return fmt.Errorf("set hostname: %w", err)
		}
	}
	if current.Timezone != desired.Timezone {
		if err := connection.Object("org.freedesktop.timedate1", "/org/freedesktop/timedate1").CallWithContext(ctx, "org.freedesktop.timedate1.SetTimezone", 0, desired.Timezone, true).Err; err != nil {
			return fmt.Errorf("set timezone: %w", err)
		}
	}
	if current.NTPEnabled != desired.NTPEnabled {
		if err := connection.Object("org.freedesktop.timedate1", "/org/freedesktop/timedate1").CallWithContext(ctx, "org.freedesktop.timedate1.SetNTP", 0, desired.NTPEnabled, true).Err; err != nil {
			return fmt.Errorf("set NTP state: %w", err)
		}
	}
	return nil
}

func NewHostConfiguration(hostname, timezone string, ntpEnabled bool) HostConfiguration {
	configuration := HostConfiguration{Hostname: hostname, Timezone: timezone, NTPEnabled: ntpEnabled}
	payload := strings.Join([]string{hostname, timezone, strconv.FormatBool(ntpEnabled)}, "\x00")
	digest := sha256.Sum256([]byte(payload))
	configuration.Fingerprint = hex.EncodeToString(digest[:])
	return configuration
}

func dbusStringProperty(ctx context.Context, connection *dbus.Conn, service, path, property string) (string, error) {
	value, err := dbusProperty(ctx, connection, service, path, property)
	if err != nil {
		return "", err
	}
	result, ok := value.Value().(string)
	if !ok {
		return "", fmt.Errorf("property %s has unexpected type", property)
	}
	return result, nil
}

func dbusBoolProperty(ctx context.Context, connection *dbus.Conn, service, path, property string) (bool, error) {
	value, err := dbusProperty(ctx, connection, service, path, property)
	if err != nil {
		return false, err
	}
	result, ok := value.Value().(bool)
	if !ok {
		return false, fmt.Errorf("property %s has unexpected type", property)
	}
	return result, nil
}

func dbusProperty(ctx context.Context, connection *dbus.Conn, service, path, property string) (dbus.Variant, error) {
	separator := strings.LastIndexByte(property, '.')
	if separator <= 0 || separator == len(property)-1 {
		return dbus.Variant{}, errors.New("invalid D-Bus property")
	}
	interfaceName, propertyName := property[:separator], property[separator+1:]
	var value dbus.Variant
	if err := connection.Object(service, dbus.ObjectPath(path)).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, interfaceName, propertyName).Store(&value); err != nil {
		return dbus.Variant{}, err
	}
	return value, nil
}
