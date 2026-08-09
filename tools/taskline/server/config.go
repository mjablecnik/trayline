package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

const (
	defaultPort     = 9090
	defaultStateDir = "./state/"
	defaultLogDir   = "./logs/"
)

// Config holds the server's runtime configuration, loaded from environment
// variables (optionally populated from a .env file).
type Config struct {
	Port         int
	StateDir     string
	LogDir       string
	NotifyEmail  string
	SMTPHost     string
	SMTPPort     string
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string

	// NotificationsEnabled reflects whether NOTIFY_EMAIL and all required
	// SMTP_* variables are present.
	NotificationsEnabled bool
}

// LoadConfig loads and validates the server configuration from the process
// environment, first attempting to populate it from a .env file if present.
func LoadConfig() (Config, error) {
	_ = godotenv.Load()

	port, err := loadPort()
	if err != nil {
		return Config{}, err
	}

	stateDir := os.Getenv("STATE_DIR")
	if stateDir == "" {
		stateDir = defaultStateDir
	}

	logDir := os.Getenv("LOG_DIR")
	if logDir == "" {
		logDir = defaultLogDir
	}

	notifyEmail := os.Getenv("NOTIFY_EMAIL")
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")
	smtpUser := os.Getenv("SMTP_USER")
	smtpPassword := os.Getenv("SMTP_PASSWORD")
	smtpFrom := os.Getenv("SMTP_FROM")
	if smtpFrom == "" {
		smtpFrom = smtpUser
	}

	notificationsEnabled := notifyEmail != "" && smtpHost != "" && smtpPort != "" &&
		smtpUser != "" && smtpPassword != ""

	return Config{
		Port:                 port,
		StateDir:             stateDir,
		LogDir:               logDir,
		NotifyEmail:          notifyEmail,
		SMTPHost:             smtpHost,
		SMTPPort:             smtpPort,
		SMTPUser:             smtpUser,
		SMTPPassword:         smtpPassword,
		SMTPFrom:             smtpFrom,
		NotificationsEnabled: notificationsEnabled,
	}, nil
}

func loadPort() (int, error) {
	raw := os.Getenv("APP_PORT")
	if raw == "" {
		return defaultPort, nil
	}

	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid APP_PORT value %q: must be a number between 1 and 65535", raw)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid APP_PORT value %q: must be between 1 and 65535", raw)
	}
	return port, nil
}
