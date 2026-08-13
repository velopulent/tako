package platform

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	MaxLoginHistoryPageSize  = 100
	maxLoginHistoryScanPages = 4
)

var (
	ErrInvalidLoginHistoryQuery = errors.New("invalid login history query")
)

// LoginHistoryQuery describes a bounded, read-only view over journal entries.
// Cursor is the decoded journal cursor; HTTP callers should use
// DecodeJournalCursor before constructing this value.
type LoginHistoryQuery struct {
	Username string
	Limit    int
	Cursor   string
	Since    time.Time
	Until    time.Time
	Outcome  string
}

type LoginHistoryIdentity struct {
	Username string `json:"username"`
	Source   string `json:"source"`
	Present  bool   `json:"present"`
}

type LoginHistoryEntry struct {
	Timestamp string `json:"timestamp"`
	Event     string `json:"event"`
	Outcome   string `json:"outcome"`
	Service   string `json:"service"`
	Remote    string `json:"remote,omitempty"`
	Session   string `json:"session,omitempty"`
}

type LoginHistoryPage struct {
	Identity   LoginHistoryIdentity `json:"identity"`
	Items      []LoginHistoryEntry  `json:"items"`
	NextCursor string               `json:"nextCursor,omitempty"`
}

func (query LoginHistoryQuery) Validate() error {
	if !validNSSName(query.Username) || len(query.Username) > 256 {
		return ErrInvalidLoginHistoryQuery
	}
	if query.Limit < 1 || query.Limit > MaxLoginHistoryPageSize {
		return ErrInvalidLoginHistoryQuery
	}
	if query.Cursor != "" && (len(query.Cursor) > MaxJournalCursor || !validJournalCursor(query.Cursor)) {
		return ErrInvalidLoginHistoryQuery
	}
	if !query.Since.IsZero() && !query.Until.IsZero() && query.Since.After(query.Until) {
		return ErrInvalidLoginHistoryQuery
	}
	if query.Outcome != "" && query.Outcome != "success" && query.Outcome != "failure" {
		return ErrInvalidLoginHistoryQuery
	}
	return nil
}

type loginHistoryQueryLogs func(context.Context, JournalQuery) (JournalPage, error)
type loginHistoryIdentityResolver func(context.Context, string) LoginHistoryIdentity

// QueryLoginHistory queries journald directly. It deliberately does not use
// the preferences store, so potentially large or sensitive journal data is
// never copied into SQLite.
func QueryLoginHistory(ctx context.Context, query LoginHistoryQuery) (LoginHistoryPage, error) {
	return QueryLoginHistoryWithDependencies(ctx, query, QueryLogs, resolveLoginHistoryIdentity)
}

func QueryLoginHistoryWithDependencies(ctx context.Context, query LoginHistoryQuery, queryLogs loginHistoryQueryLogs, resolve loginHistoryIdentityResolver) (LoginHistoryPage, error) {
	if query.Limit == 0 {
		query.Limit = 50
	}
	if err := query.Validate(); err != nil {
		return LoginHistoryPage{}, err
	}
	if queryLogs == nil || resolve == nil {
		return LoginHistoryPage{}, ErrInvalidLoginHistoryQuery
	}

	page := LoginHistoryPage{
		Identity: resolve(ctx, query.Username),
		Items:    make([]LoginHistoryEntry, 0, query.Limit),
	}
	cursor := query.Cursor
	for scan := 0; scan < maxLoginHistoryScanPages && len(page.Items) < query.Limit; scan++ {
		journalPage, err := queryLogs(ctx, JournalQuery{
			Limit:   MaxJournalPageSize,
			Cursor:  cursor,
			Since:   query.Since,
			Until:   query.Until,
			Text:    query.Username,
			Details: true,
		})
		if err != nil {
			return LoginHistoryPage{}, err
		}
		for _, entry := range journalPage.Items {
			item, ok := loginHistoryEntry(entry, query.Username)
			if !ok || (query.Outcome != "" && item.Outcome != query.Outcome) {
				continue
			}
			page.Items = append(page.Items, item)
			if len(page.Items) == query.Limit {
				page.NextCursor = journalPage.NextCursor
				return page, nil
			}
		}
		if journalPage.NextCursor == "" {
			break
		}
		page.NextCursor = journalPage.NextCursor
		cursor, err = DecodeJournalCursor(journalPage.NextCursor)
		if err != nil || cursor == "" {
			break
		}
	}
	return page, nil
}

