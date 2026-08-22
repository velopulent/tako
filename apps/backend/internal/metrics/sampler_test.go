package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadMemoryUsesAvailableMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte("MemTotal: 1000 kB\nMemAvailable: 250 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	used, total, swapUsed, swapTotal, err := readMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	if used != 750*1024 || total != 1000*1024 {
		t.Fatalf("unexpected memory values: used=%d total=%d", used, total)
	}
	if swapUsed != 0 || swapTotal != 0 {
		t.Fatalf("unexpected swap values")
	}
}

func TestHistorySinceDownsamplesAndKeepsNewest(t *testing.T) {
	now := time.Now()
	sampler := NewSampler(20)
	for index := 0; index < 10; index++ {
		sampler.samples = append(sampler.samples, Sample{Timestamp: now.Add(time.Duration(index) * time.Minute), CPUPercent: float64(index)})
	}
	result := sampler.HistorySince(now.Add(2*time.Minute), 4)
	if len(result) != 4 || result[0].CPUPercent != 2 || result[3].CPUPercent != 9 {
		t.Fatalf("unexpected downsample: %#v", result)
	}
}

func TestReadNetworkExcludesLoopback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netdev")
	payload := "Inter-| Receive | Transmit\n lo: 100 0 0 0 0 0 0 0 200 0 0 0 0 0 0 0\neth0: 300 0 0 0 0 0 0 0 400 0 0 0 0 0 0 0\n"
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	rx, tx, interfaces, err := readNetwork(path)
	if err != nil {
		t.Fatal(err)
	}
	if rx != 300 || tx != 400 {
		t.Fatalf("unexpected network values: rx=%d tx=%d", rx, tx)
	}
	if interfaces["eth0"].RX != 300 {
		t.Fatalf("interface counters missing")
	}
}

func TestReadDisksCountsWholeDisksOnce(t *testing.T) {
	dir := t.TempDir()
	stats := filepath.Join(dir, "diskstats")
	payload := "" +
		"   8       0 sda 100 0 2000 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
		"   8       1 sda1 40 0 800 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
		"   8      16 sdb 50 0 1000 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
		"   7       0 loop0 99 0 9999 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
		" 259       0 nvme0n1 70 0 1400 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
		" 259       1 nvme0n1p1 30 0 600 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
		" 252       0 dm-0 80 0 1600 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
		" 254       0 zram0 60 0 1200 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
		"   9       0 md0 20 0 400 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
		"   8      32 sdc 10 0 200 0 0 0 0 0 0 0 0 0 0 0 0 0\n"
	if err := os.WriteFile(stats, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	block := filepath.Join(dir, "block")
	for _, name := range []string{"sda", "sdb", "sdc", "nvme0n1", "dm-0", "zram0", "md0", "loop0"} {
		if err := os.MkdirAll(filepath.Join(block, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(block, "md0", "slaves"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../sda", filepath.Join(block, "md0", "slaves", "sda")); err != nil {
		t.Fatal(err)
	}
	disks, err := readDisks(stats, block)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := disks["nvme0n1p1"]; ok {
		t.Fatal("partition counted as whole disk")
	}
	for _, excluded := range []string{"loop0", "zram0", "dm-0", "md0", "sda"} {
		if _, ok := disks[excluded]; ok {
			t.Fatalf("%s should be excluded", excluded)
		}
	}
	if disks["nvme0n1"].Read != 1400*512 || disks["sdb"].Read != 1000*512 {
		t.Fatalf("unexpected counters: %+v", disks)
	}
}
