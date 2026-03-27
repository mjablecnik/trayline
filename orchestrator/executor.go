package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// CommandRunner abstracts subprocess execution for testability.
type CommandRunner interface {
	// RunAgent executes a trayline-agent command.
	RunAgent(agent string, prompt string, projectDir string, env []string, verbose bool, stdout io.Writer, stderr io.Writer) (output string, exitCode int, err error)
	// RunCommand executes a shell command via sh -c.
	RunCommand(command string, projectDir string, env []string, verbose bool, stdout io.Writer, stderr io.Writer) (output string, exitCode int, err error)
}

// OSCommandRunner is the real CommandRunner using os/exec.
type OSCommandRunner struct{}

func (r *OSCommandRunner) RunAgent(agent string, prompt string, projectDir string, env []string, verbose bool, stdout io.Writer, stderr io.Writer) (string, int, error) {
	agentBin := resolveAgentBinary()
	args := []string{agent, "-p", projectDir, prompt}
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
	cmd.Env = env

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
	Config       *Config
	Pipeline     *Pipeline
	LLM          ConditionEvaluator
	DryRun       bool
	Verbose      bool
	Runner       CommandRunner
	ResolvedVars map[string]string // for dry-run display
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

	printTotal := func(label string) {
		fmt.Printf("\n%s%s━━━ %s Total time: %s ━━━%s\n", colorBold, colorGreen, label, time.Since(start).Round(time.Millisecond), colorReset)
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
		output, exitCode, runErr := e.executeStep(step, stepNum, totalSteps)
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "%s✗ error:%s %v\n", colorRed, colorReset, runErr)
			printTotal("Pipeline failed.")
			return 1
		}
		if exitCode != 0 {
			fmt.Fprintf(os.Stderr, "%s✗ error:%s step %q failed with exit code %d\n", colorRed, colorReset, step.Name, exitCode)
			printTotal("Pipeline failed.")
			return exitCode
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
				fmt.Printf("%s⏹ Pipeline stopped by condition on step %q (LLM returned false)%s\n", colorYellow, step.Name, colorReset)
				printTotal("Pipeline complete.")
				return 0
			}
			i = nextIdx
			continue
		}

		i++
	}

	printTotal("Pipeline complete.")
	return 0
}

// flattenTopLevelElements returns all top-level pipeline elements in order.
func (e *Executor) flattenTopLevelElements() []PipelineElement {
	return e.Pipeline.Elements
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
	}
	fmt.Printf("\n%s%s▶ Step %d/%d:%s %s%q%s %s(%s)%s\n", colorBold, colorCyan, stepNum, totalSteps, colorReset, colorBold, step.Name, colorReset, colorDim, stepType, colorReset)
	e.debugLog("Executing step %q (%s)", step.Name, stepType)
	start := time.Now()

	cwd, _ := os.Getwd()
	projectDir := step.ProjectDir
	if projectDir == "" {
		projectDir = cwd
	}

	env := os.Environ()

	var output string
	var exitCode int
	var err error

	if step.Agent != "" {
		output, exitCode, err = e.Runner.RunAgent(step.Agent, step.Prompt, projectDir, env, e.Verbose, os.Stdout, os.Stderr)
	} else {
		output, exitCode, err = e.Runner.RunCommand(step.Command, projectDir, env, e.Verbose, os.Stdout, os.Stderr)
	}

	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		fmt.Printf("  %s✗ %q failed after %s: %v%s\n", colorRed, step.Name, elapsed, err, colorReset)
		e.debugError(fmt.Sprintf("step %q execution", step.Name), err)
		return output, exitCode, err
	}
	if exitCode != 0 {
		fmt.Printf("  %s✗ %q (%s) failed (exit %d) after %s%s\n", colorRed, step.Name, stepType, exitCode, elapsed, colorReset)
		e.debugLog("Step %q failed with exit code %d after %s", step.Name, exitCode, elapsed)
	} else {
		fmt.Printf("  %s✓ %q (%s) succeeded in %s%s\n", colorGreen, step.Name, stepType, elapsed, colorReset)
		e.debugLog("Step %q succeeded in %s (output: %d bytes)", step.Name, elapsed, len(output))
	}
	return output, exitCode, nil
}

