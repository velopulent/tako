package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxProcessOpenFiles = 256
	maxProcessSockets   = 256
	maxProcessFileBytes = 64 << 10
	maxProcessHistory   = 120
	maxTrackedProcesses = 2048
)

var (
	ErrProcessNotFound = errors.New("process not found")
	ErrProcessReused   = errors.New("process identity changed")
)

type ProcessSocket struct {
	Protocol string `json:"protocol"`
	Local    string `json:"local"`
	Remote   string `json:"remote,omitempty"`
	State    string `json:"state,omitempty"`
}

type ProcessResourceSample struct {
	Timestamp     time.Time `json:"timestamp"`
	CPUTime       float64   `json:"cpuTime"`
	Memory        uint64    `json:"memory"`
	VirtualMemory uint64    `json:"virtualMemory"`
	DiskRead      uint64    `json:"diskRead"`
	DiskWrite     uint64    `json:"diskWrite"`
}

type ProcessDetails struct {
	Process      Process                 `json:"process"`
	Parent       *Process                `json:"parent,omitempty"`
	Children     []Process               `json:"children"`
	CGroup       string                  `json:"cgroup,omitempty"`
	OpenFiles    []string                `json:"openFiles"`
	Sockets      []ProcessSocket         `json:"sockets"`
	History      []ProcessResourceSample `json:"history"`
	AccessIssues []string                `json:"accessIssues,omitempty"`
}

func (details ProcessDetails) MarshalJSON() ([]byte, error) {
	if details.Children == nil {
		details.Children = []Process{}
	}
	if details.OpenFiles == nil {
		details.OpenFiles = []string{}
	}
	if details.Sockets == nil {
		details.Sockets = []ProcessSocket{}
	}
	if details.History == nil {
		details.History = []ProcessResourceSample{}
	}
	type plain ProcessDetails
	return json.Marshal((plain)(details))
}

func (details *ProcessDetails) UnmarshalJSON(payload []byte) error {
	type plain ProcessDetails
	var raw plain
	if err := json.Unmarshal(payload, &raw); err != nil {
		return err
	}
	*details = ProcessDetails(raw)
	if details.Children == nil {
		details.Children = []Process{}
	}
	if details.OpenFiles == nil {
		details.OpenFiles = []string{}
	}
	if details.Sockets == nil {
		details.Sockets = []ProcessSocket{}
	}
	if details.History == nil {
		details.History = []ProcessResourceSample{}
	}
	return nil
}

type ProcessTracker struct {
	mu        sync.Mutex
	history   map[string][]ProcessResourceSample
	lastSeen  map[string]time.Time
	maxSample int
}

func NewProcessTracker() *ProcessTracker {
	return &ProcessTracker{history: make(map[string][]ProcessResourceSample), lastSeen: make(map[string]time.Time), maxSample: maxProcessHistory}
}

func processIdentity(pid int, started uint64) string {
	return fmt.Sprintf("%d:%d", pid, started)
}

func (tracker *ProcessTracker) Snapshot() ([]Process, error) {
	items, err := Processes()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		key := processIdentity(item.PID, item.Started)
		seen[key] = struct{}{}
		tracker.lastSeen[key] = now
		samples := append(tracker.history[key], ProcessResourceSample{Timestamp: now, CPUTime: item.CPUTime, Memory: item.Memory, VirtualMemory: item.VirtualMemory, DiskRead: item.DiskRead, DiskWrite: item.DiskWrite})
		if len(samples) > tracker.maxSample {
			samples = samples[len(samples)-tracker.maxSample:]
		}
		tracker.history[key] = samples
	}
	for key, last := range tracker.lastSeen {
		if _, ok := seen[key]; !ok && now.Sub(last) > 10*time.Minute {
			delete(tracker.lastSeen, key)
			delete(tracker.history, key)
		}
	}
	for len(tracker.history) > maxTrackedProcesses {
		var oldest string
		var oldestAt time.Time
		for key, at := range tracker.lastSeen {
			if oldest == "" || at.Before(oldestAt) {
				oldest, oldestAt = key, at
			}
		}
		if oldest == "" {
			break
		}
		delete(tracker.lastSeen, oldest)
		delete(tracker.history, oldest)
	}
	return items, nil
}

