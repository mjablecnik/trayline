package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// countingFlushWriter wraps a ResponseRecorder to count Flush calls, so tests
// can verify Req 2.1's "flush each chunk immediately after writing".
type countingFlushWriter struct {
	*httptest.ResponseRecorder
	flushes int
}

func (c *countingFlushWriter) Flush() { c.flushes++; c.ResponseRecorder.Flush() }

// nonFlushWriter is an http.ResponseWriter without a Flush method.
type nonFlushWriter struct{ header http.Header }

func (n *nonFlushWriter) Header() http.Header {
	if n.header == nil {
		n.header = http.Header{}
	}
	return n.header
}
func (n *nonFlushWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *nonFlushWriter) WriteHeader(int)             {}

// parseSSEFrames splits a raw SSE body into its `data:` payloads, verifying the
// exact `data: {payload}\n\n` framing required by Req 2.2 along the way.
func parseSSEFrames(t *testing.T, body string) []string {
	t.Helper()
	if body == "" {
		return nil
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("SSE body does not end with a blank line: %q", body)
	}
	var frames []string
	for _, raw := range strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n") {
		if !strings.HasPrefix(raw, "data: ") {
			t.Fatalf("frame does not start with %q: %q", "data: ", raw)
		}
		frames = append(frames, strings.TrimPrefix(raw, "data: "))
	}
	return frames
}

func decodeChunk(t *testing.T, payload string) OpenAIStreamChunk {
	t.Helper()
	var chunk OpenAIStreamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		t.Fatalf("unmarshal chunk %q: %v", payload, err)
	}
	return chunk
}

// TestSSEWriter_Headers covers Req 2.1.
func TestSSEWriter_Headers(t *testing.T) {
	rec := httptest.NewRecorder()
	if _, err := NewSSEWriter(rec, "chatcmpl-x", "kiro", 1722345678); err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}

	want := map[string]string{
		"Content-Type":  "text/event-stream",
		"Cache-Control": "no-cache",
		"Connection":    "keep-alive",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
}

// TestSSEWriter_NonFlusher: a writer that cannot flush must be rejected with an
// error rather than panicking or silently buffering the whole stream.
func TestSSEWriter_NonFlusher(t *testing.T) {
	if _, err := NewSSEWriter(&nonFlushWriter{}, "chatcmpl-x", "kiro", 0); err == nil {
		t.Fatal("NewSSEWriter accepted a non-Flusher writer, want error")
	}
}

// TestSSEWriter_RoleOnlyInFirstChunk covers Req 2.6.
func TestSSEWriter_RoleOnlyInFirstChunk(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(rec, "chatcmpl-abc123", "kiro", 1722345678)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}

	for _, c := range []string{"Hello", " there", "!"} {
		if err := w.WriteChunk(c); err != nil {
			t.Fatalf("WriteChunk(%q): %v", c, err)
		}
	}

	frames := parseSSEFrames(t, rec.Body.String())
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}

	first := decodeChunk(t, frames[0])
	if first.Choices[0].Delta.Role != "assistant" {
		t.Errorf("first chunk delta.role = %q, want %q", first.Choices[0].Delta.Role, "assistant")
	}
	for i, f := range frames[1:] {
		if role := decodeChunk(t, f).Choices[0].Delta.Role; role != "" {
			t.Errorf("chunk %d delta.role = %q, want empty", i+1, role)
		}
	}
}

// TestSSEWriter_ChunkShape covers Req 2.2 and 2.3: stable id/model/created
// across chunks, correct object type, single choice at index 0, null
// finish_reason on content chunks, and content delivered in order.
func TestSSEWriter_ChunkShape(t *testing.T) {
	rec := httptest.NewRecorder()
	const (
		id    = "chatcmpl-abc123def456ghij7890"
		model = "claude-sonnet"
		ts    = int64(1722345678)
	)
	w, err := NewSSEWriter(rec, id, model, ts)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}

	contents := []string{"one", "two", "three"}
	for _, c := range contents {
		if err := w.WriteChunk(c); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
	}

	frames := parseSSEFrames(t, rec.Body.String())
	for i, f := range frames {
		chunk := decodeChunk(t, f)
		if chunk.ID != id {
			t.Errorf("chunk %d: id = %q, want %q", i, chunk.ID, id)
		}
		if chunk.Object != "chat.completion.chunk" {
			t.Errorf("chunk %d: object = %q, want %q", i, chunk.Object, "chat.completion.chunk")
		}
		if chunk.Created != ts {
			t.Errorf("chunk %d: created = %d, want %d", i, chunk.Created, ts)
		}
		if chunk.Model != model {
			t.Errorf("chunk %d: model = %q, want %q", i, chunk.Model, model)
		}
		if len(chunk.Choices) != 1 {
			t.Fatalf("chunk %d: %d choices, want 1", i, len(chunk.Choices))
		}
		if chunk.Choices[0].Index != 0 {
			t.Errorf("chunk %d: index = %d, want 0", i, chunk.Choices[0].Index)
		}
		if chunk.Choices[0].FinishReason != nil {
			t.Errorf("chunk %d: finish_reason = %q, want null", i, *chunk.Choices[0].FinishReason)
		}
		if chunk.Choices[0].Delta.Content != contents[i] {
			t.Errorf("chunk %d: content = %q, want %q", i, chunk.Choices[0].Delta.Content, contents[i])
		}
	}
}

