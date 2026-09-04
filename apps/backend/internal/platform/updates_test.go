package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeUpdateProvider struct {
	packages     []UpdatePackage
	changes      []UpdateChange
	applied      bool
	applyStarted chan struct{}
	applyRelease chan struct{}
	applyCtxErr  error
}

func TestUpdateOutputSanitizedAndReplayBounded(t *testing.T) {
	service := NewUpdateService(&fakeUpdateProvider{})
	for index := 0; index < maxLiveEvents+20; index++ {
		service.publish(UpdateStreamEvent{Kind: "output", Output: UpdateOutput{Stream: "stdout", Line: "\x1b[31mok\x00" + strings.Repeat("x", maxLiveLine-1) + "界界"}})
	}
	observation := service.Snapshot()
	if len(observation.Output) > maxLiveEvents {
		t.Fatalf("replay exceeded event bound: %d", len(observation.Output))
	}
	for _, output := range observation.Output {
		if strings.Contains(output.Line, "\x1b") || strings.Contains(output.Line, "\x00") || len(output.Line) > maxLiveLine || strings.ToValidUTF8(output.Line, "") != output.Line {
			t.Fatalf("unsafe output retained: %q", output.Line[:min(len(output.Line), 32)])
		}
	}
}

func (provider *fakeUpdateProvider) Name() string                          { return "fake" }
func (provider *fakeUpdateProvider) Probe(context.Context) (string, error) { return "1", nil }
func (provider *fakeUpdateProvider) Inventory(context.Context) ([]UpdatePackage, error) {
	return append([]UpdatePackage(nil), provider.packages...), nil
}
func (*fakeUpdateProvider) Refresh(context.Context, bool, func(UpdateStreamEvent)) error { return nil }
func (provider *fakeUpdateProvider) Plan(context.Context) ([]UpdateChange, error) {
	return append([]UpdateChange(nil), provider.changes...), nil
}
func (provider *fakeUpdateProvider) Apply(ctx context.Context, _ func(UpdateStreamEvent)) error {
	if provider.applyStarted != nil {
		close(provider.applyStarted)
		<-provider.applyRelease
		provider.applyCtxErr = ctx.Err()
	}
	provider.applied = true
	provider.packages = nil
	return nil
}

func TestUpdateServiceCommitContinuesAfterCallerDisconnect(t *testing.T) {
	provider := &fakeUpdateProvider{
		packages:     []UpdatePackage{{Name: "one", CurrentVersion: "1", CandidateVersion: "2"}},
		changes:      []UpdateChange{{Action: "upgrade", Name: "one", CurrentVersion: "1", CandidateVersion: "2"}},
		applyStarted: make(chan struct{}),
		applyRelease: make(chan struct{}),
	}
	service := NewUpdateService(provider)
	fingerprint := UpdatePlanFingerprint("fake", provider.changes)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := service.Apply(ctx, UpdateOperation{ExpectedFingerprint: fingerprint, Confirmed: true})
		result <- err
	}()
	<-provider.applyStarted
	cancel()
	close(provider.applyRelease)
	if err := <-result; err != nil {
		t.Fatalf("committed update stopped after disconnect: %v", err)
	}
	if provider.applyCtxErr != nil {
		t.Fatalf("provider inherited caller cancellation: %v", provider.applyCtxErr)
	}
}
func (*fakeUpdateProvider) History(context.Context, int) ([]UpdateHistoryEntry, error) {
	return []UpdateHistoryEntry{}, nil
}
func (*fakeUpdateProvider) Recovery(context.Context) UpdateRecovery {
	return UpdateRecovery{RestartServices: []string{}, Hints: []string{}}
}
func (*fakeUpdateProvider) LockStatus(context.Context) (bool, string) { return false, "" }

func TestUpdateServiceRequiresExactPlanAndRiskConfirmation(t *testing.T) {
	provider := &fakeUpdateProvider{
		packages: []UpdatePackage{{Name: "old", CurrentVersion: "2", CandidateVersion: "1"}},
		changes:  []UpdateChange{{Action: "downgrade", Name: "old", CurrentVersion: "2", CandidateVersion: "1"}},
	}
	service := NewUpdateService(provider)
	status := service.Status(context.Background())
	preview, err := service.Preview(context.Background(), UpdateOperation{ExpectedFingerprint: status.Fingerprint})
	if err != nil || !preview.RequiresRiskConfirmation || preview.Fingerprint == "" {
		t.Fatalf("unexpected preview: %#v, %v", preview, err)
	}
	_, err = service.Apply(context.Background(), UpdateOperation{ExpectedFingerprint: preview.Fingerprint, Confirmed: true})
	if !errors.Is(err, ErrUpdateRiskNotAccepted) || provider.applied {
		t.Fatalf("risky plan applied without confirmation: %v", err)
	}
	result, err := service.Apply(context.Background(), UpdateOperation{ExpectedFingerprint: preview.Fingerprint, Confirmed: true, RiskAccepted: true})
	if err != nil || !result.Verified || !provider.applied {
		t.Fatalf("apply failed: %#v, %v", result, err)
	}
}

func TestUpdateServiceRejectsStalePlan(t *testing.T) {
	provider := &fakeUpdateProvider{changes: []UpdateChange{{Action: "upgrade", Name: "one", CandidateVersion: "2"}}}
	service := NewUpdateService(provider)
	stale := UpdatePlanFingerprint("fake", []UpdateChange{{Action: "upgrade", Name: "other"}})
	_, err := service.Apply(context.Background(), UpdateOperation{ExpectedFingerprint: stale, Confirmed: true})
	if !errors.Is(err, ErrUpdateConflict) {
		t.Fatalf("expected stale-plan conflict, got %v", err)
	}
}
