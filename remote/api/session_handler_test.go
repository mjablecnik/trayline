package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"remote/core"
	"remote/docker"
	"remote/store"
)

// Regression: the dashboard's cross-project "all sessions" view needs to know
// which project each session belongs to, so GET /sessions must include it.
func TestHandleGetSessions_IncludesProject(t *testing.T) {
	logger := core.NewLogger("test-token")
	cfg := &core.Config{}
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)
	sessionStore := store.NewSessionStore()
	h := NewSessionHandler(sessionStore, cm, logger, cfg, nil)

	sessionStore.Add(&store.Session{ID: "sess-1", Agent: "claude", Project: "myproject"})
	sessionStore.Add(&store.Session{ID: "sess-2", Agent: "kiro"})

	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	rec := httptest.NewRecorder()

	h.HandleGetSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []struct {
		SessionID string `json:"session_id"`
		Project   string `json:"project"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(got))
	}

	byID := map[string]string{}
	for _, s := range got {
		byID[s.SessionID] = s.Project
	}
	if byID["sess-1"] != "myproject" {
		t.Errorf("expected sess-1 project %q, got %q", "myproject", byID["sess-1"])
	}
	if byID["sess-2"] != "" {
		t.Errorf("expected sess-2 to have no project, got %q", byID["sess-2"])
	}
}

// Security regression: a WebSocket upgrade request carrying a present but
// WRONG Authorization header used to be treated as "has a header, so skip
// the post-connect auth challenge" without ever checking the header's
// value — a full authentication bypass for any client able to set a custom
// header on the upgrade request (e.g. any non-browser WS client). It must
// now be rejected with 401 before the connection is ever upgraded.
func TestHandleChat_RejectsInvalidBearerTokenBeforeUpgrade(t *testing.T) {
	logger := core.NewLogger("test-token")
	cfg := &core.Config{APIToken: "correct-token"}
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)
	h := NewSessionHandler(store.NewSessionStore(), cm, logger, cfg, nil)

	srv := httptest.NewServer(http.HandlerFunc(h.HandleChat))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"?agent=claude", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid bearer token, got %d", resp.StatusCode)
	}
}

// Cookie-based auth (see auth.go's providedToken) must also be checked
// pre-upgrade, with the same reject-if-wrong / defer-if-absent semantics as
// the Authorization header — this is what lets the dashboard authenticate
// its WebSocket connections via the HttpOnly session cookie instead of
// reading a token out of localStorage to send as a header.
func TestHandleChat_RejectsInvalidSessionCookieBeforeUpgrade(t *testing.T) {
	logger := core.NewLogger("test-token")
	cfg := &core.Config{APIToken: "correct-token"}
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)
	h := NewSessionHandler(store.NewSessionStore(), cm, logger, cfg, nil)

	srv := httptest.NewServer(http.HandlerFunc(h.HandleChat))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"?agent=claude", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "wrong-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid session cookie, got %d", resp.StatusCode)
	}
}

// Unit-level check that a valid credential (header OR cookie) is treated as
// "skip the post-upgrade handshake" — i.e. hasCred=true with a valid token
// must make wsTokenInvalid report false, the exact condition every WS
// handler's `if !hasCred { wsAuth(conn) }` branch relies on to skip wsAuth.
// (Not an HTTP-level test: letting real auth succeed on HandleChat drives it
// into container/session creation against the noop docker test double, which
// panics deeper in unrelated, pre-existing streamOutput machinery — this
// checks the same contract without that unrelated failure mode.)
func TestWsTokenInvalid_ValidCookieOrHeaderIsNotInvalid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/chat?agent=claude", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "correct-token"})

	provided, hasCred := providedToken(req)
	if !hasCred {
		t.Fatal("expected providedToken to report a credential was supplied")
	}
	if provided != "correct-token" {
		t.Fatalf("expected extracted token %q, got %q", "correct-token", provided)
	}
	if wsTokenInvalid(provided, hasCred, "correct-token") {
		t.Error("expected a valid cookie to NOT be reported invalid (would incorrectly skip upgrade)")
	}
	if !wsTokenInvalid(provided, hasCred, "different-token") {
		t.Error("expected a valid-format but wrong-value cookie to be reported invalid")
	}
}

// An absent Authorization header must still be allowed through to the
// post-upgrade wsAuth handshake (browser clients cannot set custom
// WebSocket headers), not rejected outright.
func TestHandleChat_MissingAuthHeaderDefersToPostUpgradeHandshake(t *testing.T) {
	logger := core.NewLogger("test-token")
	cfg := &core.Config{APIToken: "correct-token", MaxChatSessions: 4}
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)
	h := NewSessionHandler(store.NewSessionStore(), cm, logger, cfg, nil)

	srv := httptest.NewServer(http.HandlerFunc(h.HandleChat))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?agent=claude"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// wsAuth rejects the first message unless it is {"type":"auth","token":"..."}
	// with the correct token — send a wrong one and expect an error message
	// followed by connection close, proving the post-upgrade challenge
	// actually ran (as opposed to having been silently skipped).
	if err := conn.WriteJSON(map[string]string{"type": "auth", "token": "wrong-token"}); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected an error message before close, got read error: %v", err)
	}
	var msg WSServerMessage
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "error" {
		t.Fatalf("expected {\"type\":\"error\",...}, got %q", data)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected connection to be closed after failed post-upgrade auth")
	}
}
