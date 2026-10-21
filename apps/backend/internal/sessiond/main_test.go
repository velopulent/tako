package sessiond

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/velopulent/tako/internal/audit"
)

// TestMain keeps audit records from test operations out of the host journal.
func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "tako-audit-")
	if err != nil {
		panic(err)
	}
	restore := audit.UseSocket(filepath.Join(directory, "absent.socket"))
	code := m.Run()
	restore()
	_ = os.RemoveAll(directory)
	os.Exit(code)
}
