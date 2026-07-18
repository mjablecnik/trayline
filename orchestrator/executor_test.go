package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"pgregory.net/rapid"
)


// mockRunner is a CommandRunner for testing.
type mockRunner struct {
	calls     []runCall
	responses []runResponse
	callIdx   int
}

type runCall struct {
	kind       string // "agent" or "command"
	agent      string
	prompt     string
	command    string
	projectDir string
	verbose    bool
}

type runResponse struct {
	output   string
	exitCode int
	err      error
}

func (m *mockRunner) RunAgent(agent, prompt, model, projectDir string, env []string, verbose bool, stdout, stderr io.Writer) (string, int, error) {
	m.calls = append(m.calls, runCall{kind: "agent", agent: agent, prompt: prompt, projectDir: projectDir, verbose: verbose})
	return m.nextResponse()
}

func (m *mockRunner) RunCommand(command, projectDir string, env []string, verbose bool, stdout, stderr io.Writer) (string, int, error) {
	m.calls = append(m.calls, runCall{kind: "command", command: command, projectDir: projectDir, verbose: verbose})
	return m.nextResponse()
}

func (m *mockRunner) nextResponse() (string, int, error) {
	if m.callIdx >= len(m.responses) {
		return "", 0, nil
	}
	r := m.responses[m.callIdx]
	m.callIdx++
	return r.output, r.exitCode, r.err
}

// mockEvaluator is a ConditionEvaluator for testing.
type mockEvaluator struct {
	decisions []bool
	idx       int
}

func (m *mockEvaluator) Evaluate(content, prompt string) (bool, error) {
	if m.idx >= len(m.decisions) {
		return false, nil
	}
	d := m.decisions[m.idx]
	m.idx++
	return d, nil
}

func buildExecutor(pipeline *Pipeline, runner *mockRunner, evaluator *mockEvaluator) *Executor {
	return &Executor{
		Config:   &Config{},
		Pipeline: pipeline,
		LLM:      evaluator,
		DryRun:   false,
		Verbose:  false,
		Runner:   runner,
	}
}

// --- Unit tests ---

func TestExecutor_SequentialExecution(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{Name: "step1", Command: "echo 1"}},
			{Step: &Step{Name: "step2", Command: "echo 2"}},
			{Step: &Step{Name: "step3", Agent: "claude", Prompt: "do something"}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{output: "1", exitCode: 0},
			{output: "2", exitCode: 0},
			{output: "ok", exitCode: 0},
		},
	}
	e := buildExecutor(pipeline, runner, &mockEvaluator{})
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if len(runner.calls) != 3 {
		t.Errorf("expected 3 calls, got %d", len(runner.calls))
	}
	if runner.calls[0].command != "echo 1" {
		t.Errorf("unexpected first call: %+v", runner.calls[0])
	}
	if runner.calls[2].agent != "claude" {
		t.Errorf("unexpected third call: %+v", runner.calls[2])
	}
}

func TestExecutor_FailureStopsPipeline(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{Name: "step1", Command: "echo 1"}},
			{Step: &Step{Name: "step2", Command: "fail"}},
			{Step: &Step{Name: "step3", Command: "echo 3"}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{output: "1", exitCode: 0},
			{output: "error", exitCode: 1},
			{output: "3", exitCode: 0},
		},
	}
	e := buildExecutor(pipeline, runner, &mockEvaluator{})
	code := e.Run()
	if code == 0 {
		t.Error("expected non-zero exit code")
	}
	if len(runner.calls) != 2 {
		t.Errorf("expected 2 calls (stop at failure), got %d", len(runner.calls))
	}
}

