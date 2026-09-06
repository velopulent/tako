package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidStorageOperation = errors.New("invalid storage operation")
	ErrStorageConflict         = errors.New("storage state changed")
	ErrStorageUnavailable      = errors.New("storage adapter unavailable")
	ErrStorageUnsafe           = errors.New("storage operation is unsafe")
	ErrStorageBusy             = errors.New("storage target is busy")
)

type StoragePartition struct {
	Path        string       `json:"path"`
	Name        string       `json:"name,omitempty"`
	Size        uint64       `json:"size"`
	Filesystem  string       `json:"filesystem,omitempty"`
	Label       string       `json:"label,omitempty"`
	UUID        string       `json:"uuid,omitempty"`
	Parent      string       `json:"parent,omitempty"`
	ReadOnly    bool         `json:"readOnly"`
	MountPoints []MountPoint `json:"mountPoints"`
}

type StorageHealth struct {
	Available    bool   `json:"available"`
	Passed       bool   `json:"passed,omitempty"`
	TemperatureC int    `json:"temperatureC,omitempty"`
	PowerOnHours uint64 `json:"powerOnHours,omitempty"`
	Failing      bool   `json:"failing,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type NVMeHealth struct {
	Available       bool   `json:"available"`
	TemperatureC    int    `json:"temperatureC,omitempty"`
	PercentageUsed  int    `json:"percentageUsed,omitempty"`
	CriticalWarning int    `json:"criticalWarning,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type StorageDevice struct {
	Path       string             `json:"path"`
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	Model      string             `json:"model,omitempty"`
	Serial     string             `json:"serial,omitempty"`
	Transport  string             `json:"transport,omitempty"`
	Size       uint64             `json:"size"`
	ReadOnly   bool               `json:"readOnly"`
	Removable  bool               `json:"removable"`
	Partitions []StoragePartition `json:"partitions"`
	SMART      *StorageHealth     `json:"smart,omitempty"`
	NVMe       *NVMeHealth        `json:"nvme,omitempty"`
}

type StorageSnapshot struct {
	Filesystems []Filesystem    `json:"filesystems"`
	Devices     []StorageDevice `json:"devices"`
	Fingerprint string          `json:"fingerprint"`
	ReadOnly    bool            `json:"readOnly"`
	Reason      string          `json:"reason,omitempty"`
}

type StorageOperation struct {
	Action              string   `json:"action"`
	Device              string   `json:"device"`
	Target              string   `json:"target,omitempty"`
	Filesystem          string   `json:"filesystem,omitempty"`
	Options             []string `json:"options,omitempty"`
	ExpectedFingerprint string   `json:"expectedFingerprint,omitempty"`
	Confirmation        string   `json:"confirmation,omitempty"`
}

type StorageState struct {
	Snapshot StorageSnapshot `json:"snapshot"`
	Action   string          `json:"action"`
	Applied  bool            `json:"applied"`
	Warning  string          `json:"warning,omitempty"`
}

type StorageCommandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

type storageAdapter struct {
	runner      StorageCommandRunner
	filesystems func() ([]Filesystem, error)
}

type systemStorageCommandRunner struct{}

func (systemStorageCommandRunner) Run(ctx context.Context, name string, arguments ...string) (string, error) {
	return runStorageCommand(ctx, name, arguments...)
}

func NewStorageStrategy(runner StorageCommandRunner) StorageStrategy {
	if runner == nil {
		runner = systemStorageCommandRunner{}
	}
	return &storageAdapter{runner: runner, filesystems: Filesystems}
}

type StorageStrategy interface {
	Read(context.Context) (StorageSnapshot, error)
	Apply(context.Context, StorageOperation) (StorageState, error)
}

func ReadStorageSnapshot(ctx context.Context) (StorageSnapshot, error) {
	return NewStorageStrategy(nil).Read(ctx)
}

func StorageSnapshotFromFilesystems(filesystems []Filesystem, reason string) StorageSnapshot {
	snapshot := StorageSnapshot{Filesystems: append([]Filesystem(nil), filesystems...), Devices: []StorageDevice{}, ReadOnly: true, Reason: reason}
	snapshot.Fingerprint = storageFingerprint(snapshot)
	return snapshot
}

