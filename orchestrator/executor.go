package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ANSI color codes for terminal output.
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorBold    = "\033[1m"
	colorDim     = "\033[2m"
)

// getDepth reads the current nesting depth from TRAYLINE_DEPTH env var.
func getDepth() int {
	d, err := strconv.Atoi(os.Getenv("TRAYLINE_DEPTH"))
	if err != nil {
		return 0
	}
	return d
}

// indent returns the indentation string for the current depth.
func indent() string {
	return strings.Repeat("    ", getDepth())
}

// CommandRunner abstracts subprocess execution for testability.
type CommandRunner interface {
	// RunAgent executes a trayline-agent command.
	RunAgent(agent string, prompt string, model string, projectDir string, env []string, verbose bool, stdout io.Writer, stderr io.Writer) (output string, exitCode int, err error)
	// RunCommand executes a shell command via sh -c.
	RunCommand(command string, projectDir string, env []string, verbose bool, stdout io.Writer, stderr io.Writer) (output string, exitCode int, err error)
}

// OSCommandRunner is the real CommandRunner using os/exec.
type OSCommandRunner struct{}

func (r *OSCommandRunner) RunAgent(agent string, prompt string, model string, projectDir string, env []string, verbose bool, stdout io.Writer, stderr io.Writer) (string, int, error) {
	agentBin := resolveAgentBinary()
	args := []string{agent, "-p", projectDir}
	if model != "" {
		args = append(args, "-m", model)
	}
	args = append(args, prompt)
	return runSubprocess(agentBin, args, projectDir, env, verbose, stdout, stderr)
}

// resolveAgentBinary finds trayline-agent next to the current executable first,
// then falls back to PATH lookup.
func resolveAgentBinary() string {
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), "trayline-agent")
		if _, err := os.Stat(sibling); err == nil {
			return sibling
		}
	}
	return "trayline-agent"
}

func (r *OSCommandRunner) RunCommand(command string, projectDir string, env []string, verbose bool, stdout io.Writer, stderr io.Writer) (string, int, error) {
	return runSubprocess("sh", []string{"-c", command}, projectDir, env, verbose, stdout, stderr)
}

func runSubprocess(name string, args []string, dir string, env []string, verbose bool, stdoutW io.Writer, stderrW io.Writer) (string, int, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// Increment TRAYLINE_DEPTH for child processes
	childDepth := strconv.Itoa(getDepth() + 1)
	childEnv := make([]string, 0, len(env)+1)
	depthSet := false
	for _, e := range env {
		if strings.HasPrefix(e, "TRAYLINE_DEPTH=") {
			childEnv = append(childEnv, "TRAYLINE_DEPTH="+childDepth)
			depthSet = true
		} else {
			childEnv = append(childEnv, e)
		}
	}
	if !depthSet {
		childEnv = append(childEnv, "TRAYLINE_DEPTH="+childDepth)
	}
	cmd.Env = childEnv

	var buf bytes.Buffer
	if verbose {
		cmd.Stdout = io.MultiWriter(stdoutW, &buf)
		cmd.Stderr = io.MultiWriter(stderrW, &buf)
	} else {
		cmd.Stdout = &buf
		cmd.Stderr = &buf
	}

	err := cmd.Run()
	output := buf.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, exitErr.ExitCode(), nil
		}
		// Command not found or other OS error
		return output, 1, err
	}
	return output, 0, nil
}

// Executor runs the pipeline.
type Executor struct {
	Config        *Config
	Pipeline      *Pipeline
	LLM           ConditionEvaluator
	DryRun        bool
	Verbose       bool
	Runner        CommandRunner
	ResolvedVars  map[string]string // for dry-run display
	LogTask          string            // pipeline to run after steps with log:true
	PipelineName     string            // name of the pipeline (for checkpoint)
	Restart          bool              // if true, ignore checkpoint and start fresh
	RateLimitOutput  string            // output from the step that hit rate limit (for reset time parsing)
}
// setLLMContext sets context on the LLM logger if logging is enabled.
// debugLog writes a message to the LLM debug log if logging is enabled.
func (e *Executor) debugLog(format string, args ...interface{}) {
	if logger, ok := e.LLM.(*LLMLogger); ok {
		logger.Log(format, args...)
	}
}

