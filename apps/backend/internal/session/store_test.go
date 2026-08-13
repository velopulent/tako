package session

import (
	"sync"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
)

func TestStoreExpiresIdleSession(t *testing.T) {
	store := NewStore(time.Minute, time.Hour)
	closed := ""
	store.SetDeleteHook(func(identity auth.Identity) { closed = identity.BridgeToken })
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	created, err := store.Create(auth.Identity{Username: "operator", BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute + time.Second)
	if _, ok := store.Get(created.ID); ok {
		t.Fatal("expired session remained valid")
	}
	if closed != "bridge-token" {
		t.Fatal("expired session did not close its user bridge")
	}
}

func TestStoreNotifiesWhenSessionsClose(t *testing.T) {
	store := NewStore(time.Minute, time.Hour)
	var mu sync.Mutex
	var closed []string
	store.SetDeleteHook(func(identity auth.Identity) {
		mu.Lock()
		closed = append(closed, identity.BridgeToken)
		mu.Unlock()
	})
	first, err := store.Create(auth.Identity{Username: "first", BridgeToken: "first-token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(auth.Identity{Username: "second", BridgeToken: "second-token"}); err != nil {
		t.Fatal(err)
	}
	store.Delete(first.ID)
	store.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(closed) != 2 || closed[0] != "first-token" || closed[1] != "second-token" {
		t.Fatalf("unexpected closed sessions: %#v", closed)
	}
}

func TestStoreCreatesOpaqueTokens(t *testing.T) {
	store := NewStore(time.Minute, time.Hour)
	created, err := store.Create(auth.Identity{Username: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.ID) < 40 || len(created.CSRF) < 30 || created.ID == created.CSRF {
		t.Fatalf("tokens are not independent opaque values: %#v", created)
	}
}

func TestStorePrunesExpiredSessionsOnCreate(t *testing.T) {
	store := NewStore(time.Minute, time.Hour)
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	old, err := store.Create(auth.Identity{Username: "old"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour + time.Second)
	if _, err := store.Create(auth.Identity{Username: "new"}); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.sessions[old.ID]; exists {
		t.Fatal("expired session was not pruned")
	}
}
