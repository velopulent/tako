package zypper

import (
	"bufio"
	"context"
	"encoding/xml"
	"github.com/velopulent/tako/internal/platform"
	"os"
	"strings"
	"time"
)

type Provider struct{ tumbleweed bool }

func New() Provider {
	data, _ := os.ReadFile("/etc/os-release")
	text := strings.ToLower(string(data))
	return Provider{tumbleweed: strings.Contains(text, "tumbleweed")}
}
func (Provider) Name() string { return "zypper" }
func (Provider) Probe(ctx context.Context) (string, error) {
	if !platform.CommandExists("zypper") {
		return "", platform.ErrUpdateUnavailable
	}
	r := platform.RunUpdateCommand(ctx, "zypper", []string{"--version"}, nil, nil)
	if r.ExitCode != 0 {
		return "", platform.ErrUpdateUnavailable
	}
	return strings.TrimSpace(r.Output), nil
}
func (Provider) Inventory(ctx context.Context) ([]platform.UpdatePackage, error) {
	r := platform.RunUpdateCommand(ctx, "zypper", []string{"--no-refresh", "--non-interactive", "--xmlout", "list-updates"}, nil, nil)
	if r.ExitCode != 0 && r.ExitCode != 100 {
		return nil, r.Err
	}
	return parseXML(r.Output), nil
}
func (Provider) Refresh(ctx context.Context, force bool, emit func(platform.UpdateStreamEvent)) error {
	args := []string{"--non-interactive", "--xmlout", "refresh"}
	if force {
		args = append(args, "--force")
	}
	r := platform.RunUpdateCommand(ctx, "zypper", args, nil, emit)
	if r.ExitCode != 0 {
		return r.Err
	}
	return nil
}
func (p Provider) Plan(ctx context.Context) ([]platform.UpdateChange, error) {
	r := platform.RunUpdateCommand(ctx, "zypper", []string{"--no-refresh", "--non-interactive", "--xmlout", "--dry-run", operation(p)}, nil, nil)
	if r.ExitCode == 0 || r.ExitCode == 100 || r.ExitCode == 102 || r.ExitCode == 103 {
		if changes := parsePlanXML(r.Output); len(changes) > 0 || r.ExitCode == 0 {
			return changes, nil
		}
	}
	items, err := p.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	changes := make([]platform.UpdateChange, 0, len(items))
	for _, item := range items {
		changes = append(changes, platform.UpdateChange{Action: "upgrade", Name: item.Name, Architecture: item.Architecture, CurrentVersion: item.CurrentVersion, CandidateVersion: item.CandidateVersion})
	}
	return changes, nil
}
func (p Provider) Apply(ctx context.Context, emit func(platform.UpdateStreamEvent)) error {
	r := platform.RunUpdateCommand(ctx, "zypper", []string{"--no-refresh", "--non-interactive", "--xmlout", operation(p)}, nil, emit)
	if r.ExitCode != 0 {
		return r.Err
	}
	return nil
}

func operation(provider Provider) string {
	if provider.tumbleweed {
		return "dup"
	}
	return "update"
}
func (Provider) LockStatus(_ context.Context) (bool, string) {
	for _, path := range []string{"/var/run/zypp.pid", "/var/run/zypp-rpm.pid"} {
		if _, err := os.Stat(path); err == nil {
			return true, "Zypper lock is held: " + path
		}
	}
	return false, ""
}
func (Provider) Recovery(_ context.Context) platform.UpdateRecovery {
	recovery := platform.UpdateRecovery{RestartServices: []string{}, Hints: []string{"Restart services affected by updated libraries."}, Source: "advisory"}
	if _, err := os.Stat("/etc/zypp/needreboot"); err == nil {
		recovery.Authoritative = true
		recovery.RebootRequired = true
		recovery.Source = "/etc/zypp/needreboot"
		recovery.Hints = []string{"Reboot the host after updates complete."}
	}
	return recovery
}
func (Provider) History(_ context.Context, limit int) ([]platform.UpdateHistoryEntry, error) {
	data, err := os.ReadFile("/var/log/zypp/history")
	if err != nil {
		return nil, err
	}
	entries := parseHistory(string(data))
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	for l, r := 0, len(entries)-1; l < r; l, r = l+1, r-1 {
		entries[l], entries[r] = entries[r], entries[l]
	}
	return entries, nil
}

type stream struct {
	Updates []struct {
		Name       string `xml:"name,attr"`
		Edition    string `xml:"edition,attr"`
		EditionOld string `xml:"edition-old,attr"`
		Arch       string `xml:"arch,attr"`
	} `xml:"update-list>update"`
}

func parseXML(output string) []platform.UpdatePackage {
	var value stream
	if xml.Unmarshal([]byte(output), &value) != nil {
		return nil
	}
	items := make([]platform.UpdatePackage, 0, len(value.Updates))
	for _, item := range value.Updates {
		items = append(items, platform.UpdatePackage{Name: item.Name, Architecture: item.Arch, CurrentVersion: item.EditionOld, CandidateVersion: item.Edition})
	}
	return platform.SortUpdatePackages(items)
}

func parsePlanXML(output string) []platform.UpdateChange {
	decoder := xml.NewDecoder(strings.NewReader(output))
	changes := []platform.UpdateChange{}
	for len(changes) < platform.MaxUpdatePackages {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "solvable" {
			continue
		}
		attributes := map[string]string{}
		for _, attribute := range start.Attr {
			attributes[attribute.Name.Local] = attribute.Value
		}
		action := map[string]string{"to-be-installed": "install", "to-be-upgraded": "upgrade", "to-be-uninstalled": "remove", "to-be-downgraded": "downgrade", "to-be-reinstalled": "replace"}[attributes["status"]]
		if action == "" || attributes["name"] == "" {
			continue
		}
		changes = append(changes, platform.UpdateChange{Action: action, Name: attributes["name"], Architecture: attributes["arch"], CurrentVersion: attributes["edition-old"], CandidateVersion: attributes["edition"], TargetRepository: attributes["repository"], TargetVendor: attributes["vendor"]})
	}
	return platform.SortUpdateChanges(changes)
}
func parseHistory(content string) []platform.UpdateHistoryEntry {
	items := []platform.UpdateHistoryEntry{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "|")
		if len(fields) < 5 || strings.TrimSpace(fields[1]) != "update" {
			continue
		}
		parsed, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		items = append(items, platform.UpdateHistoryEntry{Time: parsed.UnixMilli(), Packages: map[string]string{strings.TrimSpace(fields[2]): strings.TrimSpace(fields[4])}})
	}
	return items
}
