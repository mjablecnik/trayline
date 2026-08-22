package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

const defaultServerURL = "http://localhost:9090"

// Config holds the CLI's runtime configuration, loaded from environment
// variables (optionally populated from a .env file).
type Config struct {
	ServerURL string
	Token     string
}

// LoadConfig loads and validates the CLI configuration from the process
// environment, first attempting to populate it from a .env file if present.
func LoadConfig() (Config, error) {
	_ = godotenv.Load()

	url := os.Getenv("TASKLINE_URL")
	if url == "" {
		url = defaultServerURL
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return Config{}, fmt.Errorf("invalid TASKLINE_URL %q: must start with http:// or https://", url)
	}

	return Config{ServerURL: url, Token: os.Getenv("TASKLINE_TOKEN")}, nil
}
