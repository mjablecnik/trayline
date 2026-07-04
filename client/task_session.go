package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// handleTasks implements the `tasks` subcommand: list all tasks.
func handleTasks(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("tasks", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Print(subcommandUsage["tasks"])
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: tasks takes no arguments.\nRun with --help for usage information.\n")
		return 2
	}

	client := NewAPIClient(cfg)
	tasks, err := client.GetRuns()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(FormatTasksTable(tasks))
	return 0
}

// handleTask implements the `task <id>` subcommand: show task details.
func handleTask(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("task", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Print(subcommandUsage["task"])
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Error: task ID is required.\nRun with --help for usage information.")
		return 2
	}

	client := NewAPIClient(cfg)
	task, err := client.GetRun(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	printTaskDetail(task)
	return 0
}

// handleCancel implements the `cancel <id>` subcommand: cancel a running task.
func handleCancel(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("cancel", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Print(subcommandUsage["cancel"])
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Error: task ID is required.\nRun with --help for usage information.")
		return 2
	}

	client := NewAPIClient(cfg)
	task, err := client.CancelRun(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Task %s: %s\n", task.ID, task.Status)
	return 0
}

// handleSessions implements the `sessions` subcommand: list active sessions.
func handleSessions(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Print(subcommandUsage["sessions"])
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: sessions takes no arguments.\nRun with --help for usage information.\n")
		return 2
	}

	client := NewAPIClient(cfg)
	sessions, err := client.GetSessions()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(sessions) == 0 {
		fmt.Println("No active sessions.")
		return 0
	}
	fmt.Print(FormatSessionsTable(sessions))
	return 0
}

// handleTerminate implements the `terminate <id>` subcommand: terminate a session.
func handleTerminate(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("terminate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Print(subcommandUsage["terminate"])
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Error: session ID is required.\nRun with --help for usage information.")
		return 2
	}

	client := NewAPIClient(cfg)
	session, err := client.TerminateSession(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Session %s: terminated\n", session.SessionID)
	return 0
}

// printTaskDetail writes full task details to stdout.
func printTaskDetail(task *RunResponse) {
	fmt.Printf("ID:      %s\n", task.ID)
	fmt.Printf("Status:  %s\n", task.Status)
	fmt.Printf("Agent:   %s\n", task.Agent)
	fmt.Printf("Created: %s\n", FormatTimestamp(task.CreatedAt))
	if task.CompletedAt != nil {
		fmt.Printf("Completed: %s\n", FormatTimestamp(*task.CompletedAt))
	}
	if task.Status == "completed" && task.Result != "" {
		fmt.Printf("Result:\n%s\n", task.Result)
	}
	if task.Status == "failed" && task.Error != "" {
		fmt.Printf("Error: %s\n", task.Error)
	}
}
