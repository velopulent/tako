package sessiond

import (
	"os"
	"testing"
)

// TestTransientServiceKeepsStderrOffItsStdioSocket pins the properties that
// keep a transient service's logs out of its protocol socket. systemd copies
// stdout onto stderr whenever StandardOutput= and StandardError= name the same
// target, even though the socket descriptor overrides StandardOutput=. The
// integration tests check the real manager.
func TestTransientServiceKeepsStderrOffItsStdioSocket(t *testing.T) {
	properties := map[string]any{}
	for _, property := range transientServiceProperties("test", []string{"/bin/true"}, os.Stdin) {
		properties[property.Name] = property.Value.Value()
	}
	for _, name := range []string{"StandardInputFileDescriptor", "StandardOutputFileDescriptor"} {
		if _, ok := properties[name]; !ok {
			t.Errorf("%s is not set", name)
		}
	}
	if properties["StandardError"] != "journal" {
		t.Errorf("StandardError = %v, want journal", properties["StandardError"])
	}
	if output, ok := properties["StandardOutput"]; !ok || output == properties["StandardError"] {
		t.Errorf("StandardOutput = %v alongside StandardError = %v: systemd would copy the socket onto stderr", output, properties["StandardError"])
	}
}
