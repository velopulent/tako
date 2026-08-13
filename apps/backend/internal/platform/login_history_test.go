package platform

import (
	"context"
	"errors"
	"testing"
)

func TestQueryLoginHistoryClassifiesBoundedJournalEvents(t *testing.T) {
	var received JournalQuery
	page, err := QueryLoginHistoryWithDependencies(
		context.Background(),
		LoginHistoryQuery{Username: "alice", Limit: 3},
		func(_ context.Context, query JournalQuery) (JournalPage, error) {
			received = query
			return JournalPage{Items: []LogEntry{
				{Timestamp: "2024-01-01T00:00:00Z", Unit: "sshd.service", Message: "Accepted publickey for alice from 192.0.2.10", Details: map[string]string{"_SYSTEMD_SESSION": "7"}},
				{Timestamp: "2024-01-01T00:01:00Z", Unit: "sshd.service", Message: "Failed password for alice from 192.0.2.11"},
				{Timestamp: "2024-01-01T00:02:00Z", Unit: "systemd-logind.service", Message: "New session 8 of user alice"},
				{Timestamp: "2024-01-01T00:03:00Z", Unit: "other.service", Message: "alice completed a backup"},
			}, NextCursor: EncodeJournalCursor("cursor-next")}, nil
		},
		func(_ context.Context, username string) LoginHistoryIdentity {
			return LoginHistoryIdentity{Username: username, Source: "deleted-unknown"}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if received.Text != "alice" || !received.Details || received.Limit != MaxJournalPageSize {
		t.Fatalf("unexpected journal query: %#v", received)
	}
	if len(page.Items) != 3 || page.Items[0].Event != "login" || page.Items[0].Outcome != "success" || page.Items[0].Remote != "192.0.2.10" {
		t.Fatalf("unexpected classified page: %#v", page)
	}
	if page.Items[1].Outcome != "failure" || page.Items[2].Event != "session-open" || page.NextCursor == "" || page.Identity.Source != "deleted-unknown" {
		t.Fatalf("missing bounded metadata: %#v", page)
	}
}

func TestQueryLoginHistoryFollowsOpaqueCursorForOutcomeFilter(t *testing.T) {
	queries := make([]JournalQuery, 0, 2)
	page, err := QueryLoginHistoryWithDependencies(
		context.Background(),
		LoginHistoryQuery{Username: "alice", Limit: 1, Outcome: "success", Cursor: "cursor-start"},
		func(_ context.Context, query JournalQuery) (JournalPage, error) {
			queries = append(queries, query)
			if len(queries) == 1 {
				return JournalPage{Items: []LogEntry{{Message: "Failed password for alice from 192.0.2.11"}}, NextCursor: EncodeJournalCursor("cursor-next")}, nil
			}
			return JournalPage{Items: []LogEntry{{Message: "Accepted password for alice from 192.0.2.10"}}}, nil
		},
		func(_ context.Context, username string) LoginHistoryIdentity {
			return LoginHistoryIdentity{Username: username, Source: "nss-read-only", Present: true}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || queries[0].Cursor != "cursor-start" || queries[1].Cursor != "cursor-next" {
		t.Fatalf("cursor was not advanced safely: %#v", queries)
	}
	if len(page.Items) != 1 || page.Items[0].Outcome != "success" || !page.Identity.Present {
		t.Fatalf("unexpected outcome page: %#v", page)
	}
}

func TestLoginHistoryQueryValidation(t *testing.T) {
	for _, query := range []LoginHistoryQuery{
		{Username: "", Limit: 1},
		{Username: "alice", Limit: 0},
		{Username: "alice", Limit: MaxLoginHistoryPageSize + 1},
		{Username: "alice", Limit: 1, Outcome: "unknown"},
		{Username: "alice", Limit: 1, Cursor: "bad cursor"},
	} {
		if err := query.Validate(); !errors.Is(err, ErrInvalidLoginHistoryQuery) {
			t.Fatalf("query %#v returned %v", query, err)
		}
	}
}
