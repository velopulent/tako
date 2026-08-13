package platform

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestParseLogEntry(t *testing.T) {
	entry, ok := parseLogEntry([]byte(`{"__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"3","_SYSTEMD_UNIT":"demo.service","MESSAGE":"failed safely"}`))
	if !ok || entry.Unit != "demo.service" || entry.Priority != "3" || entry.Message != "failed safely" {
		t.Fatalf("unexpected entry: %+v ok=%v", entry, ok)
	}
	if _, ok := parseLogEntry([]byte(`not json`)); ok {
		t.Fatal("invalid journal row accepted")
	}
}

func TestUnitNameFromObjectPath(t *testing.T) {
	if got := unitNameFromObjectPath(dbus.ObjectPath("/org/freedesktop/systemd1/unit/demo_2eservice")); got != "demo.service" {
		t.Fatalf("decoded unit = %q", got)
	}
	if got := unitNameFromObjectPath(dbus.ObjectPath("/org/freedesktop/systemd1/unit/no_encoding")); got != "no_encoding" {
		t.Fatalf("unencoded unit = %q", got)
	}
}

func TestProcessIdentityAndDetailsRejectPIDReuse(t *testing.T) {
	items, err := Processes()
	if err != nil {
		t.Fatal(err)
	}
	pid := os.Getpid()
	var current Process
	for _, item := range items {
		if item.PID == pid {
			current = item
			break
		}
	}
	if current.Started == 0 {
		t.Fatalf("current process start identity missing: %#v", current)
	}
	details, err := InspectProcess(context.Background(), pid, current.Started)
	if err != nil || details.Process.Started != current.Started {
		t.Fatalf("process details=%#v err=%v", details, err)
	}
	if _, err := InspectProcess(context.Background(), pid, current.Started+1); !errors.Is(err, ErrProcessReused) {
		t.Fatalf("PID reuse error=%v, want %v", err, ErrProcessReused)
	}
}

func TestProcessTrackerKeepsBoundedHistory(t *testing.T) {
	tracker := NewProcessTracker()
	for range 3 {
		if _, err := tracker.Snapshot(); err != nil {
			t.Fatal(err)
		}
	}
	items, err := Processes()
	if err != nil {
		t.Fatal(err)
	}
	var current Process
	for _, item := range items {
		if item.PID == os.Getpid() {
			current = item
			break
		}
	}
	details, err := tracker.Inspect(context.Background(), current.PID, current.Started)
	if err != nil || len(details.History) > maxProcessHistory {
		t.Fatalf("history len=%d err=%v", len(details.History), err)
	}
}
