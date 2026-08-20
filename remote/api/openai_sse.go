package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEWriter handles streaming output in OpenAI-compatible format.
// It wraps an http.ResponseWriter with Flusher support.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	id      string // "chatcmpl-..." ID shared across all chunks
	model   string
	created int64
	first   bool // true = next chunk is the first (include role)
}

// NewSSEWriter initializes streaming headers and returns the writer.
// Sets Content-Type: text/event-stream, Cache-Control: no-cache, Connection: keep-alive.
func NewSSEWriter(w http.ResponseWriter, id, model string, created int64) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	return &SSEWriter{
		w:       w,
		flusher: flusher,
		id:      id,
		model:   model,
		created: created,
		first:   true,
	}, nil
}

func (s *SSEWriter) writeChunk(chunk OpenAIStreamChunk) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteChunk writes a single content delta as an SSE event.
// On the first call, includes role: "assistant" in the delta.
func (s *SSEWriter) WriteChunk(content string) error {
	delta := OpenAIStreamDelta{Content: content}
	if s.first {
		delta.Role = "assistant"
		s.first = false
	}

	chunk := OpenAIStreamChunk{
		ID:      s.id,
		Object:  "chat.completion.chunk",
		Created: s.created,
		Model:   s.model,
		Choices: []OpenAIStreamChoice{
			{
				Index:        0,
				Delta:        delta,
				FinishReason: nil,
			},
		},
	}
	return s.writeChunk(chunk)
}

// WriteDone writes the final chunk (finish_reason: "stop", empty delta)
// followed by "data: [DONE]\n\n".
func (s *SSEWriter) WriteDone() error {
	stop := "stop"
	chunk := OpenAIStreamChunk{
		ID:      s.id,
		Object:  "chat.completion.chunk",
		Created: s.created,
		Model:   s.model,
		Choices: []OpenAIStreamChoice{
			{
				Index:        0,
				Delta:        OpenAIStreamDelta{},
				FinishReason: &stop,
			},
		},
	}
	if err := s.writeChunk(chunk); err != nil {
		return err
	}

	if _, err := fmt.Fprint(s.w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteError writes a graceful termination (stop + [DONE]) on error.
func (s *SSEWriter) WriteError() error {
	return s.WriteDone()
}
