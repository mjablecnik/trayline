package main

import (
	"os"
	"strconv"
	"testing"

	"pgregory.net/rapid"
)

var appEnvVars = []string{
	"APP_PORT", "STATE_DIR", "LOG_DIR", "NOTIFY_EMAIL",
	"SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASSWORD", "SMTP_FROM",
}

// clearAppEnv unsets all config-related env vars for the duration of the
// test, restoring their original values afterward, so tests don't leak state
// into one another or depend on the ambient environment.
func clearAppEnv(t *testing.T) {
	t.Helper()
	for _, key := range appEnvVars {
		orig, had := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(key, orig)
			}
		})
	}
}

func withEnv(t *testing.T, key, value string) {
	t.Helper()
	if err := os.Setenv(key, value); err != nil {
		t.Skipf("cannot set %s=%q: %v", key, value, err)
	}
}

func TestLoadConfig_DefaultPort(t *testing.T) {
	clearAppEnv(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != defaultPort {
		t.Errorf("expected default port %d, got %d", defaultPort, cfg.Port)
	}
}

func TestLoadConfig_DefaultStateDir(t *testing.T) {
	clearAppEnv(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.StateDir != defaultStateDir {
		t.Errorf("expected default state dir %q, got %q", defaultStateDir, cfg.StateDir)
	}
}

func TestLoadConfig_DefaultLogDir(t *testing.T) {
	clearAppEnv(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogDir != defaultLogDir {
		t.Errorf("expected default log dir %q, got %q", defaultLogDir, cfg.LogDir)
	}
}

func TestLoadConfig_StateDirAndLogDirFromEnv(t *testing.T) {
	clearAppEnv(t)
	withEnv(t, "STATE_DIR", "/tmp/custom-state/")
	withEnv(t, "LOG_DIR", "/tmp/custom-logs/")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.StateDir != "/tmp/custom-state/" {
		t.Errorf("expected state dir %q, got %q", "/tmp/custom-state/", cfg.StateDir)
	}
	if cfg.LogDir != "/tmp/custom-logs/" {
		t.Errorf("expected log dir %q, got %q", "/tmp/custom-logs/", cfg.LogDir)
	}
}

func TestLoadConfig_SMTPFromDefaultsToSMTPUser(t *testing.T) {
	clearAppEnv(t)
	withEnv(t, "SMTP_USER", "someone@example.com")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SMTPFrom != "someone@example.com" {
		t.Errorf("expected SMTPFrom to default to SMTP_USER, got %q", cfg.SMTPFrom)
	}
}

func TestLoadConfig_NotificationsDisabledWhenSMTPIncomplete(t *testing.T) {
	clearAppEnv(t)
	withEnv(t, "NOTIFY_EMAIL", "ops@example.com")
	withEnv(t, "SMTP_HOST", "smtp.example.com")
	// SMTP_PORT, SMTP_USER, SMTP_PASSWORD intentionally left unset.

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.NotificationsEnabled {
		t.Errorf("expected notifications disabled when SMTP config is incomplete")
	}
}

func TestLoadConfig_NotificationsEnabledWhenComplete(t *testing.T) {
	clearAppEnv(t)
	withEnv(t, "NOTIFY_EMAIL", "ops@example.com")
	withEnv(t, "SMTP_HOST", "smtp.example.com")
	withEnv(t, "SMTP_PORT", "587")
	withEnv(t, "SMTP_USER", "user@example.com")
	withEnv(t, "SMTP_PASSWORD", "secret")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.NotificationsEnabled {
		t.Errorf("expected notifications enabled when SMTP config is complete")
	}
}

// Feature: taskline, Property 1: Config port validation
//
// For any string value set as APP_PORT, if the value is a numeric integer in
// the range 1-65535, LoadConfig() shall return a valid Config with that port.
// If the value is non-numeric, negative, zero, or exceeds 65535, LoadConfig()
// shall return an error.
func TestProperty_ConfigPortValidation(t *testing.T) {
	clearAppEnv(t)

	rapid.Check(t, func(t *rapid.T) {
		valid := rapid.Bool().Draw(t, "valid")

		var raw string
		if valid {
			port := rapid.IntRange(1, 65535).Draw(t, "port")
			raw = strconv.Itoa(port)
		} else {
			kind := rapid.IntRange(0, 2).Draw(t, "invalidKind")
			switch kind {
			case 0:
				raw = rapid.StringMatching(`[a-zA-Z]+`).Draw(t, "nonNumeric")
			case 1:
				raw = strconv.Itoa(rapid.IntRange(-65535, 0).Draw(t, "outOfRangeLow"))
			case 2:
				raw = strconv.Itoa(rapid.IntRange(65536, 200000).Draw(t, "outOfRangeHigh"))
			}
		}

		if err := os.Setenv("APP_PORT", raw); err != nil {
			t.Skip("cannot set APP_PORT to generated value")
		}
		defer func() { _ = os.Unsetenv("APP_PORT") }()

		cfg, err := LoadConfig()
		if valid {
			if err != nil {
				t.Fatalf("expected no error for valid APP_PORT=%q, got %v", raw, err)
			}
			expected, _ := strconv.Atoi(raw)
			if cfg.Port != expected {
				t.Fatalf("expected port %d, got %d", expected, cfg.Port)
			}
		} else {
			if err == nil {
				t.Fatalf("expected error for invalid APP_PORT=%q", raw)
			}
		}
	})
}
