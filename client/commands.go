package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

const version = "0.1.0"

const usageText = `trayline-client - Terminal client for the Trayline Agent API Server

Usage:
  trayline-client [--server URL] [--token TOKEN] [--quiet|--verbose] <subcommand> [flags]

Subcommands:
  health              Check server connectivity
  chat                Start or reconnect to an interactive chat session
  run                 Execute a one-shot task and wait for the result
  tasks               List all tasks
  task <id>           Show task details
  cancel <id>         Cancel a running task
  sessions            List active sessions
  terminate <id>      Terminate a session

Global Flags:
  --server URL        Trayline server URL (default: http://localhost:8080,
                      or TRAYLINE_SERVER_URL env var)
  --token TOKEN       Bearer authentication token (or TRAYLINE_API_TOKEN env var)
  --quiet             Suppress informational messages on stderr
  --verbose           Print HTTP method, URL, status, and timing on stderr
  --help, -h          Show this help message and exit
  --version, -v       Show version information and exit

Examples:
  trayline-client health
  trayline-client chat --agent claude
  trayline-client run --agent kiro --prompt "Summarise this code"
  trayline-client tasks
  trayline-client sessions
`

var subcommandUsage = map[string]string{
	"health": `Usage:
  trayline-client health

Check server connectivity. Exits 0 on success, 1 on failure.

Examples:
  trayline-client health
  trayline-client --server http://remote:8080 health
`,
	"chat": `Usage:
  trayline-client chat --agent <agent> [flags]

Start an interactive WebSocket chat session with an AI agent.
Type /quit to end the session. Press Ctrl+C to interrupt the agent.

Flags:
  --agent AGENT       Agent to use: "kiro" or "claude" (required)
  --model MODEL       Model override (optional)
  --system PROMPT     System prompt override (optional)
  --session ID        Reconnect to an existing session ID (optional)

Examples:
  trayline-client chat --agent claude
  trayline-client chat --agent kiro --model gpt-4
  trayline-client chat --agent claude --session abc123
`,
	"run": `Usage:
  trayline-client run --agent <agent> --prompt <text> [flags]

Execute a one-shot task and display the result.

Flags:
  --agent AGENT       Agent to use: "kiro" or "claude" (required)
  --prompt TEXT       Prompt text to send (required)
  --model MODEL       Model override (optional)
  --system PROMPT     System prompt override (optional)
  --format FORMAT     Output format: "json", "text", or "markdown" (optional)
  --file PATH         Upload a file alongside the prompt (repeatable, max 10 files, 50 MB each)

Examples:
  trayline-client run --agent claude --prompt "Explain this error"
  trayline-client run --agent kiro --prompt "Summarise" --format markdown
  trayline-client run --agent claude --prompt "Analyse this data" --file report.csv --file schema.json
`,
	"tasks": `Usage:
  trayline-client tasks

List all tasks with their status, agent, and creation time.

Examples:
  trayline-client tasks
`,
	"task": `Usage:
  trayline-client task <id>

Show detailed information about a specific task.

Examples:
  trayline-client task abc-123
`,
	"cancel": `Usage:
  trayline-client cancel <id>

Cancel a running task.

Examples:
  trayline-client cancel abc-123
`,
	"sessions": `Usage:
  trayline-client sessions

List all active chat sessions.

Examples:
  trayline-client sessions
`,
	"terminate": `Usage:
  trayline-client terminate <id>

Terminate an active chat session.

Examples:
  trayline-client terminate session-abc123
`,
}

// Dispatch parses global flags and routes to the appropriate command handler.
// Returns an exit code: 0 success, 1 runtime error, 2 usage error, 130 SIGINT.
func Dispatch(args []string) int {
	fs := flag.NewFlagSet("trayline-client", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	serverFlag  := fs.String("server", "", "")
	tokenFlag   := fs.String("token", "", "")
	quietFlag   := fs.Bool("quiet", false, "")
	verboseFlag := fs.Bool("verbose", false, "")
	helpFlag    := fs.Bool("help", false, "")
	vFlag       := fs.Bool("v", false, "")
	versionFlag := fs.Bool("version", false, "")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Print(usageText)
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}

	if *helpFlag {
		fmt.Print(usageText)
		return 0
	}
	if *vFlag || *versionFlag {
		fmt.Printf("trayline-client %s\n", version)
		return 0
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		fmt.Fprintf(os.Stderr, "Error: No subcommand provided.\nRun with --help for usage information.\n")
		return 2
	}

	subcommand := remaining[0]
	subArgs := remaining[1:]

	// Subcommand-level --help/-h before config resolution.
	for _, a := range subArgs {
		if a == "--help" || a == "-h" {
			if text, ok := subcommandUsage[subcommand]; ok {
				fmt.Print(text)
			} else {
				fmt.Fprintf(os.Stderr, "Error: Unknown subcommand %q.\nRun with --help for usage information.\n", subcommand)
				return 2
			}
			return 0
		}
	}

	// Validate subcommand before config resolution so unknown commands get exit 2,
	// not a config error.
	validSubcommands := map[string]bool{
		"health": true, "chat": true, "run": true,
		"tasks": true, "task": true, "cancel": true,
		"sessions": true, "terminate": true,
	}
	if !validSubcommands[subcommand] {
		fmt.Fprintf(os.Stderr, "Error: Unknown subcommand %q.\nRun with --help for usage information.\n", subcommand)
		return 2
	}

	cfg, err := ResolveConfig(*serverFlag, *tokenFlag, *verboseFlag, *quietFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if ce, ok := err.(*ConfigError); ok {
			return ce.ExitCode
		}
		return 1
	}

	switch subcommand {
	case "health":
		return handleHealth(subArgs, cfg)
	case "chat":
		return handleChat(subArgs, cfg)
	case "run":
		return handleRun(subArgs, cfg)
	case "tasks":
		return handleTasks(subArgs, cfg)
	case "task":
		return handleTask(subArgs, cfg)
	case "cancel":
		return handleCancel(subArgs, cfg)
	case "sessions":
		return handleSessions(subArgs, cfg)
	case "terminate":
		return handleTerminate(subArgs, cfg)
	default:
		// Unreachable due to validSubcommands check above.
		fmt.Fprintf(os.Stderr, "Error: Unknown subcommand %q.\nRun with --help for usage information.\n", subcommand)
		return 2
	}
}
