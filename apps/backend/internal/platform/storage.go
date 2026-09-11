package platform

import (
	"bufio"
	"errors"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

var errStatfsUnavailable = errors.New("statfs failed")

type MountPoint struct {
	Target   string `json:"target"`
	Root     string `json:"root,omitempty"`
	ReadOnly bool   `json:"readOnly"`
}

type Filesystem struct {
	Device     string       `json:"device"`
	Type       string       `json:"type"`
	MajorMinor string       `json:"majorMinor"`
	Network    bool         `json:"network"`
	ReadOnly   bool         `json:"readOnly"`
	Total      uint64       `json:"total"`
	Used       uint64       `json:"used"`
	Available  uint64       `json:"available"`
	Percent    float64      `json:"percent"`
	Targets    []MountPoint `json:"targets"`
}

type StorageSummary struct {
	Filesystems int     `json:"filesystems"`
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Percent     float64 `json:"percent"`
}

type mountEntry struct {
	majorMinor string
	root       string
	target     string
	options    []string
	fsType     string
	source     string
}

// Filesystems reports one entry per distinct mounted filesystem, keyed by the
// kernel device number of its superblock. Bind mounts and btrfs subvolume
// mounts share a superblock, so they collapse into one entry that lists every
// mount point instead of duplicating capacity numbers.
func Filesystems() ([]Filesystem, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := parseMountInfo(file)
	if err != nil {
		return nil, err
	}
	return buildFilesystems(entries, statfsPath), nil
}

func statfsPath(target string, stat *syscall.Statfs_t) error {
	return syscall.Statfs(target, stat)
}

// SummarizeStorage aggregates unique filesystems. Because entries are already
// deduplicated by superblock, summing capacity here never double counts an
// underlying disk.
func SummarizeStorage(items []Filesystem) StorageSummary {
	var total, used uint64
	for _, item := range items {
		total += item.Total
		used += item.Used
	}
	summary := StorageSummary{Filesystems: len(items), Total: total, Used: used}
	if total > 0 {
		summary.Percent = 100 * float64(used) / float64(total)
	}
	return summary
}

func parseMountInfo(reader io.Reader) ([]mountEntry, error) {
	scanner := bufio.NewScanner(reader)
	entries := []mountEntry{}
	for scanner.Scan() {
		line := scanner.Text()
		separator := strings.Index(line, " - ")
		if separator < 0 {
			continue
		}
		left := strings.Fields(line[:separator])
		right := strings.Fields(line[separator+3:])
		if len(left) < 6 || len(right) < 2 {
			continue
		}
		entries = append(entries, mountEntry{
			majorMinor: left[2],
			root:       unescapeMount(left[3]),
			target:     unescapeMount(left[4]),
			options:    strings.Split(left[5], ","),
			fsType:     right[0],
			source:     unescapeMount(right[1]),
		})
	}
	return entries, scanner.Err()
}

type filesystemBuilder struct {
	filesystem *Filesystem
	targets    map[string]MountPoint
	measured   bool
}

type statfsFunc func(target string, stat *syscall.Statfs_t) error

func buildFilesystems(entries []mountEntry, statfs statfsFunc) []Filesystem {
	groups := map[string]*filesystemBuilder{}
	order := []string{}
	for _, entry := range entries {
		network, virtual := classifyFilesystem(entry.fsType)
		if virtual {
			continue
		}
		group := groups[entry.majorMinor]
		if group == nil {
			group = &filesystemBuilder{filesystem: &Filesystem{
				Type:       entry.fsType,
				MajorMinor: entry.majorMinor,
				Network:    network,
				Device:     entry.source,
				ReadOnly:   true,
			}, targets: map[string]MountPoint{}}
			groups[entry.majorMinor] = group
			order = append(order, entry.majorMinor)
		}
		readOnly := hasReadOnlyOption(entry.options)
		group.targets[entry.target] = MountPoint{Target: entry.target, Root: entry.root, ReadOnly: readOnly}
		group.filesystem.ReadOnly = group.filesystem.ReadOnly && readOnly
		if !group.measured {
			var stat syscall.Statfs_t
			if statfs(entry.target, &stat) == nil {
				fillUsage(group.filesystem, &stat)
				group.measured = true
			}
		}
	}
	result := make([]Filesystem, 0, len(groups))
	for _, key := range order {
		filesystem := groups[key].filesystem
		filesystem.Targets = make([]MountPoint, 0, len(groups[key].targets))
		for _, target := range groups[key].targets {
			filesystem.Targets = append(filesystem.Targets, target)
		}
		sort.Slice(filesystem.Targets, func(i, j int) bool {
			return mountPointLess(filesystem.Targets[i], filesystem.Targets[j])
		})
		result = append(result, *filesystem)
	}
	sort.Slice(result, func(i, j int) bool {
		return primaryTarget(result[i]) < primaryTarget(result[j])
	})
	return result
}

func fillUsage(filesystem *Filesystem, stat *syscall.Statfs_t) {
	blockSize := uint64(stat.Bsize)
	filesystem.Total = stat.Blocks * blockSize
	filesystem.Available = stat.Bavail * blockSize
	filesystem.Used = (stat.Blocks - stat.Bfree) * blockSize
	if filesystem.Total > 0 {
		filesystem.Percent = 100 * float64(filesystem.Used) / float64(filesystem.Total)
	}
}

func mountPointLess(left, right MountPoint) bool {
	if len(left.Target) != len(right.Target) {
		return len(left.Target) < len(right.Target)
	}
	return left.Target < right.Target
}

func primaryTarget(filesystem Filesystem) string {
	if len(filesystem.Targets) == 0 {
		return "\xff"
	}
	return filesystem.Targets[0].Target
}

func hasReadOnlyOption(options []string) bool {
	for _, option := range options {
		if option == "ro" {
			return true
		}
	}
	return false
}

var virtualFilesystems = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true,
	"tmpfs": true, "ramfs": true, "cgroup": true, "cgroup2": true,
	"securityfs": true, "pstore": true, "debugfs": true, "tracefs": true,
	"configfs": true, "fusectl": true, "mqueue": true, "hugetlbfs": true,
	"autofs": true, "bpf": true, "bpffs": true, "binfmt_misc": true,
	"nsfs": true, "efivarfs": true, "overlay": true, "squashfs": true,
	"erofs": true, "romfs": true, "iso9660": true, "rpc_pipefs": true,
	"selinuxfs": true, "functionfs": true, "fuse": true, "none": true,
}

