package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"pgregory.net/rapid"
)

// Property 3: WebSocket URL construction includes provided parameters
func TestProperty_WSURLConstruction(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		agent     := rapid.StringMatching(`[a-z][a-z0-9]{0,15}`).Draw(rt, "agent")
		model     := rapid.OneOf(rapid.Just(""), rapid.StringMatching(`[a-z0-9-]{1,20}`)).Draw(rt, "model")
		system    := rapid.OneOf(rapid.Just(""), rapid.StringMatching(`[a-zA-Z0-9 ]{1,50}`)).Draw(rt, "system")
		sessionID := rapid.OneOf(rapid.Just(""), rapid.StringMatching(`[a-z0-9]{4,16}`)).Draw(rt, "sessionID")

		path   := buildChatPath(sessionID)
		params := buildChatParams(agent, model, system)

		// Path assertion
		if sessionID == "" {
			if path != "/chat" {
				rt.Fatalf("expected /chat, got %q", path)
			}
		} else {
			if path != "/chat/"+sessionID {
				rt.Fatalf("expected /chat/%s, got %q", sessionID, path)
			}
		}

		// Agent always present
		if params.Get("agent") != agent {
			rt.Fatalf("agent param: got %q, want %q", params.Get("agent"), agent)
		}

		// Model only when non-empty
		if model == "" {
			if params.Has("model") {
				rt.Fatalf("model should be absent when empty, got %q", params.Get("model"))
			}
		} else {
			if params.Get("model") != model {
				rt.Fatalf("model param: got %q, want %q", params.Get("model"), model)
			}
		}

		// System only when non-empty
		if system == "" {
			if params.Has("system") {
				rt.Fatalf("system should be absent when empty, got %q", params.Get("system"))
			}
		} else {
			if params.Get("system") != system {
				rt.Fatalf("system param: got %q, want %q", params.Get("system"), system)
			}
		}

		// Values survive URL encode/decode round-trip (verifies URL-encoding).
		decoded, err := url.ParseQuery(params.Encode())
		if err != nil {
			rt.Fatalf("params.Encode() produced invalid query string: %v", err)
		}
		if decoded.Get("agent") != agent {
			rt.Fatalf("agent URL-roundtrip: got %q, want %q", decoded.Get("agent"), agent)
		}
		if model != "" && decoded.Get("model") != model {
			rt.Fatalf("model URL-roundtrip: got %q, want %q", decoded.Get("model"), model)
		}
		if system != "" && decoded.Get("system") != system {
			rt.Fatalf("system URL-roundtrip: got %q, want %q", decoded.Get("system"), system)
		}
	})
}

// Property 4: Empty input lines are filtered
func TestProperty_EmptyInputFiltering(t *testing.T) {
	// Whitespace-only strings are discarded.
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringOf(rapid.RuneFrom([]rune{' ', '\t'})).Draw(rt, "whitespace")
		if shouldSendLine(s) {
			rt.Fatalf("whitespace-only line %q should be filtered", s)
		}
	})

	// Strings with at least one non-whitespace character are sent.
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringMatching(`[a-zA-Z0-9!@#$%]{1,80}`).Draw(rt, "nonWhitespace")
		if !shouldSendLine(s) {
			rt.Fatalf("non-whitespace line %q should be sent", s)
		}
	})
}

// wsUpgrader is shared across test helpers.
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsTestServer starts an httptest.Server that upgrades connections and runs handler.
func wsTestServer(t *testing.T, handler func(conn *websocket.Conn)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn)
	}))
}

// dialWSServer dials the test server's WebSocket endpoint.
func dialWSServer(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + srv.URL[4:] + path
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", wsURL, err)
	}
	return conn
}

// writeSrvMsg sends a WSServerMessage on the server-side connection.
func writeSrvMsg(t *testing.T, conn *websocket.Conn, msg WSServerMessage) {
	t.Helper()
	data, _ := json.Marshal(msg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Logf("server write error (conn may be closing): %v", err)
	}
}

