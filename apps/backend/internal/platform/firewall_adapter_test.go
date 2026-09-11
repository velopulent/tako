package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type firewallFixtureResult struct {
	output string
	err    error
}

type firewallFixtureRunner struct {
	results map[string]firewallFixtureResult
	calls   []string
	hook    func(*firewallFixtureRunner, string)
}

func (runner *firewallFixtureRunner) Run(_ context.Context, name string, arguments ...string) (string, error) {
	key := name + " " + strings.Join(arguments, " ")
	runner.calls = append(runner.calls, key)
	if runner.hook != nil {
		runner.hook(runner, key)
	}
	result, ok := runner.results[key]
	if !ok {
		return "", errors.New("unconfigured fixture command: " + key)
	}
	return result.output, result.err
}

func firewalldFixture(runtime, permanent string) *firewallFixtureRunner {
	errorResult := firewallFixtureResult{err: errors.New("command unavailable")}
	results := map[string]firewallFixtureResult{
		"firewall-cmd --state":                              {output: "running\n"},
		"ufw status verbose":                                errorResult,
		"firewall-cmd --version":                            {output: "firewalld 1.3.4\n"},
		"firewall-cmd --get-active-zones":                   {output: "public\n  interfaces: eth0\n"},
		"firewall-cmd --get-zones":                          {output: "public home\n"},
		"firewall-cmd --get-default-zone":                   {output: "public\n"},
		"firewall-cmd --zone=public --list-all":             {output: runtime},
		"firewall-cmd --permanent --zone=public --list-all": {output: permanent},
		"firewall-cmd --zone=home --list-all":               {output: "home\n  target: default\n  services: \n  ports: \n"},
		"firewall-cmd --permanent --zone=home --list-all":   {output: "home\n  target: default\n  services: \n  ports: \n"},
	}
	return &firewallFixtureRunner{results: results}
}

func TestFirewallReadInventoriesRuntimeAndPersistentFirewalldRules(t *testing.T) {
	runtime := `public (active)
  target: default
  services: dhcpv6-client ssh
  ports: 8080/tcp
  rich rules:
    rule family="ipv4" source address="192.0.2.0/24" accept`
	permanent := `public
  target: default
  services: ssh
  ports:
  rich rules:`
	runner := firewalldFixture(runtime, permanent)
	snapshot, err := NewFirewallStrategy(runner).Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if snapshot.Backend != "firewalld" || !snapshot.Active || snapshot.Synchronized {
		t.Fatalf("unexpected manager state: %#v", snapshot)
	}
	if !contains(snapshot.Rules, `public services: dhcpv6-client ssh`) || !contains(snapshot.Rules, "public ports: 8080/tcp") {
		t.Fatalf("runtime inventory omitted actual rules: %#v", snapshot.Rules)
	}
	if !contains(snapshot.PersistentRules, "public services: ssh") || contains(snapshot.PersistentRules, "public ports: 8080/tcp") {
		t.Fatalf("persistent inventory incorrect: %#v", snapshot.PersistentRules)
	}
	if len(snapshot.Fingerprint) != 64 {
		t.Fatalf("fingerprint length = %d", len(snapshot.Fingerprint))
	}

	// Zone and list ordering must not cause a fingerprint change.
	runner.results["firewall-cmd --get-active-zones"] = firewallFixtureResult{output: "public\n  interfaces: eth0\n"}
	runner.results["firewall-cmd --get-zones"] = firewallFixtureResult{output: "home public\n"}
	runner.results["firewall-cmd --zone=public --list-all"] = firewallFixtureResult{output: runtime}
	runner.results["firewall-cmd --permanent --zone=public --list-all"] = firewallFixtureResult{output: permanent}
	second, err := NewFirewallStrategy(runner).Read(context.Background())
	if err != nil {
		t.Fatalf("second Read() error = %v", err)
	}
	if second.Fingerprint != snapshot.Fingerprint {
		t.Fatalf("fingerprint changed with equivalent ordering: %s != %s", second.Fingerprint, snapshot.Fingerprint)
	}
}

func TestFirewallStateErrorsDoNotLookLikeActiveFirewalld(t *testing.T) {
	runner := &firewallFixtureRunner{results: map[string]firewallFixtureResult{
		"firewall-cmd --state": {output: "running\n", err: errors.New("dbus unavailable")},
		"ufw status verbose":   {err: errors.New("ufw unavailable")},
	}}
	if _, err := NewFirewallStrategy(runner).Read(context.Background()); !errors.Is(err, ErrFirewallUnavailable) {
		t.Fatalf("state command failure error = %v", err)
	}
}

