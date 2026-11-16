package platform

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type cacheTestProvider struct {
	fakeUpdateProvider
	calls   atomic.Int32
	key     atomic.Int32
	locked  atomic.Bool
	started chan struct{}
	release chan struct{}
}

func (p *cacheTestProvider) UpdateCacheKey() string { return strconv.Itoa(int(p.key.Load())) }
func (p *cacheTestProvider) LockStatus(context.Context) (bool, string) {
	return p.locked.Load(), "test lock"
}
func (p *cacheTestProvider) Inventory(ctx context.Context) ([]UpdatePackage, error) {
	if p.calls.Add(1) == 1 && p.started != nil {
		close(p.started)
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []UpdatePackage{{Name: "one", CurrentVersion: "1", CandidateVersion: "2"}}, nil
}

func TestUpdateCacheInvalidationAndFreshApprovalReads(t *testing.T) {
	provider := &cacheTestProvider{}
	service := NewUpdateService(provider)
	first := service.Status(context.Background())
	first.Packages[0].Name = "caller mutation"
	if got := service.Status(context.Background()); got.Packages[0].Name != "one" || provider.calls.Load() != 1 {
		t.Fatal("cache not reused or caller mutated it")
	}
	provider.locked.Store(true)
	if got := service.Status(context.Background()); !got.ExternalLock || provider.calls.Load() != 1 {
		t.Fatal("cached lock status")
	}
	provider.locked.Store(false)
	provider.key.Add(1)
	service.Status(context.Background())
	if provider.calls.Load() != 2 {
		t.Fatal("metadata change did not invalidate")
	}
	service.cacheUntil = time.Now().Add(-time.Second)
	status := service.Status(context.Background())
	if provider.calls.Load() != 3 {
		t.Fatal("TTL did not expire")
	}
	if _, err := service.Preview(context.Background(), UpdateOperation{ExpectedFingerprint: status.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 4 {
		t.Fatal("preview used cached inventory")
	}
	service.Status(context.Background())
	if err := service.Refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	service.Status(context.Background())
	if provider.calls.Load() != 6 {
		t.Fatal("refresh did not invalidate inventory")
	}
}

func TestUpdateCacheCoalescesRequestsAndWaitingCanBeCanceled(t *testing.T) {
	provider := &cacheTestProvider{started: make(chan struct{}), release: make(chan struct{})}
	service := NewUpdateService(provider)
	leader := make(chan UpdateStatus, 1)
	go func() { leader <- service.Status(context.Background()) }()
	<-provider.started
	ctx, cancel := context.WithCancel(context.Background())
	waiter := make(chan UpdateStatus, 1)
	go func() { waiter <- service.Status(ctx) }()
	cancel()
	select {
	case status := <-waiter:
		if status.Available {
			t.Fatal("canceled wait succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cache wait ignored cancellation")
	}
	close(provider.release)
	if status := <-leader; !status.Available {
		t.Fatal(status)
	}
	service.Status(context.Background())
	if provider.calls.Load() != 1 {
		t.Fatal("duplicate inventory collection")
	}
}

func TestUpdateFilesFingerprintTracksCreationChangesAndDeletion(t *testing.T) {
	dir := t.TempDir()
	pattern := filepath.Join(dir, "*")
	empty := UpdateFilesFingerprint(pattern)
	path := filepath.Join(dir, "metadata")
	if err := os.WriteFile(path, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	first := UpdateFilesFingerprint(pattern)
	if first == empty {
		t.Fatal("missed creation")
	}
	if err := os.WriteFile(path, []byte("longer"), 0600); err != nil {
		t.Fatal(err)
	}
	if UpdateFilesFingerprint(pattern) == first {
		t.Fatal("missed update")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if UpdateFilesFingerprint(pattern) != empty {
		t.Fatal("missed deletion")
	}
}