// readClientMsg reads and unmarshals a WSClientMessage from the server-side connection.
// Returns (msg, true) on success or (zero, false) on connection close.
func readClientMsg(t *testing.T, conn *websocket.Conn) (WSClientMessage, bool) {
	t.Helper()
	_, data, err := conn.ReadMessage()
	if err != nil {
		return WSClientMessage{}, false
	}
	var msg WSClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal client message: %v", err)
	}
	return msg, true
}

// startLoop runs chatLoop in a goroutine and returns a channel that receives the exit code.
func startLoop(conn *websocket.Conn, cfg *Config, stdinR io.Reader, sigCh <-chan os.Signal) <-chan int {
	ch := make(chan int, 1)
	go func() { ch <- chatLoop(conn, cfg, stdinR, sigCh) }()
	return ch
}

// expectCode waits for the chatLoop exit code with a 3-second timeout.
func expectCode(t *testing.T, ch <-chan int, wantCode int) {
	t.Helper()
	select {
	case code := <-ch:
		if code != wantCode {
			t.Fatalf("exit code: got %d, want %d", code, wantCode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("chatLoop timed out waiting for exit")
	}
}

// TestChatLoop_SessionLifecycle tests: session_started → send → output → done → /quit
func TestChatLoop_SessionLifecycle(t *testing.T) {
	clientMsgCh := make(chan WSClientMessage, 4)
	serverDone  := make(chan struct{})

	srv := wsTestServer(t, func(conn *websocket.Conn) {
		writeSrvMsg(t, conn, WSServerMessage{Type: "session_started", SessionID: "sess-abc"})

		msg, ok := readClientMsg(t, conn)
		if !ok {
			return
		}
		clientMsgCh <- msg // user message

		writeSrvMsg(t, conn, WSServerMessage{Type: "output", Data: "hello "})
		writeSrvMsg(t, conn, WSServerMessage{Type: "output", Data: "world"})
		writeSrvMsg(t, conn, WSServerMessage{Type: "done"})

		msg, ok = readClientMsg(t, conn)
		if !ok {
			return
		}
		clientMsgCh <- msg // /quit terminate

		writeSrvMsg(t, conn, WSServerMessage{Type: "terminated"})
		close(serverDone)
	})
	defer srv.Close()

	clientConn := dialWSServer(t, srv, "/chat")
	defer clientConn.Close()

	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()

	sigCh  := make(chan os.Signal, 2)
	cfg    := &Config{Token: "test"}
	codeCh := startLoop(clientConn, cfg, stdinR, sigCh)

	time.Sleep(50 * time.Millisecond)
	stdinW.Write([]byte("hi there\n"))

	select {
	case msg := <-clientMsgCh:
		if msg.Type != "message" || msg.Prompt != "hi there" {
			t.Fatalf("got %+v, want message{prompt:'hi there'}", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive user message")
	}

	time.Sleep(50 * time.Millisecond)
	stdinW.Write([]byte("/quit\n"))

	select {
	case msg := <-clientMsgCh:
		if msg.Type != "terminate" {
			t.Fatalf("expected terminate, got %+v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive terminate")
	}

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server handler did not finish")
	}

	expectCode(t, codeCh, 0)
}

// TestChatLoop_SessionResumed tests the reconnect flow (session_resumed).
func TestChatLoop_SessionResumed(t *testing.T) {
	srv := wsTestServer(t, func(conn *websocket.Conn) {
		writeSrvMsg(t, conn, WSServerMessage{Type: "session_resumed", SessionID: "sess-xyz"})
		msg, ok := readClientMsg(t, conn)
		if !ok {
			return
		}
		if msg.Type == "terminate" {
			writeSrvMsg(t, conn, WSServerMessage{Type: "terminated"})
		}
	})
	defer srv.Close()

	clientConn := dialWSServer(t, srv, "/chat/sess-xyz")
	defer clientConn.Close()

	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()

	sigCh  := make(chan os.Signal, 2)
	cfg    := &Config{Token: "test"}
	codeCh := startLoop(clientConn, cfg, stdinR, sigCh)

	time.Sleep(50 * time.Millisecond)
	stdinW.Write([]byte("/quit\n"))

	expectCode(t, codeCh, 0)
}

// TestChatLoop_UnexpectedClose tests that an abrupt server close exits with code 1.
func TestChatLoop_UnexpectedClose(t *testing.T) {
	srv := wsTestServer(t, func(conn *websocket.Conn) {
		writeSrvMsg(t, conn, WSServerMessage{Type: "session_started", SessionID: "s3"})
		// Close without sending "terminated"
	})
	defer srv.Close()

	clientConn := dialWSServer(t, srv, "/chat")
	defer clientConn.Close()

	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()

	sigCh  := make(chan os.Signal, 2)
	cfg    := &Config{Token: "test"}
	codeCh := startLoop(clientConn, cfg, stdinR, sigCh)

	expectCode(t, codeCh, 1)
}

// TestChatLoop_EmptyInputFiltered tests that whitespace-only lines are not forwarded.
func TestChatLoop_EmptyInputFiltered(t *testing.T) {
	serverMsgCh := make(chan WSClientMessage, 8)

	srv := wsTestServer(t, func(conn *websocket.Conn) {
		writeSrvMsg(t, conn, WSServerMessage{Type: "session_started", SessionID: "s4"})
		for {
			msg, ok := readClientMsg(t, conn)
			if !ok {
				return
			}
			if msg.Type == "terminate" {
				// Acknowledge quit so chatLoop exits cleanly.
				writeSrvMsg(t, conn, WSServerMessage{Type: "terminated"})
				return
			}
			serverMsgCh <- msg
		}
	})
	defer srv.Close()

	clientConn := dialWSServer(t, srv, "/chat")
	defer clientConn.Close()

	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()

	sigCh  := make(chan os.Signal, 2)
	cfg    := &Config{Token: "test"}
	codeCh := startLoop(clientConn, cfg, stdinR, sigCh)

	time.Sleep(40 * time.Millisecond)

	// Send whitespace-only lines — none of these should be forwarded.
	stdinW.Write([]byte("   \n"))
	stdinW.Write([]byte("\t\t\n"))
	stdinW.Write([]byte("\n"))

	time.Sleep(20 * time.Millisecond)

	// Send a real message — this should be forwarded.
	stdinW.Write([]byte("hello\n"))

	select {
	case msg := <-serverMsgCh:
		if msg.Type != "message" || msg.Prompt != "hello" {
			t.Fatalf("first message: got %+v, want message{prompt:'hello'}", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive the real message")
	}

	// Ensure no extra messages arrived before we quit.
	select {
	case extra := <-serverMsgCh:
		t.Fatalf("unexpected extra message: %+v", extra)
	case <-time.After(50 * time.Millisecond):
		// Good — no whitespace messages were sent.
	}

	stdinW.Write([]byte("/quit\n"))
	expectCode(t, codeCh, 0)
}

// TestHandleChat_HTTP404 tests that a 404 response yields exit code 1.
func TestHandleChat_HTTP404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found","message":"Session not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	cfg  := &Config{ServerURL: srv.URL, Token: "tok"}
	code := handleChat([]string{"--agent", "claude", "--session", "missing-id"}, cfg)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

// TestHandleChat_HTTP409 tests that a 409 response yields exit code 1.
func TestHandleChat_HTTP409(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"conflict","message":"Session already active"}`, http.StatusConflict)
	}))
	defer srv.Close()

	cfg  := &Config{ServerURL: srv.URL, Token: "tok"}
	code := handleChat([]string{"--agent", "claude", "--session", "busy-id"}, cfg)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

// TestHandleChat_MissingAgent tests that omitting --agent returns exit code 2.
func TestHandleChat_MissingAgent(t *testing.T) {
	cfg  := &Config{ServerURL: "http://localhost:9999", Token: "tok"}
	code := handleChat([]string{}, cfg)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
}

// TestChatLoop_FirstSIGINT_SendsInterrupt verifies first SIGINT sends {"type":"interrupt"}.
func TestChatLoop_FirstSIGINT_SendsInterrupt(t *testing.T) {
	serverMsgCh := make(chan WSClientMessage, 8)

	srv := wsTestServer(t, func(conn *websocket.Conn) {
		writeSrvMsg(t, conn, WSServerMessage{Type: "session_started", SessionID: "s5"})
		for {
			msg, ok := readClientMsg(t, conn)
			if !ok {
				return
			}
			serverMsgCh <- msg
		}
	})
	defer srv.Close()

	clientConn := dialWSServer(t, srv, "/chat")
	defer clientConn.Close()

	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()

	sigCh  := make(chan os.Signal, 2)
	cfg    := &Config{Token: "test"}
	codeCh := startLoop(clientConn, cfg, stdinR, sigCh)

	time.Sleep(50 * time.Millisecond)

	// Send a user message first
	stdinW.Write([]byte("query\n"))

	select {
	case msg := <-serverMsgCh:
		if msg.Type != "message" {
			t.Fatalf("expected message, got %+v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive user message")
	}

	// First SIGINT → should produce "interrupt"
	sigCh <- syscall.SIGINT

	select {
	case msg := <-serverMsgCh:
		if msg.Type != "interrupt" {
			t.Fatalf("expected interrupt, got %+v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive interrupt")
	}

	// Second SIGINT → exit 130
	sigCh <- syscall.SIGINT

	expectCode(t, codeCh, 130)
}

// TestChatLoop_SecondSIGINT_ExitsCode130 verifies two consecutive SIGINTs exit with 130.
func TestChatLoop_SecondSIGINT_ExitsCode130(t *testing.T) {
	srv := wsTestServer(t, func(conn *websocket.Conn) {
		writeSrvMsg(t, conn, WSServerMessage{Type: "session_started", SessionID: "s6"})
		for {
			if _, ok := readClientMsg(t, conn); !ok {
				return
			}
		}
	})
	defer srv.Close()

	clientConn := dialWSServer(t, srv, "/chat")
	defer clientConn.Close()

	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()

	sigCh  := make(chan os.Signal, 2)
	cfg    := &Config{Token: "test"}
	codeCh := startLoop(clientConn, cfg, stdinR, sigCh)

	time.Sleep(50 * time.Millisecond)

	sigCh <- syscall.SIGINT
	sigCh <- syscall.SIGINT

	expectCode(t, codeCh, 130)
}

// TestChatLoop_SIGTERM_ExitsCode0 verifies SIGTERM sends terminate and exits 0.
func TestChatLoop_SIGTERM_ExitsCode0(t *testing.T) {
	srv := wsTestServer(t, func(conn *websocket.Conn) {
		writeSrvMsg(t, conn, WSServerMessage{Type: "session_started", SessionID: "s7"})
		msg, ok := readClientMsg(t, conn)
		if !ok {
			return
		}
		if msg.Type == "terminate" {
			writeSrvMsg(t, conn, WSServerMessage{Type: "terminated"})
		}
	})
	defer srv.Close()

	clientConn := dialWSServer(t, srv, "/chat")
	defer clientConn.Close()

	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()

	sigCh  := make(chan os.Signal, 2)
	cfg    := &Config{Token: "test"}
	codeCh := startLoop(clientConn, cfg, stdinR, sigCh)

	time.Sleep(50 * time.Millisecond)
	sigCh <- syscall.SIGTERM

	expectCode(t, codeCh, 0)
}

// TestBuildChatPath verifies path construction.
func TestBuildChatPath(t *testing.T) {
	if p := buildChatPath(""); p != "/chat" {
		t.Fatalf("empty session: got %q, want /chat", p)
	}
	if p := buildChatPath("abc123"); p != "/chat/abc123" {
		t.Fatalf("with session: got %q, want /chat/abc123", p)
	}
}

// TestBuildChatParams verifies query parameter construction.
func TestBuildChatParams(t *testing.T) {
	p := buildChatParams("claude", "", "")
	if p.Get("agent") != "claude" {
		t.Fatalf("agent: got %q, want claude", p.Get("agent"))
	}
	if p.Has("model") || p.Has("system") {
		t.Fatal("empty model/system should not appear in params")
	}

	p2 := buildChatParams("kiro", "gpt-4", "You are helpful")
	if p2.Get("model") != "gpt-4" {
		t.Fatalf("model: got %q, want gpt-4", p2.Get("model"))
	}
	if p2.Get("system") != "You are helpful" {
		t.Fatalf("system: got %q, want 'You are helpful'", p2.Get("system"))
	}
}
