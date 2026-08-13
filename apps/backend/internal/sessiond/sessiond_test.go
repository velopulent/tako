package sessiond

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
)

func TestGrantStore(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	identity := auth.Identity{Username: "octopus", UID: 1000, GID: 1000}
	token, err := store.add(identity)
	if err != nil || token == "" {
		t.Fatalf("add grant: token=%q err=%v", token, err)
	}
	got, ok := store.get(token)
	if !ok || got.Username != identity.Username {
		t.Fatalf("get grant: got=%+v ok=%v", got, ok)
	}
	if _, ok := store.claim(token); !ok {
		t.Fatal("first terminal claim rejected")
	}
	if _, ok := store.claim(token); ok {
		t.Fatal("concurrent terminal claim accepted")
	}
	store.release(token)
	store.values[token] = bridgeGrant{identity: identity, expires: time.Now().Add(-time.Second)}
	if _, ok := store.get(token); ok {
		t.Fatal("expired grant accepted")
	}
}

func TestExpiredGrantCleansTerminalBridgeAndPAM(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	reader, terminal, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	bridge := &trackingCloser{}
	var pamClosed atomic.Bool
	store.values["expired"] = bridgeGrant{
		identity: auth.Identity{Username: "octopus"},
		expires:  time.Now().Add(-time.Second),
		terminal: terminal,
		bridge:   bridge,
		closePAM: func() {
			_ = bridge.Close()
			pamClosed.Store(true)
		},
	}
	if _, ok := store.get("expired"); ok {
		t.Fatal("expired grant was accepted")
	}
	if _, err := terminal.Write([]byte("x")); err == nil {
		t.Fatal("expired grant left terminal open")
	}
	if bridge.calls.Load() != 1 {
		t.Fatalf("expired grant closed bridge %d times, want 1", bridge.calls.Load())
	}
	if !pamClosed.Load() {
		t.Fatal("expired grant did not close PAM resources")
	}
}

func TestTerminalRegistrationFailsAfterGrantCloses(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	token, err := store.add(auth.Identity{Username: "octopus"})
	if err != nil {
		t.Fatal(err)
	}
	store.close(token)
	reader, terminal, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer terminal.Close()
	if store.setTerminal(token, terminal) {
		t.Fatal("terminal registered after grant closure")
	}
}

type multiRoundOpener struct {
	canceled atomic.Bool
	closed   atomic.Bool
}

type endlessOpener struct{}

func (*endlessOpener) OpenSessionWithConversation(_ string, conversation auth.Conversation) (auth.UserSession, error) {
	for index := 0; index <= maxConversationRounds; index++ {
		if _, err := conversation(auth.PromptInfo, "Continue"); err != nil {
			return auth.UserSession{}, err
		}
	}
	return auth.UserSession{}, errors.New("unexpected completion")
}

type blockedOpener struct{ release <-chan struct{} }

func (opener *blockedOpener) OpenSessionWithConversation(_ string, _ auth.Conversation) (auth.UserSession, error) {
	<-opener.release
	return auth.UserSession{}, errors.New("released")
}

type trackingCloser struct{ calls atomic.Int32 }

func (closer *trackingCloser) Close() error {
	closer.calls.Add(1)
	return nil
}

func (opener *multiRoundOpener) OpenSessionWithConversation(username string, conversation auth.Conversation) (auth.UserSession, error) {
	password, err := conversation(auth.PromptHidden, "Password:")
	if err != nil {
		opener.canceled.Store(true)
		return auth.UserSession{}, err
	}
	if _, err := conversation(auth.PromptInfo, "Additional verification required"); err != nil {
		opener.canceled.Store(true)
		return auth.UserSession{}, err
	}
	code, err := conversation(auth.PromptText, "Verification code:")
	if err != nil {
		opener.canceled.Store(true)
		return auth.UserSession{}, err
	}
	if username != "octopus" || password != "secret" || code != "123456" {
		return auth.UserSession{}, errors.New("authentication failed")
	}
	return auth.UserSession{
		Identity: auth.Identity{Username: username, UID: 1000, GID: 1000},
		Close:    func() { opener.closed.Store(true) },
	}, nil
}

