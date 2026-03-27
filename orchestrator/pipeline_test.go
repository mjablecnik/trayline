package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"pgregory.net/rapid"
)

// --- Unit tests ---

func TestParsePipeline_AgentStep(t *testing.T) {
	content := `
steps:
  - name: "create-code"
    agent: "claude"
    prompt: "Read the spec and create the code"
    project_dir: "/tmp/project"
`
	p := writeTempPipeline(t, content)
	pipeline, err := ParsePipeline(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipeline.Elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(pipeline.Elements))
	}
	step := pipeline.Elements[0].Step
	if step == nil {
		t.Fatal("expected step, got nil")
	}
	if step.Name != "create-code" {
		t.Errorf("expected name 'create-code', got %q", step.Name)
	}
	if step.Agent != "claude" {
		t.Errorf("expected agent 'claude', got %q", step.Agent)
	}
	if step.Prompt != "Read the spec and create the code" {
		t.Errorf("unexpected prompt: %q", step.Prompt)
	}
	if step.ProjectDir != "/tmp/project" {
		t.Errorf("unexpected project_dir: %q", step.ProjectDir)
	}
}

func TestParsePipeline_CommandStep(t *testing.T) {
	content := `
steps:
  - name: "run-tests"
    command: "go test ./..."
`
	p := writeTempPipeline(t, content)
	pipeline, err := ParsePipeline(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	step := pipeline.Elements[0].Step
	if step == nil || step.Command != "go test ./..." {
		t.Errorf("unexpected command step: %+v", step)
	}
}

func TestParsePipeline_MultiLinePrompt(t *testing.T) {
	content := `
steps:
  - name: "write-code"
    agent: "kiro"
    prompt: |
      Line one
      Line two
      Line three
`
	p := writeTempPipeline(t, content)
	pipeline, err := ParsePipeline(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	prompt := pipeline.Elements[0].Step.Prompt
	if !strings.Contains(prompt, "Line one") || !strings.Contains(prompt, "Line three") {
		t.Errorf("multi-line prompt not parsed correctly: %q", prompt)
	}
}

func TestParsePipeline_FileNotFound(t *testing.T) {
	_, err := ParsePipeline("/nonexistent/path/pipeline.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "pipeline file not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParsePipeline_InvalidYAML(t *testing.T) {
	p := writeTempPipelineRaw(t, "not: valid: yaml: [[[")
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "invalid YAML") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParsePipeline_InvalidAgentType(t *testing.T) {
	content := `
steps:
  - name: "step1"
    agent: "gpt4"
    prompt: "do stuff"
`
	p := writeTempPipeline(t, content)
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error for invalid agent type")
	}
	if !strings.Contains(err.Error(), "invalid agent type") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParsePipeline_DuplicateStepName(t *testing.T) {
	content := `
steps:
  - name: "step1"
    agent: "claude"
    prompt: "do stuff"
  - name: "step1"
    command: "echo hi"
`
	p := writeTempPipeline(t, content)
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error for duplicate step name")
	}
	if !strings.Contains(err.Error(), "duplicate step name") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParsePipeline_BothAgentAndCommand(t *testing.T) {
	content := `
steps:
  - name: "step1"
    agent: "claude"
    prompt: "do stuff"
    command: "echo hi"
`
	p := writeTempPipeline(t, content)
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error for both agent and command")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParsePipeline_NeitherAgentNorCommand(t *testing.T) {
	content := `
steps:
  - name: "step1"
    project_dir: "/tmp"
`
	p := writeTempPipeline(t, content)
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error for neither agent nor command")
	}
}

func TestParsePipeline_ConditionMissingPrompt(t *testing.T) {
	content := `
steps:
  - name: "step1"
    agent: "claude"
    prompt: "do stuff"
    condition:
      file: "output.txt"
`
	p := writeTempPipeline(t, content)
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error for condition missing prompt")
	}
	if !strings.Contains(err.Error(), "condition requires") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParsePipeline_GotoNonExistentStep(t *testing.T) {
	content := `
steps:
  - name: "step1"
    agent: "claude"
    prompt: "do stuff"
    condition:
      prompt: "Is it done?"
      goto: "nonexistent"
`
	p := writeTempPipeline(t, content)
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error for invalid goto target")
	}
	if !strings.Contains(err.Error(), "goto target") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParsePipeline_Loop(t *testing.T) {
	content := `
steps:
  - loop:
      max_iterations: 3
      condition:
        prompt: "Are there still failing tests?"
        file: "test-results.txt"
      steps:
        - name: "run-tests"
          command: "go test ./..."
        - name: "fix-tests"
          agent: "claude"
          prompt: "Fix the failing tests"
`
	p := writeTempPipeline(t, content)
	pipeline, err := ParsePipeline(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipeline.Elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(pipeline.Elements))
	}
	loop := pipeline.Elements[0].Loop
	if loop == nil {
		t.Fatal("expected loop element")
	}
	if loop.MaxIterations != 3 {
		t.Errorf("expected max_iterations=3, got %d", loop.MaxIterations)
	}
	if len(loop.Steps) != 2 {
		t.Errorf("expected 2 loop steps, got %d", len(loop.Steps))
	}
}

func TestParsePipeline_LoopMissingMaxIterations(t *testing.T) {
	content := `
steps:
  - loop:
      condition:
        prompt: "Continue?"
      steps:
        - name: "step1"
          command: "echo hi"
`
	p := writeTempPipeline(t, content)
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error for loop missing max_iterations")
	}
	if !strings.Contains(err.Error(), "positive integer") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParsePipeline_LoopMaxIterationsZero(t *testing.T) {
	content := `
steps:
  - loop:
      max_iterations: 0
      condition:
        prompt: "Continue?"
      steps:
        - name: "step1"
          command: "echo hi"
`
	p := writeTempPipeline(t, content)
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error for max_iterations=0")
	}
	if !strings.Contains(err.Error(), "positive integer") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParsePipeline_GotoTargetsLoopStep(t *testing.T) {
	// goto must only target top-level steps, not steps inside loops
	content := `
steps:
  - name: "review"
    agent: "claude"
    prompt: "review"
    condition:
      prompt: "Issues found?"
      goto: "loop-step"
  - loop:
      max_iterations: 3
      condition:
        prompt: "Continue?"
      steps:
        - name: "loop-step"
          command: "echo hi"
`
	p := writeTempPipeline(t, content)
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error when goto targets a loop step")
	}
	if !strings.Contains(err.Error(), "goto target") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParsePipeline_LoopStepWithCondition(t *testing.T) {
	// conditions inside loop steps are allowed (without goto)
	content := `
steps:
  - loop:
      max_iterations: 3
      condition:
        prompt: "Continue?"
      steps:
        - name: "run-tests"
          command: "go test ./..."
          condition:
            prompt: "Did tests pass?"
`
	p := writeTempPipeline(t, content)
	pipeline, err := ParsePipeline(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	step := pipeline.Elements[0].Loop.Steps[0]
	if step.Condition == nil {
		t.Fatal("expected condition on loop step")
	}
	if step.Condition.Prompt != "Did tests pass?" {
		t.Errorf("unexpected condition prompt: %q", step.Condition.Prompt)
	}
}

func TestParsePipeline_LoopStepWithGotoCondition(t *testing.T) {
	// goto inside loop step conditions is not supported
	content := `
steps:
  - loop:
      max_iterations: 3
      condition:
        prompt: "Continue?"
      steps:
        - name: "run-tests"
          command: "go test ./..."
          condition:
            prompt: "Did tests pass?"
            goto: "some-step"
`
	p := writeTempPipeline(t, content)
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error for goto inside loop step condition")
	}
	if !strings.Contains(err.Error(), "goto inside loop step") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParsePipeline_PromptWithoutAgent(t *testing.T) {
	// step with prompt but no agent and no command should give a clear error
	content := `
steps:
  - name: "step1"
    prompt: "do something"
`
	p := writeTempPipeline(t, content)
	_, err := ParsePipeline(p)
	if err == nil {
		t.Fatal("expected error for step with prompt but no agent or command")
	}
	if !strings.Contains(err.Error(), "must have either") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNeedsLLM(t *testing.T) {
	// Pipeline without conditions
	content := `
steps:
  - name: "step1"
    command: "echo hi"
`
	p := writeTempPipeline(t, content)
	pipeline, _ := ParsePipeline(p)
	if pipeline.NeedsLLM() {
		t.Error("expected NeedsLLM=false for pipeline without conditions")
	}

	// Pipeline with condition
	content2 := `
steps:
  - name: "step1"
    agent: "claude"
    prompt: "do stuff"
    condition:
      prompt: "Is it done?"
`
	p2 := writeTempPipeline(t, content2)
	pipeline2, _ := ParsePipeline(p2)
	if !pipeline2.NeedsLLM() {
		t.Error("expected NeedsLLM=true for pipeline with condition")
	}
}

// --- Property-based tests ---

// Property 1: Pipeline parsing round trip
func TestPipelineRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a valid pipeline
		pipeline := genValidPipeline(t)

		// Serialize to YAML
		raw := rawPipeline{Steps: pipeline.Elements}
		data, err := yaml.Marshal(raw)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		// Write to temp file and parse back
		f, err := os.CreateTemp("", "pipeline-*.yaml")
		if err != nil {
			t.Fatalf("temp file error: %v", err)
		}
		defer os.Remove(f.Name())
		f.Write(data)
		f.Close()

		parsed, err := ParsePipeline(f.Name())
		if err != nil {
			t.Fatalf("parse error after round-trip: %v\nYAML:\n%s", err, data)
		}

		// Check structural equivalence: same number of elements
		if len(parsed.Elements) != len(pipeline.Elements) {
			t.Fatalf("element count mismatch: got %d, want %d", len(parsed.Elements), len(pipeline.Elements))
		}

		// Check step names are preserved
		origNames := pipeline.FlattenStepNames()
		parsedNames := parsed.FlattenStepNames()
		if len(origNames) != len(parsedNames) {
			t.Fatalf("step name count mismatch: got %d, want %d", len(parsedNames), len(origNames))
		}
		for i, name := range origNames {
			if parsedNames[i] != name {
				t.Fatalf("step name mismatch at index %d: got %q, want %q", i, parsedNames[i], name)
			}
		}
	})
}

// Property 2: Invalid pipelines are rejected
func TestInvalidPipelineRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		content := genInvalidPipelineYAML(rt)
		f, err := os.CreateTemp("", "pipeline-invalid-*.yaml")
		if err != nil {
			rt.Fatalf("temp file error: %v", err)
		}
		name := f.Name()
		f.WriteString(content)
		f.Close()
		defer os.Remove(name)

		_, parseErr := ParsePipeline(name)
		if parseErr == nil {
			rt.Fatalf("expected error for invalid pipeline:\n%s", content)
		}
	})
}

