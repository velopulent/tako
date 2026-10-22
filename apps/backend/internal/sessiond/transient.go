package sessiond

import (
	"context"
	"os"

	"github.com/godbus/dbus/v5"
)

// serviceStarter runs argv as its own transient systemd service. stdio
// becomes the service's stdin and stdout, and its stderr goes to the journal.
//
// Work that has to act on the real host (a user's login session, their
// terminal, administrative file changes) runs this way: the service gets the
// host's mount namespace and its own cgroup instead of inheriting sessiond's
// sandbox, while sessiond itself stays confined.
type serviceStarter func(ctx context.Context, unit, description string, argv []string, stdio *os.File) error

type transientExec struct {
	Path          string
	Arguments     []string
	IgnoreFailure bool
}

type transientProperty struct {
	Name  string
	Value dbus.Variant
}

type transientAuxiliary struct {
	Name       string
	Properties []transientProperty
}

// startTransientService implements serviceStarter over the system bus.
func startTransientService(ctx context.Context, unit, description string, argv []string, stdio *os.File) error {
	return startTransientServiceOn(ctx, dbus.ConnectSystemBus, unit, description, argv, stdio)
}

func startTransientServiceOn(ctx context.Context, connect func(...dbus.ConnOption) (*dbus.Conn, error), unit, description string, argv []string, stdio *os.File) error {
	conn, err := connect(dbus.WithContext(ctx))
	if err != nil {
		return err
	}
	defer conn.Close()
	var job dbus.ObjectPath
	return conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1").
		CallWithContext(ctx, "org.freedesktop.systemd1.Manager.StartTransientUnit", 0, unit, "fail", transientServiceProperties(description, argv, stdio), []transientAuxiliary{}).
		Store(&job)
}

func transientServiceProperties(description string, argv []string, stdio *os.File) []transientProperty {
	var environment []string
	if level := os.Getenv("TAKO_LOG_LEVEL"); level != "" {
		environment = append(environment, "TAKO_LOG_LEVEL="+level)
	}
	return []transientProperty{
		{"Description", dbus.MakeVariant(description)},
		{"Type", dbus.MakeVariant("exec")},
		{"ExecStart", dbus.MakeVariant([]transientExec{{Path: argv[0], Arguments: argv}})},
		{"StandardInputFileDescriptor", dbus.MakeVariant(dbus.UnixFD(stdio.Fd()))},
		{"StandardOutputFileDescriptor", dbus.MakeVariant(dbus.UnixFD(stdio.Fd()))},
		// The descriptor overrides StandardOutput=, but systemd still compares
		// it with StandardError=: when both name the journal (the default), it
		// copies stdout, the socket, onto stderr, and every log line lands in
		// the protocol. "null" keeps them apart.
		{"StandardOutput", dbus.MakeVariant("null")},
		{"StandardError", dbus.MakeVariant("journal")},
		{"Environment", dbus.MakeVariant(append([]string{}, environment...))},
		{"CollectMode", dbus.MakeVariant("inactive-or-failed")},
	}
}
