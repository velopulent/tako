package platform

import (
	"context"
	"testing"
	"time"
)

func TestCorrelateIncidentsGroupsRepeatedMessages(t *testing.T) {
	until := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	var seen JournalQuery
	timeline, err := CorrelateIncidents(context.Background(), time.Time{}, until, func(_ context.Context, query JournalQuery) (JournalPage, error) {
		seen = query
		return JournalPage{Items: []LogEntry{
			{Timestamp: "2026-01-02T11:00:00Z", Priority: "3", Unit: "app.service", Message: "worker 12 failed: code 7"},
			{Timestamp: "2026-01-02T10:00:00Z", Priority: "3", Unit: "app.service", Message: "worker 9 failed: code 7"},
			{Timestamp: "2026-01-02T09:00:00Z", Priority: "2", Unit: "kernel", Message: "Out of memory: Killed process 4411"},
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := seen.Validate(); err != nil || seen.Priority != "0..3" || !seen.Since.Equal(until.Add(-24*time.Hour)) {
		t.Fatalf("query = %+v", seen)
	}
	if len(timeline.Items) != 2 {
		t.Fatalf("items = %+v", timeline.Items)
	}
	first := timeline.Items[0]
	if first.Count != 2 || first.Summary != "worker 12 failed: code 7" || !first.FirstSeen.Equal(until.Add(-2*time.Hour)) || first.Severity != "error" {
		t.Fatalf("group = %+v", first)
	}
	if second := timeline.Items[1]; second.Kind != "memory" || second.Severity != "critical" {
		t.Fatalf("oom = %+v", second)
	}
}

func TestCorrelateIncidentsRejectsLongWindows(t *testing.T) {
	until := time.Now()
	_, err := CorrelateIncidents(context.Background(), until.Add(-8*24*time.Hour), until, func(context.Context, JournalQuery) (JournalPage, error) {
		return JournalPage{}, nil
	})
	if err != ErrInvalidIncidentWindow {
		t.Fatalf("err = %v", err)
	}
}