// TestSSEWriter_WriteDone covers Req 2.4 and 2.5: a terminating chunk carrying
// finish_reason "stop" with an empty delta, then the [DONE] sentinel.
func TestSSEWriter_WriteDone(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(rec, "chatcmpl-x", "kiro", 1)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}
	if err := w.WriteChunk("hi"); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if err := w.WriteDone(); err != nil {
		t.Fatalf("WriteDone: %v", err)
	}

	frames := parseSSEFrames(t, rec.Body.String())
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3 (content, stop, [DONE])", len(frames))
	}
	if frames[2] != "[DONE]" {
		t.Errorf("last frame = %q, want %q", frames[2], "[DONE]")
	}

	final := decodeChunk(t, frames[1])
	if final.Choices[0].FinishReason == nil || *final.Choices[0].FinishReason != "stop" {
		t.Errorf("final chunk finish_reason = %v, want \"stop\"", final.Choices[0].FinishReason)
	}
	if final.Choices[0].Delta != (OpenAIStreamDelta{}) {
		t.Errorf("final chunk delta = %+v, want empty", final.Choices[0].Delta)
	}
	// Req 2.4 says the delta must serialise as an empty JSON object, so SDKs
	// see no phantom content or role on the terminating chunk.
	if !strings.Contains(frames[1], `"delta":{}`) {
		t.Errorf("final chunk does not serialise delta as {}: %s", frames[1])
	}
}

// TestSSEWriter_ExactlyOneDone covers design Property 5 for both termination
// paths: a clean finish and an error-triggered graceful close.
func TestSSEWriter_ExactlyOneDone(t *testing.T) {
	tests := map[string]func(*SSEWriter) error{
		"WriteDone":  func(w *SSEWriter) error { return w.WriteDone() },
		"WriteError": func(w *SSEWriter) error { return w.WriteError() },
	}

	for name, terminate := range tests {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			w, err := NewSSEWriter(rec, "chatcmpl-x", "kiro", 1)
			if err != nil {
				t.Fatalf("NewSSEWriter: %v", err)
			}
			if err := w.WriteChunk("partial"); err != nil {
				t.Fatalf("WriteChunk: %v", err)
			}
			if err := terminate(w); err != nil {
				t.Fatalf("%s: %v", name, err)
			}

			body := rec.Body.String()
			if got := strings.Count(body, "data: [DONE]\n\n"); got != 1 {
				t.Errorf("[DONE] appears %d times, want exactly 1\nbody: %q", got, body)
			}
			frames := parseSSEFrames(t, body)
			if frames[len(frames)-1] != "[DONE]" {
				t.Errorf("stream does not end with [DONE]: %q", frames[len(frames)-1])
			}
		})
	}
}

// TestSSEWriter_ErrorBeforeAnyContent: if the agent fails before producing
// output, the stream must still terminate as a well-formed OpenAI stream so the
// SDK sees a clean end rather than a truncated connection (Req 2.7).
func TestSSEWriter_ErrorBeforeAnyContent(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(rec, "chatcmpl-x", "kiro", 1)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}
	if err := w.WriteError(); err != nil {
		t.Fatalf("WriteError: %v", err)
	}

	frames := parseSSEFrames(t, rec.Body.String())
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2 (stop, [DONE])", len(frames))
	}
	stop := decodeChunk(t, frames[0])
	if stop.Choices[0].FinishReason == nil || *stop.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %v, want \"stop\"", stop.Choices[0].FinishReason)
	}
	if frames[1] != "[DONE]" {
		t.Errorf("last frame = %q, want [DONE]", frames[1])
	}
}

// TestSSEWriter_FlushesEveryFrame covers Req 2.1: chunks must reach the network
// as they are produced, not sit in a buffer until the handler returns.
func TestSSEWriter_FlushesEveryFrame(t *testing.T) {
	cw := &countingFlushWriter{ResponseRecorder: httptest.NewRecorder()}
	w, err := NewSSEWriter(cw, "chatcmpl-x", "kiro", 1)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := w.WriteChunk("x"); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
	}
	if cw.flushes != 3 {
		t.Errorf("after 3 chunks: %d flushes, want 3", cw.flushes)
	}

	if err := w.WriteDone(); err != nil {
		t.Fatalf("WriteDone: %v", err)
	}
	// The stop chunk and the [DONE] sentinel must both be flushed.
	if cw.flushes < 5 {
		t.Errorf("after WriteDone: %d flushes, want at least 5", cw.flushes)
	}
}

// TestSSEWriter_UnicodeAndNewlineContent: SSE is a line-oriented protocol, so
// content containing newlines must stay inside a single JSON payload (escaped)
// rather than splitting the frame.
func TestSSEWriter_UnicodeAndNewlineContent(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(rec, "chatcmpl-x", "kiro", 1)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}

	content := "řádek jedna\nřádek dvě 🐴\n"
	if err := w.WriteChunk(content); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	frames := parseSSEFrames(t, rec.Body.String())
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1 — embedded newlines split the SSE frame", len(frames))
	}
	if got := decodeChunk(t, frames[0]).Choices[0].Delta.Content; got != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}
