package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var version = "dev"

func programName() string {
	return filepath.Base(os.Args[0])
}

func usageText() string {
	name := programName()
	return fmt.Sprintf(`%s — sequential AI agent pipeline runner

Usage:
  %s --pipeline <path> [--dry-run] [--verbose] [--log-llm] [--var key=value ...]
  %s --version
  %s --help

Flags:
  --pipeline string   Path to pipeline YAML file (required)
  --var key=value     Set or override a pipeline variable (repeatable)
  --dry-run           Print pipeline steps without executing
  --verbose           Stream trayline-agent output to stdout in real time
  --log-llm           Log all LLM requests and responses to llm-debug.log
  --version           Print version and exit
  --help, -h          Show this help message

Examples:
  %s --pipeline workflow.yaml
  %s --pipeline workflow.yaml --verbose
  %s --pipeline workflow.yaml --dry-run
  %s --pipeline workflow.yaml --var project-path=/tmp/proj --var spec-name=my-spec
  %s --version
`, name, name, name, name, name, name, name, name, name)
}

// varFlags is a repeatable --var flag that accumulates key=value strings.
type varFlags []string

func (v *varFlags) String() string { return strings.Join(*v, ", ") }
func (v *varFlags) Set(val string) error {
	*v = append(*v, val)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	name := programName()
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usageText())
	}

	pipelineFlag := fs.String("pipeline", "", "Path to pipeline YAML file (required)")
	dryRunFlag := fs.Bool("dry-run", false, "Print pipeline steps without executing")
	verboseFlag := fs.Bool("verbose", false, "Stream trayline-agent output to stdout in real time")
	versionFlag := fs.Bool("version", false, "Print version and exit")
	logLLMFlag := fs.Bool("log-llm", false, "Log all LLM requests and responses to llm-debug.log")
	var vars varFlags
	fs.Var(&vars, "var", "Set variable key=value (repeatable)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *versionFlag {
		fmt.Printf("%s version %s\n", programName(), version)
		return 0
	}

	if *pipelineFlag == "" {
		fmt.Fprint(os.Stderr, "error: --pipeline flag is required\n\n")
		fmt.Fprint(os.Stderr, usageText())
		return 1
	}

	// Parse CLI variables first (fail fast on malformed flags)
	cliVars, err := ParseCLIVars(vars)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Load config
	cfg := LoadConfig()

	// Parse pipeline (raw, no validation yet)
	pipeline, yamlVars, err := ParsePipelineRaw(*pipelineFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Merge YAML variables with CLI overrides (CLI takes precedence)
	resolvedVars := MergeVariables(yamlVars, cliVars)

	// Substitute variables in all templatable fields (fails if any placeholder is undefined)
	if err := SubstituteVariables(pipeline, resolvedVars); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Validate pipeline after substitution so validation sees resolved values
	if err := ValidatePipeline(pipeline); err != nil {
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
		raw := NewLLMClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)
		if *logLLMFlag {
			logger, err := NewLLMLogger(raw)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not create LLM log: %v\n", err)
				llmClient = raw
			} else {
				defer logger.Close()
				llmClient = logger
			}
		} else {
			llmClient = raw
		}
	}

	executor := &Executor{
		Config:       cfg,
		Pipeline:     pipeline,
		LLM:          llmClient,
		DryRun:       *dryRunFlag,
		Verbose:      *verboseFlag,
		Runner:       &OSCommandRunner{},
		ResolvedVars: resolvedVars,
	}

	return executor.Run()
}