func TestExecutor_StepCondition_GotoTrue(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{
				Name:    "step1",
				Command: "echo 1",
				Condition: &Condition{
					Prompt: "Should goto?",
					Goto:   "step3",
				},
			}},
			{Step: &Step{Name: "step2", Command: "echo 2"}},
			{Step: &Step{Name: "step3", Command: "echo 3"}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{output: "1", exitCode: 0},
			{output: "3", exitCode: 0},
		},
	}
	eval := &mockEvaluator{decisions: []bool{true}} // goto fires
	e := buildExecutor(pipeline, runner, eval)
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if len(runner.calls) != 2 {
		t.Errorf("expected 2 calls (step1 + step3, skipping step2), got %d", len(runner.calls))
	}
	if runner.calls[1].command != "echo 3" {
		t.Errorf("expected step3 to run, got: %+v", runner.calls[1])
	}
}

func TestExecutor_StepCondition_GotoFalse(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{
				Name:    "step1",
				Command: "echo 1",
				Condition: &Condition{
					Prompt: "Should goto?",
					Goto:   "step3",
				},
			}},
			{Step: &Step{Name: "step2", Command: "echo 2"}},
			{Step: &Step{Name: "step3", Command: "echo 3"}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{exitCode: 0},
			{exitCode: 0},
			{exitCode: 0},
		},
	}
	eval := &mockEvaluator{decisions: []bool{false}} // goto doesn't fire
	e := buildExecutor(pipeline, runner, eval)
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if len(runner.calls) != 3 {
		t.Errorf("expected 3 calls (all steps run), got %d", len(runner.calls))
	}
}

func TestExecutor_StepCondition_NoGotoTrue(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{
				Name:    "step1",
				Command: "echo 1",
				Condition: &Condition{
					Prompt: "Continue?",
				},
			}},
			{Step: &Step{Name: "step2", Command: "echo 2"}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{exitCode: 0},
			{exitCode: 0},
		},
	}
	eval := &mockEvaluator{decisions: []bool{true}}
	e := buildExecutor(pipeline, runner, eval)
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if len(runner.calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(runner.calls))
	}
}

func TestExecutor_StepCondition_NoGotoFalse(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{
				Name:    "step1",
				Command: "echo 1",
				Condition: &Condition{
					Prompt: "Continue?",
				},
			}},
			{Step: &Step{Name: "step2", Command: "echo 2"}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{exitCode: 0},
		},
	}
	eval := &mockEvaluator{decisions: []bool{false}}
	e := buildExecutor(pipeline, runner, eval)
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if len(runner.calls) != 1 {
		t.Errorf("expected 1 call (stopped by condition), got %d", len(runner.calls))
	}
}

func TestExecutor_Loop(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Loop: &Loop{
				MaxIterations: 3,
				Elements: []PipelineElement{
					{Step: &Step{Name: "run-tests", Command: "go test ./..."}},
				},
				Condition: Condition{Prompt: "Still failing?"},
			}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{exitCode: 0}, // iter 1
			{exitCode: 0}, // iter 2
			{exitCode: 0}, // iter 3 (won't be reached)
		},
	}
	// true on iter 1, false on iter 2 → loop runs 2 iterations
	eval := &mockEvaluator{decisions: []bool{true, false}}
	e := buildExecutor(pipeline, runner, eval)
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if len(runner.calls) != 2 {
		t.Errorf("expected 2 loop iterations, got %d", len(runner.calls))
	}
}

func TestExecutor_Loop_MaxIterations(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Loop: &Loop{
				MaxIterations: 2,
				Elements: []PipelineElement{
					{Step: &Step{Name: "s1", Command: "echo hi"}},
				},
				Condition: Condition{Prompt: "Continue?"},
			}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{exitCode: 0},
			{exitCode: 0},
		},
	}
	// Always true — should hit max_iterations and continue
	eval := &mockEvaluator{decisions: []bool{true, true, true}}
	e := buildExecutor(pipeline, runner, eval)
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if len(runner.calls) != 2 {
		t.Errorf("expected exactly 2 iterations (max), got %d", len(runner.calls))
	}
}