// debugSection writes a section header to the LLM debug log if logging is enabled.
func (e *Executor) debugSection(title string) {
	if logger, ok := e.LLM.(*LLMLogger); ok {
		logger.LogSection(title)
	}
}

// debugError writes an error to the LLM debug log if logging is enabled.
func (e *Executor) debugError(context string, err error) {
	if logger, ok := e.LLM.(*LLMLogger); ok {
		logger.LogError(context, err)
	}
}

// Run executes the entire pipeline. Returns the final exit code.
func (e *Executor) Run() int {
	if e.DryRun {
		e.printDryRun()
		return 0
	}

	start := time.Now()
	allSteps := e.flattenTopLevelElements()
	totalSteps := e.countTopLevelSteps()

	// Load checkpoint for resume capability
	var checkpoint *Checkpoint
	if !e.Restart && e.PipelineName != "" {
		checkpoint = LoadCheckpoint(e.PipelineName, e.ResolvedVars)
		if checkpoint != nil {
			if len(checkpoint.CompletedSteps) > 0 {
				fmt.Printf("%s%s%s⟳ Resuming from checkpoint (last completed: %s)%s\n", indent(), colorBold, colorYellow, checkpoint.CompletedSteps[len(checkpoint.CompletedSteps)-1], colorReset)
			} else {
				fmt.Printf("%s%s%s⟳ Resuming from checkpoint (no steps completed yet)%s\n", indent(), colorBold, colorYellow, colorReset)
			}
		}
	}

	// If no direct checkpoint exists, check for sub-pipeline checkpoints.
	// If a command step invokes a pipeline that has an active checkpoint,
	// skip all steps before it so we resume from the right place.
	var skipUntilStep string
	if checkpoint == nil && !e.Restart {
		skipUntilStep = e.findResumeStepFromSubCheckpoints(allSteps)
		if skipUntilStep != "" {
			fmt.Printf("%s%s%s⟳ Sub-pipeline checkpoint found, skipping to step %q%s\n", indent(), colorBold, colorYellow, skipUntilStep, colorReset)
		}
	}

	var completedSteps []string

	printTotal := func(label string) {
		fmt.Printf("\n%s%s%s━━━ %s Total time: %s ━━━%s\n", indent(), colorBold, colorGreen, label, time.Since(start).Round(time.Millisecond), colorReset)
	}

	i := 0
	stepNum := 0
	for i < len(allSteps) {
		elem := allSteps[i]

		if elem.Loop != nil {
			if exitCode, err := e.executeLoop(elem.Loop); err != nil || exitCode != 0 {
				if err != nil {
					fmt.Printf("\n%s✗ error:%s %v\n", colorRed, colorReset, err)
				}
				printTotal("Pipeline failed.")
				return exitCode
			}
			i++
			continue
		}

		// It's a step
		step := elem.Step
		stepNum++

		// Skip step if skip field resolves to "true"
		if step.Skip == "true" {
			fmt.Printf("\n%s%s⏭ Skipping step %d/%d:%s %q (skip=true)\n", indent(), colorYellow, stepNum, totalSteps, colorReset, step.Name)
			i++
			continue
		}

		// Skip step if already completed (resume from checkpoint)
		if checkpoint != nil && checkpoint.IsStepCompleted(step.Name) {
			fmt.Printf("\n%s%s⏭ Skipping step %d/%d:%s %q (already completed)\n", indent(), colorYellow, stepNum, totalSteps, colorReset, step.Name)
			completedSteps = append(completedSteps, step.Name)
			i++
			continue
		}

		// Skip steps before the one with an active sub-pipeline checkpoint
		if skipUntilStep != "" && step.Name != skipUntilStep {
			fmt.Printf("\n%s%s⏭ Skipping step %d/%d:%s %q (resuming from sub-pipeline checkpoint)\n", indent(), colorYellow, stepNum, totalSteps, colorReset, step.Name)
			completedSteps = append(completedSteps, step.Name)
			i++
			continue
		}
		// Once we reach the target step, clear the skip marker
		if skipUntilStep != "" && step.Name == skipUntilStep {
			skipUntilStep = ""
		}

		output, exitCode, runErr := e.executeStep(step, stepNum, totalSteps)
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "%s✗ error:%s %v\n", colorRed, colorReset, runErr)
			printTotal("Pipeline failed.")
			return 1
		}
		if exitCode != 0 {
			// Check if this is a rate limit error
			if IsRateLimitError(output) {
				fmt.Printf("\n%s%s⏸ Rate limit detected on step %q. Saving checkpoint for resume.%s\n", indent(), colorYellow, step.Name, colorReset)
				SaveCheckpoint(e.PipelineName, e.ResolvedVars, completedSteps, step.Name, true)
				e.RateLimitOutput = output
				printTotal("Pipeline paused (rate limit).")
				return 2 // Special exit code for rate limit
			}
			fmt.Fprintf(os.Stderr, "%s✗ error:%s step %q failed with exit code %d\n", colorRed, colorReset, step.Name, exitCode)
			// Save checkpoint so we can resume from this step
			if e.PipelineName != "" {
				SaveCheckpoint(e.PipelineName, e.ResolvedVars, completedSteps, step.Name, false)
			}
			printTotal("Pipeline failed.")
			return exitCode
		}

		// Step succeeded — record completion and save checkpoint
		completedSteps = append(completedSteps, step.Name)
		if e.PipelineName != "" {
			SaveCheckpoint(e.PipelineName, e.ResolvedVars, completedSteps, "", false)
		}

		// Run log-task after successful step if log:true is set
		if step.Log && e.LogTask != "" {
			e.runLogTask(step.Name)
		}

		// Evaluate step condition if present
		if step.Condition != nil {
			_, nextIdx, err := e.evaluateStepCondition(step, output, allSteps, i)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s✗ error:%s step %q: %v\n", colorRed, colorReset, step.Name, err)
				printTotal("Pipeline failed.")
				return 1
			}
			if nextIdx == -1 {
				// Stop pipeline (no-goto + false)
				fmt.Printf("%s%s⏹ Pipeline stopped by condition on step %q (LLM returned false)%s\n", indent(), colorYellow, step.Name, colorReset)
				printTotal("Pipeline complete.")
				return 0
			}
			i = nextIdx
			continue
		}

		i++
	}

	// Pipeline completed successfully — clear checkpoint
	ClearCheckpoint(e.PipelineName)
	printTotal("Pipeline complete.")
	return 0
}

