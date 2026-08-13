package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	MaxTimerNameLength        = 64
	MaxTimerDescriptionLength = 256
	MaxTimerCommandLength     = 512
	MaxTimerScheduleLength    = 256
	maxTimerFileBytes         = 1 << 20
)

var (
	ErrInvalidTimerOperation = errors.New("invalid timer operation")
	ErrTimerConflict         = errors.New("timer conflict")
	ErrTimerNotFound         = errors.New("timer not found")
	ErrTimerPairIncomplete   = errors.New("timer pair is incomplete")
)

type TimerDefinition struct {
	Description     string `json:"description,omitempty"`
	OnCalendar      string `json:"onCalendar,omitempty"`
	OnBootSec       string `json:"onBootSec,omitempty"`
	OnUnitActiveSec string `json:"onUnitActiveSec,omitempty"`
	Command         string `json:"command,omitempty"`
	Persistent      bool   `json:"persistent,omitempty"`
}

type TimerOperation struct {
	Action              string `json:"action"`
	Scope               string `json:"scope"`
	Name                string `json:"name"`
	Description         string `json:"description,omitempty"`
	OnCalendar          string `json:"onCalendar,omitempty"`
	OnBootSec           string `json:"onBootSec,omitempty"`
	OnUnitActiveSec     string `json:"onUnitActiveSec,omitempty"`
	Command             string `json:"command,omitempty"`
	Persistent          bool   `json:"persistent,omitempty"`
	ExpectedFingerprint string `json:"expectedFingerprint,omitempty"`
}

func (operation *TimerOperation) UnmarshalJSON(payload []byte) error {
	type plain TimerOperation
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var value plain
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing timer operation data")
	}
	*operation = TimerOperation(value)
	return nil
}

func (operation TimerOperation) Definition() TimerDefinition {
	return TimerDefinition{
		Description:     operation.Description,
		OnCalendar:      operation.OnCalendar,
		OnBootSec:       operation.OnBootSec,
		OnUnitActiveSec: operation.OnUnitActiveSec,
		Command:         operation.Command,
		Persistent:      operation.Persistent,
	}
}

type TimerState struct {
	Scope       string           `json:"scope"`
	Name        string           `json:"name"`
	TimerUnit   string           `json:"timerUnit"`
	ServiceUnit string           `json:"serviceUnit"`
	Exists      bool             `json:"exists"`
	Enabled     bool             `json:"enabled"`
	Fingerprint string           `json:"fingerprint,omitempty"`
	Definition  *TimerDefinition `json:"definition,omitempty"`
}

func ValidateTimerOperation(operation TimerOperation) error {
	if operation.Scope != "system" && operation.Scope != "user" {
		return ErrInvalidTimerOperation
	}
	if !validTimerName(operation.Name) {
		return ErrInvalidTimerOperation
	}
	if operation.Action != "preview" && operation.Action != "create" && operation.Action != "update" && operation.Action != "delete" && operation.Action != "enable" && operation.Action != "disable" {
		return ErrInvalidTimerOperation
	}
	if operation.ExpectedFingerprint != "" {
		if len(operation.ExpectedFingerprint) != sha256.Size*2 {
			return ErrInvalidTimerOperation
		}
		if _, err := hex.DecodeString(operation.ExpectedFingerprint); err != nil {
			return ErrInvalidTimerOperation
		}
	}
	if operation.Action == "preview" {
		return nil
	}
	if operation.Action == "delete" || operation.Action == "enable" || operation.Action == "disable" {
		if operation.ExpectedFingerprint == "" {
			return ErrInvalidTimerOperation
		}
		return nil
	}
	if operation.Action == "update" && operation.ExpectedFingerprint == "" {
		return ErrInvalidTimerOperation
	}
	if operation.Action == "create" && operation.ExpectedFingerprint != "" {
		return ErrInvalidTimerOperation
	}
	return validateTimerDefinition(operation.Definition())
}

