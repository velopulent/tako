package platform

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

type Process struct {
	PID              int     `json:"pid"`
	PPID             int     `json:"ppid"`
	Started          uint64  `json:"started"`
	UID              int     `json:"uid"`
	User             string  `json:"user"`
	Program          string  `json:"program"`
	Command          string  `json:"command"`
	State            string  `json:"state"`
	Threads          int     `json:"threads"`
	CPUTime          float64 `json:"cpuTime"`
	Memory           uint64  `json:"memory"`
	VirtualMemory    uint64  `json:"virtualMemory"`
	DiskRead         uint64  `json:"diskRead"`
	DiskWrite        uint64  `json:"diskWrite"`
	PermissionDenied bool    `json:"permissionDenied,omitempty"`
	Reason           string  `json:"reason,omitempty"`
}

func Processes() ([]Process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	clockTicks := float64(100)
	pageSize := uint64(os.Getpagesize())
	users := map[string]string{}
	result := make([]Process, 0, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				result = append(result, Process{PID: pid, User: "?", PermissionDenied: true, Reason: "process metadata is not readable"})
			}
			continue
		}
		line := string(stat)
		open, close := strings.IndexByte(line, '('), strings.LastIndexByte(line, ')')
		if open < 0 || close <= open {
			continue
		}
		fields := strings.Fields(line[close+1:])
		if len(fields) < 22 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		utime, _ := strconv.ParseUint(fields[11], 10, 64)
		stime, _ := strconv.ParseUint(fields[12], 10, 64)
		rss, _ := strconv.ParseUint(fields[21], 10, 64)
		started, _ := strconv.ParseUint(fields[19], 10, 64)
		virtualMemory, _ := strconv.ParseUint(fields[20], 10, 64)
		threads, _ := strconv.Atoi(fields[17])
		program := line[open+1 : close]
		command := program
		reason := ""
		permissionDenied := false
		if cmdline, err := readProcessFile(filepath.Join("/proc", entry.Name(), "cmdline")); err == nil && len(cmdline) > 0 {
			command = strings.TrimSpace(strings.ReplaceAll(string(cmdline), "\x00", " "))
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			permissionDenied = true
			reason = "command line is not readable"
		}
		uid := processUID(entry.Name())
		uidNumber, _ := strconv.Atoi(uid)
		username := users[uid]
		if username == "" {
			username = uid
			if account, err := user.LookupId(uid); err == nil {
				username = account.Username
			}
			users[uid] = username
		}
		diskRead, diskWrite, ioErr := processIO(entry.Name())
		if ioErr != nil && !errors.Is(ioErr, os.ErrNotExist) {
			permissionDenied = true
			if reason == "" {
				reason = "I/O counters are not readable"
			}
		}
		result = append(result, Process{PID: pid, PPID: ppid, Started: started, UID: uidNumber, User: username, Program: program, Command: command, State: fields[0], Threads: threads, CPUTime: float64(utime+stime) / clockTicks, Memory: rss * pageSize, VirtualMemory: virtualMemory, DiskRead: diskRead, DiskWrite: diskWrite, PermissionDenied: permissionDenied, Reason: reason})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Memory > result[j].Memory })
	return result, nil
}

func processIO(pid string) (uint64, uint64, error) {
	file, err := os.Open(filepath.Join("/proc", pid, "io"))
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	var read, write uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		value, _ := strconv.ParseUint(fields[1], 10, 64)
		switch strings.TrimSuffix(fields[0], ":") {
		case "read_bytes":
			read = value
		case "write_bytes":
			write = value
		}
	}
	return read, write, scanner.Err()
}

func processUID(pid string) string {
	file, err := os.Open(filepath.Join("/proc", pid, "status"))
	if err != nil {
		return "?"
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "Uid:") {
			fields := strings.Fields(scanner.Text())
			if len(fields) > 1 {
				return fields[1]
			}
		}
	}
	return "?"
}

type User struct {
	Username string   `json:"username"`
	UID      int      `json:"uid"`
	GID      int      `json:"gid"`
	Name     string   `json:"name"`
	Home     string   `json:"home"`
	Shell    string   `json:"shell"`
	System   bool     `json:"system"`
	Groups   []string `json:"groups"`
	Source   string   `json:"source"`
	Local    bool     `json:"local"`
	Mutable  bool     `json:"mutable"`
	Reason   string   `json:"reason,omitempty"`
}

