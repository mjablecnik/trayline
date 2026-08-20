package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestEstimateTokens_Unicode covers Req 8.2, which defines the estimate in terms
// of *character* length. Go's len() returns bytes, so multi-byte text (Czech
// diacritics, emoji) would otherwise be over-counted by 2–4x.
func TestEstimateTokens_Unicode(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"ascii", "hello world"},
		{"czech diacritics", "Příliš žluťoučký kůň úpěl ďábelské ódy"},
		{"emoji", "🐴🐴🐴🐴"},
		{"mixed", "Ahoj 🐴 world"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := (utf8.RuneCountInString(tt.text) + 2) / 4
			if got := estimateTokens(tt.text); got != want {
				t.Errorf("estimateTokens(%q) = %d, want %d (character count %d / 4)",
					tt.text, got, want, utf8.RuneCountInString(tt.text))
			}
		})
	}
}

// TestEstimateTokens_NonNegative covers Req 8.1: token counts are non-negative
// integers for any input.
func TestEstimateTokens_NonNegative(t *testing.T) {
	for _, s := range []string{"", "a", strings.Repeat("x", 10000), "🐴"} {
		if got := estimateTokens(s); got < 0 {
			t.Errorf("estimateTokens(len %d) = %d, want >= 0", len(s), got)
		}
	}
}

// TestGenerateCompletionID_Uniqueness guards against a truncation scheme that
// collides. Req 1.4 requires a random ID per response; a repeated ID would make
// client-side stream correlation ambiguous.
func TestGenerateCompletionID_Uniqueness(t *testing.T) {
	const n = 10000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := generateCompletionID()
		if seen[id] {
			t.Fatalf("duplicate ID %q after %d generations", id, i)
		}
		seen[id] = true
	}
}

// TestGenerateCompletionID_Charset covers Req 1.4: "chatcmpl-" followed by an
// alphanumeric string, at least 24 characters in total.
func TestGenerateCompletionID_Charset(t *testing.T) {
	for i := 0; i < 100; i++ {
		id := generateCompletionID()
		if !strings.HasPrefix(id, "chatcmpl-") {
			t.Fatalf("ID %q missing %q prefix", id, "chatcmpl-")
		}
		if len(id) < 24 {
			t.Fatalf("ID %q is %d chars, want at least 24", id, len(id))
		}
		for _, r := range strings.TrimPrefix(id, "chatcmpl-") {
			isAlnum := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
			if !isAlnum {
				t.Fatalf("ID %q contains non-alphanumeric character %q", id, r)
			}
		}
	}
}

// TestWriteOpenAIError_NullParamAndCode covers Req 5.2 and 6.1: `param` and
// `code` must be present and JSON null when unset — not omitted. SDKs read these
// keys directly, and a missing key is not the same as a null one.
func TestWriteOpenAIError_NullParamAndCode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeOpenAIError(rec, 401, "invalid_request_error", "missing header", nil, nil)

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if rec.Code != 401 {
		t.Errorf("status = %d, want 401", rec.Code)
	}

	var raw map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal body %q: %v", rec.Body.String(), err)
	}
	errObj, ok := raw["error"]
	if !ok {
		t.Fatalf("body has no \"error\" object: %s", rec.Body.String())
	}
	for _, key := range []string{"message", "type", "param", "code"} {
		if _, present := errObj[key]; !present {
			t.Errorf("error object is missing key %q: %s", key, rec.Body.String())
		}
	}
	if errObj["param"] != nil {
		t.Errorf("param = %v, want null", errObj["param"])
	}
	if errObj["code"] != nil {
		t.Errorf("code = %v, want null", errObj["code"])
	}
}

// TestWriteOpenAIError_PopulatedParamAndCode is the counterpart: when set, the
// values must round-trip as plain JSON strings.
func TestWriteOpenAIError_PopulatedParamAndCode(t *testing.T) {
	rec := httptest.NewRecorder()
	param, code := "messages[0].role", "model_not_found"
	writeOpenAIError(rec, 404, "invalid_request_error", "nope", &param, &code)

	var resp OpenAIErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error.Param == nil || *resp.Error.Param != param {
		t.Errorf("param = %v, want %q", resp.Error.Param, param)
	}
	if resp.Error.Code == nil || *resp.Error.Code != code {
		t.Errorf("code = %v, want %q", resp.Error.Code, code)
	}
	if resp.Error.Type != "invalid_request_error" {
		t.Errorf("type = %q, want invalid_request_error", resp.Error.Type)
	}
}

