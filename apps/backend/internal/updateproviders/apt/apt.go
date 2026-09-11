package apt

import (
	"bufio"
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/velopulent/tako/internal/platform"
)

type Provider struct{}

func New() Provider           { return Provider{} }
func (Provider) Name() string { return "apt" }

func (Provider) Probe(ctx context.Context) (string, error) {
	if !platform.CommandExists("apt-get") {
		return "", platform.ErrUpdateUnavailable
	}
	result := platform.RunUpdateCommand(ctx, "apt-get", []string{"--version"}, nil, nil)
	if result.ExitCode != 0 {
		return "", platform.ErrUpdateUnavailable
	}
	line, _, _ := strings.Cut(strings.TrimSpace(result.Output), "\n")
	return line, nil
}

func (Provider) Inventory(ctx context.Context) ([]platform.UpdatePackage, error) {
	if !platform.CommandExists("apt") {
		return nil, platform.ErrUpdateUnavailable
	}
	result := platform.RunUpdateCommand(ctx, "apt", []string{"list", "--upgradable"}, nil, nil)
	if result.ExitCode != 0 {
		return nil, result.Err
	}
	return parseInventory(result.Output), nil
}

func (Provider) Refresh(ctx context.Context, force bool, emit func(platform.UpdateStreamEvent)) error {
	args := []string{"update"}
	if force {
		args = append([]string{"-o", "Acquire::http::No-Cache=true"}, args...)
	}
	result := platform.RunUpdateCommand(ctx, "apt-get", args, []string{"DEBIAN_FRONTEND=noninteractive"}, emit)
	if result.ExitCode != 0 {
		return result.Err
	}
	return nil
}

func (Provider) Plan(ctx context.Context) ([]platform.UpdateChange, error) {
	result := platform.RunUpdateCommand(ctx, "apt-get", []string{"--simulate", "dist-upgrade"}, []string{"DEBIAN_FRONTEND=noninteractive"}, nil)
	if result.ExitCode != 0 {
		return nil, result.Err
	}
	return parsePlan(result.Output), nil
}

func (Provider) Apply(ctx context.Context, emit func(platform.UpdateStreamEvent)) error {
	wrapped := func(event platform.UpdateStreamEvent) {
		if emit == nil {
			return
		}
		if progress, ok := parseStatus(event); ok {
			emit(platform.UpdateStreamEvent{Kind: "progress", Progress: progress})
			return
		}
		emit(event)
	}
	result := platform.RunUpdateCommand(ctx, "apt-get", []string{"-y", "-o", "APT::Status-Fd=1", "dist-upgrade"}, []string{"DEBIAN_FRONTEND=noninteractive"}, wrapped)
	if result.ExitCode != 0 {
		return result.Err
	}
	return nil
}

func parseStatus(event platform.UpdateStreamEvent) (platform.UpdateProgress, bool) {
	if event.Kind != "output" || event.Output.Stream != "stdout" {
		return platform.UpdateProgress{}, false
	}
	fields := strings.SplitN(event.Output.Line, ":", 4)
	if len(fields) != 4 || (fields[0] != "dlstatus" && fields[0] != "pmstatus") {
		return platform.UpdateProgress{}, false
	}
	percent, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return platform.UpdateProgress{}, false
	}
	phase := "applying"
	if fields[0] == "dlstatus" {
		phase = "downloading"
	}
	return platform.UpdateProgress{Active: true, Phase: phase, Package: fields[1], Percent: int(percent), Message: fields[3], Cancelable: false}, true
}

func (Provider) LockStatus(_ context.Context) (bool, string) {
	for _, path := range []string{"/var/lib/dpkg/lock-frontend", "/var/lib/dpkg/lock", "/var/lib/apt/lists/lock", "/var/cache/apt/archives/lock"} {
		if platform.UpdateLockHeld(path) {
			return true, "APT lock is held: " + path
		}
	}
	return false, ""
}

func (Provider) Recovery(_ context.Context) platform.UpdateRecovery {
	recovery := platform.UpdateRecovery{RestartServices: []string{}, Hints: []string{"Restart services affected by updated libraries."}, Source: "advisory"}
	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		recovery.Authoritative = true
		recovery.RebootRequired = true
		recovery.Source = "/var/run/reboot-required"
		recovery.Hints = []string{"Reboot the host after updates complete."}
	}
	return recovery
}

func (Provider) History(_ context.Context, limit int) ([]platform.UpdateHistoryEntry, error) {
	data, err := os.ReadFile("/var/log/apt/history.log")
	if err != nil {
		return nil, err
	}
	entries := parseHistory(string(data))
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	return entries, nil
}

var aptLine = regexp.MustCompile(`^([^/\s]+)/\S+\s+(\S+)\s+(\S+)\s+\[upgradable from:\s*([^\]]+)\]`)

func parseInventory(output string) []platform.UpdatePackage {
	items := []platform.UpdatePackage{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() && len(items) < platform.MaxUpdatePackages {
		match := aptLine.FindStringSubmatch(strings.TrimSpace(scanner.Text()))
		if len(match) == 5 {
			items = append(items, platform.UpdatePackage{Name: match[1], CandidateVersion: match[2], Architecture: match[3], CurrentVersion: match[4]})
		}
	}
	return platform.SortUpdatePackages(items)
}

var aptPlanLine = regexp.MustCompile(`^(Inst|Remv)\s+(\S+)(?:\s+\[([^\]]+)\])?(?:\s+\(([^\s\)]+))?`)

func parsePlan(output string) []platform.UpdateChange {
	changes := []platform.UpdateChange{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		match := aptPlanLine.FindStringSubmatch(strings.TrimSpace(scanner.Text()))
		if len(match) == 0 {
			continue
		}
		action := "upgrade"
		if match[1] == "Remv" {
			action = "remove"
		} else if match[3] == "" {
			action = "install"
		}
		changes = append(changes, platform.UpdateChange{Action: action, Name: match[2], CurrentVersion: match[3], CandidateVersion: match[4]})
	}
	return platform.SortUpdateChanges(changes)
}

func parseHistory(content string) []platform.UpdateHistoryEntry {
	entries := []platform.UpdateHistoryEntry{}
	var current *platform.UpdateHistoryEntry
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "Start-Date: ") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "Start-Date: "))
			parsed, err := time.Parse("2006-01-02  15:04:05", value)
			if err == nil {
				current = &platform.UpdateHistoryEntry{Time: parsed.UnixMilli(), Packages: map[string]string{}}
			}
		} else if current != nil && strings.HasPrefix(line, "Upgrade: ") {
			for _, item := range strings.Split(strings.TrimPrefix(line, "Upgrade: "), "),") {
				fields := strings.Split(strings.TrimSpace(strings.TrimSuffix(item, ")")), " ")
				if len(fields) >= 3 {
					current.Packages[strings.Split(fields[0], ":")[0]] = strings.Trim(fields[2], ",")
				}
			}
		} else if line == "End-Date: " || strings.HasPrefix(line, "End-Date:") {
			if current != nil && len(current.Packages) > 0 {
				entries = append(entries, *current)
			}
			current = nil
		}
	}
	return entries
}
