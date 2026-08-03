package store

import (
	"context"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Session represents an active WebSocket chat session with a running agent container.
type Session struct {
	ID            string             `json:"session_id"`
	Agent         string             `json:"agent"`
	Model         string             `json:"model,omitempty"`
	System        string             `json:"-"`
	Project       string             `json:"project,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	LastMessageAt time.Time          `json:"last_message_at"`
	ContainerID   string             `json:"-"`
	Conn          *websocket.Conn    `json:"-"`
	ConnMu        sync.Mutex         `json:"-"`
	Active        bool               `json:"-"`
	// SlotHeld tracks whether this session currently holds a chat concurrency
	// slot. Slots are tied to having a connected client, not to the
	// container's lifetime: they're released on disconnect and re-acquired
	// on reconnect, so a client navigating away doesn't permanently pin a
	// slot for a session nobody is watching. Guarded by the SessionStore's
	// own lock (mutate only via SessionStore.Update).
	SlotHeld bool `json:"-"`
	Ctx           context.Context    `json:"-"`
	CancelFunc    context.CancelFunc `json:"-"`
	Stdin         io.WriteCloser     `json:"-"` // container stdin writer
	UploadedFiles []UploadedFile     `json:"-"` // files uploaded since the last prompt was sent
}

// UploadedFile tracks a file uploaded into a project agent session's container.
type UploadedFile struct {
	OriginalName string // filename as sent by the client
	SafeName     string // sanitized filename actually written into the container
}

// SessionStore is a thread-safe store for active chat sessions.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionStore creates an empty SessionStore.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
	}
}

// Add inserts a session into the store.
func (s *SessionStore) Add(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
}

// Get returns a session by ID, or nil if not found.
func (s *SessionStore) Get(id string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[id]
}

// Remove deletes a session from the store by ID.
func (s *SessionStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// Update applies fn to the session with the given ID while holding a write lock.
// Returns false if the session was not found.
func (s *SessionStore) Update(id string, fn func(*Session)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return false
	}
	fn(sess)
	return true
}

// List returns all active sessions ordered by last_message_at descending.
func (s *SessionStore) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		all = append(all, sess)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].LastMessageAt.After(all[j].LastMessageAt)
	})
	return all
}

// ListByProject returns active sessions for a given project, ordered by
// last_message_at descending. Returns an empty (non-nil) slice when no
// sessions belong to the project.
func (s *SessionStore) ListByProject(project string) []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Session, 0)
	for _, sess := range s.sessions {
		if sess.Project == project {
			result = append(result, sess)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastMessageAt.After(result[j].LastMessageAt)
	})
	return result
}

// All returns all sessions (unordered). Used for state persistence.
func (s *SessionStore) All() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		all = append(all, sess)
	}
	return all
}
