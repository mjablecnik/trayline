package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

const defaultPollInterval = 2 * time.Second
const defaultPollTimeout = 10 * time.Minute

// handleRun implements the `run` subcommand: submit a one-shot task and wait for it.
func handleRun(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	agentFlag  := fs.String("agent", "", "")
	promptFlag := fs.String("prompt", "", "")
	modelFlag  := fs.String("model", "", "")
	systemFlag := fs.String("system", "", "")
	formatFlag := fs.String("format", "", "")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Print(subcommandUsage["run"])
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: unexpected argument %q.\nRun with --help for usage information.\n", fs.Arg(0))
		return 2
	}
	if *agentFlag == "" {
		fmt.Fprintln(os.Stderr, "Error: --agent is required.\nRun with --help for usage information.")
		return 2
	}
	if *promptFlag == "" {
		fmt.Fprintln(os.Stderr, "Error: --prompt is required.\nRun with --help for usage information.")
		return 2
	}

	req := RunRequest{
		Agent:        *agentFlag,
		Prompt:       *promptFlag,
		Model:        *modelFlag,
		System:       *systemFlag,
		OutputFormat: *formatFlag,
	}

	return executeRun(req, *formatFlag, cfg, defaultPollInterval, defaultPollTimeout)
}

// executeRun is the testable core of handleRun with injectable timing parameters.
func executeRun(req RunRequest, format string, cfg *Config, pollInterval, pollTimeout time.Duration) int {
	client := NewAPIClient(cfg)
	run, accepted, err := client.PostRun(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// HTTP 200: immediate result
	if run != nil {
		return displayRunResult(run, format, cfg.Quiet)
	}

	// HTTP 202: poll until terminal status or timeout
	if !cfg.Quiet {
		fmt.Fprintf(os.Stderr, "Task %s submitted (status: %s). Waiting for completion...\n", accepted.ID, accepted.Status)
	}

	deadline := time.Now().Add(pollTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		run, err = client.GetRun(accepted.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		switch run.Status {
		case "completed":
			return displayRunResult(run, format, cfg.Quiet)
		case "failed":
			fmt.Fprintf(os.Stderr, "Error: Task failed: %s\n", run.Error)
			return 1
		case "cancelled":
			fmt.Fprintln(os.Stderr, "Error: Task was cancelled.")
			return 1
		}
	}

	fmt.Fprintln(os.Stderr, "Error: Polling timeout exceeded. Task may still be running.")
	return 1
}

// displayRunResult prints the completed task result to stdout and metadata to stderr.
func displayRunResult(run *RunResponse, format string, quiet bool) int {
	fmt.Print(run.Result)

	if !quiet {
		if run.CompletedAt != nil {
			elapsed := run.CompletedAt.Sub(run.CreatedAt).Round(time.Second)
			fmt.Fprintf(os.Stderr, "Task %s: %s (elapsed: %s)\n", run.ID, run.Status, elapsed)
		} else {
			fmt.Fprintf(os.Stderr, "Task %s: %s\n", run.ID, run.Status)
		}
	}

	if format == "json" && run.Valid != nil && !*run.Valid {
		fmt.Fprintln(os.Stderr, "Warning: Output did not pass JSON format validation.")
	}

	return 0
}
