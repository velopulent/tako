package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMemoryUsesAvailableMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte("MemTotal: 1000 kB\nMemAvailable: 250 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	used, total, err := readMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	if used != 750*1024 || total != 1000*1024 {
		t.Fatalf("unexpected memory values: used=%d total=%d", used, total)
	}
}

func TestReadNetworkExcludesLoopback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netdev")
	payload := "Inter-| Receive | Transmit\n lo: 100 0 0 0 0 0 0 0 200 0 0 0 0 0 0 0\neth0: 300 0 0 0 0 0 0 0 400 0 0 0 0 0 0 0\n"
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	rx, tx, err := readNetwork(path)
	if err != nil {
		t.Fatal(err)
	}
	if rx != 300 || tx != 400 {
		t.Fatalf("unexpected network values: rx=%d tx=%d", rx, tx)
	}
}
