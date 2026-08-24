package sessiond

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

func handleJournal(conn net.Conn, encoder *json.Encoder, request auth.Request, grants *grantStore, follow bool) {
	if request.Journal == nil || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != "" || request.PowerAction != "" || request.PowerConfirmation != "" || request.AdminTTL != 0 || request.Timer != nil || request.Override != nil || request.Signal != nil || request.Account != nil || request.GroupMembership != nil || request.AdminRole != nil || request.PasswordChange != nil || request.SSHKeys != nil || request.Updates != nil || request.File != nil || request.AutoUpdates != nil || request.Kpatch != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-journal-query"})
		return
	}
	query := *request.Journal
	if err := query.Validate(); err != nil {
		_ = encoder.Encode(auth.Response{Error: "invalid-journal-query"})
		return
	}
	_, cred, errCode := journalIdentity(request, grants)
	if errCode != "" {
		_ = encoder.Encode(auth.Response{Error: errCode})
		return
	}
	if follow {
		_ = conn.SetDeadline(time.Time{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := platform.FollowJournalAs(ctx, query, cred, func(entry platform.LogEntry) error {
			entry.Cursor = ""
			return encoder.Encode(auth.Response{LogEntry: &entry})
		})
		if err != nil && ctx.Err() == nil {
			_ = encoder.Encode(auth.Response{Error: "logs-unavailable"})
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, err := platform.QueryLogsAs(ctx, query, cred)
	if err != nil {
		if errors.Is(err, platform.ErrInvalidJournalQuery) {
			_ = encoder.Encode(auth.Response{Error: "invalid-journal-query"})
			return
		}
		_ = encoder.Encode(auth.Response{Error: "logs-unavailable"})
		return
	}
	_ = encoder.Encode(auth.Response{JournalPage: &page})
}

func journalIdentity(request auth.Request, grants *grantStore) (auth.Identity, *syscall.Credential, string) {
	if request.AdminToken != "" {
		if request.Token != "" {
			return auth.Identity{}, nil, "invalid-journal-query"
		}
		identity, ok := grants.adminIdentity(request.AdminToken)
		if !ok {
			return auth.Identity{}, nil, "invalid-admin-token"
		}
		return identity, nil, ""
	}
	if request.Token == "" {
		return auth.Identity{}, nil, "invalid-journal-query"
	}
	identity, ok := grants.get(request.Token)
	if !ok {
		return auth.Identity{}, nil, "invalid-bridge-token"
	}
	cred := &syscall.Credential{Uid: uint32(identity.UID), Gid: uint32(identity.GID)}
	account, err := user.LookupId(strconv.Itoa(identity.UID))
	if err == nil {
		if full, credErr := userCredential(account, identity); credErr == nil {
			cred = full
		}
	}
	return identity, cred, ""
}