// flattenTopLevelElements returns all top-level pipeline elements in order.
func (e *Executor) flattenTopLevelElements() []PipelineElement {
	return e.Pipeline.Elements
}

// findResumeStepFromSubCheckpoints checks if any command step in this pipeline
// invokes a sub-pipeline that has an active checkpoint. If found, returns the
// step name so the executor can skip all steps before it.
func (e *Executor) findResumeStepFromSubCheckpoints(elements []PipelineElement) string {
	checkpoints := LoadAllCheckpoints()
	if len(checkpoints) == 0 {
		return ""
	}

	// Build a set of checkpointed pipeline names (last two path components for matching)
	checkpointedPipelines := make(map[string]bool)
	for _, cp := range checkpoints {
		// Extract identifier: e.g. "processes/3-ui-refactor" from full path
		checkpointedPipelines[cp.Pipeline] = true
		// Also store the short form for matching against command strings
		name := cp.Pipeline
		name = strings.TrimSuffix(name, ".yaml")
		name = strings.TrimSuffix(name, ".yml")
		parts := strings.Split(filepath.ToSlash(name), "/")
		if len(parts) >= 2 {
			short := parts[len(parts)-2] + "/" + parts[len(parts)-1]
			checkpointedPipelines[short] = true
		}
	}

	// Check each command step to see if it invokes a checkpointed pipeline
	for _, elem := range elements {
		if elem.Step == nil || elem.Step.Command == "" {
			continue
		}
		// Parse pipeline name from command like "trayline run processes/3-ui-refactor ..."
		pipelineName := extractPipelineFromCommand(elem.Step.Command)
		if pipelineName == "" {
			continue
		}
		if checkpointedPipelines[pipelineName] {
			return elem.Step.Name
		}
	}

	return ""
}

