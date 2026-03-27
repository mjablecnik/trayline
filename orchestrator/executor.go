package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// CommandRunner abstracts subprocess execution for testability.
type CommandRunner interface {
	// RunAgent executes an agent-docker command.
	RunAgent(agent string, prompt string, projectDir string, env []string, verbose bool, stdout io.Writer, stderr io.Writer) (output string, exitCode int, err error)
	// RunCommand executes a shell command via sh -c.
	RunCommand(command string, projectDir string, env []string, verbose bool, stdout io.Writer, stderr io.Writer) (output string, exitCode int, err error)
}

// OSCommandRunner is the real CommandRunner using os/exec.
type OSCommandRunner struct{}

func (r *OSCommandRunner) RunAgent(agent string, prompt string, projectDir string, env []string, verbose bool, stdout io.Writer, stderr io.Writer) (string, int, error) {
	args := []string{agent, "-p", projectDir, prompt}
	return runSubprocess("agent-docker", args, projectDir, env, verbose, stdout, stderr)
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
	Config   *Config
	Pipeline *Pipeline
	LLM      ConditionEvaluator
	DryRun   bool
	Verbose  bool
	Runner   CommandRunner
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

	i := 0
	stepNum := 0
	for i < len(allSteps) {
		elem := allSteps[i]

		if elem.Loop != nil {
			if err := e.executeLoop(elem.Loop); err != nil {
				return 1
			}
			i++
			continue
		}

		// It's a step
		step := elem.Step
		stepNum++
		output, exitCode, runErr := e.executeStep(step, stepNum, totalSteps)
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", runErr)
			return 1
		}
		if exitCode != 0 {
			fmt.Fprintf(os.Stderr, "error: step %q failed with exit code %d\n", step.Name, exitCode)
			return exitCode
		}

		// Evaluate step condition if present
		if step.Condition != nil {
			_, nextIdx, err := e.evaluateStepCondition(step, output, allSteps, i)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: step %q: %v\n", step.Name, err)
				return 1
			}
			if nextIdx == -1 {
				// Stop pipeline (no-goto + false)
				fmt.Printf("[orchestrator] Pipeline stopped by condition on step %q (LLM returned false)\n", step.Name)
				break
			}
			i = nextIdx
			continue
		}

		i++
	}

	fmt.Printf("[orchestrator] Pipeline complete. Total time: %s\n", time.Since(start).Round(time.Millisecond))
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
	fmt.Printf("[orchestrator] Step %d/%d: %q (%s)\n", stepNum, totalSteps, step.Name, stepType)
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
		fmt.Printf("[orchestrator] Step %q failed after %s: %v\n", step.Name, elapsed, err)
		return output, exitCode, err
	}
	if exitCode != 0 {
		fmt.Printf("[orchestrator] Step %q failed (exit %d) after %s\n", step.Name, exitCode, elapsed)
	} else {
		fmt.Printf("[orchestrator] Step %q succeeded in %s\n", step.Name, elapsed)
	}
	return output, exitCode, nil
}

// evaluateStepCondition evaluates a step's condition and returns the next element index.
// Returns -1 as nextIdx to signal "stop pipeline".
func (e *Executor) evaluateStepCondition(step *Step, stepOutput string, elements []PipelineElement, currentIdx int) (bool, int, error) {
	input, err := e.conditionInput(step.Name, step.Condition, step.ProjectDir, stepOutput)
	if err != nil {
		return false, 0, err
	}

	decision, err := e.LLM.Evaluate(input, step.Condition.Prompt)
	if err != nil {
		return false, 0, err
	}

	gotoTarget := step.Condition.Goto
	fmt.Printf("[orchestrator] Condition on step %q: LLM=%v goto=%q\n", step.Name, decision, gotoTarget)

	if gotoTarget != "" {
		if decision {
			// Jump to named step
			idx := e.findElementIndex(gotoTarget, elements)
			if idx == -1 {
				return false, 0, fmt.Errorf("goto target %q not found in top-level elements", gotoTarget)
			}
			return true, idx, nil
		}
		// goto + false: continue to next
		return false, currentIdx + 1, nil
	}

	// No goto
	if decision {
		// true: continue to next step
		return true, currentIdx + 1, nil
	}
	// false: stop pipeline
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

	// Resolve file path relative to project dir
	path := cond.File
	if projectDir != "" && !isAbsPath(path) {
		path = projectDir + "/" + path
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

func isAbsPath(p string) bool {
	return len(p) > 0 && p[0] == '/'
}

// executeLoop runs a loop block with LLM-based iteration control.
func (e *Executor) executeLoop(loop *Loop) error {
	for iter := 1; iter <= loop.MaxIterations; iter++ {
		fmt.Printf("[orchestrator] Loop iteration %d/%d\n", iter, loop.MaxIterations)

		var lastOutput string
		for i := range loop.Steps {
			step := &loop.Steps[i]
			// Use a simple numbering for loop steps
			output, exitCode, err := e.executeStep(step, i+1, len(loop.Steps))
			if err != nil {
				return fmt.Errorf("loop step %q: %v", step.Name, err)
			}
			if exitCode != 0 {
				return fmt.Errorf("step %q failed with exit code %d", step.Name, exitCode)
			}
			lastOutput = output
		}

		// Evaluate loop condition
		input, err := e.conditionInput("loop", &loop.Condition, "", lastOutput)
		if err != nil {
			return err
		}

		decision, err := e.LLM.Evaluate(input, loop.Condition.Prompt)
		if err != nil {
			return err
		}

		fmt.Printf("[orchestrator] Loop iteration %d/%d: LLM=%v\n", iter, loop.MaxIterations, decision)

		if !decision {
			fmt.Printf("[orchestrator] Loop exiting after iteration %d (LLM returned false)\n", iter)
			return nil
		}

		if iter == loop.MaxIterations {
			fmt.Printf("[orchestrator] WARNING: loop reached max_iterations (%d), continuing pipeline\n", loop.MaxIterations)
			return nil
		}
	}
	return nil
}

// printDryRun prints all steps without executing them.
func (e *Executor) printDryRun() {
	stepNum := 0
	for _, elem := range e.Pipeline.Elements {
		if elem.Step != nil {
			stepNum++
			s := elem.Step
			cwd, _ := os.Getwd()
			projectDir := s.ProjectDir
			if projectDir == "" {
				projectDir = cwd
			}
			if s.Agent != "" {
				fmt.Printf("[dry-run] Step %d: %q agent=%s project_dir=%s\n  prompt: %s\n", stepNum, s.Name, s.Agent, projectDir, s.Prompt)
			} else {
				fmt.Printf("[dry-run] Step %d: %q command=%s project_dir=%s\n", stepNum, s.Name, s.Command, projectDir)
			}
		}
		if elem.Loop != nil {
			fmt.Printf("[dry-run] Loop (max_iterations=%d):\n", elem.Loop.MaxIterations)
			for j := range elem.Loop.Steps {
				stepNum++
				s := &elem.Loop.Steps[j]
				cwd, _ := os.Getwd()
				projectDir := s.ProjectDir
				if projectDir == "" {
					projectDir = cwd
				}
				if s.Agent != "" {
					fmt.Printf("[dry-run]   Step %d: %q agent=%s project_dir=%s\n    prompt: %s\n", stepNum, s.Name, s.Agent, projectDir, s.Prompt)
				} else {
					fmt.Printf("[dry-run]   Step %d: %q command=%s project_dir=%s\n", stepNum, s.Name, s.Command, projectDir)
				}
			}
		}
	}
}