func validateTimerDefinition(definition TimerDefinition) error {
	if len(definition.Description) > MaxTimerDescriptionLength || strings.ContainsAny(definition.Description, "\x00\r\n%") {
		return ErrInvalidTimerOperation
	}
	if len(definition.Command) == 0 || len(definition.Command) > MaxTimerCommandLength || strings.ContainsAny(definition.Command, "\x00\r\n") {
		return ErrInvalidTimerOperation
	}
	command := strings.Fields(definition.Command)
	if len(command) == 0 || !filepath.IsAbs(command[0]) {
		return ErrInvalidTimerOperation
	}
	for _, value := range command {
		if strings.ContainsAny(value, ";&|<>$`(){}'\"%") {
			return ErrInvalidTimerOperation
		}
	}
	schedules := 0
	if definition.OnCalendar != "" {
		schedules++
		if len(definition.OnCalendar) > MaxTimerScheduleLength || strings.ContainsAny(definition.OnCalendar, "\x00\r\n%") {
			return ErrInvalidTimerOperation
		}
	}
	if definition.OnBootSec != "" {
		schedules++
		if _, err := time.ParseDuration(definition.OnBootSec); err != nil || len(definition.OnBootSec) > MaxTimerScheduleLength {
			return ErrInvalidTimerOperation
		}
	}
	if definition.OnUnitActiveSec != "" {
		schedules++
		if _, err := time.ParseDuration(definition.OnUnitActiveSec); err != nil || len(definition.OnUnitActiveSec) > MaxTimerScheduleLength {
			return ErrInvalidTimerOperation
		}
	}
	if schedules != 1 {
		return ErrInvalidTimerOperation
	}
	return nil
}

func validTimerName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > MaxTimerNameLength || strings.HasSuffix(name, ".timer") || strings.HasSuffix(name, ".service") || strings.ContainsAny(name, "/\\\x00\r\n%") {
		return false
	}
	for _, character := range name {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && !strings.ContainsRune("@._-", character) {
			return false
		}
	}
	return true
}

func TimerFingerprint(timer, service []byte) string {
	hash := sha256.New()
	_, _ = hash.Write(timer)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(service)
	return hex.EncodeToString(hash.Sum(nil))
}

func ReadTimerState(root string, operation TimerOperation) (TimerState, error) {
	if err := ValidateTimerOperation(operation); err != nil {
		return TimerState{}, err
	}
	timerPath, servicePath, err := timerPaths(root, operation.Name)
	if err != nil {
		return TimerState{}, err
	}
	timer, timerErr := readTimerFile(timerPath)
	service, serviceErr := readTimerFile(servicePath)
	if errors.Is(timerErr, os.ErrNotExist) && errors.Is(serviceErr, os.ErrNotExist) {
		return TimerState{Scope: operation.Scope, Name: operation.Name, TimerUnit: operation.Name + ".timer", ServiceUnit: operation.Name + ".service"}, nil
	}
	if timerErr != nil || serviceErr != nil {
		return TimerState{}, ErrTimerPairIncomplete
	}
	definition := parseTimerDefinition(timer, service)
	return TimerState{
		Scope:       operation.Scope,
		Name:        operation.Name,
		TimerUnit:   operation.Name + ".timer",
		ServiceUnit: operation.Name + ".service",
		Exists:      true,
		Enabled:     timerEnabled(root, operation.Name),
		Fingerprint: TimerFingerprint(timer, service),
		Definition:  definition,
	}, nil
}

func readTimerFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxTimerFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxTimerFileBytes {
		return nil, ErrTimerPairIncomplete
	}
	return content, nil
}