// extractPipelineFromCommand parses a pipeline reference from a trayline run command string.
// e.g., "trayline run processes/3-ui-refactor --var path=x --no-lifecycle" -> "processes/3-ui-refactor"
func extractPipelineFromCommand(command string) string {
	parts := strings.Fields(command)
	// Look for "trayline run <pipeline>" or just "run <pipeline>" pattern
	for i, part := range parts {
		if part == "run" && i+1 < len(parts) {
			next := parts[i+1]
			// Skip if it's a flag
			if strings.HasPrefix(next, "-") {
				continue
			}
			return next
		}
	}
	return ""
}

// countTopLevelSteps counts only top-level step elements (not loops, not loop steps).
func (e *Executor) countTopLevelSteps() int {
	count := 0
	for _, elem := range e.Pipeline.Elements {
		if elem.Step != nil {
			count++
		}
	}
	return count
}

// executeStep runs a single step subprocess.
func (e *Executor) executeStep(step *Step, stepNum int, totalSteps int) (string, int, error) {
	stepType := "command"
	if step.Agent != "" {
		stepType = "agent:" + step.Agent
		model := step.Model
		if model == "" {
			model = e.Config.OpenRouterModel
		}
		stepType += ", model:" + model
	}
	start := time.Now()
	ind := indent()
	fmt.Printf("\n%s%s%s▶ Step %d/%d:%s %s%q%s %s(%s)%s %s[started %s]%s\n", ind, colorBold, colorCyan, stepNum, totalSteps, colorReset, colorBold, step.Name, colorReset, colorDim, stepType, colorReset, colorDim, start.Format("15:04:05"), colorReset)
	e.debugLog("Executing step %q (%s)", step.Name, stepType)

	cwd, _ := os.Getwd()
	projectDir := step.ProjectDir
	if projectDir == "" {
		projectDir = cwd
	}

	env := os.Environ()

	verbose := e.Verbose || step.Verbose

	var output string
	var exitCode int
	var err error

	if step.Agent != "" {
		output, exitCode, err = e.Runner.RunAgent(step.Agent, step.Prompt, step.Model, projectDir, env, verbose, os.Stdout, os.Stderr)
	} else {
		output, exitCode, err = e.Runner.RunCommand(step.Command, projectDir, env, verbose, os.Stdout, os.Stderr)
	}

	end := time.Now()
	elapsed := end.Sub(start).Round(time.Millisecond)
	if err != nil {
		fmt.Printf("%s  %s✗ %q failed after %s %s[finished %s]%s: %v%s\n", ind, colorRed, step.Name, elapsed, colorDim, end.Format("15:04:05"), colorReset, err, colorReset)
		e.debugError(fmt.Sprintf("step %q execution", step.Name), err)
		return output, exitCode, err
	}
	if exitCode != 0 {
		fmt.Printf("%s  %s✗ %q (%s) failed (exit %d) after %s %s[finished %s]%s\n", ind, colorRed, step.Name, stepType, exitCode, elapsed, colorDim, end.Format("15:04:05"), colorReset)
		if output != "" {
			fmt.Printf("%s  %s  output: %s%s\n", ind, colorRed, output, colorReset)
		}
		e.debugLog("Step %q failed with exit code %d after %s", step.Name, exitCode, elapsed)
		e.debugLog("Step %q failed output (%d bytes):\n%s", step.Name, len(output), output)
	} else {
		fmt.Printf("%s  %s✓ %q (%s) succeeded in %s %s[finished %s]%s\n", ind, colorGreen, step.Name, stepType, elapsed, colorDim, end.Format("15:04:05"), colorReset)
		e.debugLog("Step %q succeeded in %s (output: %d bytes)", step.Name, elapsed, len(output))
		if len(output) > 0 {
			preview := output
			if len(preview) > 1000 {
				preview = preview[:1000] + fmt.Sprintf("\n... [truncated, total %d bytes]", len(output))
			}
			e.debugLog("Step %q output:\n%s", step.Name, preview)
		}
	}
	return output, exitCode, nil
}

