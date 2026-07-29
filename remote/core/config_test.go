package core

import (
	"os"
	"strconv"
	"testing"
	"time"

	"pgregory.net/rapid"
)

func TestConfigValidationRejectsInvalidValues(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		invalidPort := rapid.OneOf(
			rapid.StringMatching(`[^0-9]+`),
			rapid.Just("0"),
			rapid.Just("65536"),
			rapid.Just("99999"),
			rapid.Just("-1"),
		).Draw(t, "invalidPort")

		if err := os.Setenv("APP_PORT", invalidPort); err != nil {
			t.Skip("os.Setenv rejected value (null bytes not allowed in env vars)")
		}
		os.Setenv("API_TOKEN", "test-token")
		os.Setenv("MAX_CONCURRENT_TASKS", "2")
		os.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		os.Unsetenv("SESSION_TIMEOUT")
		os.Unsetenv("TASK_TIMEOUT")
		os.Unsetenv("RATE_LIMIT")

		_, err := LoadConfig()
		if err == nil {
			t.Fatalf("expected error for invalid APP_PORT %q, got nil", invalidPort)
		}
	})

	t.Run("invalid MAX_CONCURRENT_TASKS", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			invalid := rapid.OneOf(
				rapid.StringMatching(`[^0-9]+`),
				rapid.Just("0"),
				rapid.Just("33"),
				rapid.Just("-5"),
				rapid.Just("100"),
			).Draw(t, "invalidMax")

			os.Setenv("APP_PORT", "8080")
			os.Setenv("API_TOKEN", "test-token")
			if err := os.Setenv("MAX_CONCURRENT_TASKS", invalid); err != nil {
				t.Skip("os.Setenv rejected value (null bytes not allowed in env vars)")
			}
			os.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
			os.Unsetenv("SESSION_TIMEOUT")
			os.Unsetenv("TASK_TIMEOUT")
			os.Unsetenv("RATE_LIMIT")

			_, err := LoadConfig()
			if err == nil {
				t.Fatalf("expected error for invalid MAX_CONCURRENT_TASKS %q, got nil", invalid)
			}
		})
	})

	t.Run("invalid SESSION_TIMEOUT", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			invalid := rapid.StringMatching(`[a-z]{2,10}`).Draw(t, "invalidDuration")

			os.Setenv("APP_PORT", "8080")
			os.Setenv("API_TOKEN", "test-token")
			os.Setenv("MAX_CONCURRENT_TASKS", "2")
			os.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
			os.Setenv("SESSION_TIMEOUT", invalid)
			os.Unsetenv("TASK_TIMEOUT")
			os.Unsetenv("RATE_LIMIT")

			_, err := LoadConfig()
			if err == nil {
				t.Fatalf("expected error for invalid SESSION_TIMEOUT %q, got nil", invalid)
			}
		})
	})

	t.Run("valid config succeeds", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			port := rapid.IntRange(1, 65535).Draw(t, "port")
			maxTasks := rapid.IntRange(1, 32).Draw(t, "maxTasks")

			os.Setenv("APP_PORT", strconv.Itoa(port))
			os.Setenv("API_TOKEN", "some-token")
			os.Setenv("MAX_CONCURRENT_TASKS", strconv.Itoa(maxTasks))
			os.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
			os.Unsetenv("SESSION_TIMEOUT")
			os.Unsetenv("TASK_TIMEOUT")
			os.Unsetenv("RATE_LIMIT")

			cfg, err := LoadConfig()
			if err != nil {
				t.Fatalf("expected no error for valid config, got: %v", err)
			}
			if cfg.Port != port {
				t.Fatalf("expected port %d, got %d", port, cfg.Port)
			}
			if cfg.MaxConcurrentTasks != maxTasks {
				t.Fatalf("expected max tasks %d, got %d", maxTasks, cfg.MaxConcurrentTasks)
			}
		})
	})

	t.Run("missing API_TOKEN fails", func(t *testing.T) {
		os.Setenv("APP_PORT", "8080")
		os.Unsetenv("API_TOKEN")
		os.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")

		_, err := LoadConfig()
		if err == nil {
			t.Fatal("expected error when API_TOKEN is missing")
		}
	})

	t.Run("missing WORKSPACE_HOST_DIR fails", func(t *testing.T) {
		os.Setenv("APP_PORT", "8080")
		os.Setenv("API_TOKEN", "test-token")
		os.Unsetenv("WORKSPACE_HOST_DIR")

		_, err := LoadConfig()
		if err == nil {
			t.Fatal("expected error when WORKSPACE_HOST_DIR is missing")
		}
	})

	t.Run("defaults applied when optional vars unset", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		os.Unsetenv("SESSION_TIMEOUT")
		os.Unsetenv("TASK_TIMEOUT")
		os.Unsetenv("RATE_LIMIT")
		os.Unsetenv("STATE_DIR")
		os.Unsetenv("MAX_CONCURRENT_TASKS")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("expected no error for minimal valid env, got: %v", err)
		}
		if cfg.SessionTimeout != 24*time.Hour {
			t.Errorf("expected SESSION_TIMEOUT default 24h, got %v", cfg.SessionTimeout)
		}
		if cfg.TaskTimeout != 10*time.Minute {
			t.Errorf("expected TASK_TIMEOUT default 10m, got %v", cfg.TaskTimeout)
		}
		if cfg.RateLimit != 60 {
			t.Errorf("expected RATE_LIMIT default 60, got %d", cfg.RateLimit)
		}
		if cfg.StateDir != "/tmp/trayline-server" {
			t.Errorf("expected STATE_DIR default /tmp/trayline-server, got %q", cfg.StateDir)
		}
		if cfg.MaxConcurrentTasks != 2 {
			t.Errorf("expected MAX_CONCURRENT_TASKS default 2, got %d", cfg.MaxConcurrentTasks)
		}
	})
}
