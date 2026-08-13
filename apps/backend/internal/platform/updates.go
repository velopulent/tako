package platform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	MaxUpdatePackages = 500
	maxUpdateOutput   = 4 << 20
)

type UpdatePackage struct {
	Name             string `json:"name"`
	Architecture     string `json:"architecture,omitempty"`
	CurrentVersion   string `json:"currentVersion,omitempty"`
	CandidateVersion string `json:"candidateVersion"`
	Severity         string `json:"severity,omitempty"`
	Size             uint64 `json:"size,omitempty"`
	Summary          string `json:"summary,omitempty"`
	Details          string `json:"details,omitempty"`
}

type UpdateStatus struct {
	Available    bool            `json:"available"`
	Backend      string          `json:"backend"`
	Version      string          `json:"version,omitempty"`
	Contract     string          `json:"contract"`
	Packages     []UpdatePackage `json:"packages"`
	ExternalLock bool            `json:"externalLock"`
	LockReason   string          `json:"lockReason,omitempty"`
	Message      string          `json:"message"`
	Reason       string          `json:"reason,omitempty"`
}

type updateCommandResult struct {
	Output   string
	ExitCode int
	Err      error
}

type updateDependencies struct {
	packageKitAvailable func(context.Context) bool
	commandExists       func(string) bool
	commandVersion      func(context.Context, string, ...string) (string, bool)
	commandOutput       func(context.Context, string, ...string) updateCommandResult
	lockHeld            func(string) bool
}

// Updates reports read-only installed-software updates. It deliberately does
// not invoke package installation or refresh package metadata.
func Updates(ctx context.Context) UpdateStatus {
	deadline, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return updatesWithDependencies(deadline, defaultUpdateDependencies())
}

func defaultUpdateDependencies() updateDependencies {
	return updateDependencies{
		packageKitAvailable: packageKitAvailable,
		commandExists:       commandExists,
		commandVersion: func(ctx context.Context, name string, arguments ...string) (string, bool) {
			return (hostProbe{}).CommandVersion(ctx, name, arguments...)
		},
		commandOutput: runUpdateCommand,
		lockHeld:      updateLockHeld,
	}
}

func updatesWithDependencies(ctx context.Context, dependencies updateDependencies) UpdateStatus {
	if dependencies.packageKitAvailable != nil && dependencies.packageKitAvailable(ctx) {
		status := UpdateStatus{Available: true, Backend: "PackageKit", Contract: "dbus-read-only", Packages: []UpdatePackage{}}
		if dependencies.commandVersion != nil {
			status.Version, _ = dependencies.commandVersion(ctx, "pkcon", "--version")
		}
		if dependencies.commandExists != nil && dependencies.commandOutput != nil && dependencies.commandExists("pkcon") {
			result := dependencies.commandOutput(ctx, "pkcon", "--noninteractive", "get-updates")
			if result.Err == nil && result.ExitCode == 0 {
				status.Packages = parsePackageKitUpdates(result.Output)
				status.Message = updateMessage(len(status.Packages))
				return status
			}
		}
		status.Message = "PackageKit is available; update inventory could not be read"
		status.Reason = "The PackageKit read-only query did not complete"
		return status
	}

	if dependencies.commandVersion != nil {
		if version, ok := dependencies.commandVersion(ctx, "apt-get", "--version"); ok && versionAtLeast(version, 1) {
			status := commandUpdateStatus("apt-get", version, dependencies, aptLockPaths)
			if dependencies.commandExists != nil && dependencies.commandExists("apt") {
				result := dependencies.commandOutput(ctx, "apt", "list", "--upgradable")
				if result.Err == nil && result.ExitCode == 0 {
					status.Packages = parseAPTUpdates(result.Output)
					status.Message = updateMessage(len(status.Packages))
					return status
				}
			}
			result := dependencies.commandOutput(ctx, "apt-get", "--just-print", "--simulate", "upgrade")
			if result.Err == nil && result.ExitCode == 0 {
				status.Packages = parseAPTGetUpdates(result.Output)
				status.Message = updateMessage(len(status.Packages))
				return status
			}
			status.Message = "APT is available; update inventory could not be read"
			status.Reason = "The bounded APT read-only query did not complete"
			return status
		}
		if version, ok := dependencies.commandVersion(ctx, "dnf", "--version"); ok && versionAtLeast(version, 4) {
			status := commandUpdateStatus("dnf", version, dependencies, dnfLockPaths)
			result := dependencies.commandOutput(ctx, "dnf", "--assumeno", "check-update")
			if (result.Err == nil && (result.ExitCode == 0 || result.ExitCode == 100)) || result.ExitCode == 100 {
				status.Packages = parseDNFUpdates(result.Output)
				status.Message = updateMessage(len(status.Packages))
				return status
			}
			status.Message = "DNF is available; update inventory could not be read"
			status.Reason = "The bounded DNF read-only query did not complete"
			return status
		}
	}
	return UpdateStatus{Available: false, Backend: "none", Contract: "unavailable", Packages: []UpdatePackage{}, Message: "No supported read-only update backend detected", Reason: "PackageKit, APT, and DNF are unavailable"}
}

