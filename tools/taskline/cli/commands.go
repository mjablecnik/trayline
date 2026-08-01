package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// parsedArgs is the result of scanning a subcommand's argument list into
// positional arguments and named flag values. Flags are recognized anywhere
// in the argument list (not just before positional args), since Go's
// flag.FlagSet stops parsing at the first non-flag token.
type parsedArgs struct {
	positional []string
	flags      map[string]string
}

// parseArgs scans args, treating any token starting with "--" as a flag
// (either "--name value" or "--name=value") if its name is present in
// known, and everything else as positional. It returns an error for
// unrecognized flags or flags missing a value.
func parseArgs(args []string, known map[string]bool) (parsedArgs, error) {
	pa := parsedArgs{flags: map[string]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			pa.positional = append(pa.positional, a)
			continue
		}
		name := strings.TrimPrefix(a, "--")
		if idx := strings.Index(name, "="); idx >= 0 {
			key := name[:idx]
			if !known[key] {
				return pa, fmt.Errorf("unknown flag --%s", key)
			}
			pa.flags[key] = name[idx+1:]
			continue
		}
		if !known[name] {
			return pa, fmt.Errorf("unknown flag --%s", name)
		}
		if i+1 >= len(args) {
			return pa, fmt.Errorf("flag --%s requires a value", name)
		}
		i++
		pa.flags[name] = args[i]
	}
	return pa, nil
}

// usageError prints msg to stderr and returns the exit code for CLI usage
// errors (Requirement 12.13, 12.14, 13.3).
func usageError(stderr io.Writer, msg string) int {
	fmt.Fprintln(stderr, "Error:", msg)
	return 2
}

// serverError prints err to stderr and returns the exit code for server /
// connection errors (Requirement 12.15, 13.6).
func serverError(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "Error:", err)
	return 1
}

// Execute dispatches a subcommand by name, running it against client and
// writing output to stdout/stderr. It returns the process exit code.
func Execute(name string, args []string, client *Client, stdout, stderr io.Writer) int {
	switch name {
	case "add":
		return cmdAdd(client, args, stdout, stderr)
	case "list":
		return cmdList(client, args, stdout, stderr)
	case "delete":
		return cmdDelete(client, args, stdout, stderr)
	case "update":
		return cmdUpdate(client, args, stdout, stderr)
	case "retry":
		return cmdRetry(client, args, stdout, stderr)
	case "skip":
		return cmdSkip(client, args, stdout, stderr)
	case "stop":
		return cmdStop(client, args, stdout, stderr)
	case "resume":
		return cmdResume(client, args, stdout, stderr)
	case "status":
		return cmdStatus(client, args, stdout, stderr)
	default:
		return usageError(stderr, fmt.Sprintf("unknown subcommand %q", name))
	}
}

func cmdAdd(client *Client, args []string, stdout, stderr io.Writer) int {
	pa, err := parseArgs(args, map[string]bool{"name": true, "position": true})
	if err != nil {
		return usageError(stderr, err.Error())
	}
	if len(pa.positional) == 0 {
		return usageError(stderr, "add requires a command argument")
	}
	if len(pa.positional) > 1 {
		return usageError(stderr, "add takes a single command argument (quote the command if it contains spaces)")
	}
	command := pa.positional[0]
	name := pa.flags["name"]

	var position *int
	if v, ok := pa.flags["position"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return usageError(stderr, "--position must be an integer")
		}
		position = &n
	}

	cwd, err := os.Getwd()
	if err != nil {
		return serverError(stderr, fmt.Errorf("get working directory: %w", err))
	}

	resp, err := client.CreateTask(command, name, cwd, position)
	if err != nil {
		return serverError(stderr, err)
	}
	fmt.Fprintf(stdout, "Task %s (%s) created: %s [%s] at position %d\n",
		resp.Name, resp.ID, resp.Command, resp.Status, resp.Position)
	return 0
}

