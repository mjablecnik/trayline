package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration for the API server.
type Config struct {
	Port               int
	APIToken           string
	MaxConcurrentTasks int
	WorkspaceDir       string
	WorkspaceHostDir   string
	SessionTimeout     time.Duration
	TaskTimeout        time.Duration
	RateLimit          int
	StateDir           string
}

// LoadConfig reads environment variables, applies defaults, validates all values,
// and returns a Config. Returns an error if any required variable is missing or
// any value fails validation.
func LoadConfig() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{}

	// APP_PORT
	portStr := os.Getenv("APP_PORT")
	if portStr == "" {
		cfg.Port = 8080
	} else {
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("APP_PORT must be a number between 1 and 65535, got %q", portStr)
		}
		cfg.Port = port
	}

	// API_TOKEN (required)
	cfg.APIToken = os.Getenv("API_TOKEN")
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("API_TOKEN is required and must not be empty")
	}

	// MAX_CONCURRENT_TASKS
	maxStr := os.Getenv("MAX_CONCURRENT_TASKS")
	if maxStr == "" {
		cfg.MaxConcurrentTasks = 2
	} else {
		max, err := strconv.Atoi(maxStr)
		if err != nil || max < 1 || max > 32 {
			return nil, fmt.Errorf("MAX_CONCURRENT_TASKS must be an integer between 1 and 32, got %q", maxStr)
		}
		cfg.MaxConcurrentTasks = max
	}

	// WORKSPACE_DIR
	cfg.WorkspaceDir = os.Getenv("WORKSPACE_DIR")
	if cfg.WorkspaceDir == "" {
		cfg.WorkspaceDir = "./workspace"
	}

	// WORKSPACE_HOST_DIR (required)
	cfg.WorkspaceHostDir = os.Getenv("WORKSPACE_HOST_DIR")
	if cfg.WorkspaceHostDir == "" {
		return nil, fmt.Errorf("WORKSPACE_HOST_DIR is required and must not be empty")
	}

	// SESSION_TIMEOUT
	sessionTimeoutStr := os.Getenv("SESSION_TIMEOUT")
	if sessionTimeoutStr == "" {
		cfg.SessionTimeout = 24 * time.Hour
	} else {
		d, err := time.ParseDuration(sessionTimeoutStr)
		if err != nil {
			return nil, fmt.Errorf("SESSION_TIMEOUT must be a valid duration (e.g. 24h), got %q: %w", sessionTimeoutStr, err)
		}
		cfg.SessionTimeout = d
	}

	// TASK_TIMEOUT
	taskTimeoutStr := os.Getenv("TASK_TIMEOUT")
	if taskTimeoutStr == "" {
		cfg.TaskTimeout = 10 * time.Minute
	} else {
		d, err := time.ParseDuration(taskTimeoutStr)
		if err != nil {
			return nil, fmt.Errorf("TASK_TIMEOUT must be a valid duration (e.g. 10m), got %q: %w", taskTimeoutStr, err)
		}
		cfg.TaskTimeout = d
	}

	// RATE_LIMIT
	rateLimitStr := os.Getenv("RATE_LIMIT")
	if rateLimitStr == "" {
		cfg.RateLimit = 60
	} else {
		rate, err := strconv.Atoi(rateLimitStr)
		if err != nil || rate < 1 {
			return nil, fmt.Errorf("RATE_LIMIT must be a positive integer, got %q", rateLimitStr)
		}
		cfg.RateLimit = rate
	}

	// STATE_DIR
	cfg.StateDir = os.Getenv("STATE_DIR")
	if cfg.StateDir == "" {
		cfg.StateDir = "/tmp/trayline-server"
	}

	return cfg, nil
}