func commandUpdateStatus(backend, version string, dependencies updateDependencies, paths func() []string) UpdateStatus {
	status := UpdateStatus{Available: true, Backend: backend, Version: version, Contract: "bounded-command-read-only", Packages: []UpdatePackage{}}
	for _, path := range paths() {
		if dependencies.lockHeld != nil && dependencies.lockHeld(path) {
			status.ExternalLock = true
			status.LockReason = "A package-manager lock file exists: " + path
			break
		}
	}
	return status
}

func updateLockHeld(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false
}

func aptLockPaths() []string {
	return []string{"/var/lib/dpkg/lock-frontend", "/var/lib/dpkg/lock", "/var/lib/apt/lists/lock", "/var/cache/apt/archives/lock"}
}

func dnfLockPaths() []string {
	return []string{"/var/cache/dnf/metadata_lock.pid", "/var/cache/dnf/lock.pid", "/var/run/dnf.pid"}
}

func packageKitAvailable(ctx context.Context) bool {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	var active []string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0).Store(&active); err == nil {
		for _, name := range active {
			if name == "org.freedesktop.PackageKit" {
				return true
			}
		}
	}
	var activatable []string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err != nil {
		return false
	}
	for _, name := range activatable {
		if name == "org.freedesktop.PackageKit" {
			return true
		}
	}
	return false
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runUpdateCommand(ctx context.Context, name string, arguments ...string) updateCommandResult {
	commandCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, name, arguments...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return updateCommandResult{Err: err, ExitCode: -1}
	}
	if err := command.Start(); err != nil {
		return updateCommandResult{Err: err, ExitCode: -1}
	}
	output, readErr := readBounded(stdout, maxUpdateOutput)
	waitErr := command.Wait()
	if commandCtx.Err() != nil {
		return updateCommandResult{Err: commandCtx.Err(), ExitCode: -1}
	}
	result := updateCommandResult{Output: string(output), ExitCode: 0}
	if readErr != nil {
		result.Err = readErr
	}
	if waitErr != nil {
		result.Err = waitErr
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}
	return result
}

func versionAtLeast(version string, minimumMajor int) bool {
	match := regexp.MustCompile(`(?:^|\s)([0-9]+)(?:\.[0-9]+)?`).FindStringSubmatch(version)
	if len(match) < 2 {
		return false
	}
	major, err := strconv.Atoi(match[1])
	return err == nil && major >= minimumMajor
}

