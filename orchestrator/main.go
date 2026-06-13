package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var version = "2.0.0"

func programName() string {
	return filepath.Base(os.Args[0])
}

func usageText() string {
	name := programName()
	return fmt.Sprintf(`%s — sequential AI agent pipeline runner

Usage:
  %s <pipeline> [--dry-run] [--verbose] [--log-llm] [--no-lifecycle] [--var key=value ...]
  %s --pipeline <path> [options]
  %s --version
  %s --help

The pipeline can be specified as a positional argument or with --pipeline flag.

Flags:
  --pipeline string   Path to pipeline YAML file (alternative to positional arg)
  --var key=value     Set or override a pipeline variable (repeatable)
  --dry-run           Print pipeline steps without executing
  --verbose           Stream trayline-agent output to stdout in real time
  --log-llm           Log all LLM requests and responses to llm-debug.log
  --no-lifecycle      Skip lifecycle.yaml before/after steps
  --version           Print version and exit
  --help, -h          Show this help message

Examples:
  %s processes/4-create-code --var specs-name=my-feature
  %s workflows/feature-implementation --var specs-name=my-feature --verbose
  %s tasks/check-build --no-lifecycle
  %s --pipeline workflow.yaml --dry-run
  %s --version
`, name, name, name, name, name, name, name, name, name, name)
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

	pipelineFlag := fs.String("pipeline", "", "Path to pipeline YAML file")
	dryRunFlag := fs.Bool("dry-run", false, "Print pipeline steps without executing")
	verboseFlag := fs.Bool("verbose", false, "Stream trayline-agent output to stdout in real time")
	versionFlag := fs.Bool("version", false, "Print version and exit")
	logLLMFlag := fs.Bool("log-llm", false, "Log all LLM requests and responses to llm-debug.log")
	noLifecycleFlag := fs.Bool("no-lifecycle", false, "Skip lifecycle.yaml before/after steps")
	var vars varFlags
	fs.Var(&vars, "var", "Set variable key=value (repeatable)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *versionFlag {
		fmt.Printf("%s version %s\n", programName(), version)
		return 0
	}

	// Resolve pipeline path: positional argument takes precedence over --pipeline flag
	pipelinePath := *pipelineFlag
	if pipelinePath == "" && fs.NArg() > 0 {
		pipelinePath = fs.Arg(0)
	}

	if pipelinePath == "" {
		fmt.Fprint(os.Stderr, "error: pipeline path is required (positional arg or --pipeline flag)\n\n")
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
	pipeline, yamlVars, err := ParsePipelineRaw(pipelinePath)
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

	// Lifecycle: wrap execution with before/after steps from lifecycle.yaml
	if !*noLifecycleFlag && !*dryRunFlag {
		lifecyclePath := findLifecycleFile()
		if lifecyclePath != "" {
			return runWithLifecycle(executor, lifecyclePath, pipelinePath, resolvedVars, *verboseFlag)
		}
	}

	return executor.Run()
}

// findLifecycleFile searches for lifecycle.yaml in the pipelines directory.
func findLifecycleFile() string {
	// Look next to the current executable first
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, "pipelines", "lifecycle.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		// Also check sibling pipelines dir (for dev mode)
		candidate = filepath.Join(dir, "..", "pipelines", "lifecycle.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Check TRAYLINE_HOME
	home := os.Getenv("TRAYLINE_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".trayline")
	}
	candidate := filepath.Join(home, "pipelines", "lifecycle.yaml")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	return ""
}

// runWithLifecycle executes before steps, runs the pipeline, then executes after steps.
func runWithLifecycle(executor *Executor, lifecyclePath string, pipelineName string, vars map[string]string, verbose bool) int {
	data, err := os.ReadFile(lifecyclePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not read lifecycle.yaml: %v\n", err)
		return executor.Run()
	}

	type LifecycleConfig struct {
		Before []Step `yaml:"before"`
		After  []Step `yaml:"after"`
	}

	var lc LifecycleConfig
	if err := yaml.Unmarshal(data, &lc); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not parse lifecycle.yaml: %v\n", err)
		return executor.Run()
	}

	runner := &OSCommandRunner{}
	env := os.Environ()
	cwd, _ := os.Getwd()

	// Execute before steps
	for i, step := range lc.Before {
		fmt.Printf("\n%s%s⟡ Lifecycle before [%d/%d]:%s %q\n", colorBold, colorCyan, i+1, len(lc.Before), colorReset, step.Name)
		projectDir := step.ProjectDir
		if projectDir == "" {
			projectDir = cwd
		}
		stepVerbose := verbose || step.Verbose
		var exitCode int
		if step.Agent != "" {
			_, exitCode, err = runner.RunAgent(step.Agent, step.Prompt, step.Model, projectDir, env, stepVerbose, os.Stdout, os.Stderr)
		} else if step.Command != "" {
			_, exitCode, err = runner.RunCommand(step.Command, projectDir, env, stepVerbose, os.Stdout, os.Stderr)
		}
		if err != nil || exitCode != 0 {
			fmt.Fprintf(os.Stderr, "%s✗ Lifecycle before step %q failed%s\n", colorRed, step.Name, colorReset)
			return 1
		}
	}

	// Execute main pipeline
	exitCode := executor.Run()

	// Execute after steps (even if pipeline failed — we still want to push results)
	for i, step := range lc.After {
		// Substitute {{pipeline-name}} in after steps
		prompt := strings.ReplaceAll(step.Prompt, "{{pipeline-name}}", filepath.Base(pipelineName))
		command := strings.ReplaceAll(step.Command, "{{pipeline-name}}", filepath.Base(pipelineName))

		fmt.Printf("\n%s%s⟡ Lifecycle after [%d/%d]:%s %q\n", colorBold, colorCyan, i+1, len(lc.After), colorReset, step.Name)
		projectDir := step.ProjectDir
		if projectDir == "" {
			projectDir = cwd
		}
		stepVerbose := verbose || step.Verbose
		if step.Agent != "" {
			runner.RunAgent(step.Agent, prompt, step.Model, projectDir, env, stepVerbose, os.Stdout, os.Stderr)
		} else if command != "" {
			runner.RunCommand(command, projectDir, env, stepVerbose, os.Stdout, os.Stderr)
		}
	}

	return exitCode
}