func ApplyTimerFiles(root string, operation TimerOperation) (TimerState, error) {
	if err := ValidateTimerOperation(operation); err != nil {
		return TimerState{}, err
	}
	current, err := ReadTimerState(root, operation)
	if err != nil {
		return TimerState{}, err
	}
	if operation.Action == "preview" {
		return current, nil
	}
	if operation.Action == "create" {
		if current.Exists {
			return TimerState{}, ErrTimerConflict
		}
	} else if !current.Exists || current.Fingerprint != operation.ExpectedFingerprint {
		return TimerState{}, ErrTimerConflict
	}
	timerPath, servicePath, err := timerPaths(root, operation.Name)
	if err != nil {
		return TimerState{}, err
	}
	switch operation.Action {
	case "create", "update":
		if err := writeAtomic(timerPath, renderTimerUnit(operation)); err != nil {
			return TimerState{}, err
		}
		if err := writeAtomic(servicePath, renderServiceUnit(operation)); err != nil {
			return TimerState{}, err
		}
	case "delete":
		if err := os.Remove(timerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return TimerState{}, err
		}
		if err := os.Remove(servicePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return TimerState{}, err
		}
		if err := os.Remove(filepath.Join(root, "timers.target.wants", operation.Name+".timer")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return TimerState{}, err
		}
	case "enable", "disable":
		link := filepath.Join(root, "timers.target.wants", operation.Name+".timer")
		if operation.Action == "enable" {
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				return TimerState{}, err
			}
			_ = os.Remove(link)
			if err := os.Symlink("../"+operation.Name+".timer", link); err != nil {
				return TimerState{}, err
			}
		} else if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
			return TimerState{}, err
		}
	}
	return ReadTimerState(root, operation)
}

func timerPaths(root, name string) (string, string, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || !validTimerName(name) {
		return "", "", ErrInvalidTimerOperation
	}
	timerPath := filepath.Join(root, name+".timer")
	servicePath := filepath.Join(root, name+".service")
	if !pathWithin(root, timerPath) || !pathWithin(root, servicePath) {
		return "", "", ErrInvalidTimerOperation
	}
	return timerPath, servicePath, nil
}

func writeAtomic(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".tako-timer-*")
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

func renderTimerUnit(operation TimerOperation) []byte {
	definition := operation.Definition()
	var builder strings.Builder
	fmt.Fprintf(&builder, "[Unit]\nDescription=%s\n\n[Timer]\nUnit=%s.service\n", timerDescription(operation), operation.Name)
	if definition.OnCalendar != "" {
		builder.WriteString("OnCalendar=" + definition.OnCalendar + "\n")
	}
	if definition.OnBootSec != "" {
		builder.WriteString("OnBootSec=" + definition.OnBootSec + "\n")
	}
	if definition.OnUnitActiveSec != "" {
		builder.WriteString("OnUnitActiveSec=" + definition.OnUnitActiveSec + "\n")
	}
	if definition.Persistent {
		builder.WriteString("Persistent=true\n")
	}
	builder.WriteString("\n[Install]\nWantedBy=timers.target\n")
	return []byte(builder.String())
}

func renderServiceUnit(operation TimerOperation) []byte {
	return []byte(fmt.Sprintf("[Unit]\nDescription=%s\n\n[Service]\nType=oneshot\nExecStart=%s\n", timerDescription(operation), operation.Command))
}

func timerDescription(operation TimerOperation) string {
	if operation.Description != "" {
		return operation.Description
	}
	return operation.Name
}

func timerEnabled(root, name string) bool {
	_, err := os.Lstat(filepath.Join(root, "timers.target.wants", name+".timer"))
	return err == nil
}

func parseTimerDefinition(timer, service []byte) *TimerDefinition {
	definition := &TimerDefinition{}
	for _, line := range strings.Split(string(timer), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "Description":
			definition.Description = value
		case "OnCalendar":
			definition.OnCalendar = value
		case "OnBootSec":
			definition.OnBootSec = value
		case "OnUnitActiveSec":
			definition.OnUnitActiveSec = value
		case "Persistent":
			definition.Persistent = value == "true"
		}
	}
	for _, line := range strings.Split(string(service), "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			definition.Command = strings.TrimPrefix(line, "ExecStart=")
			break
		}
	}
	return definition
}
