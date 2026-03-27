package main

import (
	"os"

	"github.com/joho/godotenv"
)

const defaultModel = "openai/gpt-4.1-nano"

// Config holds runtime configuration for the orchestrator.
type Config struct {
	OpenRouterAPIKey string
	OpenRouterModel  string
}

// LoadConfig loads environment variables from .env (if present) and returns a Config.
func LoadConfig() *Config {
	// Silent if .env doesn't exist
	_ = godotenv.Load()

	model := os.Getenv("OPENROUTER_MODEL")
	if model == "" {
		model = defaultModel
	}

	return &Config{
		OpenRouterAPIKey: os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel:  model,
	}
}