func TestExecutor_DryRun(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{Name: "step1", Agent: "claude", Prompt: "do stuff", ProjectDir: "/tmp"}},
			{Step: &Step{Name: "step2", Command: "echo hi"}},
		},
	}
	runner := &mockRunner{}
	e := &Executor{
		Config:   &Config{},
		Pipeline: pipeline,
		LLM:      &mockEvaluator{},
		DryRun:   true,
		Verbose:  false,
		Runner:   runner,
	}
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0 for dry-run, got %d", code)
	}
	if len(runner.calls) != 0 {
		t.Errorf("expected no subprocess calls in dry-run, got %d", len(runner.calls))
	}
}

func TestExecutor_PerStepVerbose(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{Name: "quiet-step", Command: "echo quiet"}},
			{Step: &Step{Name: "verbose-step", Command: "echo loud", Verbose: true}},
			{Step: &Step{Name: "quiet-step2", Command: "echo quiet2"}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{exitCode: 0},
			{exitCode: 0},
			{exitCode: 0},
		},
	}
	e := buildExecutor(pipeline, runner, &mockEvaluator{})
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(runner.calls))
	}
	if runner.calls[0].verbose {
		t.Error("expected quiet-step to not be verbose")
	}
	if !runner.calls[1].verbose {
		t.Error("expected verbose-step to be verbose")
	}
	if runner.calls[2].verbose {
		t.Error("expected quiet-step2 to not be verbose")
	}
}

func TestExecutor_GlobalVerboseOverridesStep(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{Name: "step1", Command: "echo 1"}},
			{Step: &Step{Name: "step2", Command: "echo 2", Verbose: true}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{exitCode: 0},
			{exitCode: 0},
		},
	}
	e := &Executor{
		Config:   &Config{},
		Pipeline: pipeline,
		LLM:      &mockEvaluator{},
		Verbose:  true, // global verbose
		Runner:   runner,
	}
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	// Both should be verbose when global flag is set
	if !runner.calls[0].verbose {
		t.Error("expected step1 to be verbose (global flag)")
	}
	if !runner.calls[1].verbose {
		t.Error("expected step2 to be verbose (global + step flag)")
	}
}

// Property 3: Command construction correctness
func TestCommandConstruction(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		isCommand := rapid.Bool().Draw(rt, "isCommand")
		projectDir := rapid.StringMatching(`^(/[a-z]+)+$`).Draw(rt, "projectDir")

		var step Step
		if isCommand {
			step = Step{
				Name:       "test-step",
				Command:    "echo hello",
				ProjectDir: projectDir,
			}
		} else {
			agent := rapid.SampledFrom([]string{"claude", "kiro"}).Draw(rt, "agent")
			step = Step{
				Name:       "test-step",
				Agent:      agent,
				Prompt:     "do something",
				ProjectDir: projectDir,
			}
		}

		// Capture what would be called
		captured := &mockRunner{}
		e := &Executor{
			Config:   &Config{},
			Pipeline: &Pipeline{Elements: []PipelineElement{{Step: &step}}},
			LLM:      &mockEvaluator{},
			Runner:   captured,
		}
		e.Run()

		if len(captured.calls) != 1 {
			rt.Fatalf("expected 1 call, got %d", len(captured.calls))
		}
		call := captured.calls[0]

		if isCommand {
			if call.kind != "command" {
				rt.Fatalf("expected command call, got %q", call.kind)
			}
			if call.command != step.Command {
				rt.Fatalf("expected command %q, got %q", step.Command, call.command)
			}
			if call.projectDir != projectDir {
				rt.Fatalf("expected projectDir %q, got %q", projectDir, call.projectDir)
			}
		} else {
			if call.kind != "agent" {
				rt.Fatalf("expected agent call, got %q", call.kind)
			}
			if call.agent != step.Agent {
				rt.Fatalf("expected agent %q, got %q", step.Agent, call.agent)
			}
			if call.prompt != step.Prompt {
				rt.Fatalf("expected prompt %q, got %q", step.Prompt, call.prompt)
			}
			if call.projectDir != projectDir {
				rt.Fatalf("expected projectDir %q, got %q", projectDir, call.projectDir)
			}
		}
	})
}

