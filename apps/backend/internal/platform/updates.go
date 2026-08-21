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
	Name             string   `json:"name"`
	Architecture     string   `json:"architecture,omitempty"`
	CurrentVersion   string   `json:"currentVersion,omitempty"`
	CandidateVersion string   `json:"candidateVersion"`
	Severity         string   `json:"severity,omitempty"`
	Size             uint64   `json:"size,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	Details          string   `json:"details,omitempty"`
	AdvisoryID       string   `json:"advisoryId,omitempty"`
	CVEUrls          []string `json:"cveUrls,omitempty"`
	BugUrls          []string `json:"bugUrls,omitempty"`
	VendorUrls       []string `json:"vendorUrls,omitempty"`
	Description      string   `json:"description,omitempty"`
	GroupKey         string   `json:"groupKey,omitempty"`
	Dependencies     []string `json:"dependencies,omitempty"`
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
	Recovery     UpdateRecovery  `json:"recovery"`
	LastChecked  string          `json:"lastChecked,omitempty"`
}

// UpdateRecovery describes post-update work without pretending that every
// package backend can authoritatively determine restart requirements.
type UpdateRecovery struct {
	Authoritative   bool     `json:"authoritative"`
	RebootRequired  bool     `json:"rebootRequired"`
	RestartServices []string `json:"restartServices"`
	Hints           []string `json:"hints"`
	Source          string   `json:"source"`
	Reason          string   `json:"reason,omitempty"`
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
	status.Recovery = UpdateRecoveryForBackend(deadline, status.Backend)
	status.LastChecked = time.Now().UTC().Format(time.RFC3339)
	return status
}

func UpdateRecoveryForBackend(ctx context.Context, backend string) UpdateRecovery {
	recovery := UpdateRecovery{RestartServices: []string{}, Hints: []string{}, Source: "advisory"}
	if backend == "none" || backend == "" {
		recovery.Reason = "No update backend is available to assess recovery needs."
		return recovery
	}
	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		recovery.RebootRequired = true
		recovery.Authoritative = true
		recovery.Source = "/var/run/reboot-required"
		recovery.Hints = append(recovery.Hints, "Reboot the host after the update job completes.")
	}
	if backend == "dnf" {
		if result := runUpdateCommand(ctx, "needs-restarting", "-r"); result.Err == nil && result.ExitCode == 1 {
			recovery.RebootRequired = true
			recovery.Authoritative = true
			recovery.Source = "needs-restarting"
			recovery.Hints = append(recovery.Hints, "A reboot is required according to needs-restarting.")
		}
	}
	if !recovery.RebootRequired {
		recovery.Hints = append(recovery.Hints, "Restart services affected by updated libraries before relying on the new versions.")
	}
	return recovery
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

func tryEnrichWithDnfInfo(ctx context.Context, deps updateDependencies, pkgs []UpdatePackage) []UpdatePackage {
	if deps.commandExists == nil || deps.commandOutput == nil || !deps.commandExists("dnf") {
		return pkgs
	}
	result := deps.commandOutput(ctx, "dnf", "updateinfo", "info")
	if result.Err != nil || result.ExitCode != 0 || strings.TrimSpace(result.Output) == "" {
		return pkgs
	}
	return applyDnfUpdateInfo(pkgs, result.Output)
}

func applyDnfUpdateInfo(packages []UpdatePackage, infoOutput string) []UpdatePackage {
	type advisory struct {
		id          string
		typ         string
		severity    string
		description string
		bugUrls     []string
		vendorUrls  []string
		pkgNames    map[string]bool
	}
	advisories := []*advisory{}
	var current *advisory
	var currentField string
	var descBuilder strings.Builder
	var inPackages bool

	scanner := bufio.NewScanner(strings.NewReader(infoOutput))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if inPackages {
				inPackages = false
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "Name") && strings.Contains(line, ":") {
			if current != nil {
				if descBuilder.Len() > 0 {
					current.description = strings.TrimSpace(descBuilder.String())
					descBuilder.Reset()
				}
				advisories = append(advisories, current)
			}
			current = &advisory{pkgNames: make(map[string]bool)}
			currentField = ""
			inPackages = false
		}
		if current == nil {
			continue
		}
		trimmedLine := line
		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			continue
		}
		rawField := strings.TrimSpace(line[:colonIdx])
		value := strings.TrimSpace(line[colonIdx+1:])
		fieldIsEmpty := rawField == ""
		var field string
		if fieldIsEmpty {
			field = currentField
		} else {
			field = rawField
			currentField = field
			if field == "Packages" {
				inPackages = true
			} else if inPackages && field != "" && field != "Packages" {
				inPackages = false
			}
		}
		if inPackages && (field == "Packages" || fieldIsEmpty) {
			if value != "" && value != ":" {
				token := strings.Fields(value)[0]
				if token != "" && token != ":" {
					name, _, _ := parsePkconPackageToken(token)
					if name == "" {
						name = token
					}
					current.pkgNames[name] = true
					base := strings.Split(name, ".")[0]
					current.pkgNames[base] = true
				}
			}
			continue
		}
		switch field {
		case "Name":
			if current.id == "" {
				current.id = value
			}
		case "Type":
			if current.typ == "" && !inPackages && descBuilder.Len() == 0 {
				current.typ = strings.ToLower(value)
			}
			if strings.EqualFold(value, "bugzilla") {
				// will capture Url next
			}
		case "Severity":
			if current.severity == "" {
				current.severity = strings.ToLower(value)
			}
		case "Description":
			if descBuilder.Len() == 0 {
				descBuilder.WriteString(value)
			} else {
				descBuilder.WriteString("\n" + value)
			}
		case "":
			if currentField == "Description" {
				if descBuilder.Len() > 0 {
					descBuilder.WriteString("\n" + value)
				} else {
					descBuilder.WriteString(value)
				}
			}
		case "Url":
			if strings.Contains(value, "bugzilla") {
				current.bugUrls = append(current.bugUrls, value)
			} else if strings.Contains(value, "cve") || strings.Contains(value, "access.redhat") {
				current.vendorUrls = append(current.vendorUrls, value)
			} else if value != "" {
				current.vendorUrls = append(current.vendorUrls, value)
			}
		}
		if field == "Description" && !fieldIsEmpty {
			// already handled
		}
		_ = trimmedLine
	}
	if current != nil {
		if descBuilder.Len() > 0 {
			current.description = strings.TrimSpace(descBuilder.String())
		}
		advisories = append(advisories, current)
	}

	pkgMap := make(map[string]*advisory)
	for _, adv := range advisories {
		for name := range adv.pkgNames {
			pkgMap[name] = adv
		}
	}

	result := make([]UpdatePackage, len(packages))
	copy(result, packages)
	for idx := range result {
		pkg := &result[idx]
		adv, ok := pkgMap[pkg.Name]
		if !ok {
			continue
		}
		if adv.id != "" {
			pkg.AdvisoryID = adv.id
		}
		if adv.typ != "" {
			switch adv.typ {
			case "security":
				pkg.Severity = "security"
			case "bugfix":
				pkg.Severity = "bugfix"
			case "enhancement":
				pkg.Severity = "enhancement"
			}
		}
		if adv.description != "" {
			pkg.Description = adv.description
			pkg.Details = adv.description
			if pkg.Summary == "" || pkg.Summary == "(updates)" {
				firstLine := strings.Split(strings.TrimSpace(adv.description), "\n")[0]
				if len(firstLine) > 120 {
					firstLine = firstLine[:120]
				}
				pkg.Summary = firstLine
			}
			if cves := cvePattern.FindAllString(adv.description, -1); len(cves) > 0 {
				seen := make(map[string]struct{})
				for _, cve := range cves {
					if _, exists := seen[cve]; exists {
						continue
					}
					seen[cve] = struct{}{}
					url := "https://cve.mitre.org/cgi-bin/cvename.cgi?name=" + cve
					already := false
					for _, existing := range pkg.CVEUrls {
						if existing == url {
							already = true
							break
						}
					}
					if !already {
						pkg.CVEUrls = append(pkg.CVEUrls, url)
					}
					if pkg.AdvisoryID == "" {
						pkg.AdvisoryID = cve
					}
				}
			}
		}
		if len(adv.bugUrls) > 0 {
			pkg.BugUrls = append(pkg.BugUrls, adv.bugUrls...)
		}
		if len(adv.vendorUrls) > 0 {
			pkg.VendorUrls = append(pkg.VendorUrls, adv.vendorUrls...)
		}
		if pkg.GroupKey == "" && adv.id != "" {
			pkg.GroupKey = adv.id + "@" + pkg.CandidateVersion
		}
	}

	return result
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
				status.Packages = tryEnrichWithDnfInfo(ctx, dependencies, status.Packages)
				status.Packages = sortUpdates(status.Packages)
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
				status.Packages = tryEnrichWithDnfInfo(ctx, dependencies, status.Packages)
				status.Packages = sortUpdates(status.Packages)
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
	Recovery    UpdateRecovery  `json:"recovery"`
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
	// Confirmation is optional; when provided it must be the dialog-confirmed value.
	// Legacy typed "APPLY UPDATES" still accepted for backward compatibility.
	if !operation.Preview && operation.Confirmation != "" && operation.Confirmation != "APPLY UPDATES" && operation.Confirmation != "CONFIRM" {
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
		return UpdateResult{Backend: current.Backend, Scope: operation.Scope, Packages: []string{}, Updated: []UpdatePackage{}, Verified: true, Message: "No updates were available.", Fingerprint: fingerprint, Recovery: UpdateRecoveryForBackend(ctx, current.Backend)}, nil
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
	return UpdateResult{Backend: current.Backend, Scope: operation.Scope, Packages: packages, Updated: updated, Verified: true, Message: "Updates applied and verified.", Fingerprint: UpdateFingerprint(final), Recovery: UpdateRecoveryForBackend(ctx, current.Backend)}, nil
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

func mapPkconSeverity(tokens string) string {
	lower := strings.ToLower(strings.TrimSpace(tokens))
	switch {
	case strings.Contains(lower, "security"):
		return "security"
	case strings.Contains(lower, "bug"):
		return "bugfix"
	case strings.Contains(lower, "enhancement"):
		return "enhancement"
	case strings.Contains(lower, "available"):
		return "enhancement"
	default:
		return ""
	}
}

func parsePkconPackageToken(token string) (name, version, arch string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", ""
	}
	dotIdx := strings.LastIndex(token, ".")
	if dotIdx != -1 {
		candidateArch := token[dotIdx+1:]
		if validArchitecture(candidateArch) {
			arch = candidateArch
			token = token[:dotIdx]
		}
	}
	splitIdx := -1
	for i := 0; i < len(token)-1; i++ {
		if token[i] == '-' && token[i+1] >= '0' && token[i+1] <= '9' {
			splitIdx = i
			break
		}
	}
	if splitIdx != -1 {
		name = token[:splitIdx]
		version = token[splitIdx+1:]
	} else if idx := strings.LastIndex(token, "-"); idx != -1 {
		name = token[:idx]
		version = token[idx+1:]
	} else {
		name = token
	}
	return name, version, arch
}

func parsePackageKitUpdates(output string) []UpdatePackage {
	updates := make([]UpdatePackage, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() && len(updates) < MaxUpdatePackages {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lowerLine := strings.ToLower(line)
		if strings.HasPrefix(lowerLine, "transaction:") || strings.HasPrefix(lowerLine, "status:") || strings.HasPrefix(lowerLine, "results:") || strings.HasPrefix(lowerLine, "loading") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.EqualFold(fields[0], "package") {
			continue
		}
		if strings.Contains(line, ";") && strings.EqualFold(fields[0], "available") {
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
				sev := mapPkconSeverity(fields[0])
				if sev != "" {
					item.Severity = sev
				}
				updates = append(updates, item)
				continue
			}
		}
		if strings.Contains(fields[0], ".") {
			if dotIdx := strings.LastIndex(fields[0], "."); dotIdx != -1 && dotIdx+1 < len(fields[0]) && validArchitecture(fields[0][dotIdx+1:]) {
				if len(fields) >= 2 && !strings.HasPrefix(fields[1], "(") && strings.ContainsAny(fields[1], "0123456789") {
					namePart := fields[0][:dotIdx]
					archPart := fields[0][dotIdx+1:]
					if validPackageField(namePart) && validArchitecture(archPart) && validPackageField(fields[1]) {
						item := UpdatePackage{Name: namePart, Architecture: archPart, CandidateVersion: fields[1], Summary: strings.Join(fields[2:], " ")}
						if strings.Contains(strings.ToLower(item.Summary), "security") {
							item.Severity = "security"
						}
						updates = append(updates, item)
						continue
					}
				}
			}
		}
		packageIdx := -1
		for i, f := range fields {
			if strings.HasPrefix(f, "(") {
				continue
			}
			if strings.Contains(f, ".") && strings.Contains(f, "-") {
				dotIdx := strings.LastIndex(f, ".")
				if dotIdx != -1 && dotIdx+1 < len(f) {
					candidateArch := f[dotIdx+1:]
					if validArchitecture(candidateArch) {
						packageIdx = i
						break
					}
				}
			}
		}
		if packageIdx == -1 {
			if len(fields) >= 4 && strings.EqualFold(fields[0], "available") && validPackageField(fields[1]) && validPackageField(fields[2]) {
				updates = append(updates, UpdatePackage{Name: fields[1], CandidateVersion: fields[2], Architecture: fields[3], Summary: strings.Join(fields[4:], " "), Severity: mapPkconSeverity(fields[0])})
			}
			continue
		}
		severityRaw := strings.Join(fields[:packageIdx], " ")
		severity := mapPkconSeverity(severityRaw)
		packageToken := fields[packageIdx]
		repo := ""
		if packageIdx+1 < len(fields) {
			tail := strings.Join(fields[packageIdx+1:], " ")
			repo = strings.TrimSpace(strings.Trim(tail, "()"))
		}
		name, version, arch := parsePkconPackageToken(packageToken)
		if !validPackageField(name) || !validPackageField(version) {
			continue
		}
		item := UpdatePackage{
			Name:             name,
			Architecture:     arch,
			CandidateVersion: version,
			Severity:         severity,
			Summary:          repo,
			Details:          "",
		}
		if item.Severity == "" {
			item.Severity = "enhancement"
		}
		updates = append(updates, item)
	}
	return sortUpdates(updates)
}

var (
	cvePattern = regexp.MustCompile(`CVE-\d{4}-\d{4,7}`)
	bugIDPattern = regexp.MustCompile(`(?i)(?:bug|bz|rhbz)[^0-9]*([0-9]{5,8})`)
)

func enrichUpdatePackages(updates []UpdatePackage) []UpdatePackage {
	for index := range updates {
		pkg := &updates[index]
		sourceText := strings.Join([]string{pkg.Summary, pkg.Details, pkg.Description}, " ")
		lower := strings.ToLower(sourceText)

		if pkg.Severity == "" {
			if strings.Contains(lower, "security") || cvePattern.MatchString(sourceText) {
				pkg.Severity = "security"
			} else if strings.Contains(lower, "bug") && strings.Contains(lower, "fix") || strings.Contains(lower, "bugfix") {
				pkg.Severity = "bugfix"
			} else if strings.Contains(lower, "enhancement") {
				pkg.Severity = "enhancement"
			}
		} else {
			normalized := strings.ToLower(strings.TrimSpace(pkg.Severity))
			switch normalized {
			case "critical", "important", "security", "sec", "cve":
				pkg.Severity = "security"
			case "bug", "bugfix", "bug-fix", "important-bug":
				pkg.Severity = "bugfix"
			case "enhancement", "feature", "recommended":
				pkg.Severity = "enhancement"
			default:
				pkg.Severity = normalized
			}
		}

		if pkg.Description == "" {
			if pkg.Details != "" {
				pkg.Description = pkg.Details
			} else if pkg.Summary != "" {
				pkg.Description = pkg.Summary
			}
		}

		foundCVEs := cvePattern.FindAllString(sourceText, -1)
		if len(foundCVEs) > 0 {
			seen := make(map[string]struct{}, len(foundCVEs))
			for _, cve := range foundCVEs {
				if _, exists := seen[cve]; exists {
					continue
				}
				seen[cve] = struct{}{}
				url := "https://cve.mitre.org/cgi-bin/cvename.cgi?name=" + cve
				already := false
				for _, existing := range pkg.CVEUrls {
					if existing == url {
						already = true
						break
					}
				}
				if !already {
					pkg.CVEUrls = append(pkg.CVEUrls, url)
				}
				if pkg.AdvisoryID == "" {
					pkg.AdvisoryID = cve
				}
			}
		}

		if strings.Contains(lower, "rhsa") || strings.Contains(lower, "errata") {
			if pkg.AdvisoryID == "" {
				re := regexp.MustCompile(`(?i)(RHSA-\d{4}:\d+)`)
				if match := re.FindString(sourceText); match != "" {
					pkg.AdvisoryID = strings.ToUpper(match)
					pkg.VendorUrls = append(pkg.VendorUrls, "https://access.redhat.com/errata/"+pkg.AdvisoryID)
				}
			}
		}

		matches := bugIDPattern.FindAllStringSubmatch(sourceText, 5)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			id := match[1]
			url := "https://bugzilla.redhat.com/show_bug.cgi?id=" + id
			already := false
			for _, existing := range pkg.BugUrls {
				if existing == url {
					already = true
					break
				}
			}
			if !already {
				pkg.BugUrls = append(pkg.BugUrls, url)
			}
		}

		if pkg.AdvisoryID != "" {
			pkg.GroupKey = pkg.AdvisoryID + "@" + pkg.CandidateVersion
		} else if pkg.Summary != "" {
			summaryKey := pkg.Summary
			if len(summaryKey) > 64 {
				summaryKey = summaryKey[:64]
			}
			pkg.GroupKey = pkg.CandidateVersion + "@" + summaryKey
		} else {
			pkg.GroupKey = pkg.CandidateVersion
		}
	}

	groups := make(map[string][]int)
	for idx, pkg := range updates {
		if pkg.GroupKey == "" {
			continue
		}
		groups[pkg.GroupKey] = append(groups[pkg.GroupKey], idx)
	}
	for _, indices := range groups {
		if len(indices) < 2 {
			continue
		}
		names := make([]string, 0, len(indices))
		for _, idx := range indices {
			names = append(names, updates[idx].Name)
		}
		for _, idx := range indices {
			peers := make([]string, 0, len(names)-1)
			for _, name := range names {
				if name != updates[idx].Name {
					peers = append(peers, name)
				}
			}
			updates[idx].Dependencies = peers
		}
	}
	return updates
}

func sortUpdates(updates []UpdatePackage) []UpdatePackage {
	sort.Slice(updates, func(left, right int) bool {
		if updates[left].Name == updates[right].Name {
			return updates[left].Architecture < updates[right].Architecture
		}
		return updates[left].Name < updates[right].Name
	})
	return enrichUpdatePackages(updates)
}

func validPackageField(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n")
}

func validArchitecture(value string) bool {
	return validPackageField(value) && len(value) <= 32 && !strings.ContainsAny(value, "/[]")
}
