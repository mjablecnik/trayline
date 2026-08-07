package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// logsPollInterval is how long to wait between polls when no workflow is running.
const logsPollInterval = 2 * time.Second

// handleScheduleLogs streams workflow logs. If an ID is provided, streams that
// specific workflow. If no ID is provided, continuously streams the currently
// running workflow and waits for the next one to start.
func handleScheduleLogs(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("schedule logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectFlag := fs.String("project", "", "")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}

	project := resolveProject(*projectFlag)
	if project == "" {
		fmt.Fprintln(os.Stderr, "Error: Could not determine project name. Use --project flag or run from a project directory.")
		return 2
	}

	// Take over signal handling from the global handler.
	signal.Stop(globalSigCh)
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	client := NewAPIClient(cfg)

	// If a specific workflow ID is given, stream only that one.
	if fs.NArg() > 0 {
		id := fs.Arg(0)
		return streamWorkflowLogs(client, project, id, cfg, sigCh)
	}

	// No ID: continuously follow logs for the project.
	return followProjectLogs(client, project, cfg, sigCh)
}

// streamWorkflowLogs connects to a specific workflow's log WebSocket and
// prints output until the workflow finishes or the user interrupts.
func streamWorkflowLogs(client *APIClient, project, id string, cfg *Config, sigCh <-chan os.Signal) int {
	conn, _, err := client.DialWorkflowLogs(project, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer conn.Close()

	return readLogStream(conn, cfg, sigCh)
}

// followProjectLogs continuously streams logs for whatever workflow is running
// or queued for a project. When one finishes, it waits for the next one.
func followProjectLogs(client *APIClient, project string, cfg *Config, sigCh <-chan os.Signal) int {
	fmtr := NewFormatter()
	var lastStreamedID string
	waitingPrinted := false

	for {
		// Check for interrupt before polling.
		select {
		case <-sigCh:
			return 130
		default:
		}

		// Find the running or next queued workflow.
		wfID, pipeline, err := findActiveWorkflow(client, project, lastStreamedID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}

		if wfID == "" {
			// Nothing running or queued — wait and retry.
			if !cfg.Quiet && !waitingPrinted {
				fmt.Fprintf(os.Stderr, "%s Waiting for workflows on project %q...\n",
					fmtr.Cyan(os.Stderr, "ℹ"), project)
				waitingPrinted = true
			}
			select {
			case <-sigCh:
				return 130
			case <-time.After(logsPollInterval):
				continue
			}
		}

		waitingPrinted = false

		if !cfg.Quiet {
			fmt.Fprintf(os.Stderr, "%s Streaming logs for %s (%s)\n",
				fmtr.Cyan(os.Stderr, "▸"), wfID[:8], pipeline)
		}

		conn, _, dialErr := client.DialWorkflowLogs(project, wfID)
		if dialErr != nil {
			// Workflow might have completed between list and connect.
			lastStreamedID = wfID
			continue
		}

		code := readLogStream(conn, cfg, sigCh)
		conn.Close()

		if code == 130 {
			return 130
		}

		lastStreamedID = wfID

		// Brief pause before checking for next workflow.
		select {
		case <-sigCh:
			return 130
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// findActiveWorkflow finds the currently running workflow for a project, or the
// next queued one. It skips lastStreamedID to avoid re-streaming a workflow
// that just finished.
func findActiveWorkflow(client *APIClient, project, lastStreamedID string) (id, pipeline string, err error) {
	workflows, apiErr := client.ListWorkflows(project)
	if apiErr != nil {
		return "", "", apiErr
	}

	// First pass: find running workflow.
	for _, wf := range workflows {
		if wf.Status == "running" && wf.ID != lastStreamedID {
			return wf.ID, wf.Pipeline, nil
		}
	}

	// Second pass: find oldest queued workflow.
	for i := len(workflows) - 1; i >= 0; i-- {
		wf := workflows[i]
		if wf.Status == "queued" && wf.ID != lastStreamedID {
			return wf.ID, wf.Pipeline, nil
		}
	}

	return "", "", nil
}

// readLogStream reads from a workflow log WebSocket and prints output to stdout
// until the stream finishes or the user sends SIGINT.
func readLogStream(conn *websocket.Conn, cfg *Config, sigCh <-chan os.Signal) int {
	type readResult struct {
		msg *WSLogMessage
		err error
	}
	readCh := make(chan readResult, 16)
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				readCh <- readResult{err: err}
				return
			}
			var msg WSLogMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				readCh <- readResult{err: err}
				return
			}
			readCh <- readResult{msg: &msg}
		}
	}()

	fmtr := NewFormatter()

	for {
		select {
		case <-sigCh:
			return 130
		case r := <-readCh:
			if r.err != nil {
				// Connection closed — workflow ended or network issue.
				return 0
			}
			switch r.msg.Type {
			case "output":
				fmt.Print(r.msg.Data)
			case "waiting":
				if !cfg.Quiet {
					fmt.Fprintf(os.Stderr, "%s Workflow queued, waiting to start...\n",
						fmtr.Yellow(os.Stderr, "⏳"))
				}
			case "finished":
				if !cfg.Quiet {
					status := r.msg.Status
					var statusStr string
					switch status {
					case "completed":
						statusStr = fmtr.Green(os.Stderr, "✓ completed")
					case "failed":
						statusStr = fmtr.Red(os.Stderr, "✗ failed")
					case "cancelled":
						statusStr = fmtr.Yellow(os.Stderr, "⚠ cancelled")
					default:
						statusStr = status
					}
					fmt.Fprintf(os.Stderr, "\n%s", statusStr)
					if r.msg.ExitCode != nil {
						fmt.Fprintf(os.Stderr, " (exit code: %d)", *r.msg.ExitCode)
					}
					fmt.Fprintln(os.Stderr)
					if r.msg.Truncated {
						fmt.Fprintln(os.Stderr, "[log was truncated]")
					}
				}
				return 0
			}
		}
	}
}
