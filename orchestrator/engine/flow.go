package engine

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"orchestrator/core"
	"orchestrator/llm"
)

// yamlUnmarshal is a thin wrapper so flow.go can decode YAML without conflicting imports.
func yamlUnmarshal(data []byte, v interface{}) error {
	return yaml.Unmarshal(data, v)
}

// FlowSegment represents a single pipeline in the flow with its own variables.
type FlowSegment struct {
	PipelinePath string
	Vars         map[string]string
}

// programName returns the base name of the running executable.
func programName() string {
	return filepath.Base(os.Args[0])
}

// varFlags is a repeatable --var flag that accumulates key=value strings.
type varFlags []string

func (v *varFlags) String() string { return strings.Join(*v, ", ") }
func (v *varFlags) Set(val string) error {
	*v = append(*v, val)
	return nil
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

func flowUsageText() string {
	name := programName()
	return fmt.Sprintf(`%s flow — run multiple pipelines sequentially

Usage:
  %s flow <pipeline> [--var key=value ...] [--then <pipeline> [--var key=value ...]] ...

Global flags (apply to all pipelines):
  --dry-run           Print all pipeline steps without executing
  --verbose           Stream output to stdout in real time
  --log-llm           Log all LLM requests and responses to llm-debug.log
  --no-lifecycle      Skip lifecycle.yaml before/after steps
  --restart           Ignore checkpoint and start pipeline from the beginning

Separator:
  --then              Separates pipeline segments in the flow

Examples:
  %s flow processes/8-code-review --var path=. --var number=5 --then processes/9-improvements --var path=. --var number=5
  %s flow processes/4-create-code --var specs-name=my-feature --then processes/8-code-review --var specs-name=my-feature --then processes/7-create-tests --var specs-name=my-feature
  %s flow workflows/feature-impl --var specs-name=feat --then processes/10-security-audit --var path=.
  %s flow processes/8-code-review --var path=. --dry-run
`, name, name, name, name, name, name)
}

// parseFlowArgs splits the argument list into global flags and pipeline segments separated by --then.
func parseFlowArgs(args []string) (segments [][]string, globalFlags []string) {
	// First, separate --then segments
	var current []string
	for _, arg := range args {
		if arg == "--then" {
			if len(current) > 0 {
				segments = append(segments, current)
			}
			current = nil
		} else {
			current = append(current, arg)
		}
	}
	if len(current) > 0 {
		segments = append(segments, current)
	}

	// Extract global flags from each segment (they can appear anywhere)
	globalFlagNames := map[string]bool{
		"--dry-run":      true,
		"--verbose":      true,
		"--log-llm":      true,
		"--no-lifecycle": true,
		"--restart":      true,
	}

	seenGlobal := make(map[string]bool)
	var cleanedSegments [][]string
	for _, seg := range segments {
		var cleaned []string
		for _, arg := range seg {
			if globalFlagNames[arg] {
				if !seenGlobal[arg] {
					globalFlags = append(globalFlags, arg)
					seenGlobal[arg] = true
				}
			} else {
				cleaned = append(cleaned, arg)
			}
		}
		if len(cleaned) > 0 {
			cleanedSegments = append(cleanedSegments, cleaned)
		}
	}

	return cleanedSegments, globalFlags
}

// parseSegment parses a single segment's arguments into a FlowSegment.
func parseSegment(args []string) (*FlowSegment, error) {
	fs := flag.NewFlagSet("segment", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var vars varFlags
	fs.Var(&vars, "var", "")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if fs.NArg() == 0 {
		return nil, fmt.Errorf("pipeline path is required in each segment")
	}

	pipelinePath := fs.Arg(0)
	cliVars, err := core.ParseCLIVars(vars)
	if err != nil {
		return nil, err
	}

	return &FlowSegment{
		PipelinePath: pipelinePath,
		Vars:         cliVars,
	}, nil
}

// runFlow is the entry point for the flow subcommand.
func RunFlow(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(os.Stderr, flowUsageText())
		return 0
	}

	// Parse segments and global flags
	rawSegments, globalFlags := parseFlowArgs(args)

	if len(rawSegments) == 0 {
		fmt.Fprintln(os.Stderr, "error: at least one pipeline is required")
		fmt.Fprint(os.Stderr, flowUsageText())
		return 1
	}

	// Parse global flags
	globalFS := flag.NewFlagSet("flow-global", flag.ContinueOnError)
	globalFS.SetOutput(os.Stderr)
	dryRun := globalFS.Bool("dry-run", false, "")
	verbose := globalFS.Bool("verbose", false, "")
	logLLM := globalFS.Bool("log-llm", false, "")
	noLifecycle := globalFS.Bool("no-lifecycle", false, "")
	restart := globalFS.Bool("restart", false, "")
	if err := globalFS.Parse(globalFlags); err != nil {
		return 1
	}

	// Parse each segment
	var segments []*FlowSegment
	for i, raw := range rawSegments {
		seg, err := parseSegment(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error in pipeline segment %d: %v\n", i+1, err)
			return 1
		}
		segments = append(segments, seg)
	}

	// Load config
	cfg := core.LoadConfig()

	// Print flow overview
	fmt.Printf("\n%s%s━━━ Flow: %d pipeline(s) ━━━%s\n", colorBold, colorCyan, len(segments), colorReset)
	for i, seg := range segments {
		varStr := ""
		if len(seg.Vars) > 0 {
			var parts []string
			for k, v := range seg.Vars {
				parts = append(parts, k+"="+v)
			}
			varStr = " (" + strings.Join(parts, ", ") + ")"
		}
		fmt.Printf("  %d. %s%s\n", i+1, seg.PipelinePath, varStr)
	}
	fmt.Println()

	// Lifecycle wrapper
	if !*noLifecycle && !*dryRun {
		lifecyclePath := findLifecycleFile()
		if lifecyclePath != "" {
			return runFlowWithLifecycle(segments, cfg, lifecyclePath, *dryRun, *verbose, *logLLM, *restart)
		}
	}

	return executeFlow(segments, cfg, *dryRun, *verbose, *logLLM, *restart)
}

// executeFlow runs all pipeline segments sequentially.
func executeFlow(segments []*FlowSegment, cfg *core.Config, dryRun, verbose, logLLM, restart bool) int {
	start := time.Now()

	// Load flow checkpoint for resume capability
	startIdx := 0
	if !restart {
		fcp := LoadFlowCheckpoint(segments)
		if fcp != nil && fcp.CompletedSegments > 0 {
			startIdx = fcp.CompletedSegments
			fmt.Printf("%s%s⟳ Resuming flow from pipeline %d/%d (skipping %d completed)%s\n",
				colorBold, colorYellow, startIdx+1, len(segments), startIdx, colorReset)
		}
	} else {
		ClearFlowCheckpoint()
		ClearAllCheckpoints()
	}

	for i := startIdx; i < len(segments); i++ {
		seg := segments[i]
		fmt.Printf("\n%s%s━━━ Pipeline %d/%d: %s ━━━%s\n", colorBold, colorBlue, i+1, len(segments), seg.PipelinePath, colorReset)

		exitCode := executeSinglePipeline(seg, cfg, dryRun, verbose, logLLM, restart)
		if exitCode != 0 {
			// Exit code 3 = graceful stop — treat as successful segment completion but stop the flow
			if exitCode == 3 {
				fmt.Printf("\n%s%s⏹ Graceful stop — flow stopping after pipeline %d/%d: %s%s\n", colorBold, colorYellow, i+1, len(segments), seg.PipelinePath, colorReset)
				SaveFlowCheckpoint(segments, i+1)
				fmt.Printf("\n%s%s━━━ Flow stopped (graceful). %d/%d pipeline(s) completed. Total time: %s ━━━%s\n",
					colorBold, colorYellow, i+1, len(segments), time.Since(start).Round(time.Millisecond), colorReset)
				return 3
			}
			fmt.Printf("\n%s%s✗ Flow failed at pipeline %d/%d: %s%s\n", colorBold, colorRed, i+1, len(segments), seg.PipelinePath, colorReset)
			fmt.Printf("%s%s━━━ Flow failed. Total time: %s ━━━%s\n", colorBold, colorRed, time.Since(start).Round(time.Millisecond), colorReset)
			// Save flow checkpoint so we can resume from this pipeline
			SaveFlowCheckpoint(segments, i)
			return exitCode
		}

		// Pipeline succeeded — save flow progress
		SaveFlowCheckpoint(segments, i+1)
		// Only clear pipeline-level checkpoint after restart flag is consumed by first pipeline
		if i == startIdx {
			restart = false
		}

		// Check for graceful stop signal between segments
		if ShouldGracefulStop() {
			ClearGracefulStop()
			fmt.Printf("\n%s%s⏹ Graceful stop requested between pipelines. Stopping flow after %d/%d.%s\n", colorBold, colorYellow, i+1, len(segments), colorReset)
			fmt.Printf("\n%s%s━━━ Flow stopped (graceful). %d/%d pipeline(s) completed. Total time: %s ━━━%s\n",
				colorBold, colorYellow, i+1, len(segments), time.Since(start).Round(time.Millisecond), colorReset)
			return 3
		}

		// Auto-commit and push between pipelines (not after the last one — lifecycle handles that)
		if !dryRun && i < len(segments)-1 {
			syncBetweenPipelines(seg.PipelinePath, verbose)
		}
	}

	// Flow completed successfully — clear both checkpoints
	ClearFlowCheckpoint()
	fmt.Printf("\n%s%s━━━ Flow complete. %d pipeline(s) succeeded. Total time: %s ━━━%s\n",
		colorBold, colorGreen, len(segments), time.Since(start).Round(time.Millisecond), colorReset)
	return 0
}

// syncBetweenPipelines commits all changes and pushes to the agent bare repo between flow segments.
func syncBetweenPipelines(pipelineName string, verbose bool) {
	fmt.Printf("\n%s%s⟳ Syncing changes after %s...%s\n", colorBold, colorDim, pipelineName, colorReset)

	runner := &OSCommandRunner{}
	cwd, _ := os.Getwd()
	env := os.Environ()

	// Check if there are any changes to commit
	output, exitCode, err := runner.RunCommand("git status --porcelain", cwd, env, false, io.Discard, io.Discard)
	if err != nil || exitCode != 0 || strings.TrimSpace(output) == "" {
		fmt.Printf("  %sNo changes to sync.%s\n", colorDim, colorReset)
		return
	}

	// Stage all changes
	_, exitCode, err = runner.RunCommand("git add -A", cwd, env, verbose, os.Stdout, os.Stderr)
	if err != nil || exitCode != 0 {
		fmt.Fprintf(os.Stderr, "  %s⚠ Failed to stage changes%s\n", colorYellow, colorReset)
		return
	}

	// Commit with pipeline name
	commitMsg := fmt.Sprintf("pipeline: %s", pipelineName)
	_, exitCode, err = runner.RunCommand(fmt.Sprintf("git commit -m %q", commitMsg), cwd, env, verbose, os.Stdout, os.Stderr)
	if err != nil || exitCode != 0 {
		fmt.Fprintf(os.Stderr, "  %s⚠ Failed to commit changes%s\n", colorYellow, colorReset)
		return
	}

	// Push to agent bare repo
	_, exitCode, err = runner.RunCommand("git push agent main", cwd, env, verbose, os.Stdout, os.Stderr)
	if err != nil || exitCode != 0 {
		// Try force push if diverged (we just committed, so it's safe)
		_, exitCode, err = runner.RunCommand("git push agent main --force", cwd, env, verbose, os.Stdout, os.Stderr)
		if err != nil || exitCode != 0 {
			fmt.Fprintf(os.Stderr, "  %s⚠ Failed to push to agent repo (will retry at end of flow)%s\n", colorYellow, colorReset)
			return
		}
	}

	fmt.Printf("  %s✓ Synced to agent repo%s\n", colorGreen, colorReset)
}

// executeSinglePipeline runs one pipeline segment (parse, resolve vars, validate, execute).
func executeSinglePipeline(seg *FlowSegment, cfg *core.Config, dryRun, verbose, logLLM, restart bool) int {
	// Parse pipeline
	pipeline, yamlVars, err := core.ParsePipelineRaw(seg.PipelinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Merge variables
	resolvedVars := core.MergeVariables(yamlVars, seg.Vars)

	// Substitute variables
	if err := core.SubstituteVariables(pipeline, resolvedVars); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Validate
	if err := core.ValidatePipeline(pipeline); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Validate API key if needed
	if !dryRun && pipeline.NeedsLLM() && cfg.OpenRouterAPIKey == "" {
		fmt.Fprintln(os.Stderr, "error: OPENROUTER_API_KEY is required when pipeline contains conditions")
		return 1
	}

	// Build LLM client
	var llmClient llm.ConditionEvaluator
	if pipeline.NeedsLLM() {
		raw := llm.NewLLMClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)
		if logLLM {
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

	// Find log task from lifecycle
	logTask := ""
	lifecyclePath := findLifecycleFile()
	if lifecyclePath != "" {
		logTask = parseLogTask(lifecyclePath)
	}

	// Build executor
	executor := &Executor{
		Config:       cfg,
		Pipeline:     pipeline,
		LLM:          llmClient,
		DryRun:       dryRun,
		Verbose:      verbose,
		Runner:       &OSCommandRunner{},
		ResolvedVars: resolvedVars,
		PipelineName: seg.PipelinePath,
		Restart:      restart,
		LogTask:      logTask,
	}

	return executor.Run()
}

// parseLogTask extracts the log-task field from lifecycle.yaml.
func parseLogTask(lifecyclePath string) string {
	data, err := os.ReadFile(lifecyclePath)
	if err != nil {
		return ""
	}
	// Simple extraction — look for "log-task:" line
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "log-task:") {
			val := strings.TrimPrefix(line, "log-task:")
			val = strings.TrimSpace(val)
			val = strings.Trim(val, "\"'")
			return val
		}
	}
	return ""
}

// runFlowWithLifecycle wraps the flow execution with lifecycle before/after steps.
func runFlowWithLifecycle(segments []*FlowSegment, cfg *core.Config, lifecyclePath string, dryRun, verbose, logLLM, restart bool) int {
	data, err := os.ReadFile(lifecyclePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not read lifecycle.yaml: %v\n", err)
		return executeFlow(segments, cfg, dryRun, verbose, logLLM, restart)
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
	if err := yamlUnmarshal(data, &lc); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not parse lifecycle.yaml: %v\n", err)
		return executeFlow(segments, cfg, dryRun, verbose, logLLM, restart)
	}

	runner := &OSCommandRunner{}
	env := os.Environ()
	cwd, _ := os.Getwd()

	// Execute before steps
	for i := 0; i < len(lc.Before); i++ {
		step := lc.Before[i]
		fmt.Printf("%s  ◇ %s%s\n", colorDim, step.Name, colorReset)
		projectDir := step.ProjectDir
		if projectDir == "" {
			projectDir = cwd
		}
		stepVerbose := verbose || step.Verbose
		var stepOutput string
		var exitCode int
		if step.Agent != "" {
			stepOutput, exitCode, err = runner.RunAgent(step.Agent, step.Prompt, step.Model, projectDir, env, stepVerbose, os.Stdout, os.Stderr)
		} else if step.Command != "" {
			stepOutput, exitCode, err = runner.RunCommand(step.Command, projectDir, env, stepVerbose, os.Stdout, os.Stderr)
		}

		if err != nil || exitCode != 0 {
			if i+1 < len(lc.Before) && lc.Before[i+1].Agent != "" {
				fmt.Printf("  %s⚠ Step failed, running fallback: %q%s\n", colorYellow, lc.Before[i+1].Name, colorReset)
				i++
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
				fmt.Fprintf(os.Stderr, "%s✗ Lifecycle before step %q failed (exit code %d)%s\n", colorRed, step.Name, exitCode, colorReset)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  %s  error: %v%s\n", colorRed, err, colorReset)
				}
				if stepOutput != "" && !stepVerbose {
					lines := strings.Split(strings.TrimRight(stepOutput, "\n"), "\n")
					if len(lines) > 20 {
						fmt.Fprintf(os.Stderr, "  %s  output (last 20 lines):%s\n", colorRed, colorReset)
						lines = lines[len(lines)-20:]
					} else {
						fmt.Fprintf(os.Stderr, "  %s  output:%s\n", colorRed, colorReset)
					}
					for _, line := range lines {
						fmt.Fprintf(os.Stderr, "  %s  | %s%s\n", colorRed, line, colorReset)
					}
				}
				return 1
			}
		} else {
			if i+1 < len(lc.Before) && lc.Before[i+1].Agent != "" {
				i++
			}
		}
	}

	// Execute the flow
	exitCode := executeFlow(segments, cfg, dryRun, verbose, logLLM, restart)

	// Execute after steps
	flowName := segments[0].PipelinePath
	if len(segments) > 1 {
		flowName = fmt.Sprintf("flow(%d pipelines)", len(segments))
	}
	for i := 0; i < len(lc.After); i++ {
		step := lc.After[i]
		prompt := strings.ReplaceAll(step.Prompt, "{{pipeline-name}}", filepath.Base(flowName))
		command := strings.ReplaceAll(step.Command, "{{pipeline-name}}", filepath.Base(flowName))

		fmt.Printf("%s  ◇ %s%s\n", colorDim, step.Name, colorReset)
		projectDir := step.ProjectDir
		if projectDir == "" {
			projectDir = cwd
		}
		stepVerbose := verbose || step.Verbose
		var stepOutput string
		var stepExitCode int
		var stepErr error
		if step.Agent != "" {
			stepOutput, stepExitCode, stepErr = runner.RunAgent(step.Agent, prompt, step.Model, projectDir, env, stepVerbose, os.Stdout, os.Stderr)
		} else if command != "" {
			stepOutput, stepExitCode, stepErr = runner.RunCommand(command, projectDir, env, stepVerbose, os.Stdout, os.Stderr)
		}

		if stepErr != nil || stepExitCode != 0 {
			if i+1 < len(lc.After) && lc.After[i+1].Agent != "" {
				fmt.Printf("  %s⚠ Step failed, running fallback: %q%s\n", colorYellow, lc.After[i+1].Name, colorReset)
				i++
				fallback := lc.After[i]
				fbPrompt := strings.ReplaceAll(fallback.Prompt, "{{pipeline-name}}", filepath.Base(flowName))
				fbDir := fallback.ProjectDir
				if fbDir == "" {
					fbDir = cwd
				}
				fbVerbose := verbose || fallback.Verbose
				runner.RunAgent(fallback.Agent, fbPrompt, fallback.Model, fbDir, env, fbVerbose, os.Stdout, os.Stderr)
			} else {
				fmt.Fprintf(os.Stderr, "  %s⚠ Lifecycle after step %q failed (exit code %d)%s\n", colorYellow, step.Name, stepExitCode, colorReset)
				if stepErr != nil {
					fmt.Fprintf(os.Stderr, "  %s  error: %v%s\n", colorYellow, stepErr, colorReset)
				}
				if stepOutput != "" && !stepVerbose {
					// Show last 20 lines of output to help diagnose the failure
					lines := strings.Split(strings.TrimRight(stepOutput, "\n"), "\n")
					if len(lines) > 20 {
						fmt.Fprintf(os.Stderr, "  %s  output (last 20 lines):%s\n", colorYellow, colorReset)
						lines = lines[len(lines)-20:]
					} else {
						fmt.Fprintf(os.Stderr, "  %s  output:%s\n", colorYellow, colorReset)
					}
					for _, line := range lines {
						fmt.Fprintf(os.Stderr, "  %s  | %s%s\n", colorYellow, line, colorReset)
					}
				}
			}
		} else {
			if i+1 < len(lc.After) && lc.After[i+1].Agent != "" {
				i++
			}
		}
	}

	// Graceful stop (exit code 3) is a successful outcome — flow stopped cleanly
	if exitCode == 3 {
		return 0
	}

	return exitCode
}