// TestOpenAIChatResponse_JSONShape pins the serialised field names and the
// presence of `usage` for non-streaming responses (Req 1.5, 8.1). Struct tag
// typos are invisible to Go tests that only inspect structs.
func TestOpenAIChatResponse_JSONShape(t *testing.T) {
	resp := OpenAIChatResponse{
		ID:      "chatcmpl-x",
		Object:  "chat.completion",
		Created: 1,
		Model:   "kiro",
		Choices: []OpenAIChoice{{
			Index:        0,
			Message:      OpenAIMessage{Role: "assistant", Content: "hi"},
			FinishReason: "stop",
		}},
		Usage: OpenAIUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{
		`"id"`, `"object"`, `"created"`, `"model"`, `"choices"`, `"usage"`,
		`"index"`, `"message"`, `"finish_reason"`, `"role"`, `"content"`,
		`"prompt_tokens"`, `"completion_tokens"`, `"total_tokens"`,
	} {
		if !strings.Contains(string(data), key) {
			t.Errorf("serialised response is missing %s: %s", key, data)
		}
	}
}

// TestOpenAIChatResponse_EmptyContentSerialises covers Req 8.3: an agent that
// produced no output must yield content "" — not a missing key, which SDKs
// surface as None and callers then crash on.
func TestOpenAIChatResponse_EmptyContentSerialises(t *testing.T) {
	data, err := json.Marshal(OpenAIChoice{
		Index:        0,
		Message:      OpenAIMessage{Role: "assistant", Content: ""},
		FinishReason: "stop",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"content":""`) {
		t.Errorf("empty content not serialised as \"\": %s", data)
	}
}

// --- Message content decoding (structured parts + the developer role) ------

// TestOpenAIMessage_UnmarshalContentForms covers the two content shapes real
// clients send, plus the edge cases around them.
func TestOpenAIMessage_UnmarshalContentForms(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		wantRole    string
		wantContent string
		wantErr     string // substring; empty means success expected
	}{
		{
			name:        "plain string content",
			json:        `{"role":"user","content":"Hello"}`,
			wantRole:    "user",
			wantContent: "Hello",
		},
		{
			name:        "single text part",
			json:        `{"role":"user","content":[{"type":"text","text":"Hello"}]}`,
			wantRole:    "user",
			wantContent: "Hello",
		},
		{
			name:        "multiple text parts are newline-joined",
			json:        `{"role":"user","content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]}`,
			wantRole:    "user",
			wantContent: "one\ntwo",
		},
		{
			name:        "empty parts array",
			json:        `{"role":"user","content":[]}`,
			wantRole:    "user",
			wantContent: "",
		},
		{
			name:        "part without an explicit type is treated as text",
			json:        `{"role":"user","content":[{"text":"Hello"}]}`,
			wantRole:    "user",
			wantContent: "Hello",
		},
		{
			name:        "null content becomes empty",
			json:        `{"role":"assistant","content":null}`,
			wantRole:    "assistant",
			wantContent: "",
		},
		{
			name:        "absent content becomes empty",
			json:        `{"role":"assistant"}`,
			wantRole:    "assistant",
			wantContent: "",
		},
		{
			name:        "developer role is normalised to system",
			json:        `{"role":"developer","content":"Be brief"}`,
			wantRole:    "system",
			wantContent: "Be brief",
		},
		{
			name:        "developer role with content parts",
			json:        `{"role":"developer","content":[{"type":"text","text":"Be brief"}]}`,
			wantRole:    "system",
			wantContent: "Be brief",
		},
		{
			name:        "unicode survives part flattening",
			json:        `{"role":"user","content":[{"type":"text","text":"Příliš 🐴"}]}`,
			wantRole:    "user",
			wantContent: "Příliš 🐴",
		},
		{
			name:    "image parts are rejected, not silently dropped",
			json:    `{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://x/y.png"}}]}`,
			wantErr: "image_url",
		},
		{
			name:    "mixed text and image is rejected",
			json:    `{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url"}]}`,
			wantErr: "image_url",
		},
		{
			name:    "numeric content is rejected",
			json:    `{"role":"user","content":42}`,
			wantErr: "must be a string or an array",
		},
		{
			name:    "object content is rejected",
			json:    `{"role":"user","content":{"text":"hi"}}`,
			wantErr: "must be a string or an array",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m OpenAIMessage
			err := json.Unmarshal([]byte(tt.json), &m)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error mentioning %q, got message %+v", tt.wantErr, m)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if m.Role != tt.wantRole {
				t.Errorf("role = %q, want %q", m.Role, tt.wantRole)
			}
			if m.Content != tt.wantContent {
				t.Errorf("content = %q, want %q", m.Content, tt.wantContent)
			}
		})
	}
}

// TestOpenAIMessage_ResponseSerialisationUnchanged: adding UnmarshalJSON must
// not disturb how messages are written back out in responses.
func TestOpenAIMessage_ResponseSerialisationUnchanged(t *testing.T) {
	data, err := json.Marshal(OpenAIMessage{Role: "assistant", Content: "hi"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"role":"assistant","content":"hi"}` {
		t.Errorf("serialised as %s, want the plain string form", data)
	}
}

// TestOpenAIMessage_RoundTrip: a message decoded from the parts form must
// re-serialise as a plain string, which is what response consumers expect.
func TestOpenAIMessage_RoundTrip(t *testing.T) {
	var m OpenAIMessage
	if err := json.Unmarshal([]byte(`{"role":"user","content":[{"type":"text","text":"hi"}]}`), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"role":"user","content":"hi"}` {
		t.Errorf("round-tripped to %s, want the flattened string form", data)
	}
}