func PreviewStorageOperation(ctx context.Context, operation StorageOperation) (StorageState, error) {
	operation.Action = "preview"
	if err := ValidateStorageOperation(operation); err != nil {
		return StorageState{}, err
	}
	snapshot, err := ReadStorageSnapshot(ctx)
	if err != nil {
		return StorageState{}, err
	}
	return StorageState{Snapshot: snapshot, Action: "preview", Warning: storagePreviewWarning(snapshot, operation)}, nil
}

func ApplyStorageOperation(ctx context.Context, operation StorageOperation) (StorageState, error) {
	return NewStorageStrategy(nil).Apply(ctx, operation)
}

func ValidateStorageOperation(operation StorageOperation) error {
	actions := map[string]bool{
		"preview": true, "mount": true, "unmount": true,
		"persistent-mount": true, "persistent-unmount": true,
	}
	if !actions[operation.Action] || !validStorageDevicePath(operation.Device) || len(operation.Filesystem) > 64 || len(operation.ExpectedFingerprint) > 128 || len(operation.Confirmation) > 128 {
		return ErrInvalidStorageOperation
	}
	if operation.Target != "" && !safeStorageTarget(operation.Target) {
		return ErrStorageUnsafe
	}
	if operation.Filesystem != "" && !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`).MatchString(operation.Filesystem) {
		return ErrInvalidStorageOperation
	}
	if len(operation.Options) > 32 {
		return ErrInvalidStorageOperation
	}
	for _, option := range operation.Options {
		if len(option) == 0 || len(option) > 128 || !regexp.MustCompile(`^[A-Za-z0-9_=+.:@/-]+$`).MatchString(option) {
			return ErrInvalidStorageOperation
		}
	}
	if operation.ExpectedFingerprint != "" && (len(operation.ExpectedFingerprint) != 64 || !isHex(operation.ExpectedFingerprint)) {
		return ErrInvalidStorageOperation
	}
	if operation.Action == "persistent-mount" && (operation.Target == "" || operation.Filesystem == "") {
		return ErrInvalidStorageOperation
	}
	if operation.Action == "mount" && operation.Target != "" {
		// UDisks2 chooses the authenticated user's mount location. A caller
		// must use persistent-mount when an exact target is required.
		return ErrInvalidStorageOperation
	}
	if operation.Action == "persistent-unmount" && operation.Target == "" {
		return ErrInvalidStorageOperation
	}
	if operation.Action == "preview" {
		return nil
	}
	if operation.ExpectedFingerprint == "" {
		return ErrInvalidStorageOperation
	}
	if operation.Action == "persistent-mount" || operation.Action == "persistent-unmount" {
		if operation.Confirmation != "CONFIRM PERSISTENT MOUNT" {
			return ErrInvalidStorageOperation
		}
	} else if operation.Confirmation != "CONFIRM STORAGE CHANGE" {
		return ErrInvalidStorageOperation
	}
	return nil
}

func (adapter *storageAdapter) Read(ctx context.Context) (StorageSnapshot, error) {
	readFilesystems := adapter.filesystems
	if readFilesystems == nil {
		readFilesystems = Filesystems
	}
	filesystems, err := readFilesystems()
	if err != nil {
		return StorageSnapshot{}, err
	}
	payload, err := adapter.runner.Run(ctx, "lsblk", "-J", "-b", "-o", "NAME,KNAME,PATH,TYPE,SIZE,RO,RM,MODEL,SERIAL,TRAN,FSTYPE,LABEL,UUID,MOUNTPOINTS,PKNAME")
	if err != nil && strings.TrimSpace(payload) == "" {
		return StorageSnapshot{}, errors.Join(ErrStorageUnavailable, err)
	}
	var document struct {
		BlockDevices []storageBlockDevice `json:"blockdevices"`
	}
	if json.Unmarshal([]byte(payload), &document) != nil {
		return StorageSnapshot{}, ErrStorageUnavailable
	}
	mountReadOnly := make(map[string]bool)
	for _, filesystem := range filesystems {
		for _, mount := range filesystem.Targets {
			mountReadOnly[mount.Target] = mount.ReadOnly
		}
	}
	devices := make([]StorageDevice, 0, len(document.BlockDevices))
	for _, raw := range document.BlockDevices {
		if raw.Type != "disk" || !validStorageDevicePath(raw.Path) {
			continue
		}
		device := StorageDevice{
			Path: raw.Path, Name: raw.Name, Type: raw.Type, Model: strings.TrimSpace(raw.Model),
			Serial: strings.TrimSpace(raw.Serial), Transport: strings.TrimSpace(raw.Transport), Size: raw.Size,
			ReadOnly: raw.RO != 0, Removable: raw.RM != 0, Partitions: []StoragePartition{},
		}
		flattenStoragePartitions(&device.Partitions, raw.Children, raw.Path, mountReadOnly)
		device.SMART = adapter.readSMART(ctx, device.Path)
		if strings.EqualFold(device.Transport, "nvme") || strings.Contains(device.Path, "nvme") {
			device.NVMe = adapter.readNVMe(ctx, device.Path)
		}
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Path < devices[j].Path })
	snapshot := StorageSnapshot{Filesystems: filesystems, Devices: devices}
	if _, statusErr := adapter.runner.Run(ctx, "udisksctl", "status"); statusErr != nil {
		snapshot.ReadOnly = true
		snapshot.Reason = "UDisks2 is unavailable; storage mutations are disabled."
	}
	snapshot.Fingerprint = storageFingerprint(snapshot)
	return snapshot, nil
}

type storageBlockDevice struct {
	Name        string               `json:"name"`
	KName       string               `json:"kname"`
	Path        string               `json:"path"`
	Type        string               `json:"type"`
	Size        uint64               `json:"size"`
	RO          int                  `json:"ro"`
	RM          int                  `json:"rm"`
	Model       string               `json:"model"`
	Serial      string               `json:"serial"`
	Transport   string               `json:"tran"`
	Filesystem  string               `json:"fstype"`
	Label       string               `json:"label"`
	UUID        string               `json:"uuid"`
	MountPoints []string             `json:"mountpoints"`
	Children    []storageBlockDevice `json:"children"`
}

func flattenStoragePartitions(result *[]StoragePartition, children []storageBlockDevice, parent string, readOnly map[string]bool) {
	for _, child := range children {
		path := child.Path
		if path == "" && child.KName != "" {
			path = "/dev/" + child.KName
		}
		if validStorageDevicePath(path) && child.Type == "part" {
			mounts := make([]MountPoint, 0, len(child.MountPoints))
			for _, target := range child.MountPoints {
				if target != "" {
					mounts = append(mounts, MountPoint{Target: target, ReadOnly: readOnly[target]})
				}
			}
			sort.Slice(mounts, func(i, j int) bool { return mounts[i].Target < mounts[j].Target })
			*result = append(*result, StoragePartition{Path: path, Name: child.Name, Size: child.Size, Filesystem: child.Filesystem, Label: child.Label, UUID: child.UUID, Parent: parent, ReadOnly: child.RO != 0, MountPoints: mounts})
		}
		flattenStoragePartitions(result, child.Children, path, readOnly)
	}
}

type smartDocument struct {
	SmartSupport struct {
		Available bool `json:"available"`
	} `json:"smart_support"`
	SmartStatus struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	Temperature struct {
		Current int `json:"current"`
	} `json:"temperature"`
	PowerOnTime struct {
		Hours uint64 `json:"hours"`
	} `json:"power_on_time"`
}

func (adapter *storageAdapter) readSMART(ctx context.Context, path string) *StorageHealth {
	payload, err := adapter.runner.Run(ctx, "smartctl", "-H", "-A", "-j", "--", path)
	health := &StorageHealth{}
	var document smartDocument
	if json.Unmarshal([]byte(payload), &document) == nil {
		health.Available = document.SmartSupport.Available || document.SmartStatus.Passed
		health.Passed = document.SmartStatus.Passed
		health.TemperatureC = document.Temperature.Current
		health.PowerOnHours = document.PowerOnTime.Hours
		health.Failing = health.Available && !health.Passed
		if health.Available {
			return health
		}
	}
	if err != nil {
		health.Reason = "SMART data unavailable."
	} else {
		health.Reason = "SMART data was not reported by device."
	}
	return health
}

type nvmeDocument struct {
	CriticalWarning int `json:"critical_warning"`
	Temperature     int `json:"temperature"`
	PercentageUsed  int `json:"percentage_used"`
}

func (adapter *storageAdapter) readNVMe(ctx context.Context, path string) *NVMeHealth {
	payload, err := adapter.runner.Run(ctx, "nvme", "smart-log", "-o", "json", "--", path)
	status := &NVMeHealth{}
	var document nvmeDocument
	if json.Unmarshal([]byte(payload), &document) == nil && (document.Temperature != 0 || document.PercentageUsed != 0 || document.CriticalWarning != 0) {
		status.Available = true
		status.TemperatureC = document.Temperature
		status.PercentageUsed = document.PercentageUsed
		status.CriticalWarning = document.CriticalWarning
		return status
	}
	if err != nil {
		status.Reason = "NVMe health data unavailable."
	} else {
		status.Reason = "NVMe health data was not reported by device."
	}
	return status
}

func storageFingerprint(snapshot StorageSnapshot) string {
	type partition struct {
		Path, Filesystem, Label, UUID, Parent string
		Size                                  uint64
		ReadOnly                              bool
		Mounts                                []string
	}
	type device struct {
		Path, Name, Type, Model, Serial, Transport string
		Size                                       uint64
		ReadOnly, Removable                        bool
		Partitions                                 []partition
	}
	devices := make([]device, 0, len(snapshot.Devices))
	for _, item := range snapshot.Devices {
		partitions := make([]partition, 0, len(item.Partitions))
		for _, part := range item.Partitions {
			mounts := make([]string, 0, len(part.MountPoints))
			for _, mount := range part.MountPoints {
				mounts = append(mounts, mount.Target)
			}
			sort.Strings(mounts)
			partitions = append(partitions, partition{part.Path, part.Filesystem, part.Label, part.UUID, part.Parent, part.Size, part.ReadOnly, mounts})
		}
		sort.Slice(partitions, func(i, j int) bool { return partitions[i].Path < partitions[j].Path })
		devices = append(devices, device{item.Path, item.Name, item.Type, item.Model, item.Serial, item.Transport, item.Size, item.ReadOnly, item.Removable, partitions})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Path < devices[j].Path })
	payload, _ := json.Marshal(struct {
		Filesystems []Filesystem
		Devices     []device
	}{snapshot.Filesystems, devices})
	return fingerprintBytes(payload)
}

func (adapter *storageAdapter) Apply(ctx context.Context, operation StorageOperation) (StorageState, error) {
	if err := ValidateStorageOperation(operation); err != nil {
		return StorageState{}, err
	}
	current, err := adapter.Read(ctx)
	if err != nil {
		return StorageState{}, err
	}
	if operation.Action == "preview" {
		return StorageState{Snapshot: current, Action: "preview", Warning: storagePreviewWarning(current, operation)}, nil
	}
	if current.ReadOnly {
		return StorageState{}, ErrStorageUnavailable
	}
	if current.Fingerprint != operation.ExpectedFingerprint {
		return StorageState{}, ErrStorageConflict
	}
	partition := findStoragePartition(current, operation.Device)
	if partition == nil {
		return StorageState{}, ErrStorageUnavailable
	}
	if partition.ReadOnly || currentDeviceReadOnly(current, operation.Device) {
		return StorageState{}, ErrStorageUnsafe
	}
	needsUnmount := false
	if operation.Action == "unmount" || operation.Action == "persistent-unmount" {
		target := operation.Target
		if target == "" && len(partition.MountPoints) > 0 {
			target = partition.MountPoints[0].Target
		}
		if target == "" || protectedStorageTarget(target) {
			return StorageState{}, ErrStorageUnsafe
		}
		needsUnmount = storagePartitionHasMount(partition, target)
		if operation.Action == "unmount" && !needsUnmount {
			return StorageState{}, ErrStorageConflict
		}
		if needsUnmount && storageTargetBusy(ctx, adapter.runner, target) {
			return StorageState{}, ErrStorageBusy
		}
	}
	var restore func() error
	mutationStarted := false
	defer func() {
		if err == nil {
			return
		}
		if mutationStarted {
			_, _ = adapter.runner.Run(ctx, "udisksctl", "unmount", "--no-user-interaction", "-b", operation.Device)
		}
		if restore != nil {
			_ = restore()
		}
	}()
	if operation.Action == "persistent-mount" {
		restore, err = addStorageFstabEntry(operation, *partition)
		if err != nil {
			return StorageState{}, err
		}
	} else if operation.Action == "persistent-unmount" {
		restore, err = removeStorageFstabEntry(operation, *partition)
		if err != nil {
			return StorageState{}, err
		}
	}
	if operation.Action != "persistent-unmount" || needsUnmount {
		arguments, argumentErr := storageMutationArguments(operation)
		if argumentErr != nil {
			return StorageState{}, argumentErr
		}
		if _, err = adapter.runner.Run(ctx, "udisksctl", arguments...); err != nil {
			return StorageState{}, errors.Join(ErrStorageUnavailable, err)
		}
		mutationStarted = true
	}
	updated, err := adapter.Read(ctx)
	if err != nil {
		return StorageState{}, err
	}
	if err = verifyStorageMutation(current, updated, operation); err != nil {
		return StorageState{}, err
	}
	warning := "Storage state changed and was re-read after UDisks2 completed."
	if operation.Action == "persistent-mount" {
		warning = "Mount was added to /etc/fstab with a Tako ownership marker and activated through UDisks2."
	}
	if operation.Action == "persistent-unmount" {
		warning = "Tako-owned /etc/fstab entry was removed; the filesystem was unmounted through UDisks2 when it was active."
	}
	return StorageState{Snapshot: updated, Action: operation.Action, Applied: true, Warning: warning}, nil
}

func storagePreviewWarning(snapshot StorageSnapshot, operation StorageOperation) string {
	if snapshot.ReadOnly {
		return snapshot.Reason
	}
	if operation.Action == "persistent-mount" || operation.Action == "persistent-unmount" {
		return "Persistent mount changes edit only Tako-owned /etc/fstab entries and require an explicit confirmation."
	}
	return "Mount changes use UDisks2, reject protected targets, and verify state after completion."
}

func storageMutationArguments(operation StorageOperation) ([]string, error) {
	if operation.Action == "mount" || operation.Action == "persistent-mount" {
		arguments := []string{"mount", "--no-user-interaction", "-b", operation.Device}
		if len(operation.Options) > 0 {
			arguments = append(arguments, "--options", strings.Join(operation.Options, ","))
		}
		return arguments, nil
	}
	if operation.Action == "unmount" || operation.Action == "persistent-unmount" {
		return []string{"unmount", "--no-user-interaction", "-b", operation.Device}, nil
	}
	return nil, ErrInvalidStorageOperation
}

func verifyStorageMutation(before, after StorageSnapshot, operation StorageOperation) error {
	beforePartition := findStoragePartition(before, operation.Device)
	afterPartition := findStoragePartition(after, operation.Device)
	if beforePartition == nil || afterPartition == nil {
		return ErrStorageUnavailable
	}
	switch operation.Action {
	case "mount", "persistent-mount":
		if operation.Target != "" && !storagePartitionHasMount(afterPartition, operation.Target) {
			return ErrStorageConflict
		}
		if len(afterPartition.MountPoints) == 0 {
			return ErrStorageConflict
		}
	case "unmount", "persistent-unmount":
		if operation.Target != "" && storagePartitionHasMount(afterPartition, operation.Target) {
			return ErrStorageConflict
		}
		if operation.Target == "" && len(afterPartition.MountPoints) >= len(beforePartition.MountPoints) {
			return ErrStorageConflict
		}
	}
	return nil
}

func findStoragePartition(snapshot StorageSnapshot, path string) *StoragePartition {
	for deviceIndex := range snapshot.Devices {
		for partitionIndex := range snapshot.Devices[deviceIndex].Partitions {
			partition := &snapshot.Devices[deviceIndex].Partitions[partitionIndex]
			if partition.Path == path {
				return partition
			}
		}
	}
	return nil
}

func storagePartitionHasMount(partition *StoragePartition, target string) bool {
	for _, mount := range partition.MountPoints {
		if mount.Target == target {
			return true
		}
	}
	return false
}

func currentDeviceReadOnly(snapshot StorageSnapshot, path string) bool {
	for _, device := range snapshot.Devices {
		if device.Path == path {
			return device.ReadOnly
		}
	}
	return false
}

func validStorageDevicePath(path string) bool {
	return strings.HasPrefix(path, "/dev/") && filepath.Clean(path) == path && filepath.IsAbs(path) && !strings.ContainsAny(path, "\x00\r\n") && !strings.Contains(path, "..")
}

func safeStorageTarget(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n") && !protectedStorageTarget(path) && !filePathWithin("/proc", path) && !filePathWithin("/sys", path) && !filePathWithin("/dev", path)
}

func protectedStorageTarget(path string) bool {
	clean := filepath.Clean(path)
	return clean == "/" || clean == "/boot" || clean == "/boot/efi"
}

func storageTargetBusy(ctx context.Context, runner StorageCommandRunner, target string) bool {
	payload, _ := runner.Run(ctx, "fuser", "-m", "--", target)
	return strings.TrimSpace(payload) != ""
}

var storageConfigRoot string

func storageConfigPath(path string) string {
	if storageConfigRoot == "" {
		return path
	}
	return filepath.Join(storageConfigRoot, strings.TrimPrefix(path, string(filepath.Separator)))
}

func readStorageFstab() ([]byte, error) {
	path := storageConfigPath("/etc/fstab")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []byte{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, ErrStorageUnavailable
	}
	return data, nil
}

func writeStorageFstab(data []byte) error {
	path := storageConfigPath("/etc/fstab")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tako-fstab-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func storageFstabSource(partition StoragePartition) string {
	if partition.UUID != "" {
		return "UUID=" + partition.UUID
	}
	return partition.Path
}

func addStorageFstabEntry(operation StorageOperation, partition StoragePartition) (func() error, error) {
	data, err := readStorageFstab()
	if err != nil {
		return nil, err
	}
	source := storageFstabSource(partition)
	target := operation.Target
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && !strings.HasPrefix(fields[0], "#") && (fields[1] == target || fields[0] == source) {
			return nil, ErrStorageConflict
		}
	}
	options := append([]string{}, operation.Options...)
	if len(options) == 0 {
		options = []string{"defaults"}
	}
	options = append(options, "x-tako-managed")
	line := source + "\t" + target + "\t" + operation.Filesystem + "\t" + strings.Join(options, ",") + "\t0 0\n"
	old := append([]byte(nil), data...)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	data = append(data, []byte(line)...)
	if err := writeStorageFstab(data); err != nil {
		return nil, err
	}
	return func() error { return writeStorageFstab(old) }, nil
}

func removeStorageFstabEntry(operation StorageOperation, partition StoragePartition) (func() error, error) {
	data, err := readStorageFstab()
	if err != nil {
		return nil, err
	}
	source := storageFstabSource(partition)
	lines := strings.Split(string(data), "\n")
	kept := make([]string, 0, len(lines))
	found := false
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == source && fields[1] == operation.Target && strings.Contains(fields[3], "x-tako-managed") {
			found = true
			continue
		}
		kept = append(kept, line)
	}
	if !found {
		return nil, ErrStorageConflict
	}
	old := append([]byte(nil), data...)
	if err := writeStorageFstab([]byte(strings.Join(kept, "\n"))); err != nil {
		return nil, err
	}
	return func() error { return writeStorageFstab(old) }, nil
}

func runStorageCommand(ctx context.Context, name string, arguments ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, name, arguments...)
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := command.Start(); err != nil {
		return "", err
	}
	payload, readErr := readBounded(stdout, 2<<20)
	if readErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if readErr != nil {
		return "", readErr
	}
	if commandCtx.Err() != nil {
		return "", commandCtx.Err()
	}
	return string(payload), waitErr
}

func storageErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidStorageOperation):
		return "invalid-storage-operation"
	case errors.Is(err, ErrStorageConflict):
		return "storage-conflict"
	case errors.Is(err, ErrStorageUnsafe):
		return "storage-unsafe"
	case errors.Is(err, ErrStorageBusy):
		return "storage-busy"
	case errors.Is(err, ErrStorageUnavailable):
		return "storage-unavailable"
	default:
		return "storage-operation-failed"
	}
}