func TestFirewallEnableStartsInactiveFirewalldThroughSystemd(t *testing.T) {
	runner := firewalldFixture("public\n  target: default\n  services: ssh\n  ports:\n", "public\n  target: default\n  services: ssh\n  ports:\n")
	runner.results["firewall-cmd --state"] = firewallFixtureResult{output: "not running\n"}
	runner.results["systemctl enable --now firewalld.service"] = firewallFixtureResult{output: ""}
	runner.hook = func(r *firewallFixtureRunner, key string) {
		if key == "systemctl enable --now firewalld.service" {
			r.results["firewall-cmd --state"] = firewallFixtureResult{output: "running\n"}
		}
	}
	before, err := NewFirewallStrategy(runner).Read(context.Background())
	if err != nil {
		t.Fatalf("inactive Read() error = %v", err)
	}
	operation := FirewallOperation{Backend: "firewalld", Action: "enable", ExpectedFingerprint: before.Fingerprint, Confirmation: "CONFIRM FIREWALL CHANGE"}
	state, err := NewFirewallStrategy(runner).Apply(context.Background(), operation)
	if err != nil {
		t.Fatalf("enable error = %v", err)
	}
	if !state.Applied || !state.Snapshot.Active {
		t.Fatalf("enable state = %#v", state)
	}
	if !contains(runner.calls, "systemctl enable --now firewalld.service") {
		t.Fatalf("systemd enable command not used: %#v", runner.calls)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "set-log-denied") {
			t.Fatalf("enable changed logging instead of service state: %#v", runner.calls)
		}
	}
}

func TestInactiveUFWDoesNotConflictWithExplicitFirewalldEnable(t *testing.T) {
	runner := firewalldFixture("public\n  target: default\n  services: ssh\n  ports:\n", "public\n  target: default\n  services: ssh\n  ports:\n")
	runner.results["firewall-cmd --state"] = firewallFixtureResult{output: "not running\n"}
	runner.results["ufw status verbose"] = firewallFixtureResult{output: "Status: inactive\n"}
	runner.results["systemctl enable --now firewalld.service"] = firewallFixtureResult{}
	runner.hook = func(r *firewallFixtureRunner, key string) {
		if key == "systemctl enable --now firewalld.service" {
			r.results["firewall-cmd --state"] = firewallFixtureResult{output: "running\n"}
		}
	}
	before, err := NewFirewallStrategy(runner).Read(context.Background())
	if err != nil || before.Backend != "firewalld" || before.Conflicted {
		t.Fatalf("inactive manager state = %#v, error = %v", before, err)
	}
	if _, err := NewFirewallStrategy(runner).Apply(context.Background(), FirewallOperation{Backend: "firewalld", Action: "enable", ExpectedFingerprint: before.Fingerprint, Confirmation: "CONFIRM FIREWALL CHANGE"}); err != nil {
		t.Fatalf("explicit enable with inactive UFW error = %v", err)
	}
}

