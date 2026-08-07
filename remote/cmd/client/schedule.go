package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// scheduleSubcommands lists valid schedule sub-actions for dispatch.
var scheduleSubcommands = map[string]bool{
	"list":   true,
	"show":   true,
	"cancel": true,
	"delete": true,
	"logs":   true,
}

// handleSchedule is the top-level dispatcher for the "schedule" subcommand.
// Usage:
//
//	trayline-client schedule <pipeline> [--var k=v]... [--project name]
//	trayline-client schedule list [--project name]
//	trayline-client schedule show <id> [--project name]
//	trayline-client schedule cancel <id> [--project name]
//	trayline-client schedule delete <id> [--project name]
//	trayline-client schedule logs [id] [--project name]
func handleSchedule(args []string, cfg *Config) int {
	if len(args) == 0 {
		// No argument = list
		return handleScheduleList(nil, cfg)
	}

	// Check for --help/-h before dispatch.
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Print(subcommandUsage["schedule"])
			return 0
		}
	}

	sub := args[0]

	// If it's a known sub-action, dispatch.
	if scheduleSubcommands[sub] {
		return dispatchScheduleSub(sub, args[1:], cfg)
	}

	// Otherwise, treat the first arg as a pipeline name → schedule add.
	return handleScheduleAdd(args, cfg)
}

// dispatchScheduleSub routes to the appropriate schedule sub-handler.
func dispatchScheduleSub(sub string, args []string, cfg *Config) int {
	switch sub {
	case "list":
		return handleScheduleList(args, cfg)
	case "show":
		return handleScheduleShow(args, cfg)
	case "cancel":
		return handleScheduleCancel(args, cfg)
	case "delete":
		return handleScheduleDelete(args, cfg)
	case "logs":
		return handleScheduleLogs(args, cfg)
	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown schedule subcommand %q.\nRun with --help for usage information.\n", sub)
		return 2
	}
}

// handleScheduleAdd schedules a new workflow (pipeline).
func handleScheduleAdd(args []string, cfg *Config) int {
	// Go's flag package stops parsing at the first non-flag argument.
	// Reorder args so flags come before the positional pipeline name.
	var flagArgs []string
	var positional []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--var" || args[i] == "--project" {
			flagArgs = append(flagArgs, args[i])
			i++
			if i < len(args) {
				flagArgs = append(flagArgs, args[i])
			}
		} else if strings.HasPrefix(args[i], "--var=") || strings.HasPrefix(args[i], "--project=") {
			flagArgs = append(flagArgs, args[i])
		} else if args[i] == "--help" || args[i] == "-h" {
			flagArgs = append(flagArgs, args[i])
		} else if strings.HasPrefix(args[i], "-") {
			flagArgs = append(flagArgs, args[i])
		} else {
			positional = append(positional, args[i])
		}
	}
	reordered := append(flagArgs, positional...)

	fs := flag.NewFlagSet("schedule", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	projectFlag := fs.String("project", "", "")
	var varFlags varSliceFlag
	fs.Var(&varFlags, "var", "")

	if err := fs.Parse(reordered); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Error: pipeline name is required.\nRun with --help for usage information.")
		return 2
	}

	pipeline := fs.Arg(0)
	if fs.NArg() > 1 {
		fmt.Fprintf(os.Stderr, "Error: unexpected argument %q.\nRun with --help for usage information.\n", fs.Arg(1))
		return 2
	}

	project := resolveProject(*projectFlag)
	if project == "" {
		fmt.Fprintln(os.Stderr, "Error: Could not determine project name. Use --project flag or run from a project directory.")
		return 2
	}

	variables, err := parseVarFlags(varFlags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}

	client := NewAPIClient(cfg)
	wf, apiErr := client.ScheduleWorkflow(project, ScheduleWorkflowRequest{
		Pipeline:  pipeline,
		Variables: variables,
	})
	if apiErr != nil {
		fmt.Fprintln(os.Stderr, apiErr)
		return 1
	}

	if !cfg.Quiet {
		fmt.Fprintf(os.Stderr, "✓ Scheduled workflow %s (%s) for project %q\n", wf.ID, wf.Pipeline, project)
	}
	fmt.Println(wf.ID)
	return 0
}

// handleScheduleList lists workflows for a project.
func handleScheduleList(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("schedule list", flag.ContinueOnError)
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

	client := NewAPIClient(cfg)
	workflows, err := client.ListWorkflows(project)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if len(workflows) == 0 {
		if !cfg.Quiet {
			fmt.Fprintf(os.Stderr, "No workflows for project %q.\n", project)
		}
		return 0
	}

	fmt.Print(FormatWorkflowsTable(workflows))
	return 0
}

