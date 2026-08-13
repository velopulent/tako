package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MaxOverrideEnvironmentEntries = 64
	MaxOverrideEnvironmentKey     = 128
	MaxOverrideEnvironmentValue   = 4096
	maxOverrideFileBytes          = 256 << 10
)

var (
	ErrInvalidOverrideOperation = errors.New("invalid service override operation")
	ErrOverrideConflict         = errors.New("service override conflict")
	ErrOverrideNotFound         = errors.New("service override not found")
	ErrOverrideUnmanaged        = errors.New("service override contains unsupported directives")
)

type OverrideDefinition struct {
	Environment     map[string]string `json:"environment,omitempty"`
	Restart         string            `json:"restart,omitempty"`
	RestartSec      string            `json:"restartSec,omitempty"`
	TimeoutStartSec string            `json:"timeoutStartSec,omitempty"`
	TimeoutStopSec  string            `json:"timeoutStopSec,omitempty"`
	Nice            *int              `json:"nice,omitempty"`
	CPUQuota        string            `json:"cpuQuota,omitempty"`
	MemoryMax       string            `json:"memoryMax,omitempty"`
	TasksMax        *uint64           `json:"tasksMax,omitempty"`
}

type OverrideOperation struct {
	Action              string            `json:"action"`
	Scope               string            `json:"scope"`
	Unit                string            `json:"unit"`
	Environment         map[string]string `json:"environment,omitempty"`
	Restart             string            `json:"restart,omitempty"`
	RestartSec          string            `json:"restartSec,omitempty"`
	TimeoutStartSec     string            `json:"timeoutStartSec,omitempty"`
	TimeoutStopSec      string            `json:"timeoutStopSec,omitempty"`
	Nice                *int              `json:"nice,omitempty"`
	CPUQuota            string            `json:"cpuQuota,omitempty"`
	MemoryMax           string            `json:"memoryMax,omitempty"`
	TasksMax            *uint64           `json:"tasksMax,omitempty"`
	ExpectedFingerprint string            `json:"expectedFingerprint,omitempty"`
}

func (operation OverrideOperation) Definition() OverrideDefinition {
	return OverrideDefinition{
		Environment:     operation.Environment,
		Restart:         operation.Restart,
		RestartSec:      operation.RestartSec,
		TimeoutStartSec: operation.TimeoutStartSec,
		TimeoutStopSec:  operation.TimeoutStopSec,
		Nice:            operation.Nice,
		CPUQuota:        operation.CPUQuota,
		MemoryMax:       operation.MemoryMax,
		TasksMax:        operation.TasksMax,
	}
}

type OverrideState struct {
	Scope       string              `json:"scope"`
	Unit        string              `json:"unit"`
	Path        string              `json:"path"`
	Exists      bool                `json:"exists"`
	Fingerprint string              `json:"fingerprint,omitempty"`
	Definition  *OverrideDefinition `json:"definition,omitempty"`
	Guidance    []string            `json:"guidance,omitempty"`
}

func ValidateOverrideOperation(operation OverrideOperation) error {
	if operation.Scope != "system" && operation.Scope != "user" {
		return ErrInvalidOverrideOperation
	}
	if err := ValidateServiceTarget(operation.Scope, operation.Unit); err != nil || !strings.HasSuffix(operation.Unit, ".service") || strings.ContainsAny(operation.Unit, "%\r\n\\") {
		return ErrInvalidOverrideOperation
	}
	if operation.Action != "preview" && operation.Action != "apply" && operation.Action != "delete" {
		return ErrInvalidOverrideOperation
	}
	if operation.ExpectedFingerprint != "" {
		if len(operation.ExpectedFingerprint) != sha256.Size*2 {
			return ErrInvalidOverrideOperation
		}
		if _, err := hex.DecodeString(operation.ExpectedFingerprint); err != nil {
			return ErrInvalidOverrideOperation
		}
	}
	if operation.Action == "preview" {
		if hasOverrideDefinition(operation.Definition()) {
			return validateOverrideDefinition(operation.Definition())
		}
		return nil
	}
	if operation.Action == "delete" {
		if hasOverrideDefinition(operation.Definition()) {
			return ErrInvalidOverrideOperation
		}
		return nil
	}
	return validateOverrideDefinition(operation.Definition())
}

func hasOverrideDefinition(definition OverrideDefinition) bool {
	return len(definition.Environment) > 0 || definition.Restart != "" || definition.RestartSec != "" || definition.TimeoutStartSec != "" || definition.TimeoutStopSec != "" || definition.Nice != nil || definition.CPUQuota != "" || definition.MemoryMax != "" || definition.TasksMax != nil
}

