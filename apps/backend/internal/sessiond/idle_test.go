package sessiond

import (
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/idle"
)

func TestGrantBlocksIdleExitUntilClosed(t *testing.T) {
	activity := idle.New(20 * time.Millisecond)
	store := &grantStore{values: make(map[string]bridgeGrant), activity: activity}
	token, err := store.add(auth.Identity{Username: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	activity.Start()
	time.Sleep(35 * time.Millisecond)
	select {
	case <-activity.Done():
		t.Fatal("sessiond became idle with active grant")
	default:
	}
	store.close(token)
	select {
	case <-activity.Done():
	case <-time.After(time.Second):
		t.Fatal("sessiond did not become idle after grant closed")
	}
}

func TestSessionConnectionBlocksIdleExit(t *testing.T) {
	activity := idle.New(20 * time.Millisecond)
	release := activity.Block()
	activity.Start()
	time.Sleep(35 * time.Millisecond)
	select {
	case <-activity.Done():
		t.Fatal("sessiond became idle with active connection")
	default:
	}
	release()
	select {
	case <-activity.Done():
	case <-time.After(time.Second):
		t.Fatal("sessiond did not become idle after connection closed")
	}
}