// handleScheduleShow shows details of a specific workflow.
func handleScheduleShow(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("schedule show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectFlag := fs.String("project", "", "")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Error: workflow ID is required.\nRun with --help for usage information.")
		return 2
	}

	id := fs.Arg(0)
	project := resolveProject(*projectFlag)
	if project == "" {
		fmt.Fprintln(os.Stderr, "Error: Could not determine project name. Use --project flag or run from a project directory.")
		return 2
	}

	client := NewAPIClient(cfg)
	wf, err := client.GetWorkflow(project, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	printWorkflowDetail(wf, cfg)
	return 0
}

// handleScheduleCancel cancels a queued or running workflow.
func handleScheduleCancel(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("schedule cancel", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectFlag := fs.String("project", "", "")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Error: workflow ID is required.\nRun with --help for usage information.")
		return 2
	}

	id := fs.Arg(0)
	project := resolveProject(*projectFlag)
	if project == "" {
		fmt.Fprintln(os.Stderr, "Error: Could not determine project name. Use --project flag or run from a project directory.")
		return 2
	}

	client := NewAPIClient(cfg)
	wf, err := client.CancelWorkflow(project, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if !cfg.Quiet {
		fmt.Fprintf(os.Stderr, "✓ Workflow %s cancelled (status: %s)\n", wf.ID, wf.Status)
	}
	return 0
}

// handleScheduleDelete deletes a queued workflow (alias for cancel on queued items).
func handleScheduleDelete(args []string, cfg *Config) int {
	// delete is semantically the same as cancel — server handles the distinction.
	return handleScheduleCancel(args, cfg)
}

// printWorkflowDetail prints a detailed view of a single workflow.
func printWorkflowDetail(wf *WorkflowSummary, cfg *Config) {
	fmtr := NewFormatter()

	fmt.Printf("ID:        %s\n", wf.ID)
	fmt.Printf("Pipeline:  %s\n", wf.Pipeline)
	fmt.Printf("Status:    %s\n", colorStatus(fmtr, wf.Status))
	fmt.Printf("Created:   %s\n", FormatTimestamp(wf.CreatedAt))

	if wf.StartedAt != nil {
		fmt.Printf("Started:   %s\n", FormatTimestamp(*wf.StartedAt))
	}
	if wf.CompletedAt != nil {
		fmt.Printf("Completed: %s\n", FormatTimestamp(*wf.CompletedAt))
		if wf.StartedAt != nil {
			elapsed := wf.CompletedAt.Sub(*wf.StartedAt).Round(1000000000) // round to second
			fmt.Printf("Duration:  %s\n", elapsed)
		}
	}
	if wf.ExitCode != nil {
		fmt.Printf("Exit Code: %d\n", *wf.ExitCode)
	}
	if wf.Error != "" {
		fmt.Printf("Error:     %s\n", wf.Error)
	}
	if len(wf.Variables) > 0 {
		fmt.Println("Variables:")
		for k, v := range wf.Variables {
			fmt.Printf("  %s = %s\n", k, v)
		}
	}
	if wf.Log != "" && !cfg.Quiet {
		fmt.Println("--- Log ---")
		fmt.Print(wf.Log)
		if wf.Truncated {
			fmt.Println("\n[log truncated]")
		}
	}
}

// colorStatus returns the status string with ANSI color based on workflow state.
func colorStatus(fmtr *Formatter, status string) string {
	switch status {
	case "completed":
		return fmtr.Green(os.Stdout, status)
	case "failed":
		return fmtr.Red(os.Stdout, status)
	case "running":
		return fmtr.Cyan(os.Stdout, status)
	case "queued":
		return fmtr.Yellow(os.Stdout, status)
	case "cancelled":
		return fmtr.Yellow(os.Stdout, status)
	default:
		return status
	}
}

// resolveProject resolves the project name from the --project flag or the current directory basename.
func resolveProject(projectFlag string) string {
	if projectFlag != "" {
		return projectFlag
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Base(cwd)
}

// varSliceFlag implements flag.Value for repeatable --var key=value flags.
type varSliceFlag []string

func (f *varSliceFlag) String() string { return strings.Join(*f, ", ") }
func (f *varSliceFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// parseVarFlags parses --var key=value strings into a map.
func parseVarFlags(flags varSliceFlag) (map[string]string, error) {
	result := make(map[string]string, len(flags))
	for _, v := range flags {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --var format %q, expected key=value", v)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid --var format %q, key must not be empty", v)
		}
		result[key] = val
	}
	return result, nil
}
