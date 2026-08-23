package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration for the API server.
type Config struct {
	Port               int
	APIToken           string
	MaxConcurrentTasks int
	MaxChatSessions    int
	WorkspaceDir       string
	WorkspaceHostDir   string
	SessionTimeout     time.Duration
	TaskTimeout        time.Duration
	RateLimit          int
	StateDir           string
	MaxUploadSize      int64
	MaxUploadFiles     int
	MaxPromptLength    int

	// Agent credential directories on the host — mounted read-only into every agent
	// container, mirroring what trayline-agent does for interactive CLI invocations.

	// KiroHostDir is the host path to ~/.kiro (workspace config, steering files).
	KiroHostDir string
	// KiroCredsHostDir is the host path to ~/.local/share/kiro-cli (auth token).
	KiroCredsHostDir string
	// ClaudeHostDir is the host path to ~/.claude (session data).
	ClaudeHostDir string
	// ClaudeConfigHostFile is the host path to ~/.claude.json (global config/token).
	ClaudeConfigHostFile string
	// FlyHostDir is the host path to ~/.fly (flyctl login token/config), mounted
	// into every agent container regardless of coding agent — flyctl is a deploy
	// tool available to all of them, not an agent credential.
	FlyHostDir string

	// ProjectsDir is the host directory scanned for dashboard projects (git repos).
	ProjectsDir string
	// DashboardOrigin is the allowed CORS origin for the dashboard frontend.
	// Empty disables CORS handling.
	DashboardOrigin string

	// TraylineHomeDir is the host path to ~/.trayline, mounted read-only into
	// workflow containers at /home/agent/.trayline.
	TraylineHomeDir string
	// PipelinesDir is the host path to the pipelines directory read by the
	// pipeline discovery endpoints and workflow execution.
	PipelinesDir string

	// AssistantDataDir is the host directory mounted as /workspace in personal
	// assistant containers. Defaults to {parent of ProjectsDir}/.assistant.
	AssistantDataDir string

	// ReposDir is the host directory containing bare git repositories used as
	// remotes by project working copies. Mounted into workflow containers at
	// the same path so `git pull agent main` works. Empty disables the mount.
	ReposDir string

	// CookieSecure controls the Secure flag on the session cookie set by
	// POST /auth/login. True (the default whenever APP_ENV is unset or
	// anything other than "development") unless explicitly relaxed, per this
	// project's Core Principle: production is strict, and only a named,
	// server-side environment setting may relax it — never client input.
	// Secure cookies cannot be sent over plain http://, which is why local
	// development needs this escape hatch.
	CookieSecure bool
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

	// MAX_CHAT_SESSIONS
	maxChatStr := os.Getenv("MAX_CHAT_SESSIONS")
	if maxChatStr == "" {
		cfg.MaxChatSessions = 4
	} else {
		maxChat, err := strconv.Atoi(maxChatStr)
		if err != nil || maxChat < 1 || maxChat > 32 {
			return nil, fmt.Errorf("MAX_CHAT_SESSIONS must be an integer between 1 and 32, got %q", maxChatStr)
		}
		cfg.MaxChatSessions = maxChat
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

	// MAX_UPLOAD_SIZE
	maxUploadSizeStr := os.Getenv("MAX_UPLOAD_SIZE")
	if maxUploadSizeStr == "" {
		cfg.MaxUploadSize = 50 * 1024 * 1024 // 50 MB
	} else {
		size, err := strconv.ParseInt(maxUploadSizeStr, 10, 64)
		if err != nil || size < 1 {
			return nil, fmt.Errorf("MAX_UPLOAD_SIZE must be a positive integer (bytes), got %q", maxUploadSizeStr)
		}
		cfg.MaxUploadSize = size
	}

	// MAX_UPLOAD_FILES
	maxUploadFilesStr := os.Getenv("MAX_UPLOAD_FILES")
	if maxUploadFilesStr == "" {
		cfg.MaxUploadFiles = 10
	} else {
		count, err := strconv.Atoi(maxUploadFilesStr)
		if err != nil || count < 1 {
			return nil, fmt.Errorf("MAX_UPLOAD_FILES must be a positive integer, got %q", maxUploadFilesStr)
		}
		cfg.MaxUploadFiles = count
	}

	// MAX_PROMPT_LENGTH
	maxPromptLenStr := os.Getenv("MAX_PROMPT_LENGTH")
	if maxPromptLenStr == "" {
		cfg.MaxPromptLength = 32000
	} else {
		maxPromptLen, err := strconv.Atoi(maxPromptLenStr)
		if err != nil || maxPromptLen < 1 {
			return nil, fmt.Errorf("MAX_PROMPT_LENGTH must be a positive integer, got %q", maxPromptLenStr)
		}
		cfg.MaxPromptLength = maxPromptLen
	}

	// Agent credential mounts — mirrors what trayline-agent does on the CLI.
	// All are optional; if unset the corresponding mount is simply skipped.
	cfg.KiroHostDir = os.Getenv("KIRO_HOST_DIR")
	cfg.KiroCredsHostDir = os.Getenv("KIRO_CREDS_HOST_DIR")
	cfg.ClaudeHostDir = os.Getenv("CLAUDE_HOST_DIR")
	cfg.ClaudeConfigHostFile = os.Getenv("CLAUDE_CONFIG_HOST_FILE")

	// FLY_HOST_DIR (optional; host path to ~/.fly, flyctl login token/config).
	// Unlike Kiro/Claude above, this defaults to ~/.fly (auto-detected if it
	// exists) rather than requiring an explicit setting — same convention as
	// REPOS_DIR below. Note: when the server itself runs inside the
	// trayline-server container (the normal deployment via start-docker.sh),
	// os.UserHomeDir() here resolves to *that* container's home, not the real
	// host's — start-docker.sh resolves the true host default and passes it in
	// explicitly via FLY_HOST_DIR, so this fallback only matters when running
	// the server binary directly on the host (e.g. local dev).
	cfg.FlyHostDir = os.Getenv("FLY_HOST_DIR")
	if cfg.FlyHostDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".fly")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				cfg.FlyHostDir = candidate
			}
		}
	}
	cfg.FlyHostDir = os.Getenv("FLY_HOST_DIR")

	// PROJECTS_DIR (required)
	cfg.ProjectsDir = os.Getenv("PROJECTS_DIR")
	if cfg.ProjectsDir == "" {
		return nil, fmt.Errorf("PROJECTS_DIR is required and must not be empty")
	}
	info, err := os.Stat(cfg.ProjectsDir)
	if err != nil {
		return nil, fmt.Errorf("PROJECTS_DIR %q is not accessible: %w", cfg.ProjectsDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("PROJECTS_DIR %q is not a directory", cfg.ProjectsDir)
	}

	// ASSISTANT_DATA_DIR (optional, defaults to {parent of PROJECTS_DIR}/.assistant)
	cfg.AssistantDataDir = os.Getenv("ASSISTANT_DATA_DIR")
	if cfg.AssistantDataDir == "" {
		cfg.AssistantDataDir = filepath.Join(filepath.Dir(cfg.ProjectsDir), ".assistant")
	}
	if assistantInfo, err := os.Stat(cfg.AssistantDataDir); err == nil {
		if !assistantInfo.IsDir() {
			return nil, fmt.Errorf("ASSISTANT_DATA_DIR %q exists but is not a directory", cfg.AssistantDataDir)
		}
	}

	// DASHBOARD_ORIGIN (optional; empty disables CORS)
	cfg.DashboardOrigin = os.Getenv("DASHBOARD_ORIGIN")

	// APP_ENV (optional; only "development" relaxes the session cookie's
	// Secure flag — unset or any other value stays strict)
	cfg.CookieSecure = os.Getenv("APP_ENV") != "development"

	// REPOS_DIR (optional; host path to bare git repos mounted into workflow containers)
	cfg.ReposDir = os.Getenv("REPOS_DIR")
	if cfg.ReposDir == "" {
		// Default: ~/repos (sibling of typical home directory layout)
		home, _ := os.UserHomeDir()
		candidate := filepath.Join(home, "repos")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			cfg.ReposDir = candidate
		}
	}

	// TRAYLINE_HOME_DIR (default: ~/.trayline, expanded using the running user's home directory)
	cfg.TraylineHomeDir = os.Getenv("TRAYLINE_HOME_DIR")
	if cfg.TraylineHomeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve default TRAYLINE_HOME_DIR (~/.trayline): %w", err)
		}
		cfg.TraylineHomeDir = filepath.Join(home, ".trayline")
	}

	// PIPELINES_DIR (default: TRAYLINE_HOME_DIR/pipelines)
	cfg.PipelinesDir = os.Getenv("PIPELINES_DIR")
	if cfg.PipelinesDir == "" {
		cfg.PipelinesDir = filepath.Join(cfg.TraylineHomeDir, "pipelines")
	}

	return cfg, nil
}
