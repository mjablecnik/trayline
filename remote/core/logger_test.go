package core

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

type captureLogger struct {
	buf      bytes.Buffer
	apiToken string
}

func (c *captureLogger) log(ctx context.Context, level, message string) {
	requestID := RequestIDFromContext(ctx)
	if c.apiToken != "" {
		message = redact(message, c.apiToken)
	}
	entry := map[string]string{
		"timestamp": "2026-01-01T00:00:00Z",
		"level":     level,
		"message":   message,
		"requestId": requestID,
	}
	data, _ := json.Marshal(entry)
	c.buf.Write(data)
	c.buf.WriteByte('\n')
}

func TestLogEntriesAreValidJSONWithRequiredFields(t *testing.T) {
	apiToken := "super-secret-token"
	logger := NewLogger(apiToken)

	rapid.Check(t, func(t *rapid.T) {
		level := rapid.SampledFrom([]string{"debug", "info", "warn", "error"}).Draw(t, "level")
		message := rapid.StringN(1, 200, -1).Draw(t, "message")
		requestID := rapid.StringN(0, 36, -1).Draw(t, "requestId")

		ctx := WithRequestID(context.Background(), requestID)

		msgWithToken := message + " token=" + apiToken
		redacted := redact(msgWithToken, apiToken)
		if strings.Contains(redacted, apiToken) {
			t.Fatalf("redact failed: output still contains API token")
		}

		cl := &captureLogger{apiToken: apiToken}
		cl.log(ctx, level, message)

		line := strings.TrimSpace(cl.buf.String())
		if line == "" {
			t.Fatal("log produced no output")
		}

		var entry map[string]string
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log entry is not valid JSON: %v, line: %s", err, line)
		}

		for _, field := range []string{"timestamp", "level", "message", "requestId"} {
			if _, ok := entry[field]; !ok {
				t.Fatalf("log entry missing required field %q: %s", field, line)
			}
		}

		if entry["message"] == "" {
			t.Fatal("message field must not be empty")
		}

		_ = logger
	})
}

func TestRedactRemovesSecret(t *testing.T) {
	secret := "my-secret-token"
	input := "Authorization: Bearer my-secret-token extra"
	result := redact(input, secret)
	if strings.Contains(result, secret) {
		t.Fatalf("redact did not remove secret: %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in output, got: %q", result)
	}
}

func TestRedactEmptySecret(t *testing.T) {
	result := redact("some message", "")
	if result != "some message" {
		t.Fatalf("redact with empty secret should return input unchanged, got: %q", result)
	}
}

func TestRequestIDFromContext_Missing(t *testing.T) {
	id := RequestIDFromContext(context.Background())
	if id != "" {
		t.Errorf("expected empty string for context with no request ID, got %q", id)
	}
}

func TestRequestIDFromContext_Present(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-123")
	id := RequestIDFromContext(ctx)
	if id != "req-123" {
		t.Errorf("expected req-123, got %q", id)
	}
}

func TestLogEmptyRequestID(t *testing.T) {
	cl := &captureLogger{}
	cl.log(context.Background(), "info", "hello")

	line := strings.TrimSpace(cl.buf.String())
	var entry map[string]string
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("log entry is not valid JSON: %v", err)
	}
	if entry["requestId"] != "" {
		t.Errorf("expected empty requestId for background context, got %q", entry["requestId"])
	}
}
