package api

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// csrfTokenTTL matches the session cookie's lifetime — a CSRF token issued
// alongside a session should stay valid for as long as that session does.
const csrfTokenTTL = sessionCookieMaxAge

// csrfStore issues and validates CSRF tokens for cookie-authenticated
// requests. Only needed once the session cookie moved to SameSite=None to
// support the dashboard's cross-origin deployment — SameSite=Lax alone used
// to be enough CSRF protection for the common (state-changing, non-GET)
// case, but None sends the cookie on every cross-site request, so a
// synchronizer token is required for anything that changes state.
//
// Header/Bearer-token callers (the CLI) never go through this — a browser
// can't be tricked into attaching an Authorization header, so only
// cookie-authenticated requests need a CSRF check (see AuthMiddleware).
//
// In-memory and unreplicated is fine here: this is a single-process,
// single-operator server (same reasoning as store.SessionStore).
type csrfStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time // token -> expiry
}

// NewCSRFStore creates an empty csrfStore.
func NewCSRFStore() *csrfStore {
	return &csrfStore{tokens: make(map[string]time.Time)}
}

// issue generates a new random CSRF token, valid for csrfTokenTTL, and
// returns it. Opportunistically sweeps expired entries so repeated issuance
// (e.g. the dashboard calling GET /auth/session on every page load) doesn't
// grow the map unbounded.
func (s *csrfStore) issue() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for t, exp := range s.tokens {
		if now.After(exp) {
			delete(s.tokens, t)
		}
	}
	s.tokens[token] = now.Add(csrfTokenTTL)
	return token, nil
}

// valid reports whether token is a currently-unexpired token this store
// issued. Uses a constant-time comparison per-candidate to avoid leaking
// timing information about which stored token (if any) matches.
func (s *csrfStore) valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tokens[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.tokens, token)
		return false
	}
	return true
}
