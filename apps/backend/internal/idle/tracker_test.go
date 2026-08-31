package idle

import (
	"sync"
	"testing"
	"time"
)

func TestTrackerFiresAfterIdleTimeout(t *testing.T) {
	tracker := New(20 * time.Millisecond)
	tracker.Start()
	select {
	case <-tracker.Done():
	case <-time.After(time.Second):
		t.Fatal("idle tracker did not fire")
	}
}

func TestTrackerActivityResetsTimeout(t *testing.T) {
	tracker := New(40 * time.Millisecond)
	tracker.Start()
	time.Sleep(25 * time.Millisecond)
	tracker.Activity()
	select {
	case <-tracker.Done():
		t.Fatal("tracker fired before reset interval elapsed")
	case <-time.After(25 * time.Millisecond):
	}
	select {
	case <-tracker.Done():
	case <-time.After(time.Second):
		t.Fatal("tracker did not fire after reset interval")
	}
}

func TestTrackerFiresImmediatelyAfterLateBlockerRelease(t *testing.T) {
	tracker := New(30 * time.Millisecond)
	release := tracker.Block()
	tracker.Start()
	time.Sleep(40 * time.Millisecond)
	select {
	case <-tracker.Done():
		t.Fatal("tracker fired with active blocker")
	default:
	}
	release()
	select {
	case <-tracker.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("tracker did not fire after late blocker release")
	}
}

func TestTrackerDisabledOrNotStartedDoesNotFire(t *testing.T) {
	for _, tracker := range []*Tracker{New(0), New(10 * time.Millisecond)} {
		if tracker.timeout == 0 {
			tracker.Start()
		}
		select {
		case <-tracker.Done():
			t.Fatal("inactive tracker fired")
		case <-time.After(30 * time.Millisecond):
		}
		tracker.Stop()
	}
}

func TestTrackerConcurrentReleaseFiresOnce(t *testing.T) {
	tracker := New(10 * time.Millisecond)
	var releases []func()
	for range 32 {
		releases = append(releases, tracker.Block())
	}
	tracker.Start()
	var workers sync.WaitGroup
	for _, release := range releases {
		workers.Add(1)
		go func() {
			defer workers.Done()
			release()
			release()
		}()
	}
	workers.Wait()
	select {
	case <-tracker.Done():
	case <-time.After(time.Second):
		t.Fatal("tracker did not fire")
	}
	tracker.Stop()
}