// Property 4: Sequential execution order
func TestSequentialExecution(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numSteps := rapid.IntRange(1, 6).Draw(rt, "numSteps")
		elements := make([]PipelineElement, numSteps)
		expectedOrder := make([]string, numSteps)

		for i := 0; i < numSteps; i++ {
			name := fmt.Sprintf("step-%d", i)
			elements[i] = PipelineElement{Step: &Step{Name: name, Command: fmt.Sprintf("echo %d", i)}}
			expectedOrder[i] = fmt.Sprintf("echo %d", i)
		}

		responses := make([]runResponse, numSteps)
		for i := range responses {
			responses[i] = runResponse{exitCode: 0}
		}

		runner := &mockRunner{responses: responses}
		e := buildExecutor(&Pipeline{Elements: elements}, runner, &mockEvaluator{})
		e.Run()

		if len(runner.calls) != numSteps {
			rt.Fatalf("expected %d calls, got %d", numSteps, len(runner.calls))
		}
		for i, call := range runner.calls {
			if call.command != expectedOrder[i] {
				rt.Fatalf("call %d: expected %q, got %q", i, expectedOrder[i], call.command)
			}
		}
	})
}

// Property 5: Failure stops pipeline
func TestFailureStopsPipeline(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numSteps := rapid.IntRange(2, 6).Draw(rt, "numSteps")
		failAt := rapid.IntRange(0, numSteps-1).Draw(rt, "failAt")

		elements := make([]PipelineElement, numSteps)
		responses := make([]runResponse, numSteps)

		for i := 0; i < numSteps; i++ {
			elements[i] = PipelineElement{Step: &Step{Name: fmt.Sprintf("step-%d", i), Command: fmt.Sprintf("echo %d", i)}}
			if i == failAt {
				responses[i] = runResponse{exitCode: 1}
			} else {
				responses[i] = runResponse{exitCode: 0}
			}
		}

		runner := &mockRunner{responses: responses}
		e := buildExecutor(&Pipeline{Elements: elements}, runner, &mockEvaluator{})
		code := e.Run()

		if code == 0 {
			rt.Fatalf("expected non-zero exit code when step %d fails", failAt)
		}
		if len(runner.calls) != failAt+1 {
			rt.Fatalf("expected %d calls (up to failure at %d), got %d", failAt+1, failAt, len(runner.calls))
		}
	})
}

// Property 7: Loop iteration control
func TestLoopIterationControl(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		maxIter := rapid.IntRange(1, 5).Draw(rt, "maxIter")
		// stopAt = number of "continue" (true) decisions before the stop (false) decision.
		// stopAt == maxIter means LLM always returns true (hits max_iterations).
		stopAt := rapid.IntRange(0, maxIter).Draw(rt, "stopAt")

		// Build LLM decisions: stopAt trues, then false (if stopAt < maxIter)
		decisions := make([]bool, maxIter)
		for i := 0; i < maxIter; i++ {
			decisions[i] = i < stopAt
		}

		// Build runner responses: one per expected iteration (up to maxIter)
		responses := make([]runResponse, maxIter)
		for i := range responses {
			responses[i] = runResponse{exitCode: 0}
		}

		pipeline := &Pipeline{
			Elements: []PipelineElement{
				{Loop: &Loop{
					MaxIterations: maxIter,
					Elements:      []PipelineElement{{Step: &Step{Name: "s1", Command: "echo test"}}},
					Condition:     Condition{Prompt: "Continue?"},
				}},
			},
		}

		runner := &mockRunner{responses: responses}
		eval := &mockEvaluator{decisions: decisions}
		e := buildExecutor(pipeline, runner, eval)
		code := e.Run()

		if code != 0 {
			rt.Fatalf("expected exit code 0, got %d", code)
		}

		// Expected iterations: stopAt+1 if stopAt < maxIter, else maxIter (max hit)
		expectedIter := stopAt + 1
		if stopAt >= maxIter {
			expectedIter = maxIter
		}

		if len(runner.calls) != expectedIter {
			rt.Fatalf("expected %d iterations, got %d (maxIter=%d, stopAt=%d)",
				expectedIter, len(runner.calls), maxIter, stopAt)
		}
	})
}

