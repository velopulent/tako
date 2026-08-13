package platform

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

func TestPreviewSignalVerifiesStartIdentityAndExpectedTree(t *testing.T) {
	command := exec.Command("sleep", "5")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	defer command.Wait()
	items, err := Processes()
	if err != nil {
		t.Fatal(err)
	}
	var target Process
	for _, item := range items {
		if item.PID == command.Process.Pid {
			target = item
			break
		}
	}
	if target.Started == 0 {
		t.Fatalf("child process start identity missing: %#v", target)
	}
	operation := SignalOperation{Action: "preview", Signal: "cont", Target: ProcessTarget{PID: target.PID, Started: target.Started}}
	preview, err := PreviewSignal(context.Background(), operation)
	if err != nil || len(preview.Targets) != 1 || preview.Signal != "CONT" {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	operation.Action = "apply"
	operation.ExpectedTargets = SignalTargets(preview.Targets)
	if _, err := PreviewSignal(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	operation.ExpectedTargets[0].Started++
	if _, err := PreviewSignal(context.Background(), operation); !errors.Is(err, ErrSignalConflict) {
		t.Fatalf("stale expected targets error=%v, want %v", err, ErrSignalConflict)
	}
}

func TestValidateSignalOperationRejectsUnsafeSignals(t *testing.T) {
	if err := ValidateSignalOperation(SignalOperation{Action: "apply", Signal: "9", Target: ProcessTarget{PID: 1, Started: 1}, ExpectedTargets: []ProcessTarget{{PID: 1, Started: 1}}}); !errors.Is(err, ErrInvalidSignalOperation) {
		t.Fatalf("unsafe signal error=%v", err)
	}
	if err := ValidateSignalOperation(SignalOperation{Action: "apply", Signal: "TERM", Target: ProcessTarget{PID: 1, Started: 1}}); !errors.Is(err, ErrInvalidSignalOperation) {
		t.Fatalf("missing preview targets error=%v", err)
	}
}