func validateOverrideDefinition(definition OverrideDefinition) error {
	if len(definition.Environment) > MaxOverrideEnvironmentEntries {
		return ErrInvalidOverrideOperation
	}
	for key, value := range definition.Environment {
		if !validEnvironmentKey(key) || len(key) > MaxOverrideEnvironmentKey || len(value) > MaxOverrideEnvironmentValue || strings.ContainsAny(value, "\x00\r\n%") {
			return ErrInvalidOverrideOperation
		}
	}
	if definition.Restart != "" {
		switch definition.Restart {
		case "no", "on-success", "on-failure", "on-abnormal", "on-watchdog", "on-abort", "always":
		default:
			return ErrInvalidOverrideOperation
		}
	}
	for _, value := range []string{definition.RestartSec, definition.TimeoutStartSec, definition.TimeoutStopSec} {
		if value != "" && !validSystemdDuration(value) {
			return ErrInvalidOverrideOperation
		}
	}
	if definition.Nice != nil && (*definition.Nice < -20 || *definition.Nice > 19) {
		return ErrInvalidOverrideOperation
	}
	if definition.CPUQuota != "" {
		quota, err := strconv.ParseFloat(strings.TrimSuffix(definition.CPUQuota, "%"), 64)
		if err != nil || !strings.HasSuffix(definition.CPUQuota, "%") || quota <= 0 || quota > 10000 {
			return ErrInvalidOverrideOperation
		}
	}
	if definition.MemoryMax != "" && !validMemoryLimit(definition.MemoryMax) {
		return ErrInvalidOverrideOperation
	}
	if definition.TasksMax != nil && (*definition.TasksMax == 0 || *definition.TasksMax > 1_000_000) {
		return ErrInvalidOverrideOperation
	}
	if len(definition.Environment) == 0 && definition.Restart == "" && definition.RestartSec == "" && definition.TimeoutStartSec == "" && definition.TimeoutStopSec == "" && definition.Nice == nil && definition.CPUQuota == "" && definition.MemoryMax == "" && definition.TasksMax == nil {
		return ErrInvalidOverrideOperation
	}
	return nil
}

func validEnvironmentKey(value string) bool {
	if value == "" || (value[0] != '_' && (value[0] < 'A' || value[0] > 'Z') && (value[0] < 'a' || value[0] > 'z')) {
		return false
	}
	for _, character := range value[1:] {
		if character != '_' && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validSystemdDuration(value string) bool {
	if value == "infinity" {
		return true
	}
	parsed, err := time.ParseDuration(value)
	return err == nil && parsed >= 0 && parsed <= 365*24*time.Hour
}

func validMemoryLimit(value string) bool {
	if value == "infinity" {
		return true
	}
	if value == "" {
		return false
	}
	index := 0
	for index < len(value) && value[index] >= '0' && value[index] <= '9' {
		index++
	}
	if index == 0 {
		return false
	}
	suffix := value[index:]
	return suffix == "" || suffix == "B" || suffix == "K" || suffix == "M" || suffix == "G" || suffix == "T" || suffix == "P" || suffix == "KiB" || suffix == "MiB" || suffix == "GiB" || suffix == "TiB" || suffix == "PiB"
}

func ReadServiceOverride(root string, operation OverrideOperation) (OverrideState, error) {
	if err := ValidateOverrideOperation(operation); err != nil {
		return OverrideState{}, err
	}
	path, err := overridePath(root, operation.Unit)
	if err != nil {
		return OverrideState{}, err
	}
	content, readErr := readOverrideFile(path)
	state := OverrideState{Scope: operation.Scope, Unit: operation.Unit, Path: path, Guidance: overrideGuidance(operation.Unit)}
	if errors.Is(readErr, os.ErrNotExist) {
		return state, nil
	}
	if readErr != nil {
		return OverrideState{}, readErr
	}
	definition, parseErr := parseOverrideDefinition(content)
	if parseErr != nil {
		return OverrideState{}, parseErr
	}
	return OverrideState{Scope: operation.Scope, Unit: operation.Unit, Path: path, Exists: true, Fingerprint: overrideFingerprint(content), Definition: &definition, Guidance: overrideGuidance(operation.Unit)}, nil
}

func ApplyServiceOverride(root string, operation OverrideOperation) (OverrideState, error) {
	if err := ValidateOverrideOperation(operation); err != nil {
		return OverrideState{}, err
	}
	current, err := ReadServiceOverride(root, operation)
	if err != nil {
		return OverrideState{}, err
	}
	if operation.Action == "preview" {
		return current, nil
	}
	if current.Exists {
		if operation.ExpectedFingerprint == "" || !strings.EqualFold(current.Fingerprint, operation.ExpectedFingerprint) {
			return OverrideState{}, ErrOverrideConflict
		}
	} else if operation.ExpectedFingerprint != "" {
		return OverrideState{}, ErrOverrideConflict
	}
	if operation.Action == "delete" {
		if !current.Exists {
			return OverrideState{}, ErrOverrideNotFound
		}
		path, pathErr := overridePath(root, operation.Unit)
		if pathErr != nil {
			return OverrideState{}, pathErr
		}
		if removeErr := os.Remove(path); removeErr != nil {
			return OverrideState{}, removeErr
		}
		_ = os.Remove(filepath.Dir(path))
		return ReadServiceOverride(root, operation)
	}
	path, err := overridePath(root, operation.Unit)
	if err != nil {
		return OverrideState{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return OverrideState{}, err
	}
	if err := writeAtomicOverride(path, renderOverrideDefinition(operation.Definition())); err != nil {
		return OverrideState{}, err
	}
	return ReadServiceOverride(root, operation)
}

func overridePath(root, unit string) (string, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || !strings.HasSuffix(unit, ".service") {
		return "", ErrInvalidOverrideOperation
	}
	path := filepath.Join(root, unit+".d", "50-tako.conf")
	if !pathWithin(root, path) {
		return "", ErrInvalidOverrideOperation
	}
	if err := rejectOverrideSymlinks(root, filepath.Dir(path)); err != nil {
		return "", err
	}
	return path, nil
}

func rejectOverrideSymlinks(root, directory string) error {
	if info, statErr := os.Lstat(root); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidOverrideOperation
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return ErrInvalidOverrideOperation
	}
	current := root
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidOverrideOperation
		}
	}
	return nil
}

func readOverrideFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxOverrideFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxOverrideFileBytes {
		return nil, ErrOverrideUnmanaged
	}
	return content, nil
}

func overrideFingerprint(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func writeAtomicOverride(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".tako-override-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func renderOverrideDefinition(definition OverrideDefinition) []byte {
	var builder strings.Builder
	builder.WriteString("[Service]\n")
	keys := make([]string, 0, len(definition.Environment))
	for key := range definition.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString("Environment=\"" + escapeSystemd(definition.Environment[key], key) + "\"\n")
	}
	if definition.Restart != "" {
		builder.WriteString("Restart=" + definition.Restart + "\n")
	}
	if definition.RestartSec != "" {
		builder.WriteString("RestartSec=" + definition.RestartSec + "\n")
	}
	if definition.TimeoutStartSec != "" {
		builder.WriteString("TimeoutStartSec=" + definition.TimeoutStartSec + "\n")
	}
	if definition.TimeoutStopSec != "" {
		builder.WriteString("TimeoutStopSec=" + definition.TimeoutStopSec + "\n")
	}
	if definition.Nice != nil {
		builder.WriteString("Nice=" + strconv.Itoa(*definition.Nice) + "\n")
	}
	if definition.CPUQuota != "" {
		builder.WriteString("CPUQuota=" + definition.CPUQuota + "\n")
	}
	if definition.MemoryMax != "" {
		builder.WriteString("MemoryMax=" + definition.MemoryMax + "\n")
	}
	if definition.TasksMax != nil {
		builder.WriteString("TasksMax=" + strconv.FormatUint(*definition.TasksMax, 10) + "\n")
	}
	return []byte(builder.String())
}

func escapeSystemd(value, key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key+"="+value, "\\", "\\\\"), "\"", "\\\"")
}

func parseOverrideDefinition(content []byte) (OverrideDefinition, error) {
	definition := OverrideDefinition{Environment: make(map[string]string)}
	seen := make(map[string]bool)
	for _, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || line == "[Service]" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || (seen[key] && key != "Environment") {
			return OverrideDefinition{}, ErrOverrideUnmanaged
		}
		seen[key] = true
		switch key {
		case "Environment":
			if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
				return OverrideDefinition{}, ErrOverrideUnmanaged
			}
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return OverrideDefinition{}, ErrOverrideUnmanaged
			}
			name, environmentValue, ok := strings.Cut(unquoted, "=")
			if !ok || !validEnvironmentKey(name) {
				return OverrideDefinition{}, ErrOverrideUnmanaged
			}
			definition.Environment[name] = environmentValue
		case "Restart":
			definition.Restart = value
		case "RestartSec":
			definition.RestartSec = value
		case "TimeoutStartSec":
			definition.TimeoutStartSec = value
		case "TimeoutStopSec":
			definition.TimeoutStopSec = value
		case "Nice":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return OverrideDefinition{}, ErrOverrideUnmanaged
			}
			definition.Nice = &parsed
		case "CPUQuota":
			definition.CPUQuota = value
		case "MemoryMax":
			definition.MemoryMax = value
		case "TasksMax":
			parsed, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return OverrideDefinition{}, ErrOverrideUnmanaged
			}
			definition.TasksMax = &parsed
		default:
			return OverrideDefinition{}, ErrOverrideUnmanaged
		}
	}
	if err := validateOverrideDefinition(definition); err != nil {
		return OverrideDefinition{}, ErrOverrideUnmanaged
	}
	return definition, nil
}

func overrideGuidance(unit string) []string {
	return []string{
		"If the unit fails, inspect its journal and revert this drop-in before retrying.",
		fmt.Sprintf("The vendor unit remains unchanged; Tako writes only 50-tako.conf for %s.", unit),
	}
}
