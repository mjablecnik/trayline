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
	kind      string // "agent" or "command"
	agent     string
	prompt    string
	command   string
	projectDir string
}

type runResponse struct {
	output   string
	exitCode int
	err      error
}

func (m *mockRunner) RunAgent(agent, prompt, projectDir string, env []string, verbose bool, stdout, stderr io.Writer) (string, int, error) {
	m.calls = append(m.calls, runCall{kind: "agent", agent: agent, prompt: prompt, projectDir: projectDir})
	return m.nextResponse()
}

func (m *mockRunner) RunCommand(command, projectDir string, env []string, verbose bool, stdout, stderr io.Writer) (string, int, error) {
	m.calls = append(m.calls, runCall{kind: "command", command: command, projectDir: projectDir})
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
				Steps: []Step{
					{Name: "run-tests", Command: "go test ./..."},
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
				Steps: []Step{
					{Name: "s1", Command: "echo hi"},
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

type captureEvaluator struct {
	fn func(content, prompt string) (bool, error)
}

func (c *captureEvaluator) Evaluate(content, prompt string) (bool, error) {
	return c.fn(content, prompt)
}
