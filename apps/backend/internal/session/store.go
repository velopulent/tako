package session

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"

	"github.com/velopulent/tako/internal/auth"
)

const CookieName = "tako_session"

type Session struct {
	ID         string
	CSRF       string
	Identity   auth.Identity
	CreatedAt  time.Time
	SeenAt     time.Time
	AdminUntil time.Time
}

func (store *Store) SetAdministrative(id string, until time.Time) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, ok := store.sessions[id]
	if !ok {
		return false
	}
	current.AdminUntil = until
	store.sessions[id] = current
	return true
}

func (store *Store) DropAdministrative(id string) { store.SetAdministrative(id, time.Time{}) }

type Store struct {
	mu       sync.Mutex
	sessions map[string]Session
	idle     time.Duration
	absolute time.Duration
	now      func() time.Time
	onDelete func(auth.Identity)
}

func NewStore(idle, absolute time.Duration) *Store {
	return &Store{sessions: make(map[string]Session), idle: idle, absolute: absolute, now: time.Now}
}

func (store *Store) SetDeleteHook(hook func(auth.Identity)) {
	store.mu.Lock()
	store.onDelete = hook
	store.mu.Unlock()
}

func (store *Store) Create(identity auth.Identity) (Session, error) {
	id, err := token(32)
	if err != nil {
		return Session{}, err
	}
	csrf, err := token(24)
	if err != nil {
		return Session{}, err
	}
	now := store.now()
	session := Session{ID: id, CSRF: csrf, Identity: identity, CreatedAt: now, SeenAt: now}
	store.mu.Lock()
	removed := store.prune(now)
	store.sessions[id] = session
	hook := store.onDelete
	store.mu.Unlock()
	notifyDeleted(hook, removed)
	return session, nil
}

func (store *Store) prune(now time.Time) []auth.Identity {
	var removed []auth.Identity
	for id, current := range store.sessions {
		if now.Sub(current.SeenAt) > store.idle || now.Sub(current.CreatedAt) > store.absolute {
			delete(store.sessions, id)
			removed = append(removed, current.Identity)
		}
	}
	return removed
}

func (store *Store) Get(id string) (Session, bool) {
	store.mu.Lock()
	session, ok := store.sessions[id]
	if !ok {
		store.mu.Unlock()
		return Session{}, false
	}
	now := store.now()
	if now.Sub(session.SeenAt) > store.idle || now.Sub(session.CreatedAt) > store.absolute {
		delete(store.sessions, id)
		hook := store.onDelete
		store.mu.Unlock()
		notifyDeleted(hook, []auth.Identity{session.Identity})
		return Session{}, false
	}
	session.SeenAt = now
	store.sessions[id] = session
	store.mu.Unlock()
	return session, true
}

func (store *Store) Delete(id string) {
	identity, ok := store.DeleteWithoutNotify(id)
	store.mu.Lock()
	hook := store.onDelete
	store.mu.Unlock()
	if ok {
		notifyDeleted(hook, []auth.Identity{identity})
	}
}

func (store *Store) DeleteWithoutNotify(id string) (auth.Identity, bool) {
	store.mu.Lock()
	current, ok := store.sessions[id]
	if ok {
		delete(store.sessions, id)
	}
	store.mu.Unlock()
	return current.Identity, ok
}

func (store *Store) Close() {
	store.mu.Lock()
	removed := make([]auth.Identity, 0, len(store.sessions))
	for id, current := range store.sessions {
		removed = append(removed, current.Identity)
		delete(store.sessions, id)
	}
	hook := store.onDelete
	store.mu.Unlock()
	notifyDeleted(hook, removed)
}

func (store *Store) Prune() {
	store.mu.Lock()
	removed := store.prune(store.now())
	hook := store.onDelete
	store.mu.Unlock()
	notifyDeleted(hook, removed)
}

func notifyDeleted(hook func(auth.Identity), identities []auth.Identity) {
	if hook == nil {
		return
	}
	for _, identity := range identities {
		hook(identity)
	}
}

func token(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
