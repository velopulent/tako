package sessiond

import (
	"strings"
	"testing"
)

func TestBoundedPolicyOutputNeverGrowsPastLimit(t *testing.T) {
	output := &boundedOutput{limit: 8}
	payload := strings.Repeat("x", 1024)
	if written, err := output.Write([]byte(payload)); err != nil || written != len(payload) {
		t.Fatalf("bounded write returned %d, %v", written, err)
	}
	if output.Len() != 8 || !output.overflow {
		t.Fatalf("output was not bounded: len=%d overflow=%v", output.Len(), output.overflow)
	}
}
