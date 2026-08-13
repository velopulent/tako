package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeGroupRunner struct {
	inventory *IdentityInventory
	calls     [][]string
}

func (runner *fakeGroupRunner) Run(_ context.Context, command string, arguments ...string) (string, error) {
	runner.calls = append(runner.calls, append([]string{command}, arguments...))
	if command != "gpasswd" || len(arguments) != 3 {
		return "", nil
	}
	action, username, groupName := arguments[0], arguments[1], arguments[2]
	for index := range runner.inventory.Groups {
		if runner.inventory.Groups[index].Name != groupName {
			continue
		}
		members := runner.inventory.Groups[index].Members[:0]
		for _, member := range runner.inventory.Groups[index].Members {
			if member != username || action == "--add" {
				members = append(members, member)
			}
		}
		if action == "--add" {
			found := false
			for _, member := range members {
				if member == username {
					found = true
				}
			}
			if !found {
				members = append(members, username)
			}
		}
		runner.inventory.Groups[index].Members = members
	}
	return "", nil
}

func localGroupInventory() *IdentityInventory {
	return &IdentityInventory{
		Users:  []User{{Username: "operator", UID: 1000, GID: 1000, Source: "local", Local: true, Mutable: true}, {Username: "target", UID: 1001, GID: 1001, Source: "local", Local: true, Mutable: true}},
		Groups: []IdentityGroup{{Name: "developers", GID: 2000, Members: []string{}, Source: "local", Local: true, Mutable: true}, {Name: "wheel", GID: 10, Members: []string{"operator", "other"}, Source: "local", Local: true, Mutable: true}},
	}
}

func TestGroupMembershipUsesFixedGpasswdAndVerifies(t *testing.T) {
	inventory := localGroupInventory()
	runner := &fakeGroupRunner{inventory: inventory}
	reader := func(context.Context) (IdentityInventory, error) { return *inventory, nil }
	current, _, err := readGroupMembershipState(context.Background(), "target", "developers", reader)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := applyGroupMembership(context.Background(), GroupMembershipOperation{Action: "add", Username: "target", Group: "developers", ExpectedFingerprint: current.Fingerprint}, "operator", reader, runner)
	if err != nil || !updated.Member {
		t.Fatalf("membership state=%#v err=%v", updated, err)
	}
	if len(runner.calls) != 1 || strings.Join(runner.calls[0], " ") != "gpasswd --add target developers" {
		t.Fatalf("gpasswd calls=%#v", runner.calls)
	}
}

func TestGroupMembershipRejectsStaleAndRemoteEntries(t *testing.T) {
	inventory := localGroupInventory()
	reader := func(context.Context) (IdentityInventory, error) { return *inventory, nil }
	if _, err := applyGroupMembership(context.Background(), GroupMembershipOperation{Action: "add", Username: "target", Group: "developers", ExpectedFingerprint: strings.Repeat("0", 64)}, "operator", reader, &fakeGroupRunner{inventory: inventory}); !errors.Is(err, ErrGroupConflict) {
		t.Fatalf("stale membership err=%v", err)
	}
	inventory.Users = append(inventory.Users, User{Username: "remote", UID: 2000, GID: 2000, Source: "nss-read-only", Local: false, Mutable: false})
	if preview, err := previewGroupMembership(context.Background(), GroupMembershipOperation{Action: "add", Username: "remote", Group: "developers", Preview: true}, "operator", reader); err != nil || preview.Allowed {
		t.Fatalf("remote membership preview=%#v err=%v", preview, err)
	}
}

func TestAdministrativeRoleProtectsCurrentAndLastAdministrator(t *testing.T) {
	inventory := localGroupInventory()
	reader := func(context.Context) (IdentityInventory, error) { return *inventory, nil }
	state, _, err := readAdministrativeRoleState(context.Background(), "target", reader)
	if err != nil || state.Group != "wheel" {
		t.Fatalf("role state=%#v err=%v", state, err)
	}
	preview, err := previewAdministrativeRole(context.Background(), AdministrativeRoleOperation{Action: "revoke", Username: "operator", Role: "administrator", Preview: true}, "operator", reader)
	if err != nil || preview.Allowed {
		t.Fatalf("self revoke preview=%#v err=%v", preview, err)
	}
	inventory.Groups[1].Members = []string{"target"}
	state, _, err = readAdministrativeRoleState(context.Background(), "target", reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyAdministrativeRole(context.Background(), AdministrativeRoleOperation{Action: "revoke", Username: "target", Role: "administrator", ExpectedFingerprint: state.Fingerprint, Confirmation: "REVOKE ADMIN target"}, "operator", reader, &fakeGroupRunner{inventory: inventory}); !errors.Is(err, ErrAdministrativeRoleProtected) {
		t.Fatalf("last admin revoke err=%v", err)
	}
}