// evaluateStepCondition evaluates a step's condition and returns the next element index.
// Returns -1 as nextIdx to signal "stop pipeline".
func (e *Executor) evaluateStepCondition(step *Step, stepOutput string, elements []PipelineElement, currentIdx int) (bool, int, error) {
	e.debugLog("Evaluating condition on step %q (file=%q, contains=%q, goto=%q)", step.Name, step.Condition.File, step.Condition.Contains, step.Condition.Goto)

	input, err := e.conditionInput(step.Name, step.Condition, step.ProjectDir, stepOutput)
	if err != nil {
		e.debugError(fmt.Sprintf("conditionInput for step %q", step.Name), err)
		return false, 0, err
	}
	e.debugLog("Condition input resolved (%d bytes)", len(input))

	decision, err := e.evaluateCondition(fmt.Sprintf("Condition on %q", step.Name), step.Condition, input)
	if err != nil {
		return false, 0, err
	}

	gotoTarget := step.Condition.Goto
	if gotoTarget != "" {
		if decision {
			idx := e.findElementIndex(gotoTarget, elements)
			if idx == -1 {
				return false, 0, fmt.Errorf("goto target %q not found in top-level elements", gotoTarget)
			}
			e.debugLog("Condition true → jumping to %q (index %d)", gotoTarget, idx)
			return true, idx, nil
		}
		e.debugLog("Condition false → goto not taken, continuing to next step")
		return false, currentIdx + 1, nil
	}

	if decision {
		e.debugLog("Condition true → continuing to next step")
		return true, currentIdx + 1, nil
	}
	e.debugLog("Condition false → stopping pipeline")
	return false, -1, nil
}

// findElementIndex returns the index of the element with the given step name.
func (e *Executor) findElementIndex(stepName string, elements []PipelineElement) int {
	for i, elem := range elements {
		if elem.Step != nil && elem.Step.Name == stepName {
			return i
		}
	}
	return -1
}

// conditionInput resolves the input for a condition (file content or step output).
func (e *Executor) conditionInput(stepName string, cond *Condition, projectDir string, stepOutput string) (string, error) {
	if cond.File == "" {
		return stepOutput, nil
	}

	// Resolve file path relative to project dir (or cwd if projectDir is empty).
	path := cond.File
	if !filepath.IsAbs(path) {
		dir := projectDir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		path = filepath.Join(dir, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("step %q: condition file not found: %s", stepName, path)
		}
		return "", fmt.Errorf("step %q: reading condition file: %w", stepName, err)
	}
	return string(data), nil
}
// evaluateCondition evaluates a condition using either string match (contains) or LLM (prompt).
// Returns the boolean decision.
func (e *Executor) evaluateCondition(context string, cond *Condition, input string) (bool, error) {
	if cond.Contains != "" {
		decision := strings.Contains(input, cond.Contains)
		e.debugLog("%s: contains %q → %v", context, cond.Contains, decision)
		fmt.Printf("%s  %s⚡ %s: contains(%q)=%v%s\n", indent(), colorMagenta, context, cond.Contains, decision, colorReset)
		return decision, nil
	}

	if cond.NotContains != "" {
		decision := !strings.Contains(input, cond.NotContains)
		e.debugLog("%s: not_contains %q → %v", context, cond.NotContains, decision)
		fmt.Printf("%s  %s⚡ %s: not_contains(%q)=%v%s\n", indent(), colorMagenta, context, cond.NotContains, decision, colorReset)
		return decision, nil
	}

	decision, err := e.LLM.Evaluate(input, cond.Prompt)
	if err != nil {
		e.debugError(fmt.Sprintf("LLM.Evaluate for %s", context), err)
		return false, err
	}
	e.debugLog("%s: LLM=%v", context, decision)
	return decision, nil
}

