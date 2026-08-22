package platform

import (
	"strings"
	"syscall"
	"testing"
)

const fixtureMountInfo = `22 1 259:3 / / rw,relatime - ext4 /dev/nvme0n1p3 rw
28 22 0:24 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw
29 22 0:26 / /sys rw,nosuid,nodev,noexec,relatime - sysfs sysfs rw
31 22 0:27 / /dev rw,nosuid,relatime - devtmpfs devtmpfs rw,size=64k
33 22 0:30 / /run rw,nosuid,nodev - tmpfs tmpfs rw
36 22 8:1 / /data rw,relatime - xfs /dev/sda1 rw,attr2
40 22 8:1 /srv/exports /data/bind rw,relatime - xfs /dev/sda1 rw,attr2
44 22 0:41 /@ /mnt/root rw,relatime - btrfs /dev/nvme1n1p2 rw,subvol=/@
48 22 0:41 /@home /home rw,relatime - btrfs /dev/nvme1n1p2 rw,subvol=/@home
52 22 0:42 / /mnt/ro ro,relatime - squashfs /dev/loop0 ro
56 22 0:43 / /mnt/overlay rw - overlay overlay rw
60 22 259:4 / /media/escape\040disk rw - ext4 /dev/sdb1 rw
64 22 0:50 / /export/backup rw,relatime - nfs4 server:/export/backup rw,vers=4.2
68 22 0:51 / /srv/files rw,relatime - cifs //nas/share rw`

func fakeStatfs(usage map[string][3]uint64) statfsFunc {
	return func(target string, stat *syscall.Statfs_t) error {
		values, ok := usage[target]
		if !ok {
			return errStatfsUnavailable
		}
		stat.Bsize = 4096
		stat.Blocks = values[0]
		stat.Bfree = values[1]
		stat.Bavail = values[2]
		return nil
	}
}

func TestParseMountInfoParsesFieldsAndEscapes(t *testing.T) {
	entries, err := parseMountInfo(strings.NewReader(fixtureMountInfo))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(entries) != 14 {
		t.Fatalf("expected 14 entries, got %d", len(entries))
	}
	bind := entries[6]
	if bind.target != "/data/bind" || bind.root != "/srv/exports" || bind.majorMinor != "8:1" {
		t.Fatalf("unexpected bind entry: %+v", bind)
	}
	escaped := entries[11]
	if escaped.target != "/media/escape disk" {
		t.Fatalf("escape decoding failed: %q", escaped.target)
	}
	nfs := entries[12]
	if nfs.fsType != "nfs4" || nfs.source != "server:/export/backup" {
		t.Fatalf("unexpected nfs entry: %+v", nfs)
	}
}

func TestBuildFilesystemsGroupsBySuperblock(t *testing.T) {
	entries, err := parseMountInfo(strings.NewReader(fixtureMountInfo))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	usage := map[string][3]uint64{
		"/":                  {1000, 250, 600},
		"/data":              {1000, 100, 800},
		"/data/bind":         {1000, 100, 800},
		"/mnt/root":          {2000, 500, 1200},
		"/home":              {2000, 500, 1200},
		"/media/escape disk": {500, 50, 400},
		"/export/backup":     {300, 60, 200},
		"/srv/files":         {700, 700, 0},
	}
	filesystems := buildFilesystems(entries, fakeStatfs(usage))
	if len(filesystems) != 6 {
		t.Fatalf("expected 6 unique filesystems, got %d: %+v", len(filesystems), filesystems)
	}
	root := filesystems[0]
	if root.Targets[0].Target != "/" || root.Device != "/dev/nvme0n1p3" {
		t.Fatalf("root filesystem misplaced: %+v", root)
	}
	if root.Used != (1000-250)*4096 || root.Percent != 75 {
		t.Fatalf("root usage wrong: used=%d percent=%f", root.Used, root.Percent)
	}

	var data *Filesystem
	for index := range filesystems {
		if filesystems[index].MajorMinor == "8:1" {
			data = &filesystems[index]
		}
	}
	if data == nil {
		t.Fatal("xfs group missing")
	}
	if len(data.Targets) != 2 {
		t.Fatalf("bind mount was not merged into its superblock group: %+v", data.Targets)
	}
	if data.Targets[0].Target != "/data" || data.Targets[1].Target != "/data/bind" || data.Targets[1].Root != "/srv/exports" {
		t.Fatalf("targets wrong: %+v", data.Targets)
	}

	var btrfs *Filesystem
	for index := range filesystems {
		if filesystems[index].Type == "btrfs" {
			btrfs = &filesystems[index]
		}
	}
	if btrfs == nil {
		t.Fatal("btrfs group missing")
	}
	if len(btrfs.Targets) != 2 || btrfs.Network {
		t.Fatalf("subvolume mounts were not merged: %+v", btrfs)
	}

	var nfs, cifs int
	for _, filesystem := range filesystems {
		switch filesystem.Type {
		case "nfs4":
			nfs++
			if !filesystem.Network || filesystem.Device != "server:/export/backup" {
				t.Fatalf("nfs4 flags wrong: %+v", filesystem)
			}
		case "cifs":
			cifs++
			if !filesystem.Network {
				t.Fatalf("cifs flags wrong: %+v", filesystem)
			}
		}
	}
	if nfs != 1 || cifs != 1 {
		t.Fatalf("network mounts missing: nfs=%d cifs=%d", nfs, cifs)
	}
	for _, filesystem := range filesystems {
		if filesystem.Type == "overlay" || filesystem.Type == "squashfs" ||
			filesystem.Type == "proc" || filesystem.Type == "tmpfs" {
			t.Fatalf("virtual filesystem leaked through: %+v", filesystem)
		}
	}
}

