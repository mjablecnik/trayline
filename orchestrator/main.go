package main

import (
	"flag"
	"fmt"
	"os"
)

var version = "dev"

const usageText = `Agent Orchestrator — sequential AI agent pipeline runner

Usage:
  orchestrator --pipeline <path> [--dry-run] [--verbose]
  orchestrator --version
  orchestrator --help

Flags:
  --pipeline string   Path to pipeline YAML file (required)
  --dry-run           Print pipeline steps without executing
  --verbose           Stream agent-docker output to stdout in real time
  --version           Print version and exit
  --help, -h          Show this help message

Examples:
  orchestrator --pipeline workflow.yaml
  orchestrator --pipeline workflow.yaml --verbose
  orchestrator --pipeline workflow.yaml --dry-run
  orchestrator --version
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("orchestrator", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usageText)
	}

	pipelineFlag := fs.String("pipeline", "", "Path to pipeline YAML file (required)")
	dryRunFlag := fs.Bool("dry-run", false, "Print pipeline steps without executing")
	verboseFlag := fs.Bool("verbose", false, "Stream agent-docker output to stdout in real time")
	versionFlag := fs.Bool("version", false, "Print version and exit")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *versionFlag {
		fmt.Printf("orchestrator version %s\n", version)
		return 0
	}

	if *pipelineFlag == "" {
		fmt.Fprint(os.Stderr, "error: --pipeline flag is required\n\n")
		fmt.Fprint(os.Stderr, usageText)
		return 1
	}

	// Load config
	cfg := LoadConfig()

	// Parse pipeline
	pipeline, err := ParsePipeline(*pipelineFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Validate API key if pipeline needs LLM (skip for dry-run)
	if !*dryRunFlag && pipeline.NeedsLLM() && cfg.OpenRouterAPIKey == "" {
		fmt.Fprintln(os.Stderr, "error: OPENROUTER_API_KEY is required when pipeline contains conditions")
		return 1
	}

	// Build executor
	var llmClient ConditionEvaluator
	if pipeline.NeedsLLM() {
		llmClient = NewLLMClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)
	}

	executor := &Executor{
		Config:   cfg,
		Pipeline: pipeline,
		LLM:      llmClient,
		DryRun:   *dryRunFlag,
		Verbose:  *verboseFlag,
		Runner:   &OSCommandRunner{},
	}

	return executor.Run()
}
