package pacman

import (
	"bufio"
	"context"
	"github.com/velopulent/tako/internal/platform"
	"os"
	"regexp"
	"strings"
	"time"
)

type Provider struct{}

func New() Provider           { return Provider{} }
func (Provider) Name() string { return "pacman" }
func (Provider) Probe(ctx context.Context) (string, error) {
	if !platform.CommandExists("pacman") || !platform.CommandExists("checkupdates") {
		return "", platform.ErrUpdateUnavailable
	}
	r := platform.RunUpdateCommand(ctx, "pacman", []string{"--version"}, nil, nil)
	if r.ExitCode != 0 {
		return "", platform.ErrUpdateUnavailable
	}
	return strings.TrimSpace(r.Output), nil
}
func (Provider) Inventory(ctx context.Context) ([]platform.UpdatePackage, error) {
	r := platform.RunUpdateCommand(ctx, "checkupdates", []string{"--nosync", "--nocolor"}, []string{"CHECKUPDATES_DB=/run/tako/checkupdates"}, nil)
	if r.ExitCode != 0 && r.ExitCode != 2 {
		return nil, r.Err
	}
	return parseInventory(r.Output), nil
}
func (Provider) Refresh(ctx context.Context, _ bool, emit func(platform.UpdateStreamEvent)) error {
	r := platform.RunUpdateCommand(ctx, "checkupdates", []string{"--nocolor"}, []string{"CHECKUPDATES_DB=/run/tako/checkupdates"}, emit)
	if r.ExitCode != 0 && r.ExitCode != 2 {
		return r.Err
	}
	return nil
}
func (p Provider) Plan(ctx context.Context) ([]platform.UpdateChange, error) {
	items, err := p.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	changes := make([]platform.UpdateChange, 0, len(items))
	for _, item := range items {
		changes = append(changes, platform.UpdateChange{Action: "upgrade", Name: item.Name, CurrentVersion: item.CurrentVersion, CandidateVersion: item.CandidateVersion})
	}
	return changes, nil
}
func (Provider) Apply(ctx context.Context, emit func(platform.UpdateStreamEvent)) error {
	r := platform.RunUpdateCommand(ctx, "pacman", []string{"--noconfirm", "-Syu"}, nil, emit)
	if r.ExitCode != 0 {
		return r.Err
	}
	return nil
}
func (Provider) LockStatus(_ context.Context) (bool, string) {
	if _, err := os.Stat("/var/lib/pacman/db.lck"); err == nil {
		return true, "Pacman database lock is held"
	}
	return false, ""
}
func (Provider) Recovery(_ context.Context) platform.UpdateRecovery {
	return platform.UpdateRecovery{RestartServices: []string{}, Hints: []string{"Reboot after kernel or core system library updates."}, Source: "advisory", Reason: "Pacman does not expose an authoritative reboot-required state."}
}
func (Provider) History(_ context.Context, limit int) ([]platform.UpdateHistoryEntry, error) {
	data, err := os.ReadFile("/var/log/pacman.log")
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
func parseInventory(output string) []platform.UpdatePackage {
	items := []platform.UpdatePackage{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 && fields[2] == "->" {
			items = append(items, platform.UpdatePackage{Name: fields[0], CurrentVersion: fields[1], CandidateVersion: fields[3]})
		}
	}
	return platform.SortUpdatePackages(items)
}

var historyLine = regexp.MustCompile(`^\[([^\]]+)\] \[ALPM\] upgraded (\S+) \((\S+) -> (\S+)\)`)

func parseHistory(content string) []platform.UpdateHistoryEntry {
	items := []platform.UpdateHistoryEntry{}
	for _, line := range strings.Split(content, "\n") {
		match := historyLine.FindStringSubmatch(line)
		if len(match) != 5 {
			continue
		}
		parsed, err := time.Parse("2006-01-02T15:04:05-0700", match[1])
		if err != nil {
			continue
		}
		items = append(items, platform.UpdateHistoryEntry{Time: parsed.UnixMilli(), Packages: map[string]string{match[2]: match[4]}})
	}
	return items
}
