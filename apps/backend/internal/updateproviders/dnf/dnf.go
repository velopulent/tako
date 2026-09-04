package dnf

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/velopulent/tako/internal/platform"
	"os"
	"regexp"
	"strings"
	"time"
)

type Provider struct{}

func New() Provider           { return Provider{} }
func (Provider) Name() string { return "dnf" }
func (Provider) Probe(ctx context.Context) (string, error) {
	if !platform.CommandExists("dnf") {
		return "", platform.ErrUpdateUnavailable
	}
	r := platform.RunUpdateCommand(ctx, "dnf", []string{"--version"}, nil, nil)
	if r.ExitCode != 0 {
		return "", platform.ErrUpdateUnavailable
	}
	line, _, _ := strings.Cut(strings.TrimSpace(r.Output), "\n")
	return line, nil
}
func (p Provider) Inventory(ctx context.Context) ([]platform.UpdatePackage, error) {
	version, _ := p.Probe(ctx)
	if strings.Contains(strings.ToLower(version), "dnf5") {
		r := platform.RunUpdateCommand(ctx, "dnf", []string{"--cacheonly", "check-upgrade", "--json"}, nil, nil)
		if r.ExitCode == 0 || r.ExitCode == 100 {
			if items := parseJSON(r.Output); len(items) > 0 || r.ExitCode == 0 {
				return items, nil
			}
		}
	}
	r := platform.RunUpdateCommand(ctx, "dnf", []string{"--cacheonly", "--assumeno", "check-update"}, nil, nil)
	if r.ExitCode != 0 && r.ExitCode != 100 {
		return nil, r.Err
	}
	return parseTable(r.Output), nil
}
func (Provider) Refresh(ctx context.Context, force bool, emit func(platform.UpdateStreamEvent)) error {
	args := []string{"makecache"}
	if force {
		args = append(args, "--refresh")
	}
	r := platform.RunUpdateCommand(ctx, "dnf", args, nil, emit)
	if r.ExitCode != 0 {
		return r.Err
	}
	return nil
}
func (p Provider) Plan(ctx context.Context) ([]platform.UpdateChange, error) {
	r := platform.RunUpdateCommand(ctx, "dnf", []string{"--cacheonly", "--assumeno", "upgrade"}, nil, nil)
	changes := parsePlan(r.Output)
	if len(changes) > 0 || r.ExitCode == 0 {
		return changes, nil
	}
	items, err := p.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	changes = make([]platform.UpdateChange, 0, len(items))
	for _, item := range items {
		changes = append(changes, platform.UpdateChange{Action: "upgrade", Name: item.Name, Architecture: item.Architecture, CurrentVersion: item.CurrentVersion, CandidateVersion: item.CandidateVersion})
	}
	return platform.SortUpdateChanges(changes), nil
}
func (Provider) Apply(ctx context.Context, emit func(platform.UpdateStreamEvent)) error {
	r := platform.RunUpdateCommand(ctx, "dnf", []string{"-y", "upgrade"}, nil, emit)
	if r.ExitCode != 0 {
		return r.Err
	}
	return nil
}
func (Provider) LockStatus(_ context.Context) (bool, string) {
	for _, path := range []string{"/var/cache/dnf/metadata_lock.pid", "/var/cache/dnf/lock.pid", "/var/run/dnf.pid"} {
		if platform.UpdateLockHeld(path) {
			return true, "DNF lock is held: " + path
		}
	}
	return false, ""
}
func (Provider) Recovery(ctx context.Context) platform.UpdateRecovery {
	recovery := platform.UpdateRecovery{RestartServices: []string{}, Hints: []string{"Restart services affected by updated libraries."}, Source: "advisory"}
	r := platform.RunUpdateCommand(ctx, "dnf", []string{"needs-restarting", "-r"}, nil, nil)
	if r.ExitCode == 1 {
		recovery.Authoritative = true
		recovery.RebootRequired = true
		recovery.Source = "needs-restarting"
		recovery.Hints = []string{"Reboot the host after updates complete."}
	}
	return recovery
}
func (Provider) History(_ context.Context, limit int) ([]platform.UpdateHistoryEntry, error) {
	data, err := os.ReadFile("/var/log/dnf.rpm.log")
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

func parseJSON(output string) []platform.UpdatePackage {
	var value any
	if json.Unmarshal([]byte(output), &value) != nil {
		return nil
	}
	items := []platform.UpdatePackage{}
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case map[string]any:
			name, _ := typed["name"].(string)
			version, _ := typed["version"].(string)
			if version == "" {
				version, _ = typed["evr"].(string)
			}
			arch, _ := typed["arch"].(string)
			if name != "" && version != "" {
				items = append(items, platform.UpdatePackage{Name: name, CandidateVersion: version, Architecture: arch})
				return
			}
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return platform.SortUpdatePackages(items)
}

var dnfRow = regexp.MustCompile(`^([A-Za-z0-9+_.:@-]+)\.([A-Za-z0-9_+-]+)\s+(\S+)\s+(\S+)`)

func parseTable(output string) []platform.UpdatePackage {
	items := []platform.UpdatePackage{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() && len(items) < platform.MaxUpdatePackages {
		match := dnfRow.FindStringSubmatch(strings.TrimSpace(scanner.Text()))
		if len(match) == 5 && match[1] != "Package" && match[1] != "Last" {
			items = append(items, platform.UpdatePackage{Name: match[1], Architecture: match[2], CandidateVersion: match[3], Summary: match[4]})
		}
	}
	return platform.SortUpdatePackages(items)
}

func parsePlan(output string) []platform.UpdateChange {
	changes := []platform.UpdateChange{}
	action := ""
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() && len(changes) < platform.MaxUpdatePackages {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "Installing"):
			action = "install"
		case strings.HasPrefix(line, "Upgrading"):
			action = "upgrade"
		case strings.HasPrefix(line, "Removing"):
			action = "remove"
		case strings.HasPrefix(line, "Downgrading"):
			action = "downgrade"
		case strings.HasPrefix(line, "Replacing"):
			action = "replace"
		case line == "" || strings.HasPrefix(line, "Transaction Summary"):
			action = ""
		default:
			fields := strings.Fields(line)
			if action != "" && len(fields) >= 4 && fields[0] != "Package" {
				changes = append(changes, platform.UpdateChange{Action: action, Name: fields[0], Architecture: fields[1], CandidateVersion: fields[2], TargetRepository: fields[3]})
			}
		}
	}
	return platform.SortUpdateChanges(changes)
}

var historyLine = regexp.MustCompile(`^(\S+)\s+(?:Upgraded|Upgrade):\s+(.+)$`)

func parseHistory(content string) []platform.UpdateHistoryEntry {
	entries := []platform.UpdateHistoryEntry{}
	for _, line := range strings.Split(content, "\n") {
		match := historyLine.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) != 3 {
			continue
		}
		parsed, err := time.Parse("2006-01-02T15:04:05-0700", match[1])
		if err != nil {
			continue
		}
		name := strings.Split(match[2], "-")[0]
		entries = append(entries, platform.UpdateHistoryEntry{Time: parsed.UnixMilli(), Packages: map[string]string{name: match[2]}})
	}
	return entries
}
