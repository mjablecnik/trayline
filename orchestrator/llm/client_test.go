package llm

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// --- Unit tests ---

func TestParseLLMDecision(t *testing.T) {
	cases := []struct {
		input    string
		expected bool
		wantErr  bool
	}{
		{"true", true, false},
		{"false", false, false},
		{"True", true, false},
		{"False", false, false},
		{"TRUE", true, false},
		{"FALSE", false, false},
		{"  true  ", true, false},
		{"  false  ", false, false},
		{"random text", false, true},
		{"", false, true},
		{"yes", false, true},
		{"1", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseLLMDecision(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v for input %q", tc.expected, result, tc.input)
			}
		})
	}
}

func TestLLMClient_Evaluate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"true"}}]}`)
	}))
	defer server.Close()

	client := &LLMClient{
		APIKey:  "test-key",
		Model:   "test-model",
		BaseURL: server.URL,
		client:  server.Client(),
	}

	result, err := client.Evaluate("some content", "Is this correct?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true")
	}
}

func TestLLMClient_Evaluate_RetryOnHTTPError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"false"}}]}`)
	}))
	defer server.Close()

	client := &LLMClient{
		APIKey:  "test-key",
		Model:   "test-model",
		BaseURL: server.URL,
		client:  server.Client(),
	}

	result, err := client.Evaluate("content", "Done?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Error("expected false")
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (1 fail + 1 retry), got %d", callCount)
	}
}

func TestLLMClient_Evaluate_FailAfterRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &LLMClient{
		APIKey:  "test-key",
		Model:   "test-model",
		BaseURL: server.URL,
		client:  server.Client(),
	}

	_, err := client.Evaluate("content", "Done?")
	if err == nil {
		t.Fatal("expected error after retry failure")
	}
	if !strings.Contains(err.Error(), "failed after retry") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// --- Property-based tests ---

// Property 12: LLM response parsing
func TestLLMResponseParsing(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		response := rapid.String().Draw(rt, "response")
		result, err := parseLLMDecision(response)

		lower := strings.TrimSpace(strings.ToLower(response))
		hasTrue := strings.Contains(lower, "true")
		hasFalse := strings.Contains(lower, "false")

		if lower == "true" || (hasTrue && !hasFalse) {
			if err != nil {
				rt.Fatalf("expected no error for %q, got: %v", response, err)
			}
			if !result {
				rt.Fatalf("expected true for %q", response)
			}
		} else if lower == "false" || (hasFalse && !hasTrue) {
			if err != nil {
				rt.Fatalf("expected no error for %q, got: %v", response, err)
			}
			if result {
				rt.Fatalf("expected false for %q", response)
			}
		} else {
			// ambiguous or neither — should return error
			if err == nil {
				rt.Fatalf("expected error for ambiguous/empty response %q, got result=%v", response, result)
			}
		}
	})
}

// Property 9: LLM client retry on failure
func TestLLMRetry(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// first call fails if failFirst=true
		failFirst := rapid.Bool().Draw(rt, "failFirst")
		callCount := 0

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if failFirst && callCount == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"choices":[{"message":{"content":"true"}}]}`)
		}))
		defer server.Close()

		client := &LLMClient{
			APIKey:  "test-key",
			Model:   "test-model",
			BaseURL: server.URL,
			client:  server.Client(),
		}

		result, err := client.Evaluate("content", "Done?")
		if err != nil {
			rt.Fatalf("unexpected error: %v", err)
		}
		if !result {
			rt.Fatalf("expected true")
		}
		if failFirst && callCount != 2 {
			rt.Fatalf("expected 2 calls when first fails, got %d", callCount)
		}
		if !failFirst && callCount != 1 {
			rt.Fatalf("expected 1 call when first succeeds, got %d", callCount)
		}
	})
}
