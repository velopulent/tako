package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

type PowerActionStatus struct {
	State     string `json:"state"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type PowerInhibitor struct {
	What string `json:"what"`
	Who  string `json:"who"`
	Why  string `json:"why"`
	Mode string `json:"mode"`
	UID  uint32 `json:"uid"`
	PID  uint32 `json:"pid"`
}

type PowerStatus struct {
	Available   bool              `json:"available"`
	Reboot      PowerActionStatus `json:"reboot"`
	Shutdown    PowerActionStatus `json:"shutdown"`
	Inhibitors  []PowerInhibitor  `json:"inhibitors"`
	Fingerprint string            `json:"fingerprint"`
	Reason      string            `json:"reason,omitempty"`
}

func ReadPowerStatus(ctx context.Context) (PowerStatus, error) {
	connection, err := dbus.ConnectSystemBus()
	if err != nil {
		return PowerStatus{}, err
	}
	defer connection.Close()
	manager := connection.Object("org.freedesktop.login1", "/org/freedesktop/login1")
	rebootState, err := powerMethod(ctx, manager, "CanReboot")
	if err != nil {
		return PowerStatus{}, err
	}
	shutdownState, err := powerMethod(ctx, manager, "CanPowerOff")
	if err != nil {
		return PowerStatus{}, err
	}
	var raw []struct {
		What string
		Who  string
		Why  string
		Mode string
		UID  uint32
		PID  uint32
	}
	if err := manager.CallWithContext(ctx, "org.freedesktop.login1.Manager.ListInhibitors", 0).Store(&raw); err != nil {
		return PowerStatus{}, err
	}
	inhibitors := make([]PowerInhibitor, 0, len(raw))
	for _, item := range raw {
		inhibitors = append(inhibitors, PowerInhibitor{What: item.What, Who: item.Who, Why: item.Why, Mode: item.Mode, UID: item.UID, PID: item.PID})
	}
	status := PowerStatus{
		Available:  true,
		Reboot:     powerActionStatus(rebootState, "reboot"),
		Shutdown:   powerActionStatus(shutdownState, "shutdown"),
		Inhibitors: inhibitors,
	}
	status.Reboot = applyPowerInhibitors(status.Reboot, "reboot", inhibitors)
	status.Shutdown = applyPowerInhibitors(status.Shutdown, "shutdown", inhibitors)
	status.Fingerprint = powerFingerprint(status)
	return status, nil
}

func applyPowerInhibitors(status PowerActionStatus, action string, inhibitors []PowerInhibitor) PowerActionStatus {
	if !status.Available {
		return status
	}
	for _, inhibitor := range inhibitors {
		if inhibitor.Mode != "block" {
			continue
		}
		for _, inhibited := range strings.FieldsFunc(inhibitor.What, func(r rune) bool { return r == ':' || r == ',' || r == ' ' }) {
			if inhibited == action || (action == "shutdown" && inhibited == "power-off") {
				return PowerActionStatus{State: "inhibited", Reason: inhibitor.Who + " is blocking " + action + ": " + inhibitor.Why}
			}
		}
	}
	return status
}

func RequestPower(ctx context.Context, action string) error {
	if action != "reboot" && action != "shutdown" {
		return errors.New("invalid-power-action")
	}
	connection, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	defer connection.Close()
	method := "org.freedesktop.login1.Manager.Reboot"
	if action == "shutdown" {
		method = "org.freedesktop.login1.Manager.PowerOff"
	}
	if err := connection.Object("org.freedesktop.login1", "/org/freedesktop/login1").CallWithContext(ctx, method, 0, false).Err; err != nil {
		return err
	}
	return nil
}

func powerMethod(ctx context.Context, manager dbus.BusObject, method string) (string, error) {
	var state string
	if err := manager.CallWithContext(ctx, "org.freedesktop.login1.Manager."+method, 0).Store(&state); err != nil {
		return "", err
	}
	return strings.ToLower(state), nil
}

func powerActionStatus(state, action string) PowerActionStatus {
	switch state {
	case "yes":
		return PowerActionStatus{State: "available", Available: true}
	case "challenge":
		return PowerActionStatus{State: "challenged", Reason: "The host requires an interactive authorization challenge."}
	case "no":
		return PowerActionStatus{State: "denied", Reason: "The host policy denied this power operation."}
	case "na":
		return PowerActionStatus{State: "unavailable", Reason: fmt.Sprintf("The host cannot perform %s.", action)}
	default:
		return PowerActionStatus{State: "unavailable", Reason: "The power-management state is unknown."}
	}
}

func powerFingerprint(status PowerStatus) string {
	status.Fingerprint = ""
	payload, _ := json.Marshal(status)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func PowerActionStatusForState(state, action string) PowerActionStatus {
	return powerActionStatus(strings.ToLower(state), action)
}
