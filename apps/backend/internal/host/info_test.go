package host

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReadContextReturnsBoundedHostInventory(t *testing.T) {
	info := ReadContext(context.Background())
	if info.Architecture == "" || info.Kernel == "" || info.BootedAt.IsZero() {
		t.Fatalf("host identity is incomplete: %#v", info)
	}
	if info.Hardware.CPUCores < 1 || info.Hardware.MemoryTotal == 0 {
		t.Fatalf("hardware inventory is incomplete: %#v", info.Hardware)
	}
	if len(info.BootID) > 128 {
		t.Fatalf("boot id is unexpectedly large: %q", info.BootID)
	}
}

func TestBoundedCommandRejectsOversizedOutput(t *testing.T) {
	if _, err := boundedCommand(context.Background(), time.Second, "/bin/sh", "-c", "printf '%*s' 70000 x"); err == nil || !strings.Contains(err.Error(), "output exceeded") {
		t.Fatalf("oversized command output error is %v", err)
	}
}

func TestBoundedCommandHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := boundedCommand(ctx, time.Second, "/bin/sh", "-c", "sleep 1"); err == nil {
		t.Fatal("canceled command succeeded")
	}
}
