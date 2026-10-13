package platform

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Incident groups repeated journal errors that share a source and message
// shape. The ID is stable across reads so acknowledgements survive refreshes.
type Incident struct {
	ID        string    `json:"id"`
	Severity  string    `json:"severity"`
	Kind      string    `json:"kind"`
	Source    string    `json:"source,omitempty"`
	Summary   string    `json:"summary"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	State     string    `json:"state"`
}

type IncidentTimeline struct {
	Items   []Incident `json:"items"`
	Since   time.Time  `json:"since"`
	Until   time.Time  `json:"until"`
	Partial bool       `json:"partial"`
}

const (
	incidentScanLimit = MaxJournalPageSize
	incidentMaxGroups = 200
)

var (
	ErrInvalidIncidentWindow = errors.New("incident window is invalid")
	incidentVolatile         = regexp.MustCompile(`0x[0-9a-fA-F]+|\b[0-9a-fA-F]{8,}\b|\d+`)
)

// CorrelateIncidents reads error-priority journal entries in the window and
// groups them, most recent first.
func CorrelateIncidents(ctx context.Context, since, until time.Time, queryLogs func(context.Context, JournalQuery) (JournalPage, error)) (IncidentTimeline, error) {
	now := time.Now()
	if until.IsZero() {
		until = now
	}
	if since.IsZero() {
		since = until.Add(-24 * time.Hour)
	}
	if !since.Before(until) || until.Sub(since) > 7*24*time.Hour {
		return IncidentTimeline{}, ErrInvalidIncidentWindow
	}
	page, err := queryLogs(ctx, JournalQuery{Limit: incidentScanLimit, Since: since, Until: until, Priority: "0..3"})
	if err != nil {
		return IncidentTimeline{}, err
	}
	groups := map[string]*Incident{}
	result := IncidentTimeline{Items: []Incident{}, Since: since, Until: until, Partial: page.NextCursor != ""}
	for _, entry := range page.Items {
		timestamp, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			continue
		}
		key := entry.Unit + "\x00" + incidentVolatile.ReplaceAllString(strings.TrimSpace(entry.Message), "#")
		group, ok := groups[key]
		if !ok {
			if len(groups) >= incidentMaxGroups {
				result.Partial = true
				continue
			}
			group = &Incident{ID: fingerprintBytes([]byte(key))[:24], Kind: classifyIncident(entry), Severity: incidentSeverity(entry.Priority), Source: entry.Unit, Summary: boundedText(entry.Message, 512), FirstSeen: timestamp, LastSeen: timestamp, State: "open"}
			groups[key] = group
		}
		group.Count++
		if timestamp.Before(group.FirstSeen) {
			group.FirstSeen = timestamp
		}
		if timestamp.After(group.LastSeen) {
			group.LastSeen = timestamp
			group.Summary = boundedText(entry.Message, 512)
		}
	}
	for _, group := range groups {
		result.Items = append(result.Items, *group)
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].LastSeen.After(result.Items[j].LastSeen) })
	return result, nil
}

func incidentSeverity(priority string) string {
	switch priority {
	case "0", "1", "2":
		return "critical"
	default:
		return "error"
	}
}

func classifyIncident(entry LogEntry) string {
	message := strings.ToLower(entry.Message)
	unit := strings.ToLower(entry.Unit)
	switch {
	case strings.Contains(message, "out of memory") || strings.Contains(message, "oom"):
		return "memory"
	case strings.Contains(message, "segfault") || strings.Contains(message, "core dump") || strings.Contains(message, "panic") || strings.Contains(unit, "systemd-coredump"):
		return "crash"
	case strings.Contains(message, "i/o error") || strings.Contains(message, "ext4-fs error") || strings.Contains(message, "btrfs") || strings.Contains(message, "nvme") || strings.Contains(message, "smart"):
		return "storage"
	case strings.Contains(message, "avc:") || strings.Contains(message, "apparmor") || strings.Contains(message, "authentication failure"):
		return "security"
	case strings.Contains(unit, "networkmanager") || strings.Contains(unit, "networkd") || strings.Contains(message, "link is down"):
		return "network"
	case strings.Contains(message, "failed with result") || strings.Contains(message, "failed to start"):
		return "service"
	default:
		return "other"
	}
}

func boundedText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}
