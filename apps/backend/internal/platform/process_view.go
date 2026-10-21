package platform

import (
	"os"
	"slices"
	"strconv"
	"strings"
)

const processRedactionNotice = "Open files, sockets, and disk I/O of other users' processes require administrative access."

// ProcessView limits a non-elevated caller to what Linux would show that user
// in /proc. The inventory is collected as root, so without it any signed-in
// user could read other users' open files, sockets, and I/O counters, and
// hidepid would not apply.
type ProcessView struct {
	UID    int
	Groups []int
	// HideOthers mirrors a /proc mounted with hidepid=1 or 2 when the caller
	// is not in its exempt gid= group.
	HideOthers bool
}

// NewProcessView builds the view for a caller from the host's /proc mount.
func NewProcessView(uid int, groups []int) ProcessView {
	view := ProcessView{UID: uid, Groups: groups}
	if mountinfo, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		if hidden, gid := procHidePID(string(mountinfo)); hidden {
			view.HideOthers = gid < 0 || !slices.Contains(groups, gid)
		}
	}
	return view
}

func (view ProcessView) owns(process Process) bool { return process.UID == view.UID }

func (view ProcessView) visible(process Process) bool {
	return !view.HideOthers || view.owns(process)
}

func (view ProcessView) redact(process Process) Process {
	if view.owns(process) {
		return process
	}
	process.DiskRead, process.DiskWrite = 0, 0
	process.DiskReadRate, process.DiskWriteRate = 0, 0
	process.IODenied = true
	return process
}

// Processes filters and redacts an inventory.
func (view ProcessView) Processes(items []Process) []Process {
	result := make([]Process, 0, len(items))
	for _, item := range items {
		if view.visible(item) {
			result = append(result, view.redact(item))
		}
	}
	return result
}

// Details redacts one process's details, or reports it missing when the
// caller could not see it at all.
func (view ProcessView) Details(details ProcessDetails) (ProcessDetails, error) {
	if !view.visible(details.Process) {
		return ProcessDetails{}, ErrProcessNotFound
	}
	if details.Parent != nil {
		if view.visible(*details.Parent) {
			parent := view.redact(*details.Parent)
			details.Parent = &parent
		} else {
			details.Parent = nil
		}
	}
	details.Children = view.Processes(details.Children)
	if view.owns(details.Process) {
		return details, nil
	}
	details.Process = view.redact(details.Process)
	details.OpenFiles = []string{}
	details.Sockets = []ProcessSocket{}
	history := make([]ProcessResourceSample, len(details.History))
	for index, sample := range details.History {
		sample.DiskRead, sample.DiskWrite, sample.IODenied = 0, 0, true
		history[index] = sample
	}
	details.History = history
	details.AccessIssues = append(details.AccessIssues, processRedactionNotice)
	return details, nil
}

// procHidePID reports whether the /proc mount hides other users' processes,
// and its exempt group (-1 when none).
func procHidePID(mountinfo string) (bool, int) {
	for _, line := range strings.Split(mountinfo, "\n") {
		fields := strings.Fields(line)
		separator := slices.Index(fields, "-")
		if len(fields) < 5 || fields[4] != "/proc" || separator < 0 || len(fields) < separator+4 || fields[separator+1] != "proc" {
			continue
		}
		hidden, gid := false, -1
		for _, option := range strings.Split(fields[separator+3], ",") {
			key, value, _ := strings.Cut(option, "=")
			switch key {
			case "hidepid":
				hidden = value == "1" || value == "2" || value == "noaccess" || value == "invisible"
			case "gid":
				if parsed, err := strconv.Atoi(value); err == nil {
					gid = parsed
				}
			}
		}
		return hidden, gid
	}
	return false, -1
}
