package platform

import "testing"

func TestParseLogEntry(t *testing.T) {
	entry, ok := parseLogEntry([]byte(`{"__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"3","_SYSTEMD_UNIT":"demo.service","MESSAGE":"failed safely"}`))
	if !ok || entry.Unit != "demo.service" || entry.Priority != "3" || entry.Message != "failed safely" {
		t.Fatalf("unexpected entry: %+v ok=%v", entry, ok)
	}
	if _, ok := parseLogEntry([]byte(`not json`)); ok {
		t.Fatal("invalid journal row accepted")
	}
}
