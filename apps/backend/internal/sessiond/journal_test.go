package sessiond

import (
	"encoding/json"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

func TestJournalQueryRejectsMissingGrantAndInvalidLimit(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handle(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop())
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "journal-query", Journal: &platform.JournalQuery{Limit: 20}}); err != nil {
		t.Fatal(err)
	}
	var response auth.Response
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "invalid-journal-query" {
		t.Fatalf("missing token returned %q", response.Error)
	}

	token, err := store.add(auth.Identity{Username: "octopus", UID: 1000, GID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn = net.Pipe()
	defer clientConn.Close()
	go handle(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop())
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "journal-query", Token: token, Journal: &platform.JournalQuery{Limit: 501}}); err != nil {
		t.Fatal(err)
	}
	response = auth.Response{}
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "invalid-journal-query" {
		t.Fatalf("oversized limit returned %q", response.Error)
	}
}

func TestJournalQueryUsesGrantedUserIdentity(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := lookupTestIdentity(account)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	payload := `{"__CURSOR":"c1","__REALTIME_TIMESTAMP":"1700000000000001","PRIORITY":"3","_SYSTEMD_UNIT":"worker.service","MESSAGE":"failed"}` + "\n"
	script := "#!/bin/sh\nprintf '%s' '" + strings.ReplaceAll(payload, "'", "'\\''") + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "journalctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	store := &grantStore{values: make(map[string]bridgeGrant)}
	token, err := store.add(identity)
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handle(serverConn, auth.PAMAuthenticator{}, nil, store, nil, zap.NewNop())
	if err := json.NewEncoder(clientConn).Encode(auth.Request{Operation: "journal-query", Token: token, Journal: &platform.JournalQuery{Limit: 20}}); err != nil {
		t.Fatal(err)
	}
	var response auth.Response
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" || response.JournalPage == nil || len(response.JournalPage.Items) != 1 || response.JournalPage.Items[0].Unit != "worker.service" {
		t.Fatalf("user journal query failed: %#v", response)
	}
	if response.JournalPage.Items[0].Cursor != "" {
		t.Fatal("cursor leaked in journal page")
	}
}
