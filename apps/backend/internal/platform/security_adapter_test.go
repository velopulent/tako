package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type securityFixtureRunner struct {
	commands []string
	boolean  bool
	mode     string
}

func (runner *securityFixtureRunner) Run(_ context.Context, name string, arguments ...string) (string, error) {
	runner.commands = append(runner.commands, name+" "+strings.Join(arguments, " "))
	switch name {
	case "getenforce":
		return "Enforcing\n", nil
	case "sestatus":
		return "SELinux status: enabled\nCurrent mode: enforcing\nLoaded policy name: targeted\n", nil
	case "getsebool":
		value := "off"
		if runner.boolean {
			value = "on"
		}
		return "httpd_can_network_connect --> " + value + "\n", nil
	case "semanage":
		return "httpd_can_network_connect (off, off)\n", nil
	case "aa-status":
		return `{"profiles":{"usr.sbin.demo":"` + runner.mode + `"}}`, nil
	case "ausearch", "journalctl":
		return "", errors.New("no denials")
	case "setsebool":
		runner.boolean = true
		return "", nil
	default:
		return "", nil
	}
}

func TestSecurityPreviewEligibilityRequiresKnownTargets(t *testing.T) {
	status := SecurityStatus{
		SELinux:  SELinuxStatus{Userspace: true, Mode: "Enforcing", Booleans: []string{"httpd_can_network_connect=off"}},
		AppArmor: AppArmorStatus{Userspace: true, Profiles: []string{"usr.sbin.demo"}},
	}
	if allowed, _ := securityPreviewEligibility(status, SecurityOperation{Action: "selinux-boolean", Framework: "SELinux", Boolean: "missing"}); allowed {
		t.Fatal("unknown SELinux boolean was previewable")
	}
	if allowed, _ := securityPreviewEligibility(status, SecurityOperation{Action: "selinux-boolean", Framework: "SELinux", Boolean: "httpd_can_network_connect"}); !allowed {
		t.Fatal("known SELinux boolean was rejected")
	}
	if allowed, _ := securityPreviewEligibility(status, SecurityOperation{Action: "apparmor-enforce", Framework: "AppArmor", Profile: "missing"}); allowed {
		t.Fatal("unknown AppArmor profile was previewable")
	}
}

func TestSecurityAdapterReadsAndAppliesKnownBoolean(t *testing.T) {
	runner := &securityFixtureRunner{mode: "enforce"}
	adapter := NewSecurityStrategy(runner)
	status, err := adapter.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.SELinux.Mode != "Enforcing" || !status.SELinux.Userspace || len(status.SELinux.Booleans) != 1 {
		t.Fatalf("SELinux status = %+v", status.SELinux)
	}
	if len(status.AppArmor.Profiles) != 1 || status.AppArmor.ProfileModes["usr.sbin.demo"] != "enforce" {
		t.Fatalf("AppArmor status = %+v", status.AppArmor)
	}
	updated, err := adapter.Apply(context.Background(), SecurityOperation{
		Action:              "selinux-boolean",
		Framework:           "SELinux",
		Boolean:             "httpd_can_network_connect",
		Value:               true,
		ExpectedFingerprint: status.Fingerprint,
		Confirmation:        "CONFIRM NARROW SECURITY CHANGE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Allowed || len(updated.Changes) != 1 || !strings.Contains(updated.Changes[0].After, "on") {
		t.Fatalf("updated status = %+v", updated)
	}
}
