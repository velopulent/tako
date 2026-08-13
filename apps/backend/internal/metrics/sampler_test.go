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
