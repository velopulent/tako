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

func TestStoreNotifiesSessionCountChanges(t *testing.T) {
	store := NewStore(time.Minute, time.Hour)
	var mu sync.Mutex
	var counts []int
	store.SetCountHook(func(count int) {
		mu.Lock()
		counts = append(counts, count)
		mu.Unlock()
	})
	created, err := store.Create(auth.Identity{Username: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	store.DeleteWithoutNotify(created.ID)
	mu.Lock()
	defer mu.Unlock()
	if len(counts) < 3 || counts[0] != 0 || counts[1] != 1 || counts[len(counts)-1] != 0 {
		t.Fatalf("session counts=%v", counts)
	}
}

func TestStoreClearsAdministrativeGrantWhenDropped(t *testing.T) {
	store := NewStore(time.Minute, time.Hour)
	var deleted auth.Identity
	store.SetDeleteHook(func(identity auth.Identity) { deleted = identity })
	created, err := store.Create(auth.Identity{Username: "operator", BridgeToken: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	if !store.SetAdministrative(created.ID, "admin-token", time.Now().Add(time.Minute)) {
		t.Fatal("failed to set administrative grant")
	}
	if token := store.DropAdministrative(created.ID); token != "admin-token" {
		t.Fatalf("dropped token = %q", token)
	}
	current, ok := store.Get(created.ID)
	if !ok || current.Identity.AdminToken != "" || !current.AdminUntil.IsZero() {
		t.Fatalf("administrative state remained after drop: %+v", current)
	}
	store.Delete(created.ID)
	if deleted.AdminToken != "" {
		t.Fatalf("deleted session retained administrative token: %+v", deleted)
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

func TestBackgroundPollingDoesNotKeepASessionAlive(t *testing.T) {
	store := NewStore(time.Minute, time.Hour)
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	created, err := store.Create(auth.Identity{Username: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		now = now.Add(25 * time.Second)
		if _, ok := store.Peek(created.ID); !ok && now.Sub(created.SeenAt) <= time.Minute {
			t.Fatal("background poll ended a live session")
		}
	}
	if _, ok := store.Peek(created.ID); ok {
		t.Fatal("background polling kept an idle session alive")
	}

	active, err := store.Create(auth.Identity{Username: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		now = now.Add(45 * time.Second)
		if _, ok := store.Get(active.ID); !ok {
			t.Fatal("activity did not extend the session")
		}
	}
}

func TestStoreSignsOutTheOldestSessionPastThePerUserCap(t *testing.T) {
	store := NewStore(time.Hour, time.Hour)
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	var closed []string
	store.SetDeleteHook(func(identity auth.Identity) { closed = append(closed, identity.BridgeToken) })
	var first Session
	for index := range MaxPerUser {
		now = now.Add(time.Second)
		created, err := store.Create(auth.Identity{Username: "operator", UID: 1000, BridgeToken: "bridge-" + string(rune('a'+index))})
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = created
		}
	}
	if _, err := store.Create(auth.Identity{Username: "other", UID: 1001}); err != nil {
		t.Fatal(err)
	}
	if len(closed) != 0 {
		t.Fatalf("another user's sign-in evicted sessions: %v", closed)
	}
	now = now.Add(time.Second)
	if _, err := store.Create(auth.Identity{Username: "operator", UID: 1000, BridgeToken: "bridge-new"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Peek(first.ID); ok || len(closed) != 1 || closed[0] != "bridge-a" {
		t.Fatalf("oldest session was not signed out: closed=%v", closed)
	}
}

func TestUsersListAndRevokeOnlyTheirOwnSessions(t *testing.T) {
	store := NewStore(time.Hour, time.Hour)
	var closed []string
	store.SetDeleteHook(func(identity auth.Identity) { closed = append(closed, identity.BridgeToken) })
	mine, _ := store.Create(auth.Identity{Username: "operator", UID: 1000, BridgeToken: "mine"})
	store.SetClient(mine.ID, "192.0.2.4", "Firefox")
	theirs, _ := store.Create(auth.Identity{Username: "other", UID: 1001, BridgeToken: "theirs"})

	listed := store.ForUser(1000)
	if len(listed) != 1 || listed[0].RemoteAddress != "192.0.2.4" || listed[0].UserAgent != "Firefox" {
		t.Fatalf("unexpected listing: %#v", listed)
	}
	if mine.PublicID() == mine.ID || len(mine.PublicID()) != 24 {
		t.Fatal("public ID exposes or mirrors the cookie value")
	}
	if store.RevokeForUser(1000, theirs.PublicID()) {
		t.Fatal("revoked another account's session")
	}
	if !store.RevokeForUser(1000, mine.PublicID()) || len(closed) != 1 || closed[0] != "mine" {
		t.Fatalf("own session was not revoked: %v", closed)
	}
	if _, ok := store.Peek(theirs.ID); !ok {
		t.Fatal("other account's session ended")
	}
}
