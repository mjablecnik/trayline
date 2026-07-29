package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_DefaultModel(t *testing.T) {
	os.Unsetenv("OPENROUTER_MODEL")
	os.Unsetenv("OPENROUTER_API_KEY")

	cfg := LoadConfig()
	if cfg.OpenRouterModel != defaultModel {
		t.Errorf("expected default model %q, got %q", defaultModel, cfg.OpenRouterModel)
	}
}

func TestLoadConfig_CustomModel(t *testing.T) {
	t.Setenv("OPENROUTER_MODEL", "openai/gpt-4o")

	cfg := LoadConfig()
	if cfg.OpenRouterModel != "openai/gpt-4o" {
		t.Errorf("expected model 'openai/gpt-4o', got %q", cfg.OpenRouterModel)
	}
}

func TestLoadConfig_APIKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key-123")

	cfg := LoadConfig()
	if cfg.OpenRouterAPIKey != "test-key-123" {
		t.Errorf("expected API key 'test-key-123', got %q", cfg.OpenRouterAPIKey)
	}
}

func TestLoadConfig_DotEnv(t *testing.T) {
	// Create a temp dir with a .env file and run from there
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	os.WriteFile(envFile, []byte("OPENROUTER_API_KEY=from-dotenv\nOPENROUTER_MODEL=openai/gpt-3.5-turbo\n"), 0644)

	// Change to temp dir
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Clear env vars so only .env matters
	os.Unsetenv("OPENROUTER_API_KEY")
	os.Unsetenv("OPENROUTER_MODEL")

	cfg := LoadConfig()
	if cfg.OpenRouterAPIKey != "from-dotenv" {
		t.Errorf("expected API key from .env, got %q", cfg.OpenRouterAPIKey)
	}
	if cfg.OpenRouterModel != "openai/gpt-3.5-turbo" {
		t.Errorf("expected model from .env, got %q", cfg.OpenRouterModel)
	}

	// Cleanup env set by godotenv
	os.Unsetenv("OPENROUTER_API_KEY")
	os.Unsetenv("OPENROUTER_MODEL")
}

func TestLoadConfig_NoDotEnv(t *testing.T) {
	// Change to a temp dir with no .env file
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.Unsetenv("OPENROUTER_API_KEY")
	os.Unsetenv("OPENROUTER_MODEL")

	// Should not panic or error
	cfg := LoadConfig()
	if cfg.OpenRouterModel != defaultModel {
		t.Errorf("expected default model, got %q", cfg.OpenRouterModel)
	}
}
