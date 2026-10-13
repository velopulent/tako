package sessiond

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/velopulent/tako/internal/auth"
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

func TestPolkitSubjectNamesProcessStartAndUID(t *testing.T) {
	subject, err := polkitSubject(auth.Identity{UID: 1234})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(subject, ",")
	if len(parts) != 3 || parts[0] != strconv.Itoa(os.Getpid()) || parts[2] != "1234" {
		t.Fatalf("subject = %q", subject)
	}
	if _, err := strconv.ParseUint(parts[1], 10, 64); err != nil {
		t.Fatalf("start time = %q", parts[1])
	}
}