// --- Helpers ---

func writeTempPipeline(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "pipeline-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	f.WriteString(content)
	f.Close()
	return f.Name()
}

func writeTempPipelineRaw(t testing.TB, content string) string {
	f, err := os.CreateTemp("", "pipeline-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	// Use Cleanup if available (testing.T), otherwise just leave it
	if tt, ok := t.(*testing.T); ok {
		tt.Cleanup(func() { os.Remove(f.Name()) })
	}
	f.WriteString(content)
	f.Close()
	return f.Name()
}

// genValidPipeline generates a random valid Pipeline for property testing.
func genValidPipeline(t *rapid.T) *Pipeline {
	numElements := rapid.IntRange(1, 4).Draw(t, "numElements")
	usedNames := map[string]bool{}
	nameCounter := 0

	uniqueName := func() string {
		nameCounter++
		name := fmt.Sprintf("step-%d", nameCounter)
		usedNames[name] = true
		return name
	}

	elements := make([]PipelineElement, numElements)
	for i := 0; i < numElements; i++ {
		isLoop := rapid.Bool().Draw(t, fmt.Sprintf("isLoop-%d", i))
		if isLoop {
			numLoopSteps := rapid.IntRange(1, 3).Draw(t, fmt.Sprintf("numLoopSteps-%d", i))
			loopSteps := make([]Step, numLoopSteps)
			for j := 0; j < numLoopSteps; j++ {
				loopSteps[j] = genSimpleStep(t, uniqueName(), fmt.Sprintf("loop-%d-step-%d", i, j))
			}
			elements[i] = PipelineElement{
				Loop: &Loop{
					MaxIterations: rapid.IntRange(1, 10).Draw(t, fmt.Sprintf("maxIter-%d", i)),
					Steps:         loopSteps,
					Condition:     Condition{Prompt: "Should we continue?"},
				},
			}
		} else {
			step := genSimpleStep(t, uniqueName(), fmt.Sprintf("step-%d", i))
			elements[i] = PipelineElement{Step: &step}
		}
	}
	return &Pipeline{Elements: elements}
}

func genSimpleStep(t *rapid.T, name string, drawPrefix string) Step {
	isCommand := rapid.Bool().Draw(t, drawPrefix+"-isCommand")
	if isCommand {
		return Step{Name: name, Command: "echo hello"}
	}
	agent := rapid.SampledFrom([]string{"claude", "kiro"}).Draw(t, drawPrefix+"-agent")
	return Step{Name: name, Agent: agent, Prompt: "do something"}
}

// genInvalidPipelineYAML generates YAML with at least one validation violation.
func genInvalidPipelineYAML(t *rapid.T) string { //nolint
	violation := rapid.IntRange(0, 8).Draw(t, "violation")
	switch violation {
	case 0: // invalid agent type
		return `
steps:
  - name: "step1"
    agent: "badagent"
    prompt: "do stuff"
`
	case 1: // duplicate step names
		return `
steps:
  - name: "step1"
    command: "echo a"
  - name: "step1"
    command: "echo b"
`
	case 2: // missing both agent and command
		return `
steps:
  - name: "step1"
    project_dir: "/tmp"
`
	case 3: // both agent and command
		return `
steps:
  - name: "step1"
    agent: "claude"
    prompt: "do"
    command: "echo hi"
`
	case 4: // condition missing prompt
		return `
steps:
  - name: "step1"
    agent: "claude"
    prompt: "do"
    condition:
      file: "out.txt"
`
	case 5: // goto non-existent
		return `
steps:
  - name: "step1"
    agent: "claude"
    prompt: "do"
    condition:
      prompt: "Done?"
      goto: "nonexistent"
`
	case 6: // loop max_iterations <= 0
		return `
steps:
  - loop:
      max_iterations: -1
      condition:
        prompt: "Continue?"
      steps:
        - name: "s1"
          command: "echo hi"
`
	case 7: // loop missing condition
		return `
steps:
  - loop:
      max_iterations: 3
      steps:
        - name: "loop-s1"
          command: "echo hi"
`
	case 8: // goto inside loop step condition
		return `
steps:
  - loop:
      max_iterations: 3
      condition:
        prompt: "Continue?"
      steps:
        - name: "loop-s1"
          command: "echo hi"
          condition:
            prompt: "Done?"
            goto: "loop-s1"
`
	}
	return `steps: []`
}
