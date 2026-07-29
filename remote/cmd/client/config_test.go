package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// setupTestDir creates a temp dir, changes CWD to it, and registers cleanup.
// Must be called on the outer *testing.T.
func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
	return dir
}

// setEnvForTest sets an env var and registers cleanup on the outer *testing.T.
func setEnvForTest(t *testing.T, key, val string) {
	t.Helper()
	orig, wasSet := os.LookupEnv(key)
	os.Setenv(key, val)
	t.Cleanup(func() {
		if wasSet {
			os.Setenv(key, orig)
		} else {
			os.Unsetenv(key)
		}
	})
}

// Property 1: Configuration resolution follows priority chain
func TestProperty_ConfigResolutionPriorityChain(t *testing.T) {
	dir := setupTestDir(t)

	rapid.Check(t, func(rt *rapid.T) {
		// Clean env before each iteration.
		os.Unsetenv("TRAYLINE_SERVER_URL")
		os.Unsetenv("TRAYLINE_API_TOKEN")
		os.Remove(filepath.Join(dir, ".env"))

		// Generate source values. We use simple, null-byte-safe generators.
		flagURL := rapid.SampledFrom([]string{"", "http://flag.example.com"}).Draw(rt, "flagURL")
		envURL := rapid.SampledFrom([]string{"", "http://env.example.com"}).Draw(rt, "envURL")
		dotenvURL := rapid.SampledFrom([]string{"", "http://dotenv.example.com"}).Draw(rt, "dotenvURL")

		flagToken := rapid.SampledFrom([]string{"", "flag-token-123"}).Draw(rt, "flagToken")
		envToken := rapid.SampledFrom([]string{"", "env-token-456"}).Draw(rt, "envToken")
		dotenvToken := rapid.SampledFrom([]string{"", "dotenv-token-789"}).Draw(rt, "dotenvToken")

		// Write .env file.
		var dotenvLines []string
		if dotenvURL != "" {
			dotenvLines = append(dotenvLines, "TRAYLINE_SERVER_URL="+dotenvURL)
		}
		if dotenvToken != "" {
			dotenvLines = append(dotenvLines, "TRAYLINE_API_TOKEN="+dotenvToken)
		}
		if len(dotenvLines) > 0 {
			content := strings.Join(dotenvLines, "\n") + "\n"
			os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600)
		}

		// Set env vars.
		if envURL != "" {
			os.Setenv("TRAYLINE_SERVER_URL", envURL)
		}
		if envToken != "" {
			os.Setenv("TRAYLINE_API_TOKEN", envToken)
		}

		// Compute expected values using priority chain.
		wantURL := firstNonEmpty(flagURL, envURL, dotenvURL, "http://localhost:8080")
		wantToken := firstNonEmpty(flagToken, envToken, dotenvToken, "")

		cfg, err := ResolveConfig(flagURL, flagToken, false, false)

		if wantToken == "" {
			if err == nil {
				rt.Fatalf("expected error when no token present, got cfg=%+v", cfg)
			}
			ce, ok := err.(*ConfigError)
			if !ok {
				rt.Fatalf("expected *ConfigError, got %T: %v", err, err)
			}
			if ce.ExitCode != 2 {
				rt.Fatalf("expected exit code 2, got %d", ce.ExitCode)
			}
			return
		}

		if err != nil {
			rt.Fatalf("unexpected error: %v", err)
		}
		if cfg.Token != wantToken {
			rt.Fatalf("token: got %q, want %q", cfg.Token, wantToken)
		}
		wantURLNorm := strings.TrimRight(wantURL, "/")
		if cfg.ServerURL != wantURLNorm {
			rt.Fatalf("serverURL: got %q, want %q", cfg.ServerURL, wantURLNorm)
		}
	})
}

// Property 2: URL validation and normalization
func TestProperty_URLValidationAndNormalization(t *testing.T) {
	setupTestDir(t)
	setEnvForTest(t, "TRAYLINE_API_TOKEN", "test-token")
	os.Unsetenv("TRAYLINE_SERVER_URL")

	rapid.Check(t, func(rt *rapid.T) {
		// Generate a URL with a random scheme and optional trailing slashes.
		scheme := rapid.SampledFrom([]string{"http://", "https://", "ftp://", "ws://", ""}).Draw(rt, "scheme")
		host := rapid.SampledFrom([]string{"a.example.com", "b.example.com", "c.example.com"}).Draw(rt, "host")
		trailingSlashes := rapid.SampledFrom([]string{"", "/", "//", "///"}).Draw(rt, "slashes")
		rawURL := scheme + host + trailingSlashes

		cfg, err := ResolveConfig(rawURL, "test-token", false, false)

		valid := strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://")

		if !valid {
			if err == nil {
				rt.Fatalf("expected error for URL %q, got cfg=%+v", rawURL, cfg)
			}
			ce, ok := err.(*ConfigError)
			if !ok {
				rt.Fatalf("expected *ConfigError, got %T", err)
			}
			if ce.ExitCode != 2 {
				rt.Fatalf("expected exit code 2, got %d", ce.ExitCode)
			}
			return
		}

		if err != nil {
			rt.Fatalf("unexpected error for URL %q: %v", rawURL, err)
		}
		if strings.HasSuffix(cfg.ServerURL, "/") {
			rt.Fatalf("serverURL %q still has trailing slash", cfg.ServerURL)
		}
		if !strings.HasPrefix(cfg.ServerURL, "http://") && !strings.HasPrefix(cfg.ServerURL, "https://") {
			rt.Fatalf("serverURL %q lost its valid scheme", cfg.ServerURL)
		}
	})
}

// TestResolveConfig_MutuallyExclusiveFlags verifies --quiet and --verbose reject each other.
func TestResolveConfig_MutuallyExclusiveFlags(t *testing.T) {
	setupTestDir(t)
	_, err := ResolveConfig("http://localhost:8080", "tok", true, true)
	if err == nil {
		t.Fatal("expected error for --quiet + --verbose combination")
	}
	ce, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("expected *ConfigError, got %T", err)
	}
	if ce.ExitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", ce.ExitCode)
	}
}

// firstNonEmpty returns the first non-empty string from the list.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