// Property 8: Step condition routing
func TestStepConditionRouting(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		hasGoto := rapid.Bool().Draw(rt, "hasGoto")
		llmDecision := rapid.Bool().Draw(rt, "llmDecision")

		// Pipeline: step1 (with condition optionally targeting step3), step2, step3
		var cond Condition
		if hasGoto {
			cond = Condition{Prompt: "Route?", Goto: "step3"}
		} else {
			cond = Condition{Prompt: "Continue?"}
		}

		pipeline := &Pipeline{
			Elements: []PipelineElement{
				{Step: &Step{Name: "step1", Command: "echo 1", Condition: &cond}},
				{Step: &Step{Name: "step2", Command: "echo 2"}},
				{Step: &Step{Name: "step3", Command: "echo 3"}},
			},
		}

		runner := &mockRunner{
			responses: []runResponse{
				{exitCode: 0},
				{exitCode: 0},
				{exitCode: 0},
			},
		}
		eval := &mockEvaluator{decisions: []bool{llmDecision}}
		e := buildExecutor(pipeline, runner, eval)
		code := e.Run()

		if hasGoto && llmDecision {
			// (a) goto fires: step1 → step3 (skip step2)
			if code != 0 {
				rt.Fatalf("expected exit code 0, got %d", code)
			}
			if len(runner.calls) != 2 {
				rt.Fatalf("(goto+true) expected 2 calls (step1+step3), got %d", len(runner.calls))
			}
			if runner.calls[1].command != "echo 3" {
				rt.Fatalf("(goto+true) expected step3 as second call, got %+v", runner.calls[1])
			}
		} else if hasGoto && !llmDecision {
			// (b) goto doesn't fire: step1 → step2 → step3
			if code != 0 {
				rt.Fatalf("expected exit code 0, got %d", code)
			}
			if len(runner.calls) != 3 {
				rt.Fatalf("(goto+false) expected 3 calls (all steps), got %d", len(runner.calls))
			}
		} else if !hasGoto && llmDecision {
			// (c) no goto, true: continue to step2, then step3
			if code != 0 {
				rt.Fatalf("expected exit code 0, got %d", code)
			}
			if len(runner.calls) != 3 {
				rt.Fatalf("(no-goto+true) expected 3 calls, got %d", len(runner.calls))
			}
		} else {
			// (d) no goto, false: stop pipeline after step1
			if code != 0 {
				rt.Fatalf("expected exit code 0, got %d", code)
			}
			if len(runner.calls) != 1 {
				rt.Fatalf("(no-goto+false) expected 1 call (stopped after step1), got %d", len(runner.calls))
			}
		}
	})
}