var (
	acceptedLoginPattern = regexp.MustCompile(`(?i)\bAccepted\s+.+?\s+for\s+([^\s]+)\s+from\s+([^\s]+)`)
	failedLoginPattern   = regexp.MustCompile(`(?i)\bFailed\s+.+?\s+for\s+(?:invalid user\s+)?([^\s]+)(?:\s+from\s+([^\s]+))?`)
	openedSessionPattern = regexp.MustCompile(`(?i)\bsession opened for user\s+([^\s(]+)`)
	closedSessionPattern = regexp.MustCompile(`(?i)\bsession closed for user\s+([^\s(]+)`)
	newSessionPattern    = regexp.MustCompile(`(?i)\bNew session\s+([^\s]+)\s+of user\s+([^\s(]+)`)
)

func loginHistoryEntry(entry LogEntry, username string) (LoginHistoryEntry, bool) {
	message := strings.TrimSpace(entry.Message)
	if message == "" {
		return LoginHistoryEntry{}, false
	}
	service := entry.Unit
	if strings.HasSuffix(service, ".service") {
		service = strings.TrimSuffix(service, ".service")
	}
	if service == "" && entry.Details != nil {
		service = entry.Details["SYSLOG_IDENTIFIER"]
	}
	result := LoginHistoryEntry{Timestamp: entry.Timestamp, Service: truncateLoginHistoryValue(service, 128)}
	if match := acceptedLoginPattern.FindStringSubmatch(message); len(match) > 0 && match[1] == username {
		result.Event, result.Outcome, result.Remote = "login", "success", truncateLoginHistoryValue(match[2], 256)
		return result, true
	}
	if match := failedLoginPattern.FindStringSubmatch(message); len(match) > 0 && match[1] == username {
		result.Event, result.Outcome = "login", "failure"
		if len(match) > 2 {
			result.Remote = truncateLoginHistoryValue(match[2], 256)
		}
		return result, true
	}
	if match := openedSessionPattern.FindStringSubmatch(message); len(match) > 0 && match[1] == username {
		result.Event, result.Outcome = "session-open", "success"
		result.Session = sessionID(entry, "")
		return result, true
	}
	if match := closedSessionPattern.FindStringSubmatch(message); len(match) > 0 && match[1] == username {
		result.Event, result.Outcome = "session-close", "success"
		result.Session = sessionID(entry, "")
		return result, true
	}
	if match := newSessionPattern.FindStringSubmatch(message); len(match) > 0 && match[2] == username {
		result.Event, result.Outcome = "session-open", "success"
		result.Session = sessionID(entry, match[1])
		return result, true
	}
	if strings.Contains(strings.ToLower(message), "authentication failure") && strings.Contains(strings.ToLower(message), "user="+strings.ToLower(username)) {
		result.Event, result.Outcome = "login", "failure"
		if entry.Details != nil {
			result.Remote = truncateLoginHistoryValue(entry.Details["rhost"], 256)
		}
		return result, true
	}
	return LoginHistoryEntry{}, false
}

func sessionID(entry LogEntry, fallback string) string {
	if entry.Details != nil {
		if value := entry.Details["_SYSTEMD_SESSION"]; value != "" {
			return truncateLoginHistoryValue(value, 128)
		}
	}
	return truncateLoginHistoryValue(fallback, 128)
}

func truncateLoginHistoryValue(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func resolveLoginHistoryIdentity(ctx context.Context, username string) LoginHistoryIdentity {
	identity := LoginHistoryIdentity{Username: username, Source: "deleted-unknown", Present: false}
	if local, err := localNames("/etc/passwd"); err == nil && local[username] {
		identity.Source, identity.Present = "local", true
		return identity
	}
	command := exec.CommandContext(ctx, "getent", "passwd", username)
	stdout, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return identity
	}
	line, readErr := readLoginHistoryLine(stdout)
	waitErr := command.Wait()
	if readErr == nil && waitErr == nil && strings.HasPrefix(line, username+":") {
		identity.Source, identity.Present = "nss-read-only", true
	}
	return identity
}

func readLoginHistoryLine(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, 4097))
	scanner.Buffer(make([]byte, 1024), 4096)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	return "", scanner.Err()
}
