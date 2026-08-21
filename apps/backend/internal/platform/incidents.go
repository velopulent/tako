package platform

import (
	"context"
	"errors"
	"strings"
	"time"
)

type IncidentEvent struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Severity  string    `json:"severity"`
	Kind      string    `json:"kind"`
	Source    string    `json:"source,omitempty"`
	Summary   string    `json:"summary"`
	Cursor    string    `json:"cursor,omitempty"`
}

type IncidentTimeline struct {
	Items    []IncidentEvent `json:"items"`
	Since    time.Time       `json:"since"`
	Until    time.Time       `json:"until"`
	Partial  bool            `json:"partial"`
	Warnings []string        `json:"warnings,omitempty"`
}

func CorrelateIncidents(ctx context.Context, since, until time.Time) (IncidentTimeline, error) {
	return CorrelateIncidentsWithLogs(ctx, since, until, QueryLogs)
}

func CorrelateIncidentsWithLogs(ctx context.Context, since, until time.Time, queryLogs func(context.Context, JournalQuery) (JournalPage, error)) (IncidentTimeline, error) {
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	if until.IsZero() {
		until = time.Now()
	}
	if since.After(until) || until.Sub(since) > 7*24*time.Hour {
		return IncidentTimeline{}, errors.New("incident window is invalid")
	}
	if queryLogs == nil {
		queryLogs = QueryLogs
	}
	page, err := queryLogs(ctx, JournalQuery{Limit: 500, Since: since, Until: until, Details: false})
	if err != nil {
		return IncidentTimeline{}, err
	}
	result := IncidentTimeline{Items: []IncidentEvent{}, Since: since, Until: until, Partial: page.NextCursor != ""}
	for _, item := range page.Items {
		kind, severity := classifyIncident(item)
		if kind == "" {
			continue
		}
		timestamp, parseErr := time.Parse(time.RFC3339Nano, item.Timestamp)
		if parseErr != nil {
			timestamp = time.Now().UTC()
		}
		id := item.Cursor
		if id == "" {
			id = timestamp.Format(time.RFC3339Nano) + ":" + item.Message
		}
		result.Items = append(result.Items, IncidentEvent{ID: id, Timestamp: timestamp, Severity: severity, Kind: kind, Source: item.Unit, Summary: boundedText(item.Message, 512), Cursor: item.Cursor})
		if len(result.Items) >= 500 {
			result.Partial = true
			break
		}
	}
	if result.Partial {
		result.Warnings = append(result.Warnings, "Only the first bounded journal page was correlated; older evidence remains in the journal browser.")
	}
	return result, nil
}

func classifyIncident(entry LogEntry) (string, string) {
	message := strings.ToLower(entry.Message + " " + entry.Unit)
	if strings.Contains(message, "oom") || strings.Contains(message, "out of memory") {
		return "memory", "critical"
	}
	if strings.Contains(message, "segfault") || strings.Contains(message, "panic") || strings.Contains(message, "kernel bug") {
		return "crash", "critical"
	}
	if strings.Contains(message, "denied") || strings.Contains(message, "avc:") || strings.Contains(message, "apparmor") {
		return "security-denial", "warning"
	}
	if strings.Contains(message, "networkmanager") || strings.Contains(message, "link is down") || strings.Contains(message, "carrier") {
		return "network", "warning"
	}
	if strings.Contains(message, "failed") || strings.Contains(message, "failure") || strings.Contains(message, "error") {
		return "service-failure", "error"
	}
	return "", ""
}

func boundedText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}
