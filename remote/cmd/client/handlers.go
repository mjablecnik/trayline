package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// handleHealth calls GET /health and reports the result.
func handleHealth(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Print(subcommandUsage["health"])
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: health takes no arguments.\nRun with --help for usage information.\n")
		return 2
	}

	client := NewAPIClient(cfg)
	if err := client.Health(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("Server is healthy")
	return 0
}

