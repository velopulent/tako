package platform

import (
	"errors"
	"strings"
	"testing"
)

func TestProcessViewRedactsOtherUsersPrivateDetails(t *testing.T) {
	view := ProcessView{UID: 1000}
	root := Process{PID: 1, UID: 0, DiskRead: 10, DiskWrite: 20, DiskReadRate: 1, DiskWriteRate: 2}
	own := Process{PID: 2, UID: 1000, DiskRead: 30}
	items := view.Processes([]Process{root, own})
	if len(items) != 2 || !items[0].IODenied || items[0].DiskRead != 0 || items[0].DiskWriteRate != 0 {
		t.Fatalf("root process I/O was not redacted: %#v", items[0])
	}
	if items[1].IODenied || items[1].DiskRead != 30 {
		t.Fatalf("own process was redacted: %#v", items[1])
	}

	details, err := view.Details(ProcessDetails{
		Process:   root,
		OpenFiles: []string{"/etc/shadow"},
		Sockets:   []ProcessSocket{{}},
		History:   []ProcessResourceSample{{DiskRead: 5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(details.OpenFiles) != 0 || len(details.Sockets) != 0 || !details.History[0].IODenied || details.History[0].DiskRead != 0 {
		t.Fatalf("root process details leaked: %#v", details)
	}
	if !strings.Contains(strings.Join(details.AccessIssues, " "), "administrative access") {
		t.Fatalf("redaction was not explained: %#v", details.AccessIssues)
	}

	mine, err := view.Details(ProcessDetails{Process: own, OpenFiles: []string{"/home/me/notes"}})
	if err != nil || len(mine.OpenFiles) != 1 || len(mine.AccessIssues) != 0 {
		t.Fatalf("own process details were redacted: %#v %v", mine, err)
	}
}

func TestProcessViewHonorsHidePID(t *testing.T) {
	view := ProcessView{UID: 1000, HideOthers: true}
	items := view.Processes([]Process{{PID: 1, UID: 0}, {PID: 2, UID: 1000}})
	if len(items) != 1 || items[0].PID != 2 {
		t.Fatalf("hidepid view showed other users' processes: %#v", items)
	}
	if _, err := view.Details(ProcessDetails{Process: Process{PID: 1, UID: 0}}); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("hidden process details error = %v", err)
	}
	details, err := view.Details(ProcessDetails{Process: Process{PID: 2, UID: 1000}, Parent: &Process{PID: 1, UID: 0}, Children: []Process{{PID: 3, UID: 0}}})
	if err != nil || details.Parent != nil || len(details.Children) != 0 {
		t.Fatalf("hidden relatives leaked: %#v %v", details, err)
	}
}

func TestProcHidePIDParsesMountinfo(t *testing.T) {
	for _, test := range []struct {
		line   string
		hidden bool
		gid    int
	}{
		{"22 1 0:21 / /proc rw,nosuid,nodev,noexec,relatime shared:12 - proc proc rw", false, -1},
		{"22 1 0:21 / /proc rw,nosuid shared:12 - proc proc rw,hidepid=invisible,gid=27", true, 27},
		{"22 1 0:21 / /proc rw - proc proc rw,hidepid=2", true, -1},
		{"22 1 0:21 / /proc rw - proc proc rw,hidepid=0", false, -1},
		{"40 22 0:35 / /proc/sys/fs/binfmt_misc rw - autofs systemd-1 rw", false, -1},
	} {
		hidden, gid := procHidePID(test.line)
		if hidden != test.hidden || gid != test.gid {
			t.Fatalf("%q = %v %d, want %v %d", test.line, hidden, gid, test.hidden, test.gid)
		}
	}
}
