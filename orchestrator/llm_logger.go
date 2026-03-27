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

// SetContext writes a context header (step name, iteration info) to the log.
// Call this before Evaluate to annotate which step triggered the LLM call.
func (l *LLMLogger) SetContext(context string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.file, "--- %s ---\n", context)
}

// Evaluate delegates to the inner evaluator and logs the request and response.
func (l *LLMLogger) Evaluate(content string, conditionPrompt string) (bool, error) {
	l.mu.Lock()
	l.seq++
	seq := l.seq
	l.mu.Unlock()

	start := time.Now()
	result, err := l.inner.Evaluate(content, conditionPrompt)
	elapsed := time.Since(start).Round(time.Millisecond)

	l.mu.Lock()
	defer l.mu.Unlock()

	separator := strings.Repeat("─", 60)
	fmt.Fprintf(l.file, "%s\n", separator)
	fmt.Fprintf(l.file, "Call #%d  |  %s  |  took %s\n", seq, time.Now().Format("15:04:05"), elapsed)
	fmt.Fprintf(l.file, "%s\n\n", separator)

	fmt.Fprintf(l.file, "REQUEST — Condition Prompt:\n%s\n\n", conditionPrompt)

	// Truncate very long content for readability
	contentPreview := content
	if len(contentPreview) > 2000 {
		contentPreview = contentPreview[:2000] + fmt.Sprintf("\n... [truncated, total %d bytes]", len(content))
	}
	fmt.Fprintf(l.file, "REQUEST — Content:\n%s\n\n", contentPreview)

	if err != nil {
		fmt.Fprintf(l.file, "RESPONSE — Error: %v\n\n", err)
	} else {
		fmt.Fprintf(l.file, "RESPONSE — Decision: %v\n\n", result)
	}

	return result, err
}
