package app

import (
	"path/filepath"
	"testing"
	"time"
)

func TestIncidentAcknowledgementsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acks.json")
	now := time.Now()
	store := newIncidentAcks(path)
	if err := store.set("disk-full", true, now); err != nil {
		t.Fatal(err)
	}
	if err := store.set("stale", true, now.Add(-incidentAckRetention-time.Hour)); err != nil {
		t.Fatal(err)
	}

	restarted := newIncidentAcks(path)
	if _, ok := restarted.times["disk-full"]; !ok {
		t.Fatal("acknowledgement was lost across a restart")
	}
	if _, ok := restarted.times["stale"]; ok {
		t.Fatal("expired acknowledgement was kept")
	}
	if err := restarted.set("disk-full", false, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := newIncidentAcks(path).times["disk-full"]; ok {
		t.Fatal("reopened incident stayed acknowledged after a restart")
	}
}
