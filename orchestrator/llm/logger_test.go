package llm

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

// changeDirForTest changes the working directory to a temp dir for the duration of the test.
// Must NOT be used with t.Parallel() — os.Chdir is process-wide.
func changeDirForTest(t *testing.T) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

// mockEvaluator is a ConditionEvaluator for testing.
type mockEvaluator struct {
	decisions []bool
	idx       int
}

func (m *mockEvaluator) Evaluate(content, prompt string) (bool, error) {
	if m.idx >= len(m.decisions) {
		return false, nil
	}
	d := m.decisions[m.idx]
	m.idx++
	return d, nil
}

// mockEvalError wraps mockEvaluator to return a configurable error.
type mockEvalError struct {
	result bool
	err    error
}

func (m *mockEvalError) Evaluate(content, prompt string) (bool, error) {
	return m.result, m.err
}

// TestNewLLMLogger_CreatesFile verifies that NewLLMLogger creates the log file with
// a non-empty header.
func TestNewLLMLogger_CreatesFile(t *testing.T) {
	changeDirForTest(t)

	logger, err := NewLLMLogger(&mockEvaluator{})
	if err != nil {
		t.Fatalf("NewLLMLogger() error: %v", err)
	}
	defer logger.Close()

	data, err := os.ReadFile(llmLogFile)
	if err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty header in log file")
	}
	if !strings.Contains(string(data), "LLM Debug Log") {
		t.Error("expected 'LLM Debug Log' in header")
	}
}

// TestNewLLMLogger_TruncatesExistingFile verifies that calling NewLLMLogger twice
// truncates the previous log file.
func TestNewLLMLogger_TruncatesExistingFile(t *testing.T) {
	changeDirForTest(t)

	// First logger writes content.
	l1, err := NewLLMLogger(&mockEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	l1.Log("first logger entry")
	l1.Close()

	// Second logger should truncate and overwrite.
	l2, err := NewLLMLogger(&mockEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()

	data, _ := os.ReadFile(llmLogFile)
	if strings.Contains(string(data), "first logger entry") {
		t.Error("expected log file to be truncated by second NewLLMLogger call")
	}
}

// TestLLMLogger_Evaluate_DelegatesAndLogs verifies that Evaluate calls the inner evaluator,
// returns its result, and writes a log entry.
func TestLLMLogger_Evaluate_DelegatesAndLogs(t *testing.T) {
	changeDirForTest(t)

	inner := &mockEvaluator{decisions: []bool{true}}
	logger, err := NewLLMLogger(inner)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	result, err := logger.Evaluate("some content", "is this true?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true from inner evaluator")
	}
	if inner.idx != 1 {
		t.Errorf("expected inner Evaluate called once, got idx=%d", inner.idx)
	}

	logger.Close()
	data, _ := os.ReadFile(llmLogFile)
	logStr := string(data)
	if !strings.Contains(logStr, "LLM Call #1") {
		t.Error("expected 'LLM Call #1' in log")
	}
	if !strings.Contains(logStr, "is this true?") {
		t.Error("expected condition prompt in log")
	}
}

// TestLLMLogger_Evaluate_InnerErrorPropagated verifies that an inner evaluator error
// is logged and propagated.
func TestLLMLogger_Evaluate_InnerErrorPropagated(t *testing.T) {
	changeDirForTest(t)

	inner := &mockEvalError{result: false, err: fmt.Errorf("llm backend down")}
	logger, err := NewLLMLogger(inner)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	_, evalErr := logger.Evaluate("content", "prompt")
	if evalErr == nil {
		t.Fatal("expected error to propagate from inner evaluator")
	}
	if evalErr.Error() != "llm backend down" {
		t.Errorf("unexpected error: %v", evalErr)
	}

	logger.Close()
	data, _ := os.ReadFile(llmLogFile)
	if !strings.Contains(string(data), "Error") {
		t.Error("expected error entry in log file")
	}
}

// TestLLMLogger_Log_WritesMessage verifies that Log writes a formatted message to the file.
func TestLLMLogger_Log_WritesMessage(t *testing.T) {
	changeDirForTest(t)

	logger, err := NewLLMLogger(&mockEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	logger.Log("test message %d", 42)
	logger.Close()

	data, _ := os.ReadFile(llmLogFile)
	if !strings.Contains(string(data), "test message 42") {
		t.Errorf("expected 'test message 42' in log, got: %s", string(data))
	}
}

// TestLLMLogger_LogSection_WritesHeader verifies that LogSection writes a section header.
func TestLLMLogger_LogSection_WritesHeader(t *testing.T) {
	changeDirForTest(t)

	logger, err := NewLLMLogger(&mockEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	logger.LogSection("My Section")
	logger.Close()

	data, _ := os.ReadFile(llmLogFile)
	if !strings.Contains(string(data), "My Section") {
		t.Error("expected section title in log")
	}
}

// TestLLMLogger_LogError_WritesError verifies that LogError writes an error line.
func TestLLMLogger_LogError_WritesError(t *testing.T) {
	changeDirForTest(t)

	logger, err := NewLLMLogger(&mockEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	logger.LogError("MyContext", fmt.Errorf("something broke"))
	logger.Close()

	data, _ := os.ReadFile(llmLogFile)
	if !strings.Contains(string(data), "MyContext") {
		t.Error("expected context name in log")
	}
	if !strings.Contains(string(data), "something broke") {
		t.Error("expected error message in log")
	}
}

// TestLLMLogger_ConcurrentLog_RaceFree verifies that concurrent Log calls do not
// produce data races (run with -race to detect).
func TestLLMLogger_ConcurrentLog_RaceFree(t *testing.T) {
	changeDirForTest(t)

	logger, err := NewLLMLogger(&mockEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			logger.Log("concurrent message %d", n)
		}(i)
	}
	wg.Wait()
}

// TestLLMLogger_Close_Safe verifies that Close can be called without panic.
func TestLLMLogger_Close_Safe(t *testing.T) {
	changeDirForTest(t)

	logger, err := NewLLMLogger(&mockEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	logger.Close() // should not panic
}
