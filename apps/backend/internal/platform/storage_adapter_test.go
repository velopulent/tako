package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type storageFixtureRunner struct {
	mounted bool
	calls   []string
}

func (runner *storageFixtureRunner) Run(_ context.Context, name string, arguments ...string) (string, error) {
	runner.calls = append(runner.calls, name+" "+strings.Join(arguments, " "))
	switch name {
	case "lsblk":
		mount := "null"
		if runner.mounted {
			mount = `"/mnt/data"`
		}
		return `{"blockdevices":[{"name":"sda","path":"/dev/sda","type":"disk","size":1000000,"ro":0,"rm":0,"tran":"sata","children":[{"name":"sda1","path":"/dev/sda1","type":"part","size":900000,"ro":0,"fstype":"ext4","uuid":"abcd-1234","mountpoints":[` + mount + `]}]}]}`, nil
	case "smartctl":
		return `{"smart_support":{"available":true},"smart_status":{"passed":true},"temperature":{"current":32},"power_on_time":{"hours":12}}`, nil
	case "udisksctl":
		if len(arguments) > 0 && arguments[0] == "mount" {
			runner.mounted = true
		}
		return "", nil
	case "fuser":
		return "", nil
	default:
		return "", errors.New("unexpected command")
	}
}

func TestStorageReadInventoryAndHealth(t *testing.T) {
	runner := &storageFixtureRunner{}
	adapter := &storageAdapter{
		runner: runner,
		filesystems: func() ([]Filesystem, error) {
			return []Filesystem{{Device: "/dev/sda1", Type: "ext4", Targets: []MountPoint{}}}, nil
		},
	}
	snapshot, err := adapter.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Devices) != 1 || len(snapshot.Devices[0].Partitions) != 1 {
		t.Fatalf("inventory = %+v", snapshot.Devices)
	}
	partition := snapshot.Devices[0].Partitions[0]
	if partition.Path != "/dev/sda1" || partition.Filesystem != "ext4" {
		t.Fatalf("partition = %+v", partition)
	}
	if snapshot.Devices[0].SMART == nil || !snapshot.Devices[0].SMART.Passed || snapshot.Devices[0].SMART.TemperatureC != 32 {
		t.Fatalf("SMART = %+v", snapshot.Devices[0].SMART)
	}
	if len(snapshot.Fingerprint) != 64 {
		t.Fatalf("fingerprint = %q", snapshot.Fingerprint)
	}
}

func TestStorageApplyChecksFingerprintAndVerifiesMount(t *testing.T) {
	runner := &storageFixtureRunner{}
	adapter := &storageAdapter{runner: runner}
	adapter.filesystems = func() ([]Filesystem, error) {
		mounts := []MountPoint{}
		if runner.mounted {
			mounts = []MountPoint{{Target: "/mnt/data"}}
		}
		return []Filesystem{{Device: "/dev/sda1", Type: "ext4", Targets: mounts}}, nil
	}
	before, err := adapter.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, err := adapter.Apply(context.Background(), StorageOperation{
		Action:              "mount",
		Device:              "/dev/sda1",
		ExpectedFingerprint: before.Fingerprint,
		Confirmation:        "CONFIRM STORAGE CHANGE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !state.Applied || len(state.Snapshot.Devices[0].Partitions[0].MountPoints) != 1 {
		t.Fatalf("state = %+v", state)
	}
	if _, err := adapter.Apply(context.Background(), StorageOperation{
		Action:              "mount",
		Device:              "/dev/sda1",
		ExpectedFingerprint: strings.Repeat("0", 64),
		Confirmation:        "CONFIRM STORAGE CHANGE",
	}); !errors.Is(err, ErrStorageConflict) {
		t.Fatalf("stale fingerprint error = %v", err)
	}
}

func TestStorageFstabChangesAreTakoOwnedAndReversible(t *testing.T) {
	root := t.TempDir()
	previous := storageConfigRoot
	storageConfigRoot = root
	defer func() { storageConfigRoot = previous }()
	partition := StoragePartition{Path: "/dev/sda1", UUID: "abcd-1234"}
	operation := StorageOperation{Action: "persistent-mount", Device: partition.Path, Target: "/mnt/data", Filesystem: "ext4"}
	restore, err := addStorageFstabEntry(operation, partition)
	if err != nil {
		t.Fatal(err)
	}
	data, err := readStorageFstab()
	if err != nil || !strings.Contains(string(data), "UUID=abcd-1234\t/mnt/data\text4") || !strings.Contains(string(data), "x-tako-managed") {
		t.Fatalf("fstab = %q, err=%v", data, err)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	operation.Action = "persistent-unmount"
	if _, err := removeStorageFstabEntry(operation, partition); !errors.Is(err, ErrStorageConflict) {
		t.Fatalf("unowned removal error = %v", err)
	}
}

func TestStorageValidationProtectsPersistentOperations(t *testing.T) {
	if err := ValidateStorageOperation(StorageOperation{Action: "persistent-mount", Device: "/dev/sda1", Target: "/mnt/data", Confirmation: "CONFIRM PERSISTENT MOUNT"}); !errors.Is(err, ErrInvalidStorageOperation) {
		t.Fatalf("missing filesystem error = %v", err)
	}
	if err := ValidateStorageOperation(StorageOperation{Action: "persistent-unmount", Device: "/dev/sda1", Confirmation: "CONFIRM PERSISTENT MOUNT"}); !errors.Is(err, ErrInvalidStorageOperation) {
		t.Fatalf("missing target error = %v", err)
	}
	if err := ValidateStorageOperation(StorageOperation{Action: "persistent-mount", Device: "/dev/sda1", Target: "/boot", Filesystem: "ext4", ExpectedFingerprint: strings.Repeat("0", 64), Confirmation: "CONFIRM PERSISTENT MOUNT"}); !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("protected target error = %v", err)
	}
}
