package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const maxSignalTargets = 1024

var (
	ErrInvalidSignalOperation = errors.New("invalid process signal operation")
	ErrSignalConflict         = errors.New("process signal targets changed")
	ErrSignalUnauthorized     = errors.New("process signal is not authorized")
)

type ProcessTarget struct {
	PID     int    `json:"pid"`
	Started uint64 `json:"started"`
}

type SignalTarget struct {
	PID     int    `json:"pid"`
	Started uint64 `json:"started"`
	UID     int    `json:"uid"`
	User    string `json:"user"`
	Program string `json:"program"`
}

type SignalOperation struct {
	Action          string          `json:"action"`
	Signal          string          `json:"signal"`
	Target          ProcessTarget   `json:"target"`
	Tree            bool            `json:"tree,omitempty"`
	ExpectedTargets []ProcessTarget `json:"expectedTargets,omitempty"`
}

type SignalPreview struct {
	Signal      string         `json:"signal"`
	Tree        bool           `json:"tree"`
	Targets     []SignalTarget `json:"targets"`
	Fingerprint string         `json:"fingerprint"`
}

type SignalFailure struct {
	PID   int    `json:"pid"`
	Error string `json:"error"`
}

type SignalResult struct {
	Signal   string          `json:"signal"`
	Tree     bool            `json:"tree"`
	Targets  []SignalTarget  `json:"targets"`
	Signaled []SignalTarget  `json:"signaled"`
	Failures []SignalFailure `json:"failures,omitempty"`
}

var allowedProcessSignals = map[string]struct{}{
	"HUP": {}, "INT": {}, "TERM": {}, "KILL": {}, "STOP": {}, "CONT": {}, "USR1": {}, "USR2": {},
}

func ValidateSignalOperation(operation SignalOperation) error {
	if operation.Action != "preview" && operation.Action != "apply" {
		return ErrInvalidSignalOperation
	}
	if _, ok := allowedProcessSignals[strings.ToUpper(operation.Signal)]; !ok {
		return ErrInvalidSignalOperation
	}
	if operation.Target.PID < 1 || operation.Target.PID > 1<<22 || operation.Target.Started == 0 {
		return ErrInvalidSignalOperation
	}
	if len(operation.ExpectedTargets) > maxSignalTargets || (operation.Action == "apply" && len(operation.ExpectedTargets) == 0) {
		return ErrInvalidSignalOperation
	}
	return nil
}

func PreviewSignal(ctx context.Context, operation SignalOperation) (SignalPreview, error) {
	if err := ValidateSignalOperation(operation); err != nil {
		return SignalPreview{}, err
	}
	items, err := Processes()
	if err != nil {
		return SignalPreview{}, err
	}
	byPID := make(map[int]Process, len(items))
	children := make(map[int][]Process)
	for _, item := range items {
		byPID[item.PID] = item
		children[item.PPID] = append(children[item.PPID], item)
	}
	root, ok := byPID[operation.Target.PID]
	if !ok {
		return SignalPreview{}, ErrProcessNotFound
	}
	if root.Started != operation.Target.Started {
		return SignalPreview{}, ErrProcessReused
	}
	targets := []SignalTarget{{PID: root.PID, Started: root.Started, UID: root.UID, User: root.User, Program: root.Program}}
	if operation.Tree {
		queue := []int{root.PID}
		seen := map[int]struct{}{root.PID: {}}
		for len(queue) > 0 {
			parent := queue[0]
			queue = queue[1:]
			for _, child := range children[parent] {
				if _, exists := seen[child.PID]; exists {
					continue
				}
				if len(targets) >= maxSignalTargets {
					return SignalPreview{}, ErrInvalidSignalOperation
				}
				seen[child.PID] = struct{}{}
				targets = append(targets, SignalTarget{PID: child.PID, Started: child.Started, UID: child.UID, User: child.User, Program: child.Program})
				queue = append(queue, child.PID)
			}
		}
	}
	sort.Slice(targets, func(left, right int) bool { return targets[left].PID < targets[right].PID })
	if len(operation.ExpectedTargets) > 0 && !sameProcessTargets(targets, operation.ExpectedTargets) {
		return SignalPreview{}, ErrSignalConflict
	}
	if err := ctx.Err(); err != nil {
		return SignalPreview{}, err
	}
	return SignalPreview{Signal: strings.ToUpper(operation.Signal), Tree: operation.Tree, Targets: targets, Fingerprint: signalFingerprint(targets)}, nil
}

func sameProcessTargets(targets []SignalTarget, expected []ProcessTarget) bool {
	if len(targets) != len(expected) {
		return false
	}
	for index, target := range targets {
		if target.PID != expected[index].PID || target.Started != expected[index].Started {
			return false
		}
	}
	return true
}

func signalFingerprint(targets []SignalTarget) string {
	hash := sha256.New()
	for _, target := range targets {
		_, _ = fmt.Fprintf(hash, "%d:%d;", target.PID, target.Started)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func SignalTargets(targets []SignalTarget) []ProcessTarget {
	result := make([]ProcessTarget, 0, len(targets))
	for _, target := range targets {
		result = append(result, ProcessTarget{PID: target.PID, Started: target.Started})
	}
	return result
}

func SignalName(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func ProcessTargetString(target ProcessTarget) string {
	return strconv.Itoa(target.PID) + ":" + strconv.FormatUint(target.Started, 10)
}