func (tracker *ProcessTracker) Inspect(ctx context.Context, pid int, started uint64) (ProcessDetails, error) {
	details, err := InspectProcess(ctx, pid, started)
	if err != nil {
		return ProcessDetails{}, err
	}
	key := processIdentity(pid, details.Process.Started)
	tracker.mu.Lock()
	history := tracker.history[key]
	if history == nil {
		history = []ProcessResourceSample{}
	}
	details.History = append([]ProcessResourceSample(nil), history...)
	if details.History == nil {
		details.History = []ProcessResourceSample{}
	}
	tracker.mu.Unlock()
	return details, nil
}

func InspectProcess(ctx context.Context, pid int, started uint64) (ProcessDetails, error) {
	if pid < 1 || pid > 1<<22 {
		return ProcessDetails{}, ErrProcessNotFound
	}
	items, err := Processes()
	if err != nil {
		return ProcessDetails{}, err
	}
	var target Process
	found := false
	for _, item := range items {
		if item.PID != pid {
			continue
		}
		target = item
		found = true
		break
	}
	if !found {
		return ProcessDetails{}, ErrProcessNotFound
	}
	if started != 0 && target.Started != started {
		return ProcessDetails{}, ErrProcessReused
	}
	details := ProcessDetails{Process: target, Children: make([]Process, 0), OpenFiles: make([]string, 0), Sockets: make([]ProcessSocket, 0)}
	for _, item := range items {
		if item.PID == pid {
			continue
		}
		if item.PID == target.PPID {
			parent := item
			details.Parent = &parent
		}
		if item.PPID == pid {
			details.Children = append(details.Children, item)
		}
	}
	if ctx.Err() != nil {
		return ProcessDetails{}, ctx.Err()
	}
	details.CGroup, err = readProcessCGroup(pid)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		details.AccessIssues = append(details.AccessIssues, "cgroup is not readable")
	}
	details.OpenFiles, err = readProcessOpenFiles(pid)
	if err != nil {
		if details.OpenFiles == nil {
			details.OpenFiles = []string{}
		}
		if !errors.Is(err, os.ErrNotExist) {
			details.AccessIssues = append(details.AccessIssues, "open files are not readable")
		}
	}
	if details.OpenFiles == nil {
		details.OpenFiles = []string{}
	}
	details.Sockets, err = readProcessSockets(pid)
	if err != nil {
		if details.Sockets == nil {
			details.Sockets = []ProcessSocket{}
		}
		if !errors.Is(err, os.ErrNotExist) {
			details.AccessIssues = append(details.AccessIssues, "sockets are not readable")
		}
	}
	if details.Sockets == nil {
		details.Sockets = []ProcessSocket{}
	}
	if details.Children == nil {
		details.Children = []Process{}
	}
	if details.History == nil {
		details.History = []ProcessResourceSample{}
	}
	return details, nil
}

func readProcessCGroup(pid int) (string, error) {
	data, err := readProcessFile(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	return strings.TrimSpace(string(data)), err
}

func readProcessFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxProcessFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxProcessFileBytes {
		return data[:maxProcessFileBytes], errors.New("process file exceeded bound")
	}
	return data, nil
}

func readProcessOpenFiles(pid int) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	if err != nil {
		return []string{}, err
	}
	files := make([]string, 0, minInt(len(entries), maxProcessOpenFiles))
	for index, entry := range entries {
		if index >= maxProcessOpenFiles {
			break
		}
		name, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", entry.Name()))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return files, err
		}
		files = append(files, name)
	}
	return files, nil
}

func readProcessSockets(pid int) ([]ProcessSocket, error) {
	base := filepath.Join("/proc", strconv.Itoa(pid), "net")
	sockets := make([]ProcessSocket, 0, maxProcessSockets)
	for _, protocol := range []string{"tcp", "tcp6", "udp", "udp6", "unix"} {
		data, err := readProcessFile(filepath.Join(base, protocol))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return sockets, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if len(sockets) >= maxProcessSockets {
				return sockets, nil
			}
			fields := strings.Fields(line)
			if len(fields) == 0 || strings.HasPrefix(fields[0], "sl") || strings.HasPrefix(fields[0], "Num") {
				continue
			}
			if protocol == "unix" {
				local := ""
				if len(fields) > 6 {
					local = strings.Join(fields[6:], " ")
				}
				state := ""
				if len(fields) > 5 {
					state = fields[5]
				}
				sockets = append(sockets, ProcessSocket{Protocol: protocol, Local: local, State: state})
				continue
			}
			if len(fields) >= 4 {
				sockets = append(sockets, ProcessSocket{Protocol: protocol, Local: fields[1], Remote: fields[2], State: fields[3]})
			}
		}
	}
	sort.Slice(sockets, func(i, j int) bool { return sockets[i].Protocol < sockets[j].Protocol })
	return sockets, nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