type Interface struct {
	Name      string   `json:"name"`
	Index     int      `json:"index"`
	MTU       int      `json:"mtu"`
	Hardware  string   `json:"hardware"`
	Addresses []string `json:"addresses"`
	Up        bool     `json:"up"`
	RX        uint64   `json:"rx"`
	TX        uint64   `json:"tx"`
	Manager   string   `json:"manager"`
	Profile   string   `json:"profile,omitempty"`
	Owner     string   `json:"owner,omitempty"`
	Conflict  bool     `json:"conflict,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

func Interfaces() ([]Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	result := make([]Interface, 0, len(interfaces))
	for _, item := range interfaces {
		addresses, _ := item.Addrs()
		values := make([]string, 0, len(addresses))
		for _, address := range addresses {
			values = append(values, address.String())
		}
		manager := "kernel"
		if fileExists("/run/NetworkManager") {
			manager = "NetworkManager"
		} else if fileExists("/run/systemd/netif") {
			manager = "systemd-networkd"
		}
		result = append(result, Interface{Name: item.Name, Index: item.Index, MTU: item.MTU, Hardware: item.HardwareAddr.String(), Addresses: values, Up: item.Flags&net.FlagUp != 0, RX: readUint(filepath.Join("/sys/class/net", item.Name, "statistics/rx_bytes")), TX: readUint(filepath.Join("/sys/class/net", item.Name, "statistics/tx_bytes")), Manager: manager})
	}
	return result, nil
}

func readUint(path string) uint64 {
	payload, _ := os.ReadFile(path)
	value, _ := strconv.ParseUint(strings.TrimSpace(string(payload)), 10, 64)
	return value
}

type Unit struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	LoadState   string `json:"loadState"`
	ActiveState string `json:"activeState"`
	SubState    string `json:"subState"`
	FileState   string `json:"fileState"`
	Scope       string `json:"scope"`
	Type        string `json:"type"`
}

func Units(ctx context.Context, scope, unitType string) ([]Unit, error) {
	var conn *dbus.Conn
	var err error
	if scope == "user" {
		conn, err = dbus.ConnectSessionBus()
	} else {
		conn, err = dbus.ConnectSystemBus()
	}
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var raw []struct {
		Name, Description, LoadState, ActiveState, SubState, Following string
		Path                                                           dbus.ObjectPath
		JobID                                                          uint32
		JobType                                                        string
		JobPath                                                        dbus.ObjectPath
	}
	err = conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1").CallWithContext(ctx, "org.freedesktop.systemd1.Manager.ListUnits", 0).Store(&raw)
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	var rawFiles []struct {
		Name  string
		State string
	}
	_ = conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1").CallWithContext(ctx, "org.freedesktop.systemd1.Manager.ListUnitFiles", 0).Store(&rawFiles)
	for _, item := range rawFiles {
		name := filepath.Base(item.Name)
		files[name] = item.State
	}
	resultByName := make(map[string]Unit, len(raw)+len(files))
	for _, item := range raw {
		typeName := strings.TrimPrefix(filepath.Ext(item.Name), ".")
		if unitType != "" && typeName != unitType {
			continue
		}
		resultByName[item.Name] = Unit{Name: item.Name, Description: item.Description, LoadState: item.LoadState, ActiveState: item.ActiveState, SubState: item.SubState, FileState: files[item.Name], Scope: scope, Type: typeName}
	}
	for name, state := range files {
		typeName := strings.TrimPrefix(filepath.Ext(name), ".")
		if unitType != "" && typeName != unitType {
			continue
		}
		if _, ok := resultByName[name]; !ok {
			resultByName[name] = Unit{Name: name, LoadState: "loaded", ActiveState: "inactive", SubState: "dead", FileState: state, Scope: scope, Type: typeName}
		}
	}
	result := make([]Unit, 0, len(resultByName))
	for _, item := range resultByName {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ActiveState != result[j].ActiveState {
			return result[i].ActiveState == "active"
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

type UnitDetail struct {
	Unit
	Path                 string   `json:"path"`
	MainPID              uint32   `json:"mainPid"`
	MemoryCurrent        uint64   `json:"memoryCurrent"`
	TasksCurrent         uint64   `json:"tasksCurrent"`
	ActiveEnterTimestamp uint64   `json:"activeEnterTimestamp"`
	Requires             []string `json:"requires"`
	Wants                []string `json:"wants"`
	WantedBy             []string `json:"wantedBy"`
	Conflicts            []string `json:"conflicts"`
	Before               []string `json:"before"`
	After                []string `json:"after"`
}

func UnitDetails(ctx context.Context, scope, name string) (UnitDetail, error) {
	if err := ValidateServiceTarget(scope, name); err != nil {
		return UnitDetail{}, err
	}
	var conn *dbus.Conn
	var err error
	if scope == "user" {
		conn, err = dbus.ConnectSessionBus()
	} else {
		conn, err = dbus.ConnectSystemBus()
	}
	if err != nil {
		return UnitDetail{}, err
	}
	defer conn.Close()
	manager := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	var path dbus.ObjectPath
	if err := manager.CallWithContext(ctx, "org.freedesktop.systemd1.Manager.LoadUnit", 0, name).Store(&path); err != nil {
		return UnitDetail{}, err
	}
	object := conn.Object("org.freedesktop.systemd1", path)
	getString := func(property string) string {
		value, err := object.GetProperty(property)
		if err != nil {
			return ""
		}
		result, _ := value.Value().(string)
		return result
	}
	getStrings := func(property string) []string {
		value, err := object.GetProperty(property)
		if err != nil {
			return []string{}
		}
		switch result := value.Value().(type) {
		case []string:
			return result
		case []dbus.ObjectPath:
			items := make([]string, 0, len(result))
			for _, path := range result {
				if name := unitNameFromObjectPath(path); name != "" {
					items = append(items, name)
				}
			}
			return items
		default:
			return []string{}
		}
	}
	getUint32 := func(property string) uint32 {
		value, err := object.GetProperty(property)
		if err != nil {
			return 0
		}
		result, _ := value.Value().(uint32)
		return result
	}
	getUint64 := func(property string) uint64 {
		value, err := object.GetProperty(property)
		if err != nil {
			return 0
		}
		result, _ := value.Value().(uint64)
		return result
	}
	unit := Unit{Name: name, Description: getString("org.freedesktop.systemd1.Unit.Description"), LoadState: getString("org.freedesktop.systemd1.Unit.LoadState"), ActiveState: getString("org.freedesktop.systemd1.Unit.ActiveState"), SubState: getString("org.freedesktop.systemd1.Unit.SubState"), Scope: scope, Type: strings.TrimPrefix(filepath.Ext(name), ".")}
	return UnitDetail{Unit: unit, Path: getString("org.freedesktop.systemd1.Unit.FragmentPath"), MainPID: getUint32("org.freedesktop.systemd1.Service.MainPID"), MemoryCurrent: getUint64("org.freedesktop.systemd1.Unit.MemoryCurrent"), TasksCurrent: getUint64("org.freedesktop.systemd1.Unit.TasksCurrent"), ActiveEnterTimestamp: getUint64("org.freedesktop.systemd1.Unit.ActiveEnterTimestamp"), Requires: getStrings("org.freedesktop.systemd1.Unit.Requires"), Wants: getStrings("org.freedesktop.systemd1.Unit.Wants"), WantedBy: getStrings("org.freedesktop.systemd1.Unit.WantedBy"), Conflicts: getStrings("org.freedesktop.systemd1.Unit.Conflicts"), Before: getStrings("org.freedesktop.systemd1.Unit.Before"), After: getStrings("org.freedesktop.systemd1.Unit.After")}, nil
}

func unitNameFromObjectPath(path dbus.ObjectPath) string {
	segment := filepath.Base(string(path))
	if segment == "." || segment == "/" || segment == "" {
		return ""
	}
	var decoded strings.Builder
	decoded.Grow(len(segment))
	for index := 0; index < len(segment); index++ {
		if segment[index] == '_' && index+2 < len(segment) {
			value, err := strconv.ParseUint(segment[index+1:index+3], 16, 8)
			if err == nil {
				decoded.WriteByte(byte(value))
				index += 2
				continue
			}
		}
		decoded.WriteByte(segment[index])
	}
	return decoded.String()
}

type LogEntry struct {
	Timestamp string            `json:"timestamp"`
	Priority  string            `json:"priority"`
	Unit      string            `json:"unit"`
	Message   string            `json:"message"`
	Details   map[string]string `json:"details,omitempty"`
	Cursor    string            `json:"-"`
}

func Logs(ctx context.Context, limit int) ([]LogEntry, error) {
	page, err := QueryLogs(ctx, JournalQuery{Limit: limit})
	return page.Items, err
}

// FollowLogs emits new journal entries until ctx is cancelled. The subprocess
// inherits cancellation, while the synchronous callback supplies backpressure.
func FollowLogs(ctx context.Context, emit func(LogEntry) error) error {
	return FollowJournal(ctx, JournalQuery{}, emit)
}

// FollowJournal emits filtered, structured journal entries until ctx is
// cancelled. It does not buffer entries; emit supplies backpressure and owns
// the stream's memory bound.
func FollowJournal(ctx context.Context, query JournalQuery, emit func(LogEntry) error) error {
	return FollowJournalAs(ctx, query, nil, emit)
}

func parseLogEntry(payload []byte) (LogEntry, bool) {
	return parseLogEntryWithDetails(payload, false)
}

func parseLogEntryWithDetails(payload []byte, details bool) (LogEntry, bool) {
	var row map[string]any
	if json.Unmarshal(payload, &row) != nil {
		return LogEntry{}, false
	}
	micros, _ := strconv.ParseInt(stringValue(row["__REALTIME_TIMESTAMP"]), 10, 64)
	unit := stringValue(row["_SYSTEMD_UNIT"])
	if unit == "" {
		unit = stringValue(row["_SYSTEMD_USER_UNIT"])
	}
	entry := LogEntry{Timestamp: time.UnixMicro(micros).UTC().Format(time.RFC3339Nano), Priority: stringValue(row["PRIORITY"]), Unit: unit, Message: stringValue(row["MESSAGE"]), Cursor: stringValue(row["__CURSOR"])}
	if details {
		const maxDetailBytes = 64 << 10
		const maxDetailValue = 16 << 10
		entry.Details = make(map[string]string)
		total := 0
		for _, field := range journalDetailFields {
			if field == "__CURSOR" || field == "__REALTIME_TIMESTAMP" {
				continue
			}
			value := stringValue(row[field])
			if value == "" || len(value) > maxDetailValue || total+len(field)+len(value) > maxDetailBytes {
				continue
			}
			entry.Details[field] = value
			total += len(field) + len(value)
		}
		if len(entry.Details) == 0 {
			entry.Details = nil
		}
	}
	return entry, true
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return ""
	}
}
