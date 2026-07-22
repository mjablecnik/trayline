package main

import (
	"os"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func clearTasklineURLEnv(t *testing.T) {
	t.Helper()
	orig, had := os.LookupEnv("TASKLINE_URL")
	_ = os.Unsetenv("TASKLINE_URL")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("TASKLINE_URL", orig)
		} else {
			_ = os.Unsetenv("TASKLINE_URL")
		}
	})
}

func TestLoadConfig_DefaultURL(t *testing.T) {
	clearTasklineURLEnv(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ServerURL != defaultServerURL {
		t.Errorf("expected default server URL %q, got %q", defaultServerURL, cfg.ServerURL)
	}
}

// Feature: taskline, Property 17: URL scheme validation
//
// For any string set as TASKLINE_URL, if it does not start with http:// or
// https://, the CLI shall reject it with an error. If it starts with either
// valid scheme prefix, it shall be accepted as the server URL.
func TestProperty_URLSchemeValidation(t *testing.T) {
	clearTasklineURLEnv(t)

	rapid.Check(t, func(t *rapid.T) {
		valid := rapid.Bool().Draw(t, "valid")

		var raw string
		if valid {
			scheme := rapid.SampledFrom([]string{"http://", "https://"}).Draw(t, "scheme")
			suffix := rapid.StringMatching(`[a-zA-Z0-9:/._-]{0,40}`).Draw(t, "suffix")
			raw = scheme + suffix
		} else {
			// First character excludes 'h' so the string can never start with
			// "http://" or "https://"; the whole string is also never empty,
			// since an empty TASKLINE_URL falls back to the (valid) default.
			first := rapid.StringMatching(`[a-gi-zA-Z0-9:/._-]`).Draw(t, "first")
			rest := rapid.StringMatching(`[a-zA-Z0-9:/._-]{0,39}`).Draw(t, "rest")
			raw = first + rest
		}

		if err := os.Setenv("TASKLINE_URL", raw); err != nil {
			t.Skipf("cannot set TASKLINE_URL=%q: %v", raw, err)
		}
		defer func() { _ = os.Unsetenv("TASKLINE_URL") }()

		cfg, err := LoadConfig()
		if valid {
			if err != nil {
				t.Fatalf("expected no error for valid TASKLINE_URL=%q, got %v", raw, err)
			}
			if cfg.ServerURL != raw {
				t.Fatalf("expected server URL %q, got %q", raw, cfg.ServerURL)
			}
		} else {
			if err == nil {
				t.Fatalf("expected error for invalid TASKLINE_URL=%q", raw)
			}
			if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
				t.Fatalf("test generator bug: %q should not have a valid scheme prefix", raw)
			}
		}
	})
}
