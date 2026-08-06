package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"orchestrator/core"
	"orchestrator/engine"
	"orchestrator/llm"
)

// ANSI color codes for terminal output (lifecycle before/after step logging).
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

var version = "2.4.0"

func programName() string {
	return filepath.Base(os.Args[0])
}

func usageText() string {
	name := programName()
	return fmt.Sprintf(`%s — sequential AI agent pipeline runner

Usage:
  %s <pipeline> [--dry-run] [--verbose] [--log-llm] [--no-lifecycle] [--restart] [--var key=value ...]
  %s flow <pipeline> [--then <pipeline> ...] [--dry-run] [--verbose] [--no-lifecycle]
  %s stop
  %s --version
  %s --help

Flags:
  --var key=value     Set or override a pipeline variable (repeatable)
  --dry-run           Print pipeline steps without executing
  --verbose           Stream trayline-agent output to stdout in real time
  --log-llm           Log all LLM requests and responses to llm-debug.log
  --no-lifecycle      Skip lifecycle.yaml before/after steps
  --restart           Ignore checkpoint and start pipeline from the beginning
  --version           Print version and exit
  --help, -h          Show this help message

Subcommands:
  flow                Run multiple pipelines sequentially
  stop                Signal running pipeline to stop gracefully after current step

Flow (multiple pipelines):
  %s flow processes/8-code-review --var path=. --then processes/9-improvements --var path=.

Graceful Stop:
  %s stop             Signal a running pipeline to finish its current step, then stop.
                      The pipeline will still run lifecycle after-steps (commit, push).

Examples:
  %s processes/4-create-code --var specs-name=my-feature
  %s workflows/feature-implementation --var specs-name=my-feature --verbose
  %s tasks/check-build --no-lifecycle
  %s stop
  %s --version
`, name, name, name, name, name, name, name, name, name, name, name, name, name)
}

// varFlags is a repeatable --var flag that accumulates key=value strings.
type varFlags []string

func (v *varFlags) String() string { return strings.Join(*v, ", ") }
func (v *varFlags) Set(val string) error {
	*v = append(*v, val)
	return nil
}

func main() {
	// Check for "flow" subcommand before standard flag parsing
	if len(os.Args) > 1 && os.Args[1] == "flow" {
		os.Exit(engine.RunFlow(os.Args[2:]))
		return
	}
	// Check for "stop" subcommand
	if len(os.Args) > 1 && os.Args[1] == "stop" {
		os.Exit(runStop())
		return
	}
	os.Exit(run(os.Args[1:]))
}

// runStop handles the "stop" subcommand — creates the stop signal file.
func runStop() int {
	if err := engine.RequestGracefulStop(); err != nil {
		fmt.Fprintf(os.Stderr, "%s✗ Failed to request graceful stop: %v%s\n", colorRed, err, colorReset)
		return 1
	}
	fmt.Printf("%s✓ Graceful stop requested. The running pipeline will stop after its current step completes.%s\n", colorYellow, colorReset)
	fmt.Printf("%s  Lifecycle after-steps (commit, push) will still execute.%s\n", colorDim, colorReset)
	return 0
}

