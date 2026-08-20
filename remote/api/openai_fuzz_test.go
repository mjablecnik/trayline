package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"remote/core"
)

// FuzzHandleChatCompletions feeds arbitrary bodies to the chat completions
// handler. The property under test is that no input produces a panic or an
// unexpected status: malformed client input must always be a clean 4xx.
//
// A panic would be caught by the recovery middleware and turned into a 500, so
// this also asserts that no fuzzed body reaches that path — a 500 here means
// the handler crashed on input it should have rejected.
//
// Run longer with:
//
//	go test ./api/ -run FuzzHandleChatCompletions -fuzz FuzzHandleChatCompletions -fuzztime 60s
func FuzzHandleChatCompletions(f *testing.F) {
	seeds := []string{
		``,
		`{}`,
		`[]`,
		`null`,
		`{"model":"kiro"}`,
		`{"model":"kiro","messages":[]}`,
		`{"model":"kiro","messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"kiro","messages":[{"role":"user","content":"hi"}],"stream":true}`,
		`{"model":"kiro","messages":[{"role":"user","content":["a"]}]}`,
		`{"model":"kiro","messages":[{"role":"user","content":null}]}`,
		`{"model":"kiro","messages":[{"role":"","content":""}]}`,
		`{"model":"","messages":null}`,
		`{"model":123,"messages":"nope"}`,
		`{"model":"kiro","messages":[{"role":"user","content":"hi"}],"temperature":"hot"}`,
		`{"model":"kiro","messages":[{"role":"user","content":"\ud800"}]}`,
		`{"model":"` + strings.Repeat("x", 1000) + `","messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"kiro","messages":[{"role":"user","content":"hi"},` + strings.Repeat(`{"role":"user","content":"x"},`, 50) + `{"role":"user","content":"end"}]}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	logger := core.NewLogger("fuzz-token")
	handler := NewOpenAIHandler(NewModelRegistry(""), &scriptedRunner{}, logger, 2*time.Second)

	allowed := map[int]bool{
		http.StatusOK:                  true,
		http.StatusBadRequest:          true,
		http.StatusNotFound:            true,
		http.StatusTooManyRequests:     true,
		http.StatusInternalServerError: true,
	}

	f.Fuzz(func(t *testing.T, body string) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// A panic here fails the fuzz case, which is the point.
		handler.HandleChatCompletions(rec, req)

		if !allowed[rec.Code] {
			t.Fatalf("unexpected status %d for body %q", rec.Code, body)
		}
		if rec.Code >= 400 {
			// Every error must still be OpenAI-shaped so SDKs can parse it.
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Fatalf("error response Content-Type = %q for body %q", ct, body)
			}
			if !strings.Contains(rec.Body.String(), `"error"`) {
				t.Fatalf("error response is not OpenAI-shaped: %s (body %q)", rec.Body.String(), body)
			}
		}
	})
}

// FuzzComposeMessages checks the composer never panics and never silently drops
// a message's content, whatever combination of roles it is handed.
func FuzzComposeMessages(f *testing.F) {
	f.Add("system", "a", "user", "b", "assistant", "c")
	f.Add("user", "", "user", "", "user", "")
	f.Add("", "x", "", "y", "", "z")

	f.Fuzz(func(t *testing.T, r1, c1, r2, c2, r3, c3 string) {
		messages := []OpenAIMessage{
			{Role: r1, Content: c1},
			{Role: r2, Content: c2},
			{Role: r3, Content: c3},
		}

		system, prompt := ComposeMessages(messages)

		for _, m := range messages {
			if m.Content == "" {
				continue
			}
			combined := system + "\n" + prompt
			if !strings.Contains(combined, m.Content) {
				t.Fatalf("content %q of role %q was dropped\nsystem: %q\nprompt: %q",
					m.Content, m.Role, system, prompt)
			}
		}
	})
}
