package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const llmLogFile = "llm-debug.log"

// LLMLogger wraps a ConditionEvaluator and logs all requests/responses to a file.
// It also provides general-purpose logging for pipeline execution events.
type LLMLogger struct {
	inner ConditionEvaluator
	mu    sync.Mutex
	file  *os.File
	seq   int
}

// NewLLMLogger creates a logging wrapper around a ConditionEvaluator.
// The log file is created (or truncated) at startup.
func NewLLMLogger(inner ConditionEvaluator) (*LLMLogger, error) {
	f, err := os.Create(llmLogFile)
	if err != nil {
		return nil, fmt.Errorf("creating LLM log file: %w", err)
	}
	fmt.Fprintf(f, "=== LLM Debug Log — started %s ===\n\n", time.Now().Format(time.RFC3339))
	return &LLMLogger{inner: inner, file: f}, nil
}

// Close closes the log file.
func (l *LLMLogger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
	}
}

// Log writes a general message to the debug log.
func (l *LLMLogger) Log(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ts := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(l.file, "[%s] %s\n", ts, msg)
}

// LogSection writes a section header to the debug log.
func (l *LLMLogger) LogSection(title string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	separator := strings.Repeat("═", 60)
	fmt.Fprintf(l.file, "\n%s\n  %s\n%s\n", separator, title, separator)
}

// LogError writes an error to the debug log.
func (l *LLMLogger) LogError(context string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(l.file, "[%s] ✗ ERROR in %s: %v\n", ts, context, err)
}

// Evaluate delegates to the inner evaluator and logs the request and response.
func (l *LLMLogger) Evaluate(content string, conditionPrompt string) (bool, error) {
	l.mu.Lock()
	l.seq++
	seq := l.seq
	l.mu.Unlock()

	l.Log("LLM Call #%d — sending request...", seq)

	start := time.Now()
	result, err := l.inner.Evaluate(content, conditionPrompt)
	elapsed := time.Since(start).Round(time.Millisecond)

	l.mu.Lock()
	defer l.mu.Unlock()

	separator := strings.Repeat("─", 60)
	fmt.Fprintf(l.file, "\n%s\n", separator)
	fmt.Fprintf(l.file, "  LLM Call #%d  |  %s  |  took %s\n", seq, time.Now().Format("15:04:05"), elapsed)
	fmt.Fprintf(l.file, "%s\n\n", separator)

	fmt.Fprintf(l.file, "REQUEST — Condition Prompt:\n  %s\n\n", conditionPrompt)

	// Truncate very long content for readability
	contentPreview := content
	if len(contentPreview) > 2000 {
		contentPreview = contentPreview[:2000] + fmt.Sprintf("\n... [truncated, total %d bytes]", len(content))
	}
	fmt.Fprintf(l.file, "REQUEST — Content (%d bytes):\n%s\n\n", len(content), contentPreview)

	if err != nil {
		fmt.Fprintf(l.file, "RESPONSE — Error: %v\n\n", err)
	} else {
		fmt.Fprintf(l.file, "RESPONSE — Decision: %v\n\n", result)
	}

	return result, err
}