func run(args []string) int {
	name := programName()
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usageText())
	}

	pipelineFlag := fs.String("pipeline", "", "")
	// Hidden flag — kept only for internal sub-pipeline calls, not advertised
	dryRunFlag := fs.Bool("dry-run", false, "Print pipeline steps without executing")
	verboseFlag := fs.Bool("verbose", false, "Stream trayline-agent output to stdout in real time")
	versionFlag := fs.Bool("version", false, "Print version and exit")
	logLLMFlag := fs.Bool("log-llm", false, "Log all LLM requests and responses to llm-debug.log")
	noLifecycleFlag := fs.Bool("no-lifecycle", false, "Skip lifecycle.yaml before/after steps")
	restartFlag := fs.Bool("restart", false, "Ignore checkpoint and start pipeline from the beginning")
	var vars varFlags
	fs.Var(&vars, "var", "Set variable key=value (repeatable)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *versionFlag {
		fmt.Printf("%s version %s\n", programName(), version)
		return 0
	}

	// Resolve pipeline path: positional argument takes precedence over --pipeline flag (hidden)
	pipelinePath := *pipelineFlag
	if pipelinePath == "" && fs.NArg() > 0 {
		pipelinePath = fs.Arg(0)
	}

	if pipelinePath == "" {
		fmt.Fprint(os.Stderr, "error: pipeline path is required\n\n")
		fmt.Fprint(os.Stderr, usageText())
		return 1
	}

	// Parse CLI variables first (fail fast on malformed flags)
	cliVars, err := core.ParseCLIVars(vars)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Load config
	cfg := core.LoadConfig()

	// Parse pipeline (raw, no validation yet)
	pipeline, yamlVars, err := core.ParsePipelineRaw(pipelinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Merge YAML variables with CLI overrides (CLI takes precedence)
	resolvedVars := core.MergeVariables(yamlVars, cliVars)

	// Substitute variables in all templatable fields (fails if any placeholder is undefined)
	if err := core.SubstituteVariables(pipeline, resolvedVars); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Validate pipeline after substitution so validation sees resolved values
	if err := core.ValidatePipeline(pipeline); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Validate API key if pipeline needs LLM (skip for dry-run)
	if !*dryRunFlag && pipeline.NeedsLLM() && cfg.OpenRouterAPIKey == "" {
		fmt.Fprintln(os.Stderr, "error: OPENROUTER_API_KEY is required when pipeline contains conditions")
		return 1
	}

	// Build executor
	var llmClient llm.ConditionEvaluator
	if pipeline.NeedsLLM() {
		raw := llm.NewLLMClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)
		if *logLLMFlag {
			logger, err := llm.NewLLMLogger(raw)
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

	executor := &engine.Executor{
		Config:       cfg,
		Pipeline:     pipeline,
		LLM:          llmClient,
		DryRun:       *dryRunFlag,
		Verbose:      *verboseFlag,
		Runner:       &engine.OSCommandRunner{},
		ResolvedVars: resolvedVars,
		PipelineName: pipelinePath,
		Restart:      *restartFlag,
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
func runWithLifecycle(executor *engine.Executor, lifecyclePath string, pipelineName string, vars map[string]string, verbose bool) int {
	data, err := os.ReadFile(lifecyclePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not read lifecycle.yaml: %v\n", err)
		return executor.Run()
	}

	type RetryConfig struct {
		OnRateLimit bool `yaml:"on-rate-limit"`
		WaitMinutes int  `yaml:"wait-minutes"`
		MaxRetries  int  `yaml:"max-retries"`
	}

	type LifecycleConfig struct {
		LogTask string      `yaml:"log-task"`
		Retry   RetryConfig `yaml:"retry"`
		Before  []core.Step `yaml:"before"`
		After   []core.Step `yaml:"after"`
	}

	var lc LifecycleConfig
	if err := yaml.Unmarshal(data, &lc); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not parse lifecycle.yaml: %v\n", err)
		return executor.Run()
	}

	// Pass log-task to executor so it can run it after steps with log:true
	if lc.LogTask != "" {
		executor.LogTask = lc.LogTask
	}

	runner := &engine.OSCommandRunner{}
	env := os.Environ()
	cwd, _ := os.Getwd()

	// Execute before steps (with fallback: if command fails, next agent step resolves it)
	for i := 0; i < len(lc.Before); i++ {
		step := lc.Before[i]
		fmt.Printf("%s  ◇ %s%s\n", colorDim, step.Name, colorReset)
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
			// Command failed — check if next step is an agent fallback
			if i+1 < len(lc.Before) && lc.Before[i+1].Agent != "" {
				fmt.Printf("  %s⚠ Step failed, running fallback: %q%s\n", colorYellow, lc.Before[i+1].Name, colorReset)
				i++ // advance to fallback step
				fallback := lc.Before[i]
				fbDir := fallback.ProjectDir
				if fbDir == "" {
					fbDir = cwd
				}
				fbVerbose := verbose || fallback.Verbose
				_, fbExit, fbErr := runner.RunAgent(fallback.Agent, fallback.Prompt, fallback.Model, fbDir, env, fbVerbose, os.Stdout, os.Stderr)
				if fbErr != nil || fbExit != 0 {
					fmt.Fprintf(os.Stderr, "%s✗ Lifecycle fallback %q also failed%s\n", colorRed, fallback.Name, colorReset)
					return 1
				}
			} else {
				fmt.Fprintf(os.Stderr, "%s✗ Lifecycle before step %q failed%s\n", colorRed, step.Name, colorReset)
				return 1
			}
		} else {
			// Command succeeded — skip next step if it's an agent fallback
			if i+1 < len(lc.Before) && lc.Before[i+1].Agent != "" {
				i++ // skip the fallback agent step
			}
		}
	}

	// Execute main pipeline (with rate limit retry)
	var exitCode int
	maxAttempts := 1
	if lc.Retry.OnRateLimit && lc.Retry.MaxRetries > 0 {
		maxAttempts = lc.Retry.MaxRetries + 1 // +1 for initial attempt
	}
	waitDuration := time.Duration(lc.Retry.WaitMinutes) * time.Minute
	if waitDuration == 0 {
		waitDuration = 120 * time.Minute // default 2 hours
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		exitCode = executor.Run()

		// Exit code 2 = rate limit. Retry if configured.
		if exitCode == 2 && lc.Retry.OnRateLimit && attempt < maxAttempts {
			// Check if we can parse a specific reset time from the output
			var sleepDuration time.Duration
			resetTime := engine.ParseResetTime(executor.RateLimitOutput)
			if !resetTime.IsZero() {
				// Wait until reset time + 10 minute buffer
				sleepDuration = time.Until(resetTime) + 10*time.Minute
				if sleepDuration <= 0 {
					sleepDuration = waitDuration // fallback if somehow in the past
				}
				fmt.Printf("\n%s%s⏳ Rate limit hit (attempt %d/%d). Reset at %s UTC — waiting until %s UTC...%s\n",
					colorBold, colorYellow, attempt, maxAttempts,
					resetTime.Format("15:04"),
					resetTime.Add(10*time.Minute).Format("15:04"),
					colorReset)
			} else {
				sleepDuration = waitDuration
				fmt.Printf("\n%s%s⏳ Rate limit hit (attempt %d/%d). Waiting %d minutes before retry...%s\n",
					colorBold, colorYellow, attempt, maxAttempts, int(sleepDuration.Minutes()), colorReset)
			}
			time.Sleep(sleepDuration)
			executor.RateLimitOutput = "" // clear for next attempt
			fmt.Printf("\n%s%s⟳ Retrying pipeline (attempt %d/%d)...%s\n",
				colorBold, colorCyan, attempt+1, maxAttempts, colorReset)
			continue
		}
		break
	}

	// Execute after steps (with fallback: if command fails, next agent step resolves it)
	for i := 0; i < len(lc.After); i++ {
		step := lc.After[i]
		// Substitute {{pipeline-name}} in after steps
		prompt := strings.ReplaceAll(step.Prompt, "{{pipeline-name}}", filepath.Base(pipelineName))
		command := strings.ReplaceAll(step.Command, "{{pipeline-name}}", filepath.Base(pipelineName))

		fmt.Printf("%s  ◇ %s%s\n", colorDim, step.Name, colorReset)
		projectDir := step.ProjectDir
		if projectDir == "" {
			projectDir = cwd
		}
		stepVerbose := verbose || step.Verbose
		var stepExitCode int
		var stepErr error
		if step.Agent != "" {
			_, stepExitCode, stepErr = runner.RunAgent(step.Agent, prompt, step.Model, projectDir, env, stepVerbose, os.Stdout, os.Stderr)
		} else if command != "" {
			_, stepExitCode, stepErr = runner.RunCommand(command, projectDir, env, stepVerbose, os.Stdout, os.Stderr)
		}

		if stepErr != nil || stepExitCode != 0 {
			// Command failed — check if next step is an agent fallback
			if i+1 < len(lc.After) && lc.After[i+1].Agent != "" {
				fmt.Printf("  %s⚠ Step failed, running fallback: %q%s\n", colorYellow, lc.After[i+1].Name, colorReset)
				i++ // advance to fallback step
				fallback := lc.After[i]
				fbPrompt := strings.ReplaceAll(fallback.Prompt, "{{pipeline-name}}", filepath.Base(pipelineName))
				fbDir := fallback.ProjectDir
				if fbDir == "" {
					fbDir = cwd
				}
				fbVerbose := verbose || fallback.Verbose
				runner.RunAgent(fallback.Agent, fbPrompt, fallback.Model, fbDir, env, fbVerbose, os.Stdout, os.Stderr)
			} else {
				fmt.Fprintf(os.Stderr, "  %s⚠ Lifecycle after step %q failed%s\n", colorYellow, step.Name, colorReset)
			}
		} else {
			// Command succeeded — skip next step if it's an agent fallback
			if i+1 < len(lc.After) && lc.After[i+1].Agent != "" {
				i++ // skip the fallback agent step
			}
		}
	}

	// Graceful stop (exit code 3) is a successful outcome — pipeline stopped cleanly
	if exitCode == 3 {
		return 0
	}

	return exitCode
}
