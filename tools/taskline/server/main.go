package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// shutdownGraceTimeout is how long the server waits for each project's
// running Task to finish on its own after a SIGTERM/SIGINT before escalating
// to SIGKILL (Requirements 1.4, 1.5, NFR-3.1).
const shutdownGraceTimeout = 30 * time.Second

// recoveredRunningExitCode marks a Task that was "running" in a loaded
// state file, meaning the server exited (or crashed) without recording the
// command's actual outcome (Requirement 1.10).
const recoveredRunningExitCode = -2

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		logError("invalid configuration: %v", err)
		os.Exit(1)
	}

	names := NewNameGenerator()
	notifier := NewNotifier(cfg)
	if !cfg.NotificationsEnabled {
		logWarn("notifications disabled: NOTIFY_EMAIL or SMTP_HOST/SMTP_PORT/SMTP_USER/SMTP_PASSWORD not fully configured")
	}

	registry := NewRegistry(cfg.StateDir, cfg.LogDir, names, notifier)
	if err := registry.RestoreAll(); err != nil {
		logError("failed to restore project state: %v", err)
	}
	projectsRestored := len(registry.List())

	handler := NewHandler(registry)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
	}

	logInfo("server started: port=%d state_dir=%s log_dir=%s notifications=%s projects_restored=%d",
		cfg.Port, cfg.StateDir, cfg.LogDir, enabledLabel(cfg.NotificationsEnabled), projectsRestored)

	serveErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-sigCh:
		logInfo("received signal %s", sig)
	case err := <-serveErr:
		logError("http server error: %v", err)
	}

	shutdown(server, registry)
}

// recoverRunningTask implements Requirement 1.10: if queue (as loaded from a
// project's state file) contains a Task left "running" from a previous,
// uncleanly terminated run, it is transitioned to "failed" with exit code
// -2, the Queue is halted, and a failure notification is sent, before the
// project's Worker starts.
func recoverRunningTask(queue *Queue, notifier Notifier, stateFile string) {
	stale := queue.CurrentTask()
	if stale == nil {
		return
	}

	task, err := queue.MarkFailed(recoveredRunningExitCode)
	if err != nil {
		logError("failed to recover running task %s (%s): %v", stale.ID, stale.Name, err)
		return
	}
	logWarn("task %s (%s) was still running when the server last stopped; marked as failed", task.ID, task.Name)

	if err := notifier.NotifyFailure(task); err != nil {
		logError("task %s (%s): failed to send failure notification: %v", task.ID, task.Name, err)
	}

	if stateFile != "" {
		if err := SaveState(queue, stateFile); err != nil {
			logError("failed to persist state: %v", err)
		}
	}
}

// shutdown implements the SIGTERM/SIGINT sequence: stop accepting new
// connections, then let the Registry stop every project's Worker (waiting up
// to shutdownGraceTimeout each for a running Task to finish, escalating to
// SIGKILL if it hasn't) and persist all project state, and exit 0
// (Requirements 1.4, 1.5, 1.6, NFR-3.1, NFR-3.2, NFR-3.3).
func shutdown(server *http.Server, registry *Registry) {
	logInfo("shutdown initiated")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logError("http server shutdown error: %v", err)
	}

	registry.Shutdown(shutdownGraceTimeout)

	logInfo("shutdown complete")
	os.Exit(0)
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
