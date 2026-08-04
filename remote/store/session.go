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
	ID            string          `json:"session_id"`
	Agent         string          `json:"agent"`
	Model         string          `json:"model,omitempty"`
	System        string          `json:"-"`
	Project       string          `json:"project,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	LastMessageAt time.Time       `json:"last_message_at"`
	ContainerID   string          `json:"-"`
	Conn          *websocket.Conn `json:"-"`
	ConnMu        sync.Mutex      `json:"-"`
	Active        bool            `json:"-"`
	// SlotHeld tracks whether this session currently holds a chat concurrency
	// slot. Slots are tied to having a connected client, not to the
	// container's lifetime: they're released on disconnect and re-acquired
	// on reconnect, so a client navigating away doesn't permanently pin a
	// slot for a session nobody is watching. Guarded by the SessionStore's
	// own lock (mutate only via SessionStore.Update).
	SlotHeld      bool               `json:"-"`
	Ctx           context.Context    `json:"-"`
	CancelFunc    context.CancelFunc `json:"-"`
	Stdin         io.WriteCloser     `json:"-"` // container stdin writer
	UploadedFiles []UploadedFile     `json:"-"` // files uploaded since the last prompt was sent
	// Messages is the session's chat transcript, accumulated regardless of
	// whether a client is currently connected so a client that reconnects
	// (or connects for the first time after the agent replied while nobody
	// was watching) can be caught up in full. Guarded by SessionStore's lock
	// - mutate only via SessionStore.Update or its Append*/MarkAgentDone
	// helpers below.
	Messages []ChatMessage `json:"-"`
}

// ChatMessage is a single turn in a project agent chat session's transcript.
type ChatMessage struct {
	Role     string `json:"role"` // "user" or "agent"
	Content  string `json:"content"`
	Complete bool   `json:"complete"` // false while an agent reply is still streaming in
}

// maxStoredMessages caps a session's in-memory transcript so a very long-running
// chat can't grow the server's memory usage without bound.
const maxStoredMessages = 500

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

// AppendUserMessage appends a completed user message to the session's transcript.
func (s *SessionStore) AppendUserMessage(id, content string) {
	s.Update(id, func(sess *Session) {
		sess.Messages = appendCapped(sess.Messages, ChatMessage{Role: "user", Content: content, Complete: true})
	})
}

// AppendAgentOutput appends a chunk of streamed agent output to the session's
// transcript, extending the last message if it's still an in-progress agent
// reply, or starting a new one otherwise - mirroring the dashboard's own
// client-side accumulation so replayed history renders identically.
func (s *SessionStore) AppendAgentOutput(id, text string) {
	s.Update(id, func(sess *Session) {
		if n := len(sess.Messages); n > 0 && sess.Messages[n-1].Role == "agent" && !sess.Messages[n-1].Complete {
			sess.Messages[n-1].Content += text
			return
		}
		sess.Messages = appendCapped(sess.Messages, ChatMessage{Role: "agent", Content: text, Complete: false})
	})
}

// MarkAgentDone marks the last agent message in the transcript complete, if any.
func (s *SessionStore) MarkAgentDone(id string) {
	s.Update(id, func(sess *Session) {
		if n := len(sess.Messages); n > 0 && sess.Messages[n-1].Role == "agent" {
			sess.Messages[n-1].Complete = true
		}
	})
}

// appendCapped appends m to msgs, dropping the oldest entries if the result
// would exceed maxStoredMessages.
func appendCapped(msgs []ChatMessage, m ChatMessage) []ChatMessage {
	msgs = append(msgs, m)
	if len(msgs) > maxStoredMessages {
		msgs = msgs[len(msgs)-maxStoredMessages:]
	}
	return msgs
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