// Property 6: Condition input selection
func TestConditionInputSelection(t *testing.T) {
	t.Run("uses step output when no file", func(t *testing.T) {
		stepOutput := "step output content"
		cond := Condition{Prompt: "Done?"}
		e := &Executor{}
		input, err := e.conditionInput("test-step", &cond, "", stepOutput)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if input != stepOutput {
			t.Fatalf("expected step output, got: %q", input)
		}
	})

	t.Run("uses file content when file specified", func(t *testing.T) {
		fileContent := "file content"
		tmpFile, _ := os.CreateTemp("", "cond-input-*.txt")
		tmpFile.WriteString(fileContent)
		tmpFile.Close()
		defer os.Remove(tmpFile.Name())

		cond := Condition{Prompt: "Done?", File: tmpFile.Name()}
		e := &Executor{}
		input, err := e.conditionInput("test-step", &cond, "", "step output")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(input, fileContent) {
			t.Fatalf("expected file content, got: %q", input)
		}
	})

	rapid.Check(t, func(rt *rapid.T) {
		useFile := rapid.Bool().Draw(rt, "useFile")
		stepOutput := "step output"

		var cond Condition
		var expectedContent string

		if useFile {
			tmpFile, err := os.CreateTemp("", "cond-input-*.txt")
			if err != nil {
				rt.Skip()
			}
			expectedContent = "file-data"
			tmpFile.WriteString(expectedContent)
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())
			cond = Condition{Prompt: "Done?", File: tmpFile.Name()}
		} else {
			expectedContent = stepOutput
			cond = Condition{Prompt: "Done?"}
		}

		e := &Executor{}
		input, err := e.conditionInput("test-step", &cond, "", stepOutput)
		if err != nil {
			rt.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(input, expectedContent) {
			rt.Fatalf("expected %q in input, got: %q", expectedContent, input)
		}
	})
}

// Test for condition file not found at runtime — returns empty string (not an error)
// so that contains/not_contains conditions can evaluate gracefully.
func TestConditionFileNotFound(t *testing.T) {
	cond := Condition{Contains: "- [ ]", File: "/nonexistent/path/missing-file.txt"}
	e := &Executor{}
	input, err := e.conditionInput("test-step", &cond, "", "step output")
	if err != nil {
		t.Fatalf("expected no error for missing condition file, got: %v", err)
	}
	if input != "" {
		t.Errorf("expected empty string for missing file, got: %q", input)
	}
}

// Test that rate limit inside a loop propagates exit code 2 (pause, not fail).
func TestExecutor_LoopRateLimitPause(t *testing.T) {
	changeDirForExecutorTest(t)

	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Loop: &Loop{
				MaxIterations: 5,
				Elements: []PipelineElement{
					{Step: &Step{Name: "create-code", Agent: "claude", Prompt: "implement feature"}},
				},
				Condition: Condition{Prompt: "Continue?"},
			}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{output: "ok", exitCode: 0},
			{output: "ok", exitCode: 0},
			{output: "You've hit your session limit - resets 8pm (UTC)", exitCode: 1},
		},
	}
	// Loop condition: iter 1 → true (continue), iter 2 → true (continue)
	// Iter 3 will hit rate limit before condition is evaluated.
	eval := &mockEvaluator{decisions: []bool{true, true}}

	e := buildExecutor(pipeline, runner, eval)
	e.PipelineName = "processes/4-create-code"
	e.ResolvedVars = map[string]string{}
	code := e.Run()

	if code != 2 {
		t.Errorf("expected exit code 2 (rate limit pause), got %d", code)
	}
	if len(runner.calls) != 3 {
		t.Errorf("expected 3 calls (2 success + 1 rate limited), got %d", len(runner.calls))
	}
	if e.RateLimitOutput == "" {
		t.Error("expected RateLimitOutput to be set")
	}
}

// Test for loop step failure (issue #22 / Requirement 9.10).
func TestExecutor_LoopStepFailure(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Loop: &Loop{
				MaxIterations: 3,
				Elements: []PipelineElement{
					{Step: &Step{Name: "run-tests", Command: "go test ./..."}},
				},
				Condition: Condition{Prompt: "Still failing?"},
			}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{exitCode: 2}, // step inside loop fails
		},
	}
	e := buildExecutor(pipeline, runner, &mockEvaluator{})
	code := e.Run()
	if code == 0 {
		t.Error("expected non-zero exit code when loop step fails")
	}
	if len(runner.calls) != 1 {
		t.Errorf("expected 1 call (stopped at failure), got %d", len(runner.calls))
	}
}
func TestExecutor_LoopStepCondition_False(t *testing.T) {
	// Step condition false inside loop → skip remaining steps and exit loop
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Loop: &Loop{
				MaxIterations: 3,
				Elements: []PipelineElement{
					{Step: &Step{Name: "review", Agent: "kiro", Prompt: "do review",
						Condition: &Condition{Prompt: "Issues found?"}}},
					{Step: &Step{Name: "fix", Command: "trayline run --pipeline fix"}},
				},
				Condition: Condition{Prompt: "Continue?"},
			}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{output: "no issues", exitCode: 0}, // review step
		},
	}
	// Step condition returns false → loop exits, fix step is skipped
	eval := &mockEvaluator{decisions: []bool{false}}
	e := buildExecutor(pipeline, runner, eval)
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if len(runner.calls) != 1 {
		t.Errorf("expected 1 call (only review, fix skipped), got %d", len(runner.calls))
	}
}

