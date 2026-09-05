package platform

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	MaxJournalPageSize = 500
	MaxJournalCursor   = 4096
	maxJournalOutput   = 8 << 20
)

var (
	ErrInvalidJournalQuery = errors.New("invalid journal query")
)

var journalDetailFields = []string{
	"__CURSOR",
	"__REALTIME_TIMESTAMP",
	"PRIORITY",
	"_SYSTEMD_UNIT",
	"_SYSTEMD_USER_UNIT",
	"_EXE",
	"MESSAGE",
	"_PID",
	"_UID",
	"_GID",
	"_COMM",
	"_CMDLINE",
	"_HOSTNAME",
	"_BOOT_ID",
	"_MACHINE_ID",
	"_SYSTEMD_OWNER_UID",
	"_SYSTEMD_SESSION",
	"_CAP_EFFECTIVE",
	"_SELINUX_CONTEXT",
	"SYSLOG_IDENTIFIER",
	"SYSLOG_PID",
	"CODE_FILE",
	"CODE_LINE",
	"CODE_FUNC",
	"MESSAGE_ID",
}

func journalFields(details bool) []string {
	if details {
		return journalDetailFields
	}
	return journalDetailFields[:7]
}

type JournalQuery struct {
	Limit      int
	Cursor     string
	Boot       string
	Since      time.Time
	Until      time.Time
	Priority   string
	Unit       string
	Executable string
	Text       string
	Details    bool
}

