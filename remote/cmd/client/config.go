package main

import (
	"fmt"
	"strings"

	"github.com/joho/godotenv"
	"os"
)

// ConfigError wraps a config validation failure with an exit code.
type ConfigError struct {
	Message  string
	ExitCode int
}

func (e *ConfigError) Error() string {
	return e.Message
}

// ResolveConfig loads configuration from flags, environment variables, .env file, and defaults.
// Priority: flag > env var > .env file (CWD) > ~/.trayline/env/server.env > default (for URL) / error (for token).
func ResolveConfig(serverFlag, tokenFlag string, verbose, quiet bool) (*Config, error) {
	if quiet && verbose {
		return nil, &ConfigError{
			Message:  "Error: --quiet and --verbose are mutually exclusive flags.",
			ExitCode: 2,
		}
	}

	// Load .env silently — ignore error if file missing.
	dotenv, _ := godotenv.Read(".env")

	// Also load ~/.trayline/env/server.env as a lower-priority fallback.
	traylineHome := os.Getenv("TRAYLINE_HOME")
	if traylineHome == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			traylineHome = home + "/.trayline"
		}
	}
	var serverEnv map[string]string
	if traylineHome != "" {
		serverEnv, _ = godotenv.Read(traylineHome + "/env/server.env")
	}

	serverURL := resolveValueWithFallback(serverFlag, "TRAYLINE_SERVER_URL", dotenv, serverEnv, "http://localhost:8080")
	token := resolveValueWithFallback(tokenFlag, "TRAYLINE_API_TOKEN", dotenv, serverEnv, "")

	// If token not found under TRAYLINE_API_TOKEN, try API_TOKEN (server.env uses this key).
	if token == "" {
		token = resolveValueWithFallback("", "API_TOKEN", dotenv, serverEnv, "")
	}

	if token == "" {
		return nil, &ConfigError{
			Message:  "Error: Authentication token not configured. Set TRAYLINE_API_TOKEN or use --token flag.",
			ExitCode: 2,
		}
	}

	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		return nil, &ConfigError{
			Message:  fmt.Sprintf("Error: Invalid server URL scheme %q. URL must start with http:// or https://.", serverURL),
			ExitCode: 2,
		}
	}

	serverURL = strings.TrimRight(serverURL, "/")

	return &Config{
		ServerURL: serverURL,
		Token:     token,
		Verbose:   verbose,
		Quiet:     quiet,
	}, nil
}

// resolveValue returns the highest-priority non-empty value across flag, env var, .env file, and default.
func resolveValue(flagVal, envKey string, dotenv map[string]string, defaultVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if v, ok := dotenv[envKey]; ok && v != "" {
		return v
	}
	return defaultVal
}

// resolveValueWithFallback extends resolveValue with a second dotenv map (lower priority).
// Priority: flag > env var > primary dotenv > fallback dotenv > default.
func resolveValueWithFallback(flagVal, envKey string, primary, fallback map[string]string, defaultVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if v, ok := primary[envKey]; ok && v != "" {
		return v
	}
	if v, ok := fallback[envKey]; ok && v != "" {
		return v
	}
	return defaultVal
}
