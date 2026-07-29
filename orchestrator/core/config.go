package core

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

const defaultModel = "openai/gpt-4.1-mini"

// Config holds runtime configuration for the orchestrator.
type Config struct {
	OpenRouterAPIKey string
	OpenRouterModel  string
}

// LoadConfig loads environment variables and returns a Config.
// It tries these .env files in order (first found wins):
//  1. .env in the current working directory (for development)
//  2. ~/.trayline/env/orchestrator.env (installed config)
func LoadConfig() *Config {
	// Try local .env first (development override)
	if err := godotenv.Load(); err != nil {
		// Fall back to installed env file
		home, _ := os.UserHomeDir()
		if home != "" {
			_ = godotenv.Load(filepath.Join(home, ".trayline", "env", "orchestrator.env"))
		}
	}

	model := os.Getenv("OPENROUTER_MODEL")
	if model == "" {
		model = defaultModel
	}

	return &Config{
		OpenRouterAPIKey: os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel:  model,
	}
}
