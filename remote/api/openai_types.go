package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// --- Request types ---

// OpenAIChatRequest is the request body for POST /v1/chat/completions.
type OpenAIChatRequest struct {
	Model            string          `json:"model"`
	Messages         []OpenAIMessage `json:"messages"`
	Stream           bool            `json:"stream,omitempty"`
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	Stop             json.RawMessage `json:"stop,omitempty"`
	N                *int            `json:"n,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	LogitBias        json.RawMessage `json:"logit_bias,omitempty"`
	User             string          `json:"user,omitempty"`
}

// OpenAIMessage is a single message in the messages array.
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
func estimateTokens(text string) int {
	n := len(text)
	return (n + 2) / 4
}