func TestBuildFilesystemsKeepsUnmountableNetworkMount(t *testing.T) {
	entries, _ := parseMountInfo(strings.NewReader("64 22 0:50 / /stale rw - nfs4 server:/gone rw\n"))
	filesystems := buildFilesystems(entries, func(string, *syscall.Statfs_t) error { return errStatfsUnavailable })
	if len(filesystems) != 1 {
		t.Fatalf("expected stale mount kept, got %d", len(filesystems))
	}
	if filesystems[0].Total != 0 || filesystems[0].Percent != 0 {
		t.Fatalf("unmeasurable filesystem should report zeros: %+v", filesystems[0])
	}
}

func TestClassifyFilesystem(t *testing.T) {
	virtual := []string{"proc", "sysfs", "tmpfs", "cgroup2", "overlay", "squashfs", "fuse", "nsfs"}
	for _, fsType := range virtual {
		if network, skip := classifyFilesystem(fsType); !skip || network {
			t.Errorf("%q should be skipped", fsType)
		}
	}
	network := []string{"nfs", "nfs4", "cifs", "sshfs", "fuse.sshfs", "cephfs", "9p"}
	for _, fsType := range network {
		if networkFlag, skip := classifyFilesystem(fsType); skip || !networkFlag {
			t.Errorf("%q should be network", fsType)
		}
	}
	local := []string{"ext4", "xfs", "btrfs", "vfat", "ntfs3", "fuseblk", "zfs", "f2fs"}
	for _, fsType := range local {
		if networkFlag, skip := classifyFilesystem(fsType); skip || networkFlag {
			t.Errorf("%q should be local", fsType)
		}
	}
	desktopHelpers := []string{"fuse.portal", "fuse.gvfsd-fuse"}
	for _, fsType := range desktopHelpers {
		if _, skip := classifyFilesystem(fsType); !skip {
			t.Errorf("%q should be skipped as a desktop helper", fsType)
		}
	}
}

func TestUnescapeMount(t *testing.T) {
	cases := map[string]string{
		"/plain":            "/plain",
		"/with\\040space":   "/with space",
		"a\\011b":           "a\tb",
		"back\\134slash":    "back\\slash",
		"incomplete\\04":    "incomplete\\04",
		"invalid\\099octal": "invalid\\099octal",
	}
	for input, want := range cases {
		if got := unescapeMount(input); got != want {
			t.Errorf("unescapeMount(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSummarizeStorageSumsUniqueFilesystems(t *testing.T) {
	items := []Filesystem{
		{Total: 1000, Used: 750},
		{Total: 500, Used: 100},
	}
	summary := SummarizeStorage(items)
	if summary.Filesystems != 2 || summary.Total != 1500 || summary.Used != 850 {
		t.Fatalf("summary math wrong: %+v", summary)
	}
	if summary.Percent != 850.0/1500.0*100 {
		t.Fatalf("percent wrong: %f", summary.Percent)
	}
	if empty := SummarizeStorage(nil); empty.Total != 0 || empty.Percent != 0 {
		t.Fatalf("empty summary wrong: %+v", empty)
	}
}
