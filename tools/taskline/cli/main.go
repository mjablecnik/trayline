package main

import (
	"fmt"
	"io"
	"os"
)

// version is the CLI's semver version string (Requirement 13.5).
const version = "1.0.0"

const usageText = `Usage: taskline <subcommand> [arguments]

Manage the Taskline sequential command queue.

Subcommands:
  add <command> [--name NAME] [--position N]   Add a task to the queue
  list                                          List tasks in the queue
  delete <identifier>                           Delete a task by ID or name
  update <identifier> [--command CMD] [--name NAME]
                                                 Update a pending task
  retry                                         Retry the failed task
  skip                                          Skip the failed task
  stop                                          Stop the running task
  resume                                        Resume queue execution
  status                                        Show queue status

Options:
  -h, --help     Show this help message and exit
  -v, --version  Show version information and exit

Environment:
  TASKLINE_URL   Taskline server address (default http://localhost:9090)
  NO_COLOR       Disable colored output when set
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run implements the CLI entry point: --help/--version handling, config
// loading, and subcommand dispatch (Requirements 13.4, 13.5).
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}

	switch args[0] {
	case "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return 0
	case "-v", "--version":
		fmt.Fprintf(stdout, "taskline %s\n", version)
		return 0
	}

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 2
	}

	client := NewClient(cfg.ServerURL)
	return Execute(args[0], args[1:], client, stdout, stderr)
}
