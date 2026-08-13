//go:build linux && integration

package platform

import (
	"context"
	"os"
	"testing"
)

func TestProcessDetailsLinuxVMSeam(t *testing.T) {
	if os.Getenv("TAKO_TEST_PROCESS_VM") != "1" {
		t.Skip("set TAKO_TEST_PROCESS_VM=1 in a supported Linux VM")
	}
	pid := os.Getpid()
	items, err := Processes()
	if err != nil {
		t.Fatal(err)
	}
	var current Process
	for _, item := range items {
		if item.PID == pid {
			current = item
			break
		}
	}
	if current.Started == 0 {
		t.Fatal("VM process inventory omitted current process start identity")
	}
	details, err := InspectProcess(context.Background(), pid, current.Started)
	if err != nil {
		t.Fatal(err)
	}
	if details.Process.PID != pid || details.Process.Started != current.Started {
		t.Fatalf("unexpected process identity: %#v", details.Process)
	}
}
