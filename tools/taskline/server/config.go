package main

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

const (
	defaultPort     = 9090
	defaultStateDir = "./state/"
	defaultLogDir   = "./logs/"
	defaultBindAddr = "127.0.0.1"
)

// Config holds the server's runtime configuration, loaded from environment
// variables (optionally populated from a .env file).
type Config struct {
	Port         int
	BindAddr     string
	Token        string
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

	bindAddr := os.Getenv("BIND_ADDR")
	if bindAddr == "" {
		bindAddr = defaultBindAddr
	}

	token := os.Getenv("APP_TOKEN")

	// Every command submitted to this server runs as an arbitrary shell
	// command with no allowlist (that's the tool's job) — this is only safe
	// when either the server is unreachable off-host (loopback bind) or every
	// caller must present a bearer token. Reject any other combination
	// instead of silently starting exposed and unauthenticated.
	if !isLoopbackAddr(bindAddr) && token == "" {
		return Config{}, fmt.Errorf(
			"BIND_ADDR=%q binds to a non-loopback address but APP_TOKEN is not set: "+
				"set APP_TOKEN to require a bearer token on every request, or unset BIND_ADDR to bind to 127.0.0.1", bindAddr)
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
		BindAddr:             bindAddr,
		Token:                token,
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

// isLoopbackAddr reports whether addr (a BIND_ADDR value, not a host:port
// pair) resolves to a loopback-only interface, i.e. unreachable from any
// other host on the network.
func isLoopbackAddr(addr string) bool {
	if addr == "localhost" {
		return true
	}
	ip := net.ParseIP(addr)
	return ip != nil && ip.IsLoopback()
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
