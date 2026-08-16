package sessiond

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/velopulent/tako/internal/auth"
)

const (
	conversationLifetime  = 2 * time.Minute
	maxConversationRounds = 12
	maxPromptBytes        = 1024
	maxResponseBytes      = 4096
)

var (
	errConversationInvalid = errors.New("invalid conversation")
	errConversationBounds  = errors.New("conversation bounds exceeded")
	errConversationBusy    = errors.New("too many authentication conversations")
	errSessionFailed       = errors.New("session-failed")
)

type sessionOpener interface {
	OpenSessionWithConversation(string, auth.Conversation) (auth.UserSession, error)
}

type conversationResult struct {
	session auth.UserSession
	err     error
}

type conversationState struct {
	id       string
	ctx      context.Context
	cancel   context.CancelFunc
	prompts  chan auth.Prompt
	answers  chan auth.PromptResponse
	result   chan conversationResult
	step     sync.Mutex
	current  *auth.Prompt
	rounds   int
	expires  *time.Timer
	username string
}

type conversationStore struct {
	mu       sync.Mutex
	values   map[string]*conversationState
	workers  int
	byUser   map[string]int
	opener   sessionOpener
	complete func(auth.UserSession) (string, error)
}

const (
	maxConcurrentConversations = 32
	maxConversationsPerUser    = 2
)

func newConversationStore(opener sessionOpener, complete func(auth.UserSession) (string, error)) *conversationStore {
	return &conversationStore{
		values:   make(map[string]*conversationState),
		byUser:   make(map[string]int),
		opener:   opener,
		complete: complete,
	}
}

func (store *conversationStore) advance(ctx context.Context, request auth.ConversationRequest) (auth.ConversationResponse, error) {
	if len(request.Password) > maxResponseBytes || !utf8.ValidString(request.Password) {
		return auth.ConversationResponse{}, errConversationInvalid
	}
	state, err := store.state(request)
	if err != nil {
		return auth.ConversationResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		store.cancel(state.id)
		return auth.ConversationResponse{}, err
	}
	state.step.Lock()
	defer state.step.Unlock()

	if request.ConversationID == "" {
		if len(request.Responses) != 0 {
			store.cancel(state.id)
			return auth.ConversationResponse{}, errConversationInvalid
		}
	} else if err := store.answer(state, request.Responses); err != nil {
		store.cancel(state.id)
		return auth.ConversationResponse{}, err
	}

	for {
		select {
		case prompt := <-state.prompts:
			state.rounds++
			if state.rounds > maxConversationRounds {
				store.cancel(state.id)
				return auth.ConversationResponse{}, errConversationBounds
			}
			state.current = &prompt
			if request.ConversationID == "" && request.Password != "" && prompt.Style == auth.PromptHidden {
				password := request.Password
				request.Password = ""
				if err := store.answer(state, []auth.PromptResponse{{ID: prompt.ID, Value: password}}); err != nil {
					store.cancel(state.id)
					return auth.ConversationResponse{}, err
				}
				password = ""
				continue
			}
			return auth.ConversationResponse{ConversationID: state.id, Prompts: []auth.Prompt{prompt}}, nil
		case result := <-state.result:
			if !store.remove(state.id) {
				if result.err == nil && result.session.Close != nil {
					result.session.Close()
				}
				return auth.ConversationResponse{}, errConversationInvalid
			}
			if result.err != nil {
				return auth.ConversationResponse{}, result.err
			}
			token, err := store.complete(result.session)
			if err != nil {
				result.session.Close()
				return auth.ConversationResponse{}, fmt.Errorf("%w: %v", errSessionFailed, err)
			}
			identity := result.session.Identity
			return auth.ConversationResponse{Identity: &identity, BridgeToken: token}, nil
		case <-ctx.Done():
			store.cancel(state.id)
			return auth.ConversationResponse{}, ctx.Err()
		case <-state.ctx.Done():
			return auth.ConversationResponse{}, errConversationInvalid
		}
	}
}

func (store *conversationStore) state(request auth.ConversationRequest) (*conversationState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if request.ConversationID != "" {
		state, ok := store.values[request.ConversationID]
		if !ok {
			return nil, errConversationInvalid
		}
		return state, nil
	}
	if request.Username == "" || len(request.Username) > 256 || !utf8.ValidString(request.Username) {
		return nil, errConversationInvalid
	}
	if store.workers >= maxConcurrentConversations {
		return nil, errConversationBusy
	}
	if store.byUser[request.Username] >= maxConversationsPerUser {
		return nil, errConversationBusy
	}
	id, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	conversationCtx, cancel := context.WithCancel(context.Background())
	state := &conversationState{
		id:       id,
		ctx:      conversationCtx,
		cancel:   cancel,
		prompts:  make(chan auth.Prompt),
		answers:  make(chan auth.PromptResponse),
		result:   make(chan conversationResult),
		username: request.Username,
	}
	state.expires = time.AfterFunc(conversationLifetime, func() { store.cancel(id) })
	store.values[id] = state
	store.workers++
	store.byUser[request.Username]++
	go store.run(conversationCtx, state)
	return state, nil
}

func (store *conversationStore) run(ctx context.Context, state *conversationState) {
	defer store.workerDone(state.username)
	session, err := store.opener.OpenSessionWithConversation(state.username, func(style auth.PromptStyle, message string) (string, error) {
		if len(message) > maxPromptBytes || !utf8.ValidString(message) {
			return "", errConversationBounds
		}
		id, err := randomToken(12)
		if err != nil {
			return "", err
		}
		prompt := auth.Prompt{ID: id, Style: style, Message: message}
		select {
		case state.prompts <- prompt:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		select {
		case response := <-state.answers:
			if style == auth.PromptInfo || style == auth.PromptError {
				return "", nil
			}
			return response.Value, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	select {
	case state.result <- conversationResult{session: session, err: err}:
	case <-ctx.Done():
		if err == nil && session.Close != nil {
			session.Close()
		}
	}
}

func (store *conversationStore) workerDone(username string) {
	store.mu.Lock()
	store.workers--
	store.byUser[username]--
	if store.byUser[username] == 0 {
		delete(store.byUser, username)
	}
	store.mu.Unlock()
}

func (store *conversationStore) answer(state *conversationState, responses []auth.PromptResponse) error {
	if state.current == nil || len(responses) != 1 || responses[0].ID != state.current.ID || len(responses[0].Value) > maxResponseBytes || !utf8.ValidString(responses[0].Value) {
		return errConversationInvalid
	}
	response := responses[0]
	state.current = nil
	select {
	case state.answers <- response:
		return nil
	case <-time.After(time.Second):
		return errConversationInvalid
	}
}

func (store *conversationStore) cancel(id string) {
	store.mu.Lock()
	state, ok := store.values[id]
	if ok {
		delete(store.values, id)
	}
	store.mu.Unlock()
	if ok {
		state.expires.Stop()
		state.cancel()
	}
}

func (store *conversationStore) remove(id string) bool {
	store.mu.Lock()
	state, ok := store.values[id]
	delete(store.values, id)
	store.mu.Unlock()
	if ok {
		state.expires.Stop()
		state.cancel()
	}
	return ok
}

func (store *conversationStore) closeAll() {
	store.mu.Lock()
	states := store.values
	store.values = make(map[string]*conversationState)
	store.mu.Unlock()
	for _, state := range states {
		state.expires.Stop()
		state.cancel()
	}
}
