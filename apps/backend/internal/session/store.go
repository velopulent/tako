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
}

func NewStore(idle, absolute time.Duration) *Store {
	return &Store{sessions: make(map[string]Session), idle: idle, absolute: absolute, now: time.Now}
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
	store.prune(now)
	store.sessions[id] = session
	store.mu.Unlock()
	return session, nil
}

func (store *Store) prune(now time.Time) {
	for id, current := range store.sessions {
		if now.Sub(current.SeenAt) > store.idle || now.Sub(current.CreatedAt) > store.absolute {
			delete(store.sessions, id)
		}
	}
}

func (store *Store) Get(id string) (Session, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	session, ok := store.sessions[id]
	if !ok {
		return Session{}, false
	}
	now := store.now()
	if now.Sub(session.SeenAt) > store.idle || now.Sub(session.CreatedAt) > store.absolute {
		delete(store.sessions, id)
		return Session{}, false
	}
	session.SeenAt = now
	store.sessions[id] = session
	return session, true
}

func (store *Store) Delete(id string) {
	store.mu.Lock()
	delete(store.sessions, id)
	store.mu.Unlock()
}

func token(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
