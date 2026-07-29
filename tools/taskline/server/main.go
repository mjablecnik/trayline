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

// shutdownGraceTimeout is how long the server waits for a running Task to
// finish on its own after a SIGTERM/SIGINT before escalating to SIGKILL
// (Requirements 1.4, 1.5).
const shutdownGraceTimeout = 30 * time.Second

// recoveredRunningExitCode marks a Task that was "running" in a loaded
// State_File, meaning the server exited (or crashed) without recording the
// command's actual outcome (Requirement 1.10).
const recoveredRunningExitCode = -2

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		logError("invalid configuration: %v", err)
		os.Exit(1)
	}

	names := NewNameGenerator()
	queue, err := LoadState(cfg.StateFile, names)
	if err != nil {
		if errors.Is(err, ErrCorruptedState) {
			logWarn("state file %s is corrupted; renamed with .corrupted suffix, starting with an empty queue", cfg.StateFile)
		} else {
			logError("failed to load state file %s: %v", cfg.StateFile, err)
		}
	}
	tasksLoaded := len(queue.List())

	notifier := NewNotifier(cfg)
	if !cfg.NotificationsEnabled {
		logWarn("notifications disabled: NOTIFY_EMAIL or SMTP_HOST/SMTP_PORT/SMTP_USER/SMTP_PASSWORD not fully configured")
	}

	recoverRunningTask(queue, notifier, cfg.StateFile)

	worker := NewWorker(queue, ShellRunner{}, notifier, cfg.StateFile, os.Stdout)
	go worker.Run()

	handler := NewHandler(queue, worker, cfg.StateFile)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
	}

	logInfo("server started: port=%d state_file=%s notifications=%s tasks_loaded=%d",
		cfg.Port, cfg.StateFile, enabledLabel(cfg.NotificationsEnabled), tasksLoaded)

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

	shutdown(server, worker, queue, cfg.StateFile)
}

// recoverRunningTask implements Requirement 1.10: if the Queue loaded from
// State_File contains a Task left "running" from a previous, uncleanly
// terminated run, it is transitioned to "failed" with exit code -2, the
// Queue is halted, and a failure notification is sent, before the Worker
// starts.
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
// connections, wait up to shutdownGraceTimeout for a running Task to finish,
// escalate to SIGKILL if it hasn't, persist state, and exit 0 (Requirements
// 1.4, 1.5, 1.6).
func shutdown(server *http.Server, worker *Worker, queue *Queue, stateFile string) {
	logInfo("shutdown initiated")

	// Prevent the Worker from starting another Task once the current one (if
	// any) finishes; it does not interrupt a Task already executing.
	worker.Shutdown()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logError("http server shutdown error: %v", err)
	}

	if queue.CurrentTask() != nil {
		if waitForIdle(queue, shutdownGraceTimeout) {
			logInfo("running task finished before shutdown timeout")
		} else {
			logWarn("running task did not finish within %s; sending SIGKILL", shutdownGraceTimeout)
			if _, err := worker.ForceKill(); err != nil && !errors.Is(err, ErrNoRunningTask) {
				logError("failed to force-kill running task: %v", err)
			}
		}
	}

	if stateFile != "" {
		if err := SaveState(queue, stateFile); err != nil {
			logError("failed to persist state: %v", err)
		} else {
			logInfo("state persisted to %s", stateFile)
		}
	}

	logInfo("shutdown complete")
	os.Exit(0)
}

// waitForIdle polls queue for up to timeout, returning true as soon as no
// Task is running. It returns false if the Task is still running once
// timeout elapses.
func waitForIdle(queue *Queue, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if queue.CurrentTask() == nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return queue.CurrentTask() == nil
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
