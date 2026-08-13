//go:build linux && integration

package platform

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestLocalAccountLifecycle runs only in a disposable root VM. It exercises
// the real shadow-utils adapters and verifies every postcondition.
func TestLocalAccountLifecycle(t *testing.T) {
	if os.Getenv("TAKO_TEST_ACCOUNT_VM") != "1" || os.Geteuid() != 0 {
		t.Skip("set TAKO_TEST_ACCOUNT_VM=1 in a disposable root VM")
	}
	username := fmt.Sprintf("tako-%d", time.Now().UnixNano()%1_000_000_000)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer func() {
		if current, readErr := readLocalAccountState(context.Background(), username, ListIdentityInventory, execShadowCommandRunner{}); readErr == nil && current.Exists {
			_, _ = ApplyLocalAccount(context.Background(), LocalAccountOperation{Action: "delete", Username: username, Confirmation: "DELETE " + username, ExpectedFingerprint: current.Fingerprint}, "vm-operator")
		}
	}()
	created, err := ApplyLocalAccount(ctx, LocalAccountOperation{Action: "create", Username: username, Name: "Tako test", Home: "/home/" + username, Shell: "/bin/bash"}, "vm-operator")
	if err != nil || !created.Exists {
		t.Fatalf("create state=%#v err=%v", created, err)
	}
	updated, err := ApplyLocalAccount(ctx, LocalAccountOperation{Action: "update", Username: username, Name: "Tako updated", ExpectedFingerprint: created.Fingerprint}, "vm-operator")
	if err != nil || updated.User == nil || updated.User.Name != "Tako updated" {
		t.Fatalf("update state=%#v err=%v", updated, err)
	}
	locked, err := ApplyLocalAccount(ctx, LocalAccountOperation{Action: "lock", Username: username, ExpectedFingerprint: updated.Fingerprint, Confirmation: "LOCK " + username}, "vm-operator")
	if err != nil || !locked.Locked {
		t.Fatalf("lock state=%#v err=%v", locked, err)
	}
	unlocked, err := ApplyLocalAccount(ctx, LocalAccountOperation{Action: "unlock", Username: username, ExpectedFingerprint: locked.Fingerprint}, "vm-operator")
	if err != nil || unlocked.Locked {
		t.Fatalf("unlock state=%#v err=%v", unlocked, err)
	}
	deleted, err := ApplyLocalAccount(ctx, LocalAccountOperation{Action: "delete", Username: username, ExpectedFingerprint: unlocked.Fingerprint, Confirmation: "DELETE " + username}, "vm-operator")
	if err != nil || deleted.Exists {
		t.Fatalf("delete state=%#v err=%v", deleted, err)
	}
}
