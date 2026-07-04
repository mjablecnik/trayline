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

	"server/api"
	"server/core"
	"server/docker"
	"server/store"
)

func main() {
	cfg, err := core.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	logger := core.NewLogger(cfg.APIToken)
	ctx := context.Background()

	if err := ensureWorkspaceDir(cfg.WorkspaceDir); err != nil {
		logger.Error(ctx, "workspace directory error: "+err.Error())
		os.Exit(1)
	}

	if err := store.EnsureStateDir(cfg.StateDir); err != nil {
		logger.Error(ctx, "state directory error: "+err.Error())
		os.Exit(1)
	}

	dockerClient, err := docker.NewDockerClient()
	if err != nil {
		logger.Error(ctx, "docker client error: "+err.Error())
		os.Exit(1)
	}

	taskStore := store.NewTaskStore()
	sessionStore := store.NewSessionStore()
	cm := docker.NewContainerManager(dockerClient, cfg, logger)
	stateMgr := store.NewStateManager(cfg.StateDir, taskStore, sessionStore, cm, logger)

	taskH := api.NewTaskHandler(taskStore, cm, logger, stateMgr)

	health := &api.HealthHandler{}
	sessionH := api.NewSessionHandler(sessionStore, cm, logger, cfg, stateMgr)

	// Wire sessionH into stateMgr before recovery so recovered sessions can stream output.
	stateMgr.SetSessionHandler(sessionH)

	if err := stateMgr.Recover(ctx); err != nil {
		logger.Error(ctx, "state recovery error: "+err.Error())
		// Non-fatal: continue with whatever state was recovered.
	}

	// Background idle-timeout checker for sessions.
	sessionH.StartIdleTimeoutChecker(ctx)

	rl := api.NewRateLimiter(cfg.RateLimit)
	router := api.NewRouter(health, taskH, sessionH, cfg.APIToken, rl, logger)

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
		terminatedMsg, _ := json.Marshal(api.WSServerMessage{Type: "terminated"})
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

		// Cancel all active one-shot tasks so their containers start shutting down.
		activeTasks := taskStore.All()
		for _, t := range activeTasks {
			if !store.IsTerminal(t.Status) && t.CancelFunc != nil {
				t.CancelFunc()
			}
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error(ctx, "server shutdown error: "+err.Error())
		}

		for _, t := range activeTasks {
			if t.Done != nil {
				select {
				case <-t.Done:
				case <-shutdownCtx.Done():
				}
			}
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
