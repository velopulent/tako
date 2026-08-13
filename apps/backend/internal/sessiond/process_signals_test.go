package sessiond

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

func TestExecuteProcessSignalChecksUserAuthority(t *testing.T) {
	command := exec.Command("sleep", "5")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	defer command.Wait()
	items, err := platform.Processes()
	if err != nil {
		t.Fatal(err)
	}
	var target platform.Process
	for _, item := range items {
		if item.PID == command.Process.Pid {
			target = item
			break
		}
	}
	if target.Started == 0 {
		t.Fatalf("child process start identity missing: %#v", target)
	}
	operation := platform.SignalOperation{Action: "apply", Signal: "CONT", Target: platform.ProcessTarget{PID: target.PID, Started: target.Started}, ExpectedTargets: []platform.ProcessTarget{{PID: target.PID, Started: target.Started}}}
	result, err := executeProcessSignal(context.Background(), auth.Identity{UID: target.UID}, false, operation)
	if err != nil || len(result.Signaled) != 1 {
		t.Fatalf("signal result=%#v err=%v", result, err)
	}
	if _, err := executeProcessSignal(context.Background(), auth.Identity{UID: target.UID + 1}, false, operation); !errors.Is(err, platform.ErrSignalUnauthorized) {
		t.Fatalf("unauthorized signal error=%v", err)
	}
}
