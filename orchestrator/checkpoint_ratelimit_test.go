package main

import (
	"testing"
	"time"
)

// --- IsRateLimitError tests ---

func TestIsRateLimitError_True(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"rate limit lowercase", "you hit the rate limit for this model"},
		{"rate limit uppercase", "Rate Limit Exceeded"},
		{"rate_limit underscore", "error: rate_limit reached"},
		{"429", "HTTP 429"},
		{"quota exceeded", "quota exceeded for billing period"},
		{"Quota Exceeded mixed case", "Quota Exceeded"},
		{"token limit", "token limit reached for this request"},
		{"usage limit", "usage limit exceeded"},
		{"request limit", "request limit hit"},
		{"session limit", "session limit reached"},
		{"overloaded lowercase", "the system is overloaded, please retry"},
		{"OVERLOADED uppercase", "SERVER OVERLOADED"},
		{"too many requests", "too many requests sent"},
		{"multi-line with 429", "line1\n429 Too Many Requests\nline3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !IsRateLimitError(tc.input) {
				t.Errorf("expected IsRateLimitError(%q) = true", tc.input)
			}
		})
	}
}

func TestIsRateLimitError_False(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"normal output", "step completed successfully"},
		{"rate alone", "the rate of change is increasing"},
		{"limit alone", "memory limit is 512mb"},
		{"quota info", "quota usage: 50% consumed"},
		{"request count", "processed 1000 requests today"},
		{"session info", "session duration: 5 minutes"},
		{"overload topic", "overloading a function in Go"},
		{"token count", "tokens: 100"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsRateLimitError(tc.input) {
				t.Errorf("expected IsRateLimitError(%q) = false", tc.input)
			}
		})
	}
}

// --- ParseResetTime tests ---

func TestParseResetTime_ValidInputs(t *testing.T) {
	// For valid inputs: result must be non-zero, in the future (or at most 1s in the past
	// due to test timing), and at most 25h from now.
	cases := []struct {
		name  string
		input string
	}{
		{"resets 2am", "resets 2am (UTC)"},
		{"resets 3pm", "resets 3pm (UTC)"},
		{"resets 14:00", "resets 14:00 (UTC)"},
		{"resets 3:30pm", "resets 3:30pm (UTC)"},
		{"reset at 3am", "reset at 3am"},
		{"reset at 11:30pm", "reset at 11:30pm (UTC)"},
		{"resets 12am midnight", "resets 12am (UTC)"},
		{"resets with surrounding text", "rate limit exceeded; resets 6pm (UTC) please retry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := time.Now()
			result := ParseResetTime(tc.input)
			after := time.Now().Add(25 * time.Hour)

			if result.IsZero() {
				t.Errorf("expected non-zero time for input %q", tc.input)
				return
			}
			// Result must be within 25 hours from when the call was made.
			if result.After(after) {
				t.Errorf("expected result within 25h, got %v (now=%v)", result, before)
			}
			// Result must be in the future (the implementation always adjusts to tomorrow if past).
			if result.Before(before.Add(-time.Second)) {
				t.Errorf("expected result in the future for %q, got %v (before=%v)", tc.input, result, before)
			}
		})
	}
}

func TestParseResetTime_InvalidInputs(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"no reset keyword", "rate limit exceeded, try again later"},
		{"garbled time", "resets garbled-xyz (UTC)"},
		{"resets with no time", "resets"},
		{"reset at no time", "reset at"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ParseResetTime(tc.input)
			if !result.IsZero() {
				t.Errorf("expected zero time for input %q, got %v", tc.input, result)
			}
		})
	}
}

func TestParseResetTime_TodayVsTomorrow(t *testing.T) {
	// The implementation picks today if the time is in the future, tomorrow if in the past.
	// Both "today" and "tomorrow" variants should produce a time within 24h of now.
	result := ParseResetTime("resets 2am (UTC)")
	if result.IsZero() {
		t.Fatal("expected non-zero result")
	}
	now := time.Now().UTC()
	if result.Before(now.Add(-time.Second)) {
		t.Errorf("expected result in the future, got %v", result)
	}
	if result.After(now.Add(25 * time.Hour)) {
		t.Errorf("expected result within 25h, got %v", result)
	}
}
