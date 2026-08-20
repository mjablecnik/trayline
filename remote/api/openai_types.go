package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// --- Request types ---

// OpenAIChatRequest is the request body for POST /v1/chat/completions.
//
// Every field the server ignores is typed as json.RawMessage rather than its
// nominal OpenAI type. Req 10.4 requires that an ignored parameter sent with
// the wrong type (a string where a number belongs, say) still be accepted —
// and json.Decode rejects the *whole* body on the first type mismatch, so a
// nominally-typed unused field would turn a harmless client quirk into a 400.
type OpenAIChatRequest struct {
	Model            string          `json:"model"`
	Messages         []OpenAIMessage `json:"messages"`
	Stream           bool            `json:"stream,omitempty"`
	Temperature      json.RawMessage `json:"temperature,omitempty"`
	TopP             json.RawMessage `json:"top_p,omitempty"`
	MaxTokens        json.RawMessage `json:"max_tokens,omitempty"`
	Stop             json.RawMessage `json:"stop,omitempty"`
	N                json.RawMessage `json:"n,omitempty"`
	PresencePenalty  json.RawMessage `json:"presence_penalty,omitempty"`
	FrequencyPenalty json.RawMessage `json:"frequency_penalty,omitempty"`
	LogitBias        json.RawMessage `json:"logit_bias,omitempty"`
	User             json.RawMessage `json:"user,omitempty"`
}

// OpenAIMessage is a single message in the messages array.
//
// Content is always a plain string internally, but the wire format accepts both
// the string form and the structured "content parts" array — see
// UnmarshalJSON.
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// contentPart is one element of a structured content array.
type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// UnmarshalJSON accepts both shapes of an OpenAI message that real clients send.
//
//	{"role": "user", "content": "Hello"}
//	{"role": "user", "content": [{"type": "text", "text": "Hello"}]}
//
// The second form is what the OpenAI SDKs emit through their multimodal
// helpers and what Cline, Continue, LangChain and LibreChat send by default.
// Text parts are joined with newlines; a message whose parts are all text is
// therefore indistinguishable downstream from the plain string form.
//
// The role "developer" — OpenAI's newer name for "system" — is normalised to
// "system" so those messages reach the agent as a system prompt instead of
// being rejected as an unknown role.
//
// Parts the CLI agents cannot render (images, audio, files) are rejected rather
// than silently dropped: answering as if an attachment had been read would be
// worse than a clear error.
func (m *OpenAIMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	m.Role = raw.Role
	if m.Role == "developer" {
		m.Role = "system"
	}

	content, err := decodeMessageContent(raw.Content)
	if err != nil {
		return err
	}
	m.Content = content
	return nil
}

// decodeMessageContent renders a message's content field as plain text.
func decodeMessageContent(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		// Absent or null content — an assistant turn carrying only tool calls
		// looks like this. Treated as empty and rejected by validation, which
		// reports the missing field properly.
		return "", nil
	}

	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", err
		}
		return s, nil

	case '[':
		var parts []contentPart
		if err := json.Unmarshal(trimmed, &parts); err != nil {
			return "", fmt.Errorf("content array is malformed: %w", err)
		}
		texts := make([]string, 0, len(parts))
		for _, p := range parts {
			if p.Type != "" && p.Type != "text" {
				return "", fmt.Errorf("unsupported content part type %q: only text is supported", p.Type)
			}
			texts = append(texts, p.Text)
		}
		return strings.Join(texts, "\n"), nil

	default:
		return "", fmt.Errorf("content must be a string or an array of content parts")
	}
}

// --- Response types (non-streaming) ---

// OpenAIChatResponse is the response for POST /v1/chat/completions (non-streaming).
type OpenAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
}

// OpenAIChoice is one element in the choices array.
type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// OpenAIUsage holds estimated token usage.
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --- Response types (streaming) ---

// OpenAIStreamChunk is one SSE chunk for streaming responses.
type OpenAIStreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []OpenAIStreamChoice `json:"choices"`
}

// OpenAIStreamChoice is one element in a streaming chunk's choices array.
type OpenAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        OpenAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

// OpenAIStreamDelta holds incremental content in a stream chunk.
type OpenAIStreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// --- Error types ---

// OpenAIErrorResponse wraps an OpenAI-format error.
type OpenAIErrorResponse struct {
	Error OpenAIError `json:"error"`
}

// OpenAIError is the error object matching the OpenAI error schema.
type OpenAIError struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    *string `json:"code"`
}

// --- Models types ---

// OpenAIModelList is the response for GET /v1/models.
type OpenAIModelList struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

// OpenAIModel is a single model object.
type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// writeOpenAIError writes an OpenAI-format error response.
func writeOpenAIError(w http.ResponseWriter, status int, errType, message string, param, code *string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(OpenAIErrorResponse{
		Error: OpenAIError{
			Message: message,
			Type:    errType,
			Param:   param,
			Code:    code,
		},
	})
}

// generateCompletionID returns "chatcmpl-" + 24 random alphanumeric characters.
func generateCompletionID() string {
	return "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
}

// estimateTokens approximates token count as character_count / 4, rounded to the nearest integer.
// Counts runes rather than bytes so multi-byte text (accented Latin, CJK,
// emoji) is not over-counted by a factor of 2–4.
func estimateTokens(text string) int {
	n := utf8.RuneCountInString(text)
	return (n + 2) / 4
}
