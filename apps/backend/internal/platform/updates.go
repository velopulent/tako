package platform

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

var (
	ErrInvalidUpdateOperation = errors.New("invalid update operation")
	ErrUpdateConflict         = errors.New("update inventory changed")
	ErrUpdateLocked           = errors.New("package manager lock is held")
	ErrUpdateUnavailable      = errors.New("update backend unavailable")
	ErrUpdateVerification     = errors.New("update verification failed")
	ErrUpdateApply            = errors.New("update command failed")
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
	Fingerprint  string          `json:"fingerprint"`
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
	status := updatesWithDependencies(deadline, defaultUpdateDependencies())
	status.Fingerprint = UpdateFingerprint(status)
	return status
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

type UpdateOperation struct {
	Scope               string   `json:"scope"`
	Packages            []string `json:"packages,omitempty"`
	ExpectedFingerprint string   `json:"expectedFingerprint,omitempty"`
	Confirmation        string   `json:"confirmation,omitempty"`
	Preview             bool     `json:"preview,omitempty"`
}

type UpdatePreview struct {
	Operation            UpdateOperation `json:"operation"`
	Current              UpdateStatus    `json:"current"`
	Selected             []UpdatePackage `json:"selected"`
	Changes              []string        `json:"changes"`
	Warnings             []string        `json:"warnings"`
	Fingerprint          string          `json:"fingerprint"`
	Stale                bool            `json:"stale"`
	Allowed              bool            `json:"allowed"`
	RequiresConfirmation bool            `json:"requiresConfirmation"`
	Reason               string          `json:"reason,omitempty"`
}

type UpdateResult struct {
	Backend     string          `json:"backend"`
	Scope       string          `json:"scope"`
	Packages    []string        `json:"packages"`
	Updated     []UpdatePackage `json:"updated"`
	Verified    bool            `json:"verified"`
	Message     string          `json:"message"`
	Fingerprint string          `json:"fingerprint"`
}

func ValidateUpdateOperation(operation UpdateOperation) error {
	if operation.Scope != "all" && operation.Scope != "selected" {
		return ErrInvalidUpdateOperation
	}
	if len(operation.Packages) > MaxUpdatePackages {
		return ErrInvalidUpdateOperation
	}
	seen := make(map[string]struct{}, len(operation.Packages))
	for _, name := range operation.Packages {
		if !validPackageName(name) || len(name) > 256 {
			return ErrInvalidUpdateOperation
		}
		if _, exists := seen[name]; exists {
			return ErrInvalidUpdateOperation
		}
		seen[name] = struct{}{}
	}
	if operation.Scope == "all" && len(operation.Packages) != 0 {
		return ErrInvalidUpdateOperation
	}
	if operation.Scope == "selected" && len(operation.Packages) == 0 {
		return ErrInvalidUpdateOperation
	}
	if operation.ExpectedFingerprint != "" {
		if len(operation.ExpectedFingerprint) != sha256.Size*2 {
			return ErrInvalidUpdateOperation
		}
		if _, err := hex.DecodeString(operation.ExpectedFingerprint); err != nil {
			return ErrInvalidUpdateOperation
		}
	}
	if len(operation.Confirmation) > 128 || strings.ContainsAny(operation.Confirmation, "\x00\r\n") {
		return ErrInvalidUpdateOperation
	}
	if !operation.Preview && operation.Confirmation != "APPLY UPDATES" {
		return ErrInvalidUpdateOperation
	}
	return nil
}

func UpdateFingerprint(status UpdateStatus) string {
	packages := append([]UpdatePackage(nil), status.Packages...)
	sort.Slice(packages, func(left, right int) bool {
		if packages[left].Name != packages[right].Name {
			return packages[left].Name < packages[right].Name
		}
		if packages[left].Architecture != packages[right].Architecture {
			return packages[left].Architecture < packages[right].Architecture
		}
		return packages[left].CandidateVersion < packages[right].CandidateVersion
	})
	payload, _ := json.Marshal(struct {
		Backend      string
		Version      string
		Packages     []UpdatePackage
		ExternalLock bool
	}{status.Backend, status.Version, packages, status.ExternalLock})
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func PreviewUpdates(ctx context.Context, operation UpdateOperation, statusFn func(context.Context) UpdateStatus) (UpdatePreview, error) {
	operation.Preview = true
	if err := ValidateUpdateOperation(operation); err != nil {
		return UpdatePreview{}, err
	}
	if statusFn == nil {
		return UpdatePreview{}, ErrUpdateUnavailable
	}
	current := statusFn(ctx)
	preview := UpdatePreview{Operation: operation, Current: current, Selected: make([]UpdatePackage, 0), Changes: []string{}, Warnings: []string{}, Fingerprint: UpdateFingerprint(current)}
	if operation.ExpectedFingerprint != "" && operation.ExpectedFingerprint != preview.Fingerprint {
		preview.Stale = true
		preview.Reason = "The available update inventory changed; refresh before applying."
		return preview, nil
	}
	if !current.Available {
		preview.Reason = current.Reason
		if preview.Reason == "" {
			preview.Reason = "No supported update backend is available."
		}
		return preview, nil
	}
	if current.ExternalLock {
		preview.Reason = current.LockReason
		if preview.Reason == "" {
			preview.Reason = "Another package operation currently holds a lock."
		}
		preview.Warnings = append(preview.Warnings, preview.Reason)
		return preview, nil
	}
	if operation.Scope == "all" {
		preview.Selected = append(preview.Selected, current.Packages...)
	} else {
		for _, requested := range operation.Packages {
			found := false
			for _, available := range current.Packages {
				if available.Name == requested {
					preview.Selected = append(preview.Selected, available)
					found = true
				}
			}
			if !found {
				preview.Stale = true
				preview.Reason = "One or more selected packages are no longer available."
				return preview, nil
			}
		}
	}
	if len(preview.Selected) == 0 {
		preview.Reason = "No updates are available for the selected scope."
		return preview, nil
	}
	preview.Allowed = true
	preview.RequiresConfirmation = true
	preview.Changes = append(preview.Changes, fmt.Sprintf("update %d package%s", len(preview.Selected), pluralSuffix(len(preview.Selected))))
	preview.Warnings = append(preview.Warnings, "Updates can restart services or require a host reboot.", "An interrupted package operation will not be retried automatically.")
	return preview, nil
}

func ApplyUpdates(ctx context.Context, operation UpdateOperation) (UpdateResult, error) {
	if err := ValidateUpdateOperation(operation); err != nil {
		return UpdateResult{}, err
	}
	if operation.Preview {
		return UpdateResult{}, ErrInvalidUpdateOperation
	}
	current := Updates(ctx)
	if !current.Available {
		return UpdateResult{}, ErrUpdateUnavailable
	}
	fingerprint := UpdateFingerprint(current)
	if operation.ExpectedFingerprint == "" || operation.ExpectedFingerprint != fingerprint {
		return UpdateResult{}, ErrUpdateConflict
	}
	if current.ExternalLock {
		return UpdateResult{}, ErrUpdateLocked
	}
	selected := make([]UpdatePackage, 0, len(current.Packages))
	if operation.Scope == "all" {
		selected = append(selected, current.Packages...)
	} else {
		for _, requested := range operation.Packages {
			found := false
			for _, available := range current.Packages {
				if available.Name == requested {
					selected = append(selected, available)
					found = true
				}
			}
			if !found {
				return UpdateResult{}, ErrUpdateConflict
			}
		}
	}
	if len(selected) == 0 {
		return UpdateResult{Backend: current.Backend, Scope: operation.Scope, Packages: []string{}, Updated: []UpdatePackage{}, Verified: true, Message: "No updates were available.", Fingerprint: fingerprint}, nil
	}
	arguments, err := updateApplyArguments(current.Backend, operation.Scope, operation.Packages)
	if err != nil {
		return UpdateResult{}, err
	}
	result := runLongUpdateCommand(ctx, arguments[0], arguments[1:]...)
	if result.Err != nil || result.ExitCode != 0 {
		return UpdateResult{}, fmt.Errorf("%w: %s", ErrUpdateApply, boundedUpdateError(result.Err, result.Output))
	}
	final := Updates(ctx)
	if !final.Available {
		return UpdateResult{}, ErrUpdateVerification
	}
	remaining := make(map[string]struct{}, len(final.Packages))
	for _, item := range final.Packages {
		remaining[item.Name] = struct{}{}
	}
	for _, item := range selected {
		if _, exists := remaining[item.Name]; exists {
			return UpdateResult{}, ErrUpdateVerification
		}
	}
	packages := make([]string, 0, len(selected))
	updated := make([]UpdatePackage, 0, len(selected))
	for _, item := range selected {
		packages = append(packages, item.Name)
		updated = append(updated, UpdatePackage{
			Name:             item.Name,
			Architecture:     item.Architecture,
			CurrentVersion:   item.CurrentVersion,
			CandidateVersion: item.CandidateVersion,
			Severity:         item.Severity,
			Size:             item.Size,
		})
	}
	return UpdateResult{Backend: current.Backend, Scope: operation.Scope, Packages: packages, Updated: updated, Verified: true, Message: "Updates applied and verified.", Fingerprint: UpdateFingerprint(final)}, nil
}

func updateApplyArguments(backend, scope string, packages []string) ([]string, error) {
	if backend == "PackageKit" {
		arguments := []string{"pkcon", "--noninteractive", "update"}
		if scope == "selected" {
			arguments = append(arguments, packages...)
		}
		return arguments, nil
	}
	if backend == "apt-get" {
		arguments := []string{"apt-get", "-y", "--no-remove", "--only-upgrade"}
		if scope == "all" {
			return append(arguments, "upgrade"), nil
		}
		return append(arguments, append([]string{"install", "--"}, packages...)...), nil
	}
	if backend == "dnf" {
		arguments := []string{"dnf", "-y", "upgrade"}
		if scope == "selected" {
			arguments = append(arguments, "--")
			arguments = append(arguments, packages...)
		}
		return arguments, nil
	}
	return nil, ErrUpdateUnavailable
}

func validPackageName(value string) bool {
	if !validPackageField(value) {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && !strings.ContainsRune("+_.:@-", character) {
			return false
		}
	}
	return true
}

func runLongUpdateCommand(ctx context.Context, name string, arguments ...string) updateCommandResult {
	if err := ctx.Err(); err != nil {
		return updateCommandResult{Err: err, ExitCode: -1}
	}
	command := exec.Command(name, arguments...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return updateCommandResult{Err: err, ExitCode: -1}
	}
	if err := command.Start(); err != nil {
		return updateCommandResult{Err: err, ExitCode: -1}
	}
	processDone := make(chan struct{})
	go func(pid int) {
		select {
		case <-ctx.Done():
			if pid > 0 {
				_ = syscall.Kill(-pid, syscall.SIGTERM)
				timer := time.NewTimer(time.Second)
				select {
				case <-timer.C:
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				case <-processDone:
					_ = timer.Stop()
				}
			}
		case <-processDone:
		}
	}(command.Process.Pid)
	output, readErr := readBounded(stdout, 8<<20)
	if readErr != nil && ctx.Err() == nil && command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	waitErr := command.Wait()
	close(processDone)
	if ctx.Err() != nil {
		return updateCommandResult{Err: ctx.Err(), ExitCode: -1, Output: string(output)}
	}
	result := updateCommandResult{Output: string(output), ExitCode: 0, Err: readErr}
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

func boundedUpdateError(err error, output string) string {
	message := strings.TrimSpace(output)
	if message == "" && err != nil {
		message = err.Error()
	}
	if len(message) > 512 {
		message = message[:512]
	}
	if message == "" {
		return "package manager rejected the update request"
	}
	return message
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
