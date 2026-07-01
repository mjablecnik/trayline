package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	logger := NewLogger(cfg.APIToken)
	ctx := context.Background()

	if err := ensureWorkspaceDir(cfg.WorkspaceDir); err != nil {
		logger.Error(ctx, "workspace directory error: "+err.Error())
		os.Exit(1)
	}

	if err := EnsureStateDir(cfg.StateDir); err != nil {
		logger.Error(ctx, "state directory error: "+err.Error())
		os.Exit(1)
	}

	dockerClient, err := NewDockerClient()
	if err != nil {
		logger.Error(ctx, "docker client error: "+err.Error())
		os.Exit(1)
	}

	taskStore := NewTaskStore()
	sessionStore := NewSessionStore()
	cm := NewContainerManager(dockerClient, cfg, logger)
	stateMgr := NewStateManager(cfg.StateDir, taskStore, sessionStore, cm, logger)

	if err := stateMgr.Recover(ctx); err != nil {
		logger.Error(ctx, "state recovery error: "+err.Error())
		// Non-fatal: continue with whatever state was recovered.
	}

	taskH := NewTaskHandler(taskStore, cm, logger)
	health := &HealthHandler{}
	sessionH := NewSessionHandler(sessionStore, cm, logger, cfg)

	// Background idle-timeout checker for sessions.
	sessionH.StartIdleTimeoutChecker(ctx)

	rl := NewRateLimiter(cfg.RateLimit)
	router := NewRouter(health, taskH, sessionH, cfg.APIToken, rl, logger)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: router,
	}

	// Signal handling for graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-quit
		logger.Info(ctx, "shutdown signal received, draining connections")
		health.SetShuttingDown()

		// Notify all active WebSocket sessions before shutdown.
		terminatedMsg, _ := json.Marshal(WSServerMessage{Type: "terminated"})
		for _, sess := range sessionStore.All() {
			sess.ConnMu.Lock()
			if sess.Conn != nil {
				sess.Conn.WriteMessage(websocket.TextMessage, terminatedMsg)
				sess.Conn.Close()
				sess.Conn = nil
			}
			sess.ConnMu.Unlock()
			if sess.CancelFunc != nil {
				sess.CancelFunc()
			}
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error(ctx, "server shutdown error: "+err.Error())
		}
	}()

	logger.Info(ctx, fmt.Sprintf("server starting on port %d", cfg.Port))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error(ctx, "server error: "+err.Error())
		os.Exit(1)
	}
	logger.Info(ctx, "server stopped")
}

// ensureWorkspaceDir verifies the workspace directory exists and is writable,
// creating it if necessary.
func ensureWorkspaceDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create WORKSPACE_DIR %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return fmt.Errorf("WORKSPACE_DIR %q is not writable: %w", dir, err)
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return nil
}