func TestExecutor_LoopStepCondition_True(t *testing.T) {
	// Step condition true inside loop → continue to next step
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Loop: &Loop{
				MaxIterations: 3,
				Elements: []PipelineElement{
					{Step: &Step{Name: "review", Agent: "kiro", Prompt: "do review",
						Condition: &Condition{Prompt: "Issues found?"}}},
					{Step: &Step{Name: "fix", Command: "trayline run --pipeline fix"}},
				},
				Condition: Condition{Prompt: "Continue?"},
			}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{output: "issues found", exitCode: 0}, // review step iter 1
			{output: "fixed", exitCode: 0},         // fix step iter 1
			{output: "no issues", exitCode: 0},     // review step iter 2
		},
	}
	// iter 1: step condition true → continue to fix; loop condition true → iterate
	// iter 2: step condition false → exit loop
	eval := &mockEvaluator{decisions: []bool{true, true, false}}
	e := buildExecutor(pipeline, runner, eval)
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if len(runner.calls) != 3 {
		t.Errorf("expected 3 calls (review+fix iter1, review iter2), got %d", len(runner.calls))
	}
}
func TestExecutor_LoopWithoutLoopCondition(t *testing.T) {
	// Loop with only step conditions, no loop-level condition.
	// Should iterate until step condition returns false or max_iterations.
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Loop: &Loop{
				MaxIterations: 5,
				Elements: []PipelineElement{
					{Step: &Step{Name: "review", Agent: "kiro", Prompt: "do review",
						Condition: &Condition{Prompt: "Issues found?"}}},
					{Step: &Step{Name: "fix", Command: "trayline run --pipeline fix"}},
				},
			}},
		},
	}
	runner := &mockRunner{
		responses: []runResponse{
			{output: "issues", exitCode: 0},  // review iter 1
			{output: "fixed", exitCode: 0},    // fix iter 1
			{output: "clean", exitCode: 0},    // review iter 2
		},
	}
	// iter 1: step condition true → run fix; no loop condition → iterate
	// iter 2: step condition false → exit loop
	eval := &mockEvaluator{decisions: []bool{true, false}}
	e := buildExecutor(pipeline, runner, eval)
	code := e.Run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if len(runner.calls) != 3 {
		t.Errorf("expected 3 calls (review+fix iter1, review iter2), got %d", len(runner.calls))
	}
}

// Test that --verbose mode streams output to stdout (issue #23).
func TestVerboseMode_StreamsOutput(t *testing.T) {
	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{Name: "step1", Command: "echo hello-verbose-test"}},
		},
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe error: %v", err)
	}
	old := os.Stdout
	os.Stdout = w

	e := &Executor{
		Config:   &Config{},
		Pipeline: pipeline,
		LLM:      &mockEvaluator{},
		DryRun:   false,
		Verbose:  true,
		Runner:   &OSCommandRunner{},
	}
	code := e.Run()

	w.Close()
	out, _ := io.ReadAll(r)
	os.Stdout = old

	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(string(out), "hello-verbose-test") {
		t.Errorf("expected 'hello-verbose-test' in verbose stdout, got: %s", out)
	}
}

// --- extractPipelineFromCommand tests ---

