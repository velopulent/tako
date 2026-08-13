package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeAccountRunner struct {
	inventory *IdentityInventory
	locked    bool
	calls     [][]string
}

func (runner *fakeAccountRunner) Run(_ context.Context, command string, arguments ...string) (string, error) {
	runner.calls = append(runner.calls, append([]string{command}, arguments...))
	if command == "passwd" {
		status := "P"
		if runner.locked {
			status = "L"
		}
		return "operator " + status + " 2026-01-01 0 99999 7 -1", nil
	}
	username := arguments[len(arguments)-1]
	switch command {
	case "useradd":
		runner.inventory.Users = append(runner.inventory.Users, User{Username: username, UID: 2000, GID: 2000, Name: "Created", Home: "/home/" + username, Shell: "/bin/bash", Source: "local", Local: true, Mutable: true})
	case "usermod":
		for index := range runner.inventory.Users {
			if runner.inventory.Users[index].Username != username {
				continue
			}
			for argIndex := 0; argIndex < len(arguments)-1; argIndex++ {
				switch arguments[argIndex] {
				case "--comment":
					if argIndex+1 < len(arguments) {
						runner.inventory.Users[index].Name = arguments[argIndex+1]
					}
				case "--home-dir":
					if argIndex+1 < len(arguments) {
						runner.inventory.Users[index].Home = arguments[argIndex+1]
					}
				case "--shell":
					if argIndex+1 < len(arguments) {
						runner.inventory.Users[index].Shell = arguments[argIndex+1]
					}
				case "--lock":
					runner.locked = true
				case "--unlock":
					runner.locked = false
				}
			}
		}
	case "userdel":
		filtered := runner.inventory.Users[:0]
		for _, user := range runner.inventory.Users {
			if user.Username != username {
				filtered = append(filtered, user)
			}
		}
		runner.inventory.Users = filtered
	}
	return "", nil
}

func TestLocalAccountUsesFixedShadowArgumentsAndVerifies(t *testing.T) {
	inventory := &IdentityInventory{}
	runner := &fakeAccountRunner{inventory: inventory}
	reader := func(context.Context) (IdentityInventory, error) { return *inventory, nil }
	state, err := applyLocalAccount(context.Background(), LocalAccountOperation{Action: "create", Username: "newuser", Name: "Created", Home: "/home/newuser", Shell: "/bin/bash"}, "operator", reader, runner)
	if err != nil || !state.Exists {
		t.Fatalf("create state=%#v err=%v", state, err)
	}
	if len(runner.calls) < 2 || strings.Join(runner.calls[0], " ") != "useradd --create-home --home-dir /home/newuser --shell /bin/bash --comment Created -- newuser" {
		t.Fatalf("shadow calls=%#v", runner.calls)
	}
	fingerprint := state.Fingerprint
	state, err = applyLocalAccount(context.Background(), LocalAccountOperation{Action: "update", Username: "newuser", Name: "Updated", ExpectedFingerprint: fingerprint}, "operator", reader, runner)
	if err != nil || state.User == nil || state.User.Name != "Updated" {
		t.Fatalf("update state=%#v err=%v", state, err)
	}
}

func TestLocalAccountRejectsStaleAndProtectsCurrentOperator(t *testing.T) {
	inventory := &IdentityInventory{Users: []User{{Username: "operator", UID: 1000, GID: 1000, Name: "Operator", Home: "/home/operator", Shell: "/bin/bash", Source: "local", Local: true, Mutable: true}}}
	runner := &fakeAccountRunner{inventory: inventory}
	reader := func(context.Context) (IdentityInventory, error) { return *inventory, nil }
	current, err := readLocalAccountState(context.Background(), "operator", reader, runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyLocalAccount(context.Background(), LocalAccountOperation{Action: "delete", Username: "operator", ExpectedFingerprint: current.Fingerprint, Confirmation: "DELETE operator"}, "operator", reader, runner); !errors.Is(err, ErrLocalAccountProtected) {
		t.Fatalf("self-delete err=%v", err)
	}
	if _, err := applyLocalAccount(context.Background(), LocalAccountOperation{Action: "lock", Username: "operator", ExpectedFingerprint: strings.Repeat("0", 64), Confirmation: "LOCK operator"}, "another", reader, runner); !errors.Is(err, ErrLocalAccountConflict) {
		t.Fatalf("stale lock err=%v", err)
	}
}

func TestLocalAccountPreviewMarksRemoteReadOnly(t *testing.T) {
	inventory := func(context.Context) (IdentityInventory, error) {
		return IdentityInventory{Users: []User{{Username: "remote", UID: 2000, GID: 2000, Source: "nss-read-only", Local: false, Mutable: false}}}, nil
	}
	preview, err := previewLocalAccount(context.Background(), LocalAccountOperation{Action: "delete", Username: "remote", Preview: true}, "operator", inventory, &fakeAccountRunner{})
	if err != nil || preview.Allowed || preview.Reason == "" {
		t.Fatalf("remote preview=%#v err=%v", preview, err)
	}
}

func TestLocalAccountCommandOutputIsBounded(t *testing.T) {
	directory := t.TempDir()
	command := filepath.Join(directory, "fake-shadow")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf '%20000s\\n' ''\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	_, err := (execShadowCommandRunner{}).Run(context.Background(), "fake-shadow")
	if err == nil || !strings.Contains(err.Error(), "output exceeded limit") {
		t.Fatalf("oversized shadow output err=%v", err)
	}
}
