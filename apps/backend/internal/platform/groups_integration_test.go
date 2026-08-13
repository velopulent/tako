//go:build linux && integration

package platform

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestGroupAndAdministrativeRoleLifecycle runs only in a disposable root VM.
func TestGroupAndAdministrativeRoleLifecycle(t *testing.T) {
	if os.Getenv("TAKO_TEST_ACCOUNT_VM") != "1" || os.Geteuid() != 0 {
		t.Skip("set TAKO_TEST_ACCOUNT_VM=1 in a disposable root VM")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	username := fmt.Sprintf("tako-%d", time.Now().UnixNano()%1_000_000_000)
	created, err := ApplyLocalAccount(ctx, LocalAccountOperation{Action: "create", Username: username, Name: "Tako group test", Home: "/home/" + username, Shell: "/bin/bash"}, "vm-operator")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if current, readErr := readLocalAccountState(context.Background(), username, ListIdentityInventory, execShadowCommandRunner{}); readErr == nil && current.Exists {
			_, _ = ApplyLocalAccount(context.Background(), LocalAccountOperation{Action: "delete", Username: username, Confirmation: "DELETE " + username, ExpectedFingerprint: current.Fingerprint}, "vm-operator")
		}
	}()
	inventory, err := ListIdentityInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	role, _, err := readAdministrativeRoleState(ctx, username, func(context.Context) (IdentityInventory, error) { return inventory, nil })
	if err != nil {
		t.Skipf("no sudo or wheel group in VM: %v", err)
	}
	preview, err := PreviewAdministrativeRole(ctx, AdministrativeRoleOperation{Action: "grant", Username: username, Role: "administrator"}, "vm-operator")
	if err != nil || !preview.Allowed {
		t.Fatalf("role preview=%#v err=%v", preview, err)
	}
	granted, err := ApplyAdministrativeRole(ctx, AdministrativeRoleOperation{Action: "grant", Username: username, Role: "administrator", ExpectedFingerprint: preview.Current.Fingerprint, Confirmation: "GRANT ADMIN " + username}, "vm-operator")
	if err != nil || !granted.Member {
		t.Fatalf("role grant=%#v err=%v", granted, err)
	}
	removed, err := ApplyAdministrativeRole(ctx, AdministrativeRoleOperation{Action: "revoke", Username: username, Role: "administrator", ExpectedFingerprint: granted.Fingerprint, Confirmation: "REVOKE ADMIN " + username}, "vm-operator")
	if err != nil || removed.Member {
		t.Fatalf("role revoke=%#v err=%v", removed, err)
	}
	_ = role
	_ = created
}