func TestConversationSupportsMultiplePromptTypesAndRounds(t *testing.T) {
	opener := &multiRoundOpener{}
	store := newConversationStore(opener, func(session auth.UserSession) (string, error) {
		return "bridge-token", nil
	})
	defer store.closeAll()

	response, err := store.advance(context.Background(), auth.ConversationRequest{Username: "octopus"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Prompts) != 1 || response.Prompts[0].Style != auth.PromptHidden {
		t.Fatalf("unexpected first prompt: %#v", response)
	}
	response, err = store.advance(context.Background(), auth.ConversationRequest{
		ConversationID: response.ConversationID,
		Responses:      []auth.PromptResponse{{ID: response.Prompts[0].ID, Value: "secret"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Prompts[0].Style != auth.PromptInfo {
		t.Fatalf("unexpected information prompt: %#v", response)
	}
	response, err = store.advance(context.Background(), auth.ConversationRequest{
		ConversationID: response.ConversationID,
		Responses:      []auth.PromptResponse{{ID: response.Prompts[0].ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Prompts[0].Style != auth.PromptText {
		t.Fatalf("unexpected verification prompt: %#v", response)
	}
	response, err = store.advance(context.Background(), auth.ConversationRequest{
		ConversationID: response.ConversationID,
		Responses:      []auth.PromptResponse{{ID: response.Prompts[0].ID, Value: "123456"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Identity == nil || response.BridgeToken != "bridge-token" || response.Identity.Username != "octopus" {
		t.Fatalf("unexpected completion: %#v", response)
	}
}

func TestConversationRejectsMismatchedAndOversizedResponses(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		id    string
	}{
		{name: "mismatched prompt", id: "wrong", value: "secret"},
		{name: "oversized response", value: strings.Repeat("x", maxResponseBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			opener := &multiRoundOpener{}
			store := newConversationStore(opener, func(auth.UserSession) (string, error) { return "", nil })
			response, err := store.advance(context.Background(), auth.ConversationRequest{Username: "octopus"})
			if err != nil {
				t.Fatal(err)
			}
			id := test.id
			if id == "" {
				id = response.Prompts[0].ID
			}
			_, err = store.advance(context.Background(), auth.ConversationRequest{
				ConversationID: response.ConversationID,
				Responses:      []auth.PromptResponse{{ID: id, Value: test.value}},
			})
			if !errors.Is(err, errConversationInvalid) {
				t.Fatalf("expected invalid conversation, got %v", err)
			}
			for deadline := time.Now().Add(time.Second); !opener.canceled.Load() && time.Now().Before(deadline); {
				time.Sleep(time.Millisecond)
			}
			if !opener.canceled.Load() {
				t.Fatal("canceled conversation did not stop PAM worker")
			}
		})
	}
}

func TestConversationStopsWhenRequestIsCanceled(t *testing.T) {
	opener := &multiRoundOpener{}
	store := newConversationStore(opener, func(auth.UserSession) (string, error) { return "", nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.advance(ctx, auth.ConversationRequest{Username: "octopus"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled conversation, got %v", err)
	}
	for deadline := time.Now().Add(time.Second); !opener.canceled.Load() && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if !opener.canceled.Load() {
		t.Fatal("request cancellation did not stop PAM worker")
	}
}

func TestConversationEnforcesRoundLimit(t *testing.T) {
	store := newConversationStore(&endlessOpener{}, func(auth.UserSession) (string, error) { return "", nil })
	response, err := store.advance(context.Background(), auth.ConversationRequest{Username: "octopus"})
	if err != nil {
		t.Fatal(err)
	}
	for round := 1; round < maxConversationRounds; round++ {
		response, err = store.advance(context.Background(), auth.ConversationRequest{
			ConversationID: response.ConversationID,
			Responses:      []auth.PromptResponse{{ID: response.Prompts[0].ID}},
		})
		if err != nil {
			t.Fatalf("round %d failed early: %v", round, err)
		}
	}
	_, err = store.advance(context.Background(), auth.ConversationRequest{
		ConversationID: response.ConversationID,
		Responses:      []auth.PromptResponse{{ID: response.Prompts[0].ID}},
	})
	if !errors.Is(err, errConversationBounds) {
		t.Fatalf("expected round bound, got %v", err)
	}
}

func TestConversationCapsConcurrentAttemptsPerUser(t *testing.T) {
	store := newConversationStore(&multiRoundOpener{}, func(auth.UserSession) (string, error) { return "", nil })
	defer store.closeAll()
	for attempt := 0; attempt < maxConversationsPerUser; attempt++ {
		if _, err := store.advance(context.Background(), auth.ConversationRequest{Username: "octopus"}); err != nil {
			t.Fatalf("attempt %d rejected early: %v", attempt, err)
		}
	}
	if _, err := store.advance(context.Background(), auth.ConversationRequest{Username: "octopus"}); !errors.Is(err, errConversationBusy) {
		t.Fatalf("expected busy response, got %v", err)
	}
}

func TestCanceledBlockedPAMWorkersStillCountTowardCap(t *testing.T) {
	release := make(chan struct{})
	store := newConversationStore(&blockedOpener{release: release}, func(auth.UserSession) (string, error) { return "", nil })
	for index := 0; index < maxConcurrentConversations; index++ {
		username := "user-" + strconv.Itoa(index)
		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, _ = store.advance(ctx, auth.ConversationRequest{Username: username})
		}()
	}
	for deadline := time.Now().Add(time.Second); ; {
		store.mu.Lock()
		workers := store.workers
		store.mu.Unlock()
		if workers == maxConcurrentConversations {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("started %d PAM workers, want %d", workers, maxConcurrentConversations)
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := store.advance(context.Background(), auth.ConversationRequest{Username: "new-user"}); !errors.Is(err, errConversationBusy) {
		t.Fatalf("expected busy while PAM workers remain blocked, got %v", err)
	}
	close(release)
}

func TestGrantCloseEndsPAMSession(t *testing.T) {
	store := &grantStore{values: make(map[string]bridgeGrant)}
	var closed atomic.Bool
	bridge := &trackingCloser{}
	token, err := store.addGrant(auth.Identity{Username: "octopus"}, bridge, func() { closed.Store(true) })
	if err != nil {
		t.Fatal(err)
	}
	store.close(token)
	if !closed.Load() {
		t.Fatal("grant close did not end PAM session")
	}
	if bridge.calls.Load() != 1 {
		t.Fatalf("grant close stopped user bridge %d times, want 1", bridge.calls.Load())
	}
	store.close(token)
	if bridge.calls.Load() != 1 {
		t.Fatal("repeated grant close stopped user bridge twice")
	}
}