func cmdList(client *Client, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return usageError(stderr, "list takes no arguments")
	}
	tasks, err := client.ListTasks()
	if err != nil {
		return serverError(stderr, err)
	}
	fmt.Fprintln(stdout, FormatTaskList(tasks, ColorEnabled()))
	return 0
}

func cmdDelete(client *Client, args []string, stdout, stderr io.Writer) int {
	pa, err := parseArgs(args, map[string]bool{})
	if err != nil {
		return usageError(stderr, err.Error())
	}
	if len(pa.positional) != 1 {
		return usageError(stderr, "delete requires a task identifier argument")
	}
	task, err := client.DeleteTask(pa.positional[0])
	if err != nil {
		return serverError(stderr, err)
	}
	fmt.Fprintf(stdout, "Task %s (%s) deleted\n", task.Name, task.ID)
	return 0
}

func cmdUpdate(client *Client, args []string, stdout, stderr io.Writer) int {
	pa, err := parseArgs(args, map[string]bool{"command": true, "name": true})
	if err != nil {
		return usageError(stderr, err.Error())
	}
	if len(pa.positional) != 1 {
		return usageError(stderr, "update requires a task identifier argument")
	}
	command := pa.flags["command"]
	name := pa.flags["name"]
	if command == "" && name == "" {
		return usageError(stderr, "update requires at least one of --command or --name")
	}

	task, err := client.UpdateTask(pa.positional[0], command, name)
	if err != nil {
		return serverError(stderr, err)
	}
	fmt.Fprintf(stdout, "Task %s (%s) updated: %s\n", task.Name, task.ID, task.Command)
	return 0
}

func cmdRetry(client *Client, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return usageError(stderr, "retry takes no arguments")
	}
	task, err := client.Retry()
	if err != nil {
		return serverError(stderr, err)
	}
	fmt.Fprintf(stdout, "Task %s (%s) retried: %s\n", task.Name, task.ID, task.Status)
	return 0
}

func cmdSkip(client *Client, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return usageError(stderr, "skip takes no arguments")
	}
	result, err := client.Skip()
	if err != nil {
		return serverError(stderr, err)
	}
	fmt.Fprintf(stdout, "Task %s (%s) skipped\n", result.Name, result.ID)
	return 0
}

func cmdStop(client *Client, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return usageError(stderr, "stop takes no arguments")
	}
	task, err := client.Stop()
	if err != nil {
		return serverError(stderr, err)
	}
	fmt.Fprintf(stdout, "Task %s (%s) stopped: %s\n", task.Name, task.ID, task.Command)
	return 0
}

func cmdResume(client *Client, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return usageError(stderr, "resume takes no arguments")
	}
	result, err := client.Resume()
	if err != nil {
		return serverError(stderr, err)
	}
	if result.Message != "" {
		fmt.Fprintf(stdout, "Queue state: %s (%s)\n", result.State, result.Message)
	} else {
		fmt.Fprintf(stdout, "Queue state: %s\n", result.State)
	}
	return 0
}

func cmdStatus(client *Client, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return usageError(stderr, "status takes no arguments")
	}
	result, err := client.Status()
	if err != nil {
		return serverError(stderr, err)
	}

	fmt.Fprintf(stdout, "State: %s\n", result.State)
	fmt.Fprintf(stdout, "Pending: %d\n", result.PendingCount)
	if result.CurrentTask != nil {
		fmt.Fprintf(stdout, "Current task: %s (%s) - %s\n",
			result.CurrentTask.Name, result.CurrentTask.ID, result.CurrentTask.Command)
	}
	if result.FailedTask != nil {
		fmt.Fprintf(stdout, "Failed task: %s (%s) - %s [exit %d]\n",
			result.FailedTask.Name, result.FailedTask.ID, result.FailedTask.Command, result.FailedTask.ExitCode)
	}
	return 0
}