var networkFilesystemPrefixes = []string{
	"nfs", "cifs", "smb", "sshfs", "ceph", "glusterfs", "lustre",
	"davfs", "ncpfs", "9p", "ocfs2", "acfs", "afs", "dafs",
}

// classifyFilesystem sorts fstypes into network mounts worth surfacing and
// everything else; virtual kernel, container, and desktop-helper mounts are
// skipped outright.
func classifyFilesystem(fsType string) (network bool, skip bool) {
	if fsType == "" {
		return false, true
	}
	if virtualFilesystems[fsType] {
		return false, true
	}
	for _, prefix := range networkFilesystemPrefixes {
		if strings.HasPrefix(fsType, prefix) || strings.HasPrefix(fsType, "fuse."+prefix) {
			return true, false
		}
	}
	// Remaining fuse.* mounts are desktop helpers (gvfs, portals), not storage.
	return false, strings.HasPrefix(fsType, "fuse.")
}

// unescapeMount decodes the three-digit octal escapes (\040 space, \011 tab,
// \012 newline, \134 backslash) that /proc/self/mountinfo uses in paths.
func unescapeMount(value string) string {
	if !strings.Contains(value, "\\") {
		return value
	}
	var decoded strings.Builder
	decoded.Grow(len(value))
	for index := 0; index < len(value); index++ {
		if value[index] == '\\' && index+3 < len(value) {
			if parsed, err := strconv.ParseUint(value[index+1:index+4], 8, 8); err == nil {
				decoded.WriteByte(byte(parsed))
				index += 3
				continue
			}
		}
		decoded.WriteByte(value[index])
	}
	return decoded.String()
}