// executeLoop runs a loop block with iteration control.
// Loop elements may be steps or nested loops. If a step condition evaluates to false,
// the remaining elements in the current iteration are skipped and the loop exits.
func (e *Executor) executeLoop(loop *Loop) (int, error) {
	hasLoopCondition := loop.Condition.Prompt != "" || loop.Condition.Contains != "" || loop.Condition.NotContains != ""
	e.debugSection(fmt.Sprintf("Loop start — max_iterations=%d, has_loop_condition=%v, elements=%d", loop.MaxIterations, hasLoopCondition, len(loop.Elements)))

	for iter := 1; iter <= loop.MaxIterations; iter++ {
		fmt.Printf("\n%s%s%s🔁 Loop iteration %d/%d%s\n", indent(), colorBold, colorBlue, iter, loop.MaxIterations, colorReset)
		e.debugSection(fmt.Sprintf("Loop iteration %d/%d", iter, loop.MaxIterations))

		var lastOutput string
		breakLoop := false
		stepCount := countStepsInElements(loop.Elements)
		stepNum := 0
		for i := range loop.Elements {
			elem := &loop.Elements[i]

			if elem.Loop != nil {
				exitCode, err := e.executeLoop(elem.Loop)
				if err != nil || exitCode != 0 {
					return exitCode, err
				}
				continue
			}

			step := elem.Step
			stepNum++

			// Skip step if skip field resolves to "true"
			if step.Skip == "true" {
				fmt.Printf("%s  %s⏭ Skipping %q (skip=true)%s\n", indent(), colorYellow, step.Name, colorReset)
				continue
			}

			output, exitCode, err := e.executeStep(step, stepNum, stepCount)
			if err != nil {
				e.debugError(fmt.Sprintf("loop step %q", step.Name), err)
				return 1, fmt.Errorf("loop step %q: %v", step.Name, err)
			}
			if exitCode != 0 {
				fmt.Fprintf(os.Stderr, "  %s✗ error:%s step %q failed with exit code %d\n", colorRed, colorReset, step.Name, exitCode)
				e.debugLog("Step %q failed with exit code %d — aborting loop", step.Name, exitCode)
				return exitCode, fmt.Errorf("step %q failed with exit code %d", step.Name, exitCode)
			}
			lastOutput = output

			// Evaluate step condition if present (goto is not allowed inside loops).
			// true = continue to next element; false = skip remaining elements and exit loop.
			if step.Condition != nil {
				e.debugLog("Evaluating step condition on %q (file=%q, contains=%q)", step.Name, step.Condition.File, step.Condition.Contains)
				condDir := step.ProjectDir
				if condDir == "" {
					condDir, _ = os.Getwd()
				}
				input, err := e.conditionInput(step.Name, step.Condition, condDir, output)
				if err != nil {
					e.debugError(fmt.Sprintf("conditionInput for loop step %q", step.Name), err)
					return 1, err
				}
				e.debugLog("Condition input resolved (%d bytes)", len(input))
				decision, err := e.evaluateCondition(fmt.Sprintf("Condition on %q", step.Name), step.Condition, input)
				if err != nil {
					return 1, err
				}
				if !decision {
					fmt.Printf("%s  %s⏹ Loop exiting: step %q condition returned false%s\n", indent(), colorYellow, step.Name, colorReset)
					e.debugLog("Loop exiting early — step %q condition returned false", step.Name)
					breakLoop = true
					break
				}
			}
		}

		if breakLoop {
			e.debugLog("Loop ended (step condition break)")
			return 0, nil
		}

		// Evaluate loop-level condition if present.
		if hasLoopCondition {
			e.debugLog("Evaluating loop-level condition (file=%q, contains=%q)", loop.Condition.File, loop.Condition.Contains)
			condProjectDir := ""
			for k := len(loop.Elements) - 1; k >= 0; k-- {
				if loop.Elements[k].Step != nil && loop.Elements[k].Step.ProjectDir != "" {
					condProjectDir = loop.Elements[k].Step.ProjectDir
					break
				}
			}
			if condProjectDir == "" {
				condProjectDir, _ = os.Getwd()
			}
			input, err := e.conditionInput("loop", &loop.Condition, condProjectDir, lastOutput)
			if err != nil {
				e.debugError("conditionInput for loop condition", err)
				return 1, err
			}
			e.debugLog("Loop condition input resolved (%d bytes)", len(input))

			decision, err := e.evaluateCondition(fmt.Sprintf("Loop iteration %d/%d", iter, loop.MaxIterations), &loop.Condition, input)
			if err != nil {
				return 1, err
			}

			if !decision {
				fmt.Printf("%s  %s⏹ Loop exiting after iteration %d%s\n", indent(), colorYellow, iter, colorReset)
				e.debugLog("Loop ended (condition returned false)")
				return 0, nil
			}
		} else {
			e.debugLog("No loop-level condition — continuing to next iteration")
		}

		if iter == loop.MaxIterations {
			fmt.Printf("%s  %s⚠ WARNING: loop reached max_iterations (%d), continuing pipeline%s\n", indent(), colorYellow, loop.MaxIterations, colorReset)
			e.debugLog("Loop reached max_iterations (%d) — exiting", loop.MaxIterations)
			return 0, nil
		}
	}
	return 0, nil
}

