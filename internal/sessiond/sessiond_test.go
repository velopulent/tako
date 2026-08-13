package sessiond

import (
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
)

func TestGrantStore(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	identity := auth.Identity{Username: "octopus", UID: 1000, GID: 1000}
	token, err := store.add(identity)
	if err != nil || token == "" {
		t.Fatalf("add grant: token=%q err=%v", token, err)
	}
	got, ok := store.get(token)
	if !ok || got.Username != identity.Username {
		t.Fatalf("get grant: got=%+v ok=%v", got, ok)
	}
	if _, ok := store.claim(token); !ok {
		t.Fatal("first terminal claim rejected")
	}
	if _, ok := store.claim(token); ok {
		t.Fatal("concurrent terminal claim accepted")
	}
	store.release(token)
	store.values[token] = bridgeGrant{identity: identity, expires: time.Now().Add(-time.Second)}
	if _, ok := store.get(token); ok {
		t.Fatal("expired grant accepted")
	}
}