func TestExtractPipelineFromCommand(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"trayline run processes/3-ui-refactor", "processes/3-ui-refactor"},
		{"trayline run processes/3-ui-refactor --var path=x --no-lifecycle", "processes/3-ui-refactor"},
		{"trayline run pipelines/deploy.yaml", "pipelines/deploy.yaml"},
		// run followed immediately by a flag → current behavior returns "" (flag stops parsing)
		{"trayline run --restart processes/resume", ""},
		// no "run" keyword
		{"trayline check something", ""},
		{"echo hello", ""},
		{"", ""},
		// "run" with no following arg
		{"trayline run", ""},
	}

	for _, tc := range cases {
		got := extractPipelineFromCommand(tc.command)
		if got != tc.want {
			t.Errorf("extractPipelineFromCommand(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

// --- findResumeStepFromSubCheckpoints tests ---

// changeDirForExecutorTest changes CWD to a temp dir; must not be used with t.Parallel.
func changeDirForExecutorTest(t *testing.T) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

// TestFindResumeStepFromSubCheckpoints_MatchFound verifies that when a command step
// invokes a checkpointed pipeline, its step name is returned.
func TestFindResumeStepFromSubCheckpoints_MatchFound(t *testing.T) {
	changeDirForExecutorTest(t)

	// Save a checkpoint for "processes/3-ui-refactor".
	if err := SaveCheckpoint("processes/3-ui-refactor", map[string]string{}, []string{"step-a"}, "step-b", false); err != nil {
		t.Fatalf("SaveCheckpoint error: %v", err)
	}

	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{Name: "prep", Command: "echo preparing"}},
			{Step: &Step{Name: "run-sub", Command: "trayline run processes/3-ui-refactor --var x=1"}},
			{Step: &Step{Name: "post", Command: "echo done"}},
		},
	}

	e := &Executor{Config: &Config{}, Pipeline: pipeline, LLM: &mockEvaluator{}, Runner: &mockRunner{}}
	got := e.findResumeStepFromSubCheckpoints(pipeline.Elements)
	if got != "run-sub" {
		t.Errorf("expected step 'run-sub', got %q", got)
	}
}

// TestFindResumeStepFromSubCheckpoints_NoCheckpoint verifies that an empty string is
// returned when no sub-pipeline checkpoint exists.
func TestFindResumeStepFromSubCheckpoints_NoCheckpoint(t *testing.T) {
	changeDirForExecutorTest(t)
	// No checkpoints saved in this temp dir.

	pipeline := &Pipeline{
		Elements: []PipelineElement{
			{Step: &Step{Name: "step1", Command: "trayline run processes/some-pipeline"}},
		},
	}

	e := &Executor{Config: &Config{}, Pipeline: pipeline, LLM: &mockEvaluator{}, Runner: &mockRunner{}}
	got := e.findResumeStepFromSubCheckpoints(pipeline.Elements)
	if got != "" {
		t.Errorf("expected empty string when no checkpoints exist, got %q", got)
	}
}

// TestFindResumeStepFromSubCheckpoints_AgentStepSkipped verifies that steps with
// no Command (agent steps) are skipped.
func TestFindResumeStepFromSubCheckpoints_AgentStepSkipped(t *testing.T) {
	changeDirForExecutorTest(t)

	if err := SaveCheckpoint("processes/llm-task", map[string]string{}, nil, "", false); err != nil {
		t.Fatalf("SaveCheckpoint error: %v", err)
	}

	pipeline := &Pipeline{
		Elements: []PipelineElement{
			// Agent step — has no Command, should be skipped.
			{Step: &Step{Name: "llm-agent", Agent: "claude", Prompt: "processes/llm-task"}},
		},
	}

	e := &Executor{Config: &Config{}, Pipeline: pipeline, LLM: &mockEvaluator{}, Runner: &mockRunner{}}
	got := e.findResumeStepFromSubCheckpoints(pipeline.Elements)
	if got != "" {
		t.Errorf("expected empty string for agent step (no Command), got %q", got)
	}
}