func updateMessage(count int) string {
	if count == 0 {
		return "No installed-software updates are currently available."
	}
	return fmt.Sprintf("%d installed-software update%s available.", count, pluralSuffix(count))
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func parseAPTUpdates(output string) []UpdatePackage {
	updates := make([]UpdatePackage, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() && len(updates) < MaxUpdatePackages {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 || !strings.Contains(fields[0], "/") || strings.EqualFold(fields[0], "Listing...") {
			continue
		}
		name := strings.SplitN(fields[0], "/", 2)[0]
		if !validPackageField(name) || !validPackageField(fields[1]) {
			continue
		}
		item := UpdatePackage{Name: name, CandidateVersion: fields[1], Architecture: fields[2]}
		line := scanner.Text()
		if start := strings.Index(line, "[upgradable from:"); start >= 0 {
			value := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line[start+len("[upgradable from:"):], "["), "]"))
			item.CurrentVersion = strings.TrimSpace(value)
		}
		updates = append(updates, item)
	}
	return sortUpdates(updates)
}

func parseAPTGetUpdates(output string) []UpdatePackage {
	updates := make([]UpdatePackage, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() && len(updates) < MaxUpdatePackages {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[0] != "Inst" || !validPackageField(fields[1]) {
			continue
		}
		item := UpdatePackage{Name: fields[1]}
		if len(fields) > 2 {
			item.CurrentVersion = strings.Trim(fields[2], "[]")
		}
		if len(fields) > 3 {
			item.CandidateVersion = strings.Trim(fields[3], "()")
		}
		updates = append(updates, item)
	}
	return sortUpdates(updates)
}

func parseDNFUpdates(output string) []UpdatePackage {
	updates := make([]UpdatePackage, 0)
	started := false
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() && len(updates) < MaxUpdatePackages {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.EqualFold(fields[0], "Package") && strings.EqualFold(fields[1], "Arch") {
			started = true
			continue
		}
		if !started || len(fields) < 4 || !validPackageField(fields[0]) || !validPackageField(fields[2]) {
			continue
		}
		updates = append(updates, UpdatePackage{Name: fields[0], Architecture: fields[1], CandidateVersion: fields[2], Summary: strings.Join(fields[3:], " ")})
	}
	return sortUpdates(updates)
}

func parsePackageKitUpdates(output string) []UpdatePackage {
	updates := make([]UpdatePackage, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() && len(updates) < MaxUpdatePackages {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.EqualFold(fields[0], "package") {
			continue
		}
		if strings.EqualFold(fields[0], "available") {
			if len(fields) < 3 {
				continue
			}
			if strings.Contains(fields[1], ";") {
				parts := strings.Split(fields[1], ";")
				if len(parts) < 3 || !validPackageField(parts[0]) || !validPackageField(parts[1]) {
					continue
				}
				item := UpdatePackage{Name: parts[0], CandidateVersion: parts[1], Architecture: parts[2]}
				if len(parts) > 3 {
					item.Summary = strings.Join(append(parts[3:], fields[2:]...), " ")
				}
				updates = append(updates, item)
				continue
			}
			if len(fields) < 4 || !validPackageField(fields[1]) || !validPackageField(fields[2]) {
				continue
			}
			updates = append(updates, UpdatePackage{Name: fields[1], CandidateVersion: fields[2], Architecture: fields[3], Summary: strings.Join(fields[4:], " ")})
			continue
		}
		name := fields[0]
		architecture := ""
		if index := strings.LastIndex(name, "."); index > 0 && validArchitecture(name[index+1:]) {
			architecture = name[index+1:]
			name = name[:index]
		}
		if !validPackageField(name) {
			continue
		}
		updates = append(updates, UpdatePackage{Name: name, Architecture: architecture, CandidateVersion: fields[1], Summary: strings.Join(fields[2:], " ")})
	}
	return sortUpdates(updates)
}

func sortUpdates(updates []UpdatePackage) []UpdatePackage {
	sort.Slice(updates, func(left, right int) bool {
		if updates[left].Name == updates[right].Name {
			return updates[left].Architecture < updates[right].Architecture
		}
		return updates[left].Name < updates[right].Name
	})
	return updates
}

func validPackageField(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n")
}

func validArchitecture(value string) bool {
	return validPackageField(value) && len(value) <= 32 && !strings.ContainsAny(value, "/[]")
}