type JournalPage struct {
	Items      []LogEntry `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

func (query JournalQuery) Validate() error {
	if query.Limit < 1 || query.Limit > MaxJournalPageSize {
		return ErrInvalidJournalQuery
	}
	if query.Cursor != "" {
		if len(query.Cursor) > MaxJournalCursor || !validJournalCursor(query.Cursor) {
			return ErrInvalidJournalQuery
		}
	}
	if query.Boot != "" && query.Boot != "current" {
		if len(query.Boot) != 32 {
			return ErrInvalidJournalQuery
		}
		if _, err := hex.DecodeString(query.Boot); err != nil {
			return ErrInvalidJournalQuery
		}
	}
	if !query.Since.IsZero() && !query.Until.IsZero() && query.Since.After(query.Until) {
		return ErrInvalidJournalQuery
	}
	if query.Priority != "" && !regexp.MustCompile(`^[0-7](\.\.[0-7])?$`).MatchString(query.Priority) {
		return ErrInvalidJournalQuery
	}
	if query.Unit != "" && (len(query.Unit) > MaxServiceUnitLength || strings.ContainsAny(query.Unit, "/\x00\r\n")) {
		return ErrInvalidJournalQuery
	}
	if query.Executable != "" && (len(query.Executable) > 4096 || !strings.HasPrefix(query.Executable, "/") || strings.ContainsAny(query.Executable, "\x00\r\n")) {
		return ErrInvalidJournalQuery
	}
	if len(query.Text) > 512 || strings.ContainsAny(query.Text, "\x00\r\n") {
		return ErrInvalidJournalQuery
	}
	return nil
}

func validJournalCursor(cursor string) bool {
	for _, character := range cursor {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func EncodeJournalCursor(cursor string) string {
	if cursor == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(cursor))
}

func DecodeJournalCursor(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > MaxJournalCursor*2 {
		return "", ErrInvalidJournalQuery
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) > MaxJournalCursor || !validJournalCursor(string(decoded)) {
		return "", ErrInvalidJournalQuery
	}
	return string(decoded), nil
}

func QueryLogs(ctx context.Context, query JournalQuery) (JournalPage, error) {
	return QueryLogsAs(ctx, query, nil)
}

// QueryLogsAs runs journalctl with optional UNIX credentials. A nil credential
// keeps the caller identity. sessiond passes the authenticated operator, or
// nil when an administrative grant is active (root journal, Cockpit-style).
func QueryLogsAs(ctx context.Context, query JournalQuery, cred *syscall.Credential) (JournalPage, error) {
	if query.Limit == 0 {
		query.Limit = 200
	}
	if err := query.Validate(); err != nil {
		return JournalPage{}, err
	}
	fetchLimit := query.Limit + 1
	if query.Cursor != "" {
		fetchLimit++
	}
	command := journalctlCommand(ctx, journalctlArgs(query, false, fetchLimit), cred)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return JournalPage{}, err
	}
	if err := command.Start(); err != nil {
		return JournalPage{}, err
	}
	output, readErr := readBounded(stdout, maxJournalOutput)
	if readErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if readErr != nil {
		return JournalPage{}, readErr
	}
	if waitErr != nil && ctx.Err() != nil {
		return JournalPage{}, ctx.Err()
	}
	entries := make([]LogEntry, 0, query.Limit)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		entry, ok := parseLogEntryWithDetails(scanner.Bytes(), query.Details)
		if ok {
			if query.Cursor != "" && entry.Cursor == query.Cursor {
				continue
			}
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return JournalPage{}, err
	}
	if waitErr != nil && len(entries) == 0 {
		return JournalPage{Items: []LogEntry{}}, nil
	}
	page := JournalPage{Items: entries}
	if len(entries) > query.Limit {
		page.NextCursor = EncodeJournalCursor(entries[query.Limit-1].Cursor)
		page.Items = entries[:query.Limit]
	}
	for index := range page.Items {
		page.Items[index].Cursor = ""
	}
	return page, nil
}

func journalctlArgs(query JournalQuery, follow bool, fetchLimit int) []string {
	arguments := []string{"--no-pager", "--output=json", "--output-fields=" + strings.Join(journalFields(query.Details), ",")}
	if follow {
		arguments = append(arguments, "--follow", "--lines=0")
	} else {
		arguments = append(arguments, "--reverse", "-n", strconv.Itoa(fetchLimit))
	}
	if query.Cursor != "" && !follow {
		arguments = append(arguments, "--cursor="+query.Cursor)
	}
	if query.Boot != "" {
		arguments = append(arguments, "--boot="+query.Boot)
	}
	if !query.Since.IsZero() {
		arguments = append(arguments, "--since="+query.Since.UTC().Format(time.RFC3339Nano))
	}
	if !query.Until.IsZero() {
		arguments = append(arguments, "--until="+query.Until.UTC().Format(time.RFC3339Nano))
	}
	if query.Priority != "" {
		arguments = append(arguments, "--priority="+query.Priority)
	}
	if query.Unit != "" {
		arguments = append(arguments, "--unit="+query.Unit)
	}
	if query.Executable != "" {
		arguments = append(arguments, "_EXE="+query.Executable)
	}
	if query.Text != "" {
		arguments = append(arguments, "--grep="+regexp.QuoteMeta(query.Text))
	}
	return arguments
}

func journalctlCommand(ctx context.Context, arguments []string, cred *syscall.Credential) *exec.Cmd {
	command := exec.CommandContext(ctx, "journalctl", arguments...)
	applyJournalCredential(command, cred)
	return command
}

func applyJournalCredential(command *exec.Cmd, cred *syscall.Credential) {
	if cred == nil || uint32(os.Geteuid()) == cred.Uid {
		return
	}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
}

func FollowJournalAs(ctx context.Context, query JournalQuery, cred *syscall.Credential, emit func(LogEntry) error) error {
	if query.Limit == 0 {
		query.Limit = 200
	}
	if err := query.Validate(); err != nil {
		return err
	}
	command := journalctlCommand(ctx, journalctlArgs(query, true, 0), cred)
	pipe, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		entry, ok := parseLogEntryWithDetails(scanner.Bytes(), query.Details)
		if ok && emit(entry) != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	err = command.Wait()
	if ctx.Err() != nil {
		return nil
	}
	return err
}
