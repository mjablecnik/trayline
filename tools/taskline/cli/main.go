package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// version is the CLI's semver version string (Requirement 13.5).
const version = "1.1.0"

const usageText = `Usage: taskline <subcommand> [arguments]

Manage the Taskline sequential command queue.

Subcommands:
  add <command> [--name NAME] [--position N] [--project P]  Add a task
  list [--project P]                                        List tasks
  delete <identifier> [--project P]                         Delete a task
  update <identifier> [--command CMD] [--name NAME] [--project P]
                                                              Update a pending task
  retry [--project P]                                       Retry the failed task
  skip [--project P]                                        Skip the failed task
  stop [--project P]                                        Stop the running task
  resume [--project P]                                      Resume queue execution
  status [--project P]                                      Show queue status
  projects                                                  List all known projects
  logs [--project P] [--follow] [--tail N]                  Show project logs

Options:
  --project P    Project namespace (default: current directory name)
  -h, --help     Show this help message and exit
  -v, --version  Show version information and exit

Environment:
  TASKLINE_URL   Taskline server address (default http://localhost:9090)
  NO_COLOR       Disable colored output when set
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run implements the CLI entry point: --help/--version handling, the global
// --project flag, config loading, and subcommand dispatch (Requirements
// 13.4, 13.5, FR-5.1, FR-5.2).
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

	projectFlag, rest, err := extractProjectFlag(args)
	if err != nil {
		return usageError(stderr, err.Error())
	}
	if len(rest) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 2
	}

	client := NewClient(cfg.ServerURL, resolveProject(projectFlag))
	return Execute(rest[0], rest[1:], client, stdout, stderr)
}

// extractProjectFlag scans args for a "--project value" or "--project=value"
// pair, occurring anywhere in the list (before or after the subcommand
// name), and returns its value along with args with that pair removed. It
// is parsed globally, before subcommand dispatch, so individual subcommand
// parsers never see it (design.md "CLI Design").
func extractProjectFlag(args []string) (project string, rest []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--project" {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("flag --project requires a value")
			}
			rest = append(append([]string{}, args[:i]...), args[i+2:]...)
			return args[i+1], rest, nil
		}
		if strings.HasPrefix(a, "--project=") {
			rest = append(append([]string{}, args[:i]...), args[i+1:]...)
			return strings.TrimPrefix(a, "--project="), rest, nil
		}
	}
	return "", args, nil
}

// resolveProject returns flagValue if set, otherwise the basename of the
// current working directory, otherwise "default" (FR-5.2).
func resolveProject(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "default"
	}
	return filepath.Base(cwd)
}
