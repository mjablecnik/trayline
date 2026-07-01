package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type contextKey string

const requestIDKey contextKey = "requestId"

// Logger writes newline-delimited JSON log entries to stdout.
type Logger struct {
	apiToken string
}

// NewLogger creates a logger that redacts the given API token from all log output.
func NewLogger(apiToken string) *Logger {
	return &Logger{apiToken: apiToken}
}

type logEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

func (l *Logger) log(ctx context.Context, level, message string) {
	requestID := ""
	if v := ctx.Value(requestIDKey); v != nil {
		requestID, _ = v.(string)
	}

	// Redact API token from message to prevent accidental leakage.
	if l.apiToken != "" {
		message = redact(message, l.apiToken)
	}

	entry := logEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Message:   message,
		RequestID: requestID,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		// Fallback: write a minimal valid JSON entry.
		fmt.Fprintf(os.Stdout, `{"timestamp":%q,"level":"error","message":"failed to marshal log entry","requestId":"%s"}`+"\n",
			time.Now().UTC().Format(time.RFC3339), requestID)
		return
	}
	fmt.Fprintf(os.Stdout, "%s\n", data)
}

func (l *Logger) Debug(ctx context.Context, message string) { l.log(ctx, "debug", message) }
func (l *Logger) Info(ctx context.Context, message string)  { l.log(ctx, "info", message) }
func (l *Logger) Warn(ctx context.Context, message string)  { l.log(ctx, "warn", message) }
func (l *Logger) Error(ctx context.Context, message string) { l.log(ctx, "error", message) }

// WithRequestID returns a new context carrying the given request ID.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext extracts the request ID from a context.
func RequestIDFromContext(ctx context.Context) string {
	if v := ctx.Value(requestIDKey); v != nil {
		if id, ok := v.(string); ok {
			return id
		}
	}
	return ""
}

// redact replaces all occurrences of secret in s with "[REDACTED]".
func redact(s, secret string) string {
	if secret == "" {
		return s
	}
	result := ""
	remaining := s
	for {
		idx := -1
		for i := 0; i <= len(remaining)-len(secret); i++ {
			if remaining[i:i+len(secret)] == secret {
				idx = i
				break
			}
		}
		if idx == -1 {
			result += remaining
			break
		}
		result += remaining[:idx] + "[REDACTED]"
		remaining = remaining[idx+len(secret):]
	}
	return result
}