// evaluateStepCondition evaluates a step's condition and returns the next element index.
// Returns -1 as nextIdx to signal "stop pipeline".
func (e *Executor) evaluateStepCondition(step *Step, stepOutput string, elements []PipelineElement, currentIdx int) (bool, int, error) {
	e.debugLog("Evaluating condition on step %q (file=%q, goto=%q)", step.Name, step.Condition.File, step.Condition.Goto)

	input, err := e.conditionInput(step.Name, step.Condition, step.ProjectDir, stepOutput)
	if err != nil {
		e.debugError(fmt.Sprintf("conditionInput for step %q", step.Name), err)
		return false, 0, err
	}
	e.debugLog("Condition input resolved (%d bytes), calling LLM...", len(input))

	decision, err := e.LLM.Evaluate(input, step.Condition.Prompt)
	if err != nil {
		e.debugError(fmt.Sprintf("LLM.Evaluate for step %q", step.Name), err)
		return false, 0, err
	}

	gotoTarget := step.Condition.Goto
	fmt.Printf("  %s⚡ Condition on %q: LLM=%v%s", colorMagenta, step.Name, decision, colorReset)
	if gotoTarget != "" {
		fmt.Printf(" %s→ goto %q%s", colorMagenta, gotoTarget, colorReset)
	}
	fmt.Println()

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

// executeLoop runs a loop block with LLM-based iteration control.
// Loop steps may have conditions (without goto). If a step condition evaluates to false,
// the remaining steps in the current iteration are skipped and the loop exits.
func (e *Executor) executeLoop(loop *Loop) (int, error) {
	hasLoopCondition := loop.Condition.Prompt != ""
	e.debugSection(fmt.Sprintf("Loop start — max_iterations=%d, has_loop_condition=%v, steps=%d", loop.MaxIterations, hasLoopCondition, len(loop.Steps)))

	for iter := 1; iter <= loop.MaxIterations; iter++ {
		fmt.Printf("\n%s%s🔁 Loop iteration %d/%d%s\n", colorBold, colorBlue, iter, loop.MaxIterations, colorReset)
		e.debugSection(fmt.Sprintf("Loop iteration %d/%d", iter, loop.MaxIterations))

		var lastOutput string
		breakLoop := false
		for i := range loop.Steps {
			step := &loop.Steps[i]
			output, exitCode, err := e.executeStep(step, i+1, len(loop.Steps))
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
			// true = continue to next step; false = skip remaining steps and exit loop.
			if step.Condition != nil {
				e.debugLog("Evaluating step condition on %q (file=%q)", step.Name, step.Condition.File)
				condDir := step.ProjectDir
				if condDir == "" {
					condDir, _ = os.Getwd()
				}
				input, err := e.conditionInput(step.Name, step.Condition, condDir, output)
				if err != nil {
					e.debugError(fmt.Sprintf("conditionInput for loop step %q", step.Name), err)
					return 1, err
				}
				e.debugLog("Condition input resolved (%d bytes), calling LLM...", len(input))
				decision, err := e.LLM.Evaluate(input, step.Condition.Prompt)
				if err != nil {
					e.debugError(fmt.Sprintf("LLM.Evaluate for loop step %q", step.Name), err)
					return 1, err
				}
				fmt.Printf("  %s⚡ Condition on %q: LLM=%v%s\n", colorMagenta, step.Name, decision, colorReset)
				e.debugLog("Step condition on %q → decision=%v", step.Name, decision)
				if !decision {
					fmt.Printf("  %s⏹ Loop exiting: step %q condition returned false%s\n", colorYellow, step.Name, colorReset)
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
			e.debugLog("Evaluating loop-level condition (file=%q)", loop.Condition.File)
			condProjectDir := ""
			for k := len(loop.Steps) - 1; k >= 0; k-- {
				if loop.Steps[k].ProjectDir != "" {
					condProjectDir = loop.Steps[k].ProjectDir
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
			e.debugLog("Loop condition input resolved (%d bytes), calling LLM...", len(input))

			decision, err := e.LLM.Evaluate(input, loop.Condition.Prompt)
			if err != nil {
				e.debugError("LLM.Evaluate for loop condition", err)
				return 1, err
			}

			fmt.Printf("  %s⚡ Loop iteration %d/%d: LLM=%v%s\n", colorMagenta, iter, loop.MaxIterations, decision, colorReset)
			e.debugLog("Loop condition → decision=%v", decision)

			if !decision {
				fmt.Printf("  %s⏹ Loop exiting after iteration %d (LLM returned false)%s\n", colorYellow, iter, colorReset)
				e.debugLog("Loop ended (loop condition returned false)")
				return 0, nil
			}
		} else {
			e.debugLog("No loop-level condition — continuing to next iteration")
		}

		if iter == loop.MaxIterations {
			fmt.Printf("  %s⚠ WARNING: loop reached max_iterations (%d), continuing pipeline%s\n", colorYellow, loop.MaxIterations, colorReset)
			e.debugLog("Loop reached max_iterations (%d) — exiting", loop.MaxIterations)
			return 0, nil
		}
	}
	return 0, nil
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
	for _, elem := range e.Pipeline.Elements {
		if elem.Step != nil {
			stepNum++
			s := elem.Step
			projectDir := s.ProjectDir
			if projectDir == "" {
				projectDir = cwd
			}
			if s.Agent != "" {
				fmt.Printf("\n%s▶ Step %d:%s %s%q%s agent=%s project_dir=%s\n  prompt: %s\n", colorCyan, stepNum, colorReset, colorBold, s.Name, colorReset, s.Agent, projectDir, s.Prompt)
			} else {
				fmt.Printf("\n%s▶ Step %d:%s %s%q%s command=%s project_dir=%s\n", colorCyan, stepNum, colorReset, colorBold, s.Name, colorReset, s.Command, projectDir)
			}
			printCondition(s.Condition, "  ")
		}
		if elem.Loop != nil {
			lc := &elem.Loop.Condition
			fmt.Printf("\n%s🔁 Loop%s (max_iterations=%d):\n", colorBlue, colorReset, elem.Loop.MaxIterations)
			printCondition(lc, "  ")
			for j := range elem.Loop.Steps {
				stepNum++
				s := &elem.Loop.Steps[j]
				projectDir := s.ProjectDir
				if projectDir == "" {
					projectDir = cwd
				}
				if s.Agent != "" {
					fmt.Printf("  %s▶ Step %d:%s %s%q%s agent=%s project_dir=%s\n    prompt: %s\n", colorCyan, stepNum, colorReset, colorBold, s.Name, colorReset, s.Agent, projectDir, s.Prompt)
				} else {
					fmt.Printf("  %s▶ Step %d:%s %s%q%s command=%s project_dir=%s\n", colorCyan, stepNum, colorReset, colorBold, s.Name, colorReset, s.Command, projectDir)
				}
				printCondition(s.Condition, "    ")
			}
		}
	}
	fmt.Println()
}