// countStepsInElements counts only direct Step elements (not nested loops).
func countStepsInElements(elements []PipelineElement) int {
	count := 0
	for _, elem := range elements {
		if elem.Step != nil {
			count++
		}
	}
	return count
}

// printDryRun prints all steps without executing them.
// printCondition prints condition details for dry-run output.
func printCondition(cond *Condition, indent string) {
	if cond == nil {
		return
	}
	line := indent + colorMagenta + "condition: \"" + cond.Prompt + "\""
	if cond.File != "" {
		line += " [file: " + cond.File + "]"
	}
	if cond.Goto != "" {
		line += " [goto: " + cond.Goto + "]"
	}
	line += colorReset
	fmt.Println(line)
}

func (e *Executor) printDryRun() {
	if len(e.ResolvedVars) > 0 {
		fmt.Printf("%s%sVariables:%s\n", colorBold, colorYellow, colorReset)
		// Sort keys for deterministic output
		keys := make([]string, 0, len(e.ResolvedVars))
		for k := range e.ResolvedVars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %s%s%s = %s\n", colorYellow, k, colorReset, e.ResolvedVars[k])
		}
	}
	cwd, _ := os.Getwd()
	stepNum := 0
	printElements(e.Pipeline.Elements, cwd, &stepNum, "")
	fmt.Println()
}

// printElements recursively prints pipeline elements for dry-run output.
func printElements(elements []PipelineElement, cwd string, stepNum *int, indent string) {
	for _, elem := range elements {
		if elem.Step != nil {
			*stepNum++
			s := elem.Step
			projectDir := s.ProjectDir
			if projectDir == "" {
				projectDir = cwd
			}
			verboseTag := ""
			if s.Verbose {
				verboseTag = " verbose=true"
			}
			if s.Agent != "" {
				fmt.Printf("\n%s%s▶ Step %d:%s %s%q%s agent=%s project_dir=%s%s\n%s  prompt: %s\n", indent, colorCyan, *stepNum, colorReset, colorBold, s.Name, colorReset, s.Agent, projectDir, verboseTag, indent, s.Prompt)
			} else {
				fmt.Printf("\n%s%s▶ Step %d:%s %s%q%s command=%s project_dir=%s%s\n", indent, colorCyan, *stepNum, colorReset, colorBold, s.Name, colorReset, s.Command, projectDir, verboseTag)
			}
			printCondition(s.Condition, indent+"  ")
		}
		if elem.Loop != nil {
			lc := &elem.Loop.Condition
			fmt.Printf("\n%s%s🔁 Loop%s (max_iterations=%d):\n", indent, colorBlue, colorReset, elem.Loop.MaxIterations)
			printCondition(lc, indent+"  ")
			printElements(elem.Loop.Elements, cwd, stepNum, indent+"  ")
		}
	}
}

// runLogTask executes the configured log task after a step with log:true.
func (e *Executor) runLogTask(stepName string) {
	fmt.Printf("%s  %s📝 Running log task for %q%s\n", indent(), colorDim, stepName, colorReset)

	// Resolve the log task pipeline path
	home := os.Getenv("TRAYLINE_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".trayline")
	}
	logTaskPath := filepath.Join(home, "pipelines", e.LogTask+".yaml")

	// Call trayline-run directly (bypass wrapper to avoid argument parsing issues)
	traylineRun := filepath.Join(home, "trayline-run")
	if _, err := os.Stat(traylineRun); err != nil {
		// Fallback: try next to current executable
		if exe, err2 := os.Executable(); err2 == nil {
			traylineRun = filepath.Join(filepath.Dir(exe), "trayline-run")
		}
	}

	args := []string{logTaskPath, "--var", "pipeline-name=" + stepName, "--no-lifecycle"}
	cwd, _ := os.Getwd()

	cmd := exec.Command(traylineRun, args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "  %s⚠ Log task failed: %v%s\n", colorYellow, err, colorReset)
	}
}