func TestFirewallPersistentMutationUsesBothStoresAndPreservesUnmanagedRules(t *testing.T) {
	runtime := "public\n  target: default\n  services: ssh\n  ports:\n"
	permanent := "public\n  target: default\n  services: ssh\n  ports:\n"
	runner := firewalldFixture(runtime, permanent)
	runner.results["firewall-cmd --zone=public --add-port=8443/tcp"] = firewallFixtureResult{output: "success\n"}
	runner.results["firewall-cmd --permanent --zone=public --add-port=8443/tcp"] = firewallFixtureResult{output: "success\n"}
	addedRuntime, addedPermanent := false, false
	runner.hook = func(r *firewallFixtureRunner, key string) {
		switch key {
		case "firewall-cmd --zone=public --add-port=8443/tcp":
			addedRuntime = true
		case "firewall-cmd --permanent --zone=public --add-port=8443/tcp":
			addedPermanent = true
		}
		if strings.Contains(key, "--zone=public --list-all") && !strings.Contains(key, "--permanent") {
			value := runtime
			if addedRuntime {
				value = "public\n  target: default\n  services: ssh\n  ports: 8443/tcp\n"
			}
			r.results[key] = firewallFixtureResult{output: value}
		}
		if strings.Contains(key, "--permanent --zone=public --list-all") {
			value := permanent
			if addedPermanent {
				value = "public\n  target: default\n  services: ssh\n  ports: 8443/tcp\n"
			}
			r.results[key] = firewallFixtureResult{output: value}
		}
	}
	before, err := NewFirewallStrategy(runner).Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	state, err := NewFirewallStrategy(runner).Apply(context.Background(), FirewallOperation{
		Backend: "firewalld", Action: "add-port", Port: "8443/tcp", Persist: true,
		ExpectedFingerprint: before.Fingerprint, Confirmation: "CONFIRM FIREWALL CHANGE",
	})
	if err != nil {
		t.Fatalf("persistent add error = %v", err)
	}
	if !state.Applied || !contains(state.Snapshot.PersistentRules, "public ports: 8443/tcp") {
		t.Fatalf("persistent add state = %#v", state)
	}
	if !contains(runner.calls, "firewall-cmd --zone=public --add-port=8443/tcp") || !contains(runner.calls, "firewall-cmd --permanent --zone=public --add-port=8443/tcp") {
		t.Fatalf("runtime and permanent commands missing: %#v", runner.calls)
	}

	// A pre-existing runtime rule must never be removed by rollback when the
	// permanent command fails.
	preexisting := firewalldFixture("public\n  target: default\n  services: ssh\n  ports: 22/tcp\n", permanent)
	preexisting.results["firewall-cmd --zone=public --add-port=22/tcp"] = firewallFixtureResult{output: "success\n"}
	preexisting.results["firewall-cmd --permanent --zone=public --add-port=22/tcp"] = firewallFixtureResult{err: errors.New("permanent write failed")}
	before, err = NewFirewallStrategy(preexisting).Read(context.Background())
	if err != nil {
		t.Fatalf("pre-existing Read() error = %v", err)
	}
	_, err = NewFirewallStrategy(preexisting).Apply(context.Background(), FirewallOperation{
		Backend: "firewalld", Action: "add-port", Port: "22/tcp", Persist: true,
		ExpectedFingerprint: before.Fingerprint, Confirmation: "CONFIRM FIREWALL CHANGE",
	})
	if err == nil {
		t.Fatal("permanent failure was reported as success")
	}
	for _, call := range preexisting.calls {
		if call == "firewall-cmd --zone=public --remove-port=22/tcp" {
			t.Fatalf("rollback removed a pre-existing rule: %#v", preexisting.calls)
		}
	}
}

func TestUFWServiceAndSourceCommandsAreBounded(t *testing.T) {
	runner := &firewallFixtureRunner{results: map[string]firewallFixtureResult{}}
	runner.results["ufw allow ssh"] = firewallFixtureResult{output: "Rule added\n"}
	runner.results["ufw delete allow from 192.0.2.0/24"] = firewallFixtureResult{output: "Rule deleted\n"}
	adapter := NewFirewallStrategy(runner).(*firewallAdapter)
	if _, err := adapter.applyUFW(context.Background(), FirewallOperation{Action: "add-service", Service: "ssh"}); err != nil {
		t.Fatalf("add service error = %v", err)
	}
	if _, err := adapter.applyUFW(context.Background(), FirewallOperation{Action: "remove-source", Source: "192.0.2.0/24"}); err != nil {
		t.Fatalf("remove source error = %v", err)
	}
	want := []string{"ufw allow ssh", "ufw delete allow from 192.0.2.0/24"}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("commands = %#v, want %#v", runner.calls, want)
	}
	if err := ValidateFirewallOperation(FirewallOperation{Backend: "UFW", Action: "add-source", Source: "192.0.2.0/24", ExpectedFingerprint: "fingerprint", Confirmation: "CONFIRM FIREWALL CHANGE"}); err != nil {
		t.Fatalf("UFW source validation error = %v", err)
	}
}

func TestFirewalldDefaultZoneUsesDocumentedCombinedOperation(t *testing.T) {
	runner := &firewallFixtureRunner{results: map[string]firewallFixtureResult{
		"firewall-cmd --set-default-zone=home": {output: "success\n"},
	}}
	adapter := NewFirewallStrategy(runner).(*firewallAdapter)
	if _, err := adapter.applyFirewalld(context.Background(), FirewallOperation{Action: "default-zone", DefaultZone: "home", Persist: true}, FirewallSnapshot{Backend: "firewalld", Active: true, DefaultZone: "public"}); err != nil {
		t.Fatalf("default-zone error = %v", err)
	}
	if !reflect.DeepEqual(runner.calls, []string{"firewall-cmd --set-default-zone=home"}) {
		t.Fatalf("default-zone commands = %#v", runner.calls)
	}
}
