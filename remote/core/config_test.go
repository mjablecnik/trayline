package core

import (
	"os"
	"path/filepath"
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
		os.Setenv("PROJECTS_DIR", "/tmp")
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
			os.Setenv("PROJECTS_DIR", "/tmp")
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
			os.Setenv("PROJECTS_DIR", "/tmp")
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
			os.Setenv("PROJECTS_DIR", "/tmp")
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
		os.Setenv("PROJECTS_DIR", "/tmp")
		os.Unsetenv("WORKSPACE_HOST_DIR")

		_, err := LoadConfig()
		if err == nil {
			t.Fatal("expected error when WORKSPACE_HOST_DIR is missing")
		}
	})

	t.Run("missing PROJECTS_DIR fails", func(t *testing.T) {
		os.Setenv("APP_PORT", "8080")
		os.Setenv("API_TOKEN", "test-token")
		os.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		os.Unsetenv("PROJECTS_DIR")

		_, err := LoadConfig()
		if err == nil {
			t.Fatal("expected error when PROJECTS_DIR is missing")
		}
	})

	t.Run("PROJECTS_DIR not a directory fails", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "not-a-dir")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		f.Close()

		os.Setenv("APP_PORT", "8080")
		os.Setenv("API_TOKEN", "test-token")
		os.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		os.Setenv("PROJECTS_DIR", f.Name())

		_, err = LoadConfig()
		if err == nil {
			t.Fatal("expected error when PROJECTS_DIR is not a directory")
		}
	})

	t.Run("PROJECTS_DIR does not exist fails", func(t *testing.T) {
		os.Setenv("APP_PORT", "8080")
		os.Setenv("API_TOKEN", "test-token")
		os.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		os.Setenv("PROJECTS_DIR", "/nonexistent/path/does-not-exist")

		_, err := LoadConfig()
		if err == nil {
			t.Fatal("expected error when PROJECTS_DIR does not exist")
		}
	})

	t.Run("DASHBOARD_ORIGIN empty by default", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
		os.Unsetenv("DASHBOARD_ORIGIN")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if cfg.DashboardOrigin != "" {
			t.Errorf("expected empty DashboardOrigin by default, got %q", cfg.DashboardOrigin)
		}
	})

	t.Run("DASHBOARD_ORIGIN set is honored", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
		t.Setenv("DASHBOARD_ORIGIN", "http://localhost:5173")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if cfg.DashboardOrigin != "http://localhost:5173" {
			t.Errorf("expected DashboardOrigin to be set, got %q", cfg.DashboardOrigin)
		}
	})

	t.Run("TRAYLINE_HOME_DIR default derives from user home", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
		os.Unsetenv("TRAYLINE_HOME_DIR")
		os.Unsetenv("PIPELINES_DIR")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		home, _ := os.UserHomeDir()
		wantHome := home + "/.trayline"
		if cfg.TraylineHomeDir != wantHome {
			t.Errorf("expected TraylineHomeDir %q, got %q", wantHome, cfg.TraylineHomeDir)
		}
		if cfg.PipelinesDir != wantHome+"/pipelines" {
			t.Errorf("expected PipelinesDir %q, got %q", wantHome+"/pipelines", cfg.PipelinesDir)
		}
	})

	t.Run("TRAYLINE_HOME_DIR and PIPELINES_DIR overrides honored", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
		t.Setenv("TRAYLINE_HOME_DIR", "/custom/trayline")
		t.Setenv("PIPELINES_DIR", "/custom/pipelines")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if cfg.TraylineHomeDir != "/custom/trayline" {
			t.Errorf("expected TraylineHomeDir override, got %q", cfg.TraylineHomeDir)
		}
		if cfg.PipelinesDir != "/custom/pipelines" {
			t.Errorf("expected PipelinesDir override, got %q", cfg.PipelinesDir)
		}
	})

	t.Run("TRAYLINE_HOME_DIR and PIPELINES_DIR are not validated to exist at load time", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
		t.Setenv("TRAYLINE_HOME_DIR", "/nonexistent/does-not-exist")
		os.Unsetenv("PIPELINES_DIR")

		if _, err := LoadConfig(); err != nil {
			t.Fatalf("expected no error even though TRAYLINE_HOME_DIR does not exist on disk, got: %v", err)
		}
	})

	t.Run("defaults applied when optional vars unset", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
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

	t.Run("ASSISTANT_DATA_DIR defaults to parent of PROJECTS_DIR plus .assistant", func(t *testing.T) {
		projectsDir := filepath.Join(t.TempDir(), "projects")
		if err := os.Mkdir(projectsDir, 0755); err != nil {
			t.Fatalf("failed to create projects dir: %v", err)
		}

		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", projectsDir)
		os.Unsetenv("ASSISTANT_DATA_DIR")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		want := filepath.Join(filepath.Dir(projectsDir), ".assistant")
		if cfg.AssistantDataDir != want {
			t.Errorf("expected AssistantDataDir default %q, got %q", want, cfg.AssistantDataDir)
		}
	})

	t.Run("ASSISTANT_DATA_DIR set is honored", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
		t.Setenv("ASSISTANT_DATA_DIR", "/tmp/custom-assistant")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if cfg.AssistantDataDir != "/tmp/custom-assistant" {
			t.Errorf("expected AssistantDataDir /tmp/custom-assistant, got %q", cfg.AssistantDataDir)
		}
	})

	t.Run("ASSISTANT_DATA_DIR not a directory fails", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "not-a-dir")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		f.Close()

		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
		t.Setenv("ASSISTANT_DATA_DIR", f.Name())

		_, err = LoadConfig()
		if err == nil {
			t.Fatal("expected error when ASSISTANT_DATA_DIR is not a directory")
		}
	})

	t.Run("ASSISTANT_DATA_DIR does not exist is not validated at load time", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
		t.Setenv("ASSISTANT_DATA_DIR", "/nonexistent/does-not-exist")

		if _, err := LoadConfig(); err != nil {
			t.Fatalf("expected no error even though ASSISTANT_DATA_DIR does not exist on disk (creation deferred to AssistantFolderManager.Init), got: %v", err)
		}
	})

	// Security regression: the session cookie's Secure flag must default to
	// strict (true) whenever APP_ENV is unset or anything other than exactly
	// "development" — per code-security.md's Core Principle, relaxation must
	// be explicit and never assumed.
	t.Run("CookieSecure defaults to true when APP_ENV is unset", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
		os.Unsetenv("APP_ENV")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.CookieSecure {
			t.Error("expected CookieSecure=true by default")
		}
	})

	t.Run("CookieSecure is true for any APP_ENV value other than development", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
		t.Setenv("APP_ENV", "production")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.CookieSecure {
			t.Error("expected CookieSecure=true for APP_ENV=production")
		}
	})

	t.Run("CookieSecure is false only when APP_ENV=development", func(t *testing.T) {
		t.Setenv("APP_PORT", "8080")
		t.Setenv("API_TOKEN", "test-token")
		t.Setenv("WORKSPACE_HOST_DIR", "/tmp/workspace")
		t.Setenv("PROJECTS_DIR", "/tmp")
		t.Setenv("APP_ENV", "development")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.CookieSecure {
			t.Error("expected CookieSecure=false for APP_ENV=development")
		}
	})
}
