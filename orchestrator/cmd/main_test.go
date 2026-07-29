package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"orchestrator/core"
	"orchestrator/engine"
)

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

// mockRunner is a CommandRunner for testing.
type mockRunner struct {
	calls []string
}

func (m *mockRunner) RunAgent(agent, prompt, model, projectDir string, env []string, verbose bool, stdout, stderr io.Writer) (string, int, error) {
	m.calls = append(m.calls, "agent:"+agent)
	return "", 0, nil
}

func (m *mockRunner) RunCommand(command, projectDir string, env []string, verbose bool, stdout, stderr io.Writer) (string, int, error) {
	m.calls = append(m.calls, "command:"+command)
	return "", 0, nil
}

// mockEvaluator is a ConditionEvaluator for testing.
type mockEvaluator struct{}

func (m *mockEvaluator) Evaluate(content, prompt string) (bool, error) {
	return false, nil
}

func TestMain_MissingPipelineFlag(t *testing.T) {
	code := run([]string{})
	if code == 0 {
		t.Error("expected non-zero exit code for missing --pipeline flag")
	}
}

func TestMain_VersionFlag(t *testing.T) {
	code := run([]string{"--version"})
	if code != 0 {
		t.Errorf("expected exit code 0 for --version, got %d", code)
	}
}

func TestMain_InvalidFlag(t *testing.T) {
	code := run([]string{"--nonexistent-flag"})
	if code == 0 {
		t.Error("expected non-zero exit code for invalid flag")
	}
}

func TestMain_PipelineNotFound(t *testing.T) {
	code := run([]string{"--pipeline", "/nonexistent/pipeline.yaml"})
	if code == 0 {
		t.Error("expected non-zero exit for missing pipeline file")
	}
}

func TestMain_APIKeyMissingWithCondition(t *testing.T) {
	content := `
steps:
  - name: "step1"
    command: "echo hi"
    condition:
      prompt: "Done?"
`
	f := writeTempPipeline(t, content)
	t.Setenv("OPENROUTER_API_KEY", "")

	code := run([]string{"--pipeline", f})
	if code == 0 {
		t.Error("expected non-zero exit code when API key missing and condition present")
	}
}

func TestMain_DryRun(t *testing.T) {
	content := `
steps:
  - name: "step1"
    agent: "claude"
    prompt: "do stuff"
    project_dir: "/tmp"
  - name: "step2"
    command: "echo hi"
`
	f := writeTempPipeline(t, content)
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := run([]string{"--pipeline", f, "--dry-run"})

	w.Close()
	out, _ := io.ReadAll(r)
	os.Stdout = old

	if code != 0 {
		t.Errorf("expected exit code 0 for dry-run, got %d", code)
	}
	output := string(out)
	if !strings.Contains(output, "step1") {
		t.Errorf("expected step1 in dry-run output, got: %s", output)
	}
	if !strings.Contains(output, "step2") {
		t.Errorf("expected step2 in dry-run output, got: %s", output)
	}
}

func TestMain_VarFlagSubstitution(t *testing.T) {
	content := `
variables:
  target: ""
steps:
  - name: "step1"
    command: "echo {{target}}"
`
	f := writeTempPipeline(t, content)
	code := run([]string{"--pipeline", f, "--dry-run", "--var", "target=hello"})
	if code != 0 {
		t.Errorf("expected exit code 0 with --var substitution, got %d", code)
	}
}

func TestMain_VarFlagOverridesYAML(t *testing.T) {
	content := `
variables:
  target: "original"
steps:
  - name: "step1"
    command: "echo {{target}}"
`
	f := writeTempPipeline(t, content)
	code := run([]string{"--pipeline", f, "--dry-run", "--var", "target=overridden"})
	if code != 0 {
		t.Errorf("expected exit code 0 with --var override, got %d", code)
	}
}

func TestMain_VarFlagMissingEquals(t *testing.T) {
	content := `
steps:
  - name: "step1"
    command: "echo hi"
`
	f := writeTempPipeline(t, content)
	code := run([]string{"--pipeline", f, "--var", "noequals"})
	if code == 0 {
		t.Error("expected non-zero exit code for --var flag missing '='")
	}
}

func TestMain_VarFlagUndefinedPlaceholder(t *testing.T) {
	content := `
steps:
  - name: "step1"
    command: "echo {{undefined-var}}"
`
	f := writeTempPipeline(t, content)
	code := run([]string{"--pipeline", f})
	if code == 0 {
		t.Error("expected non-zero exit code for undefined variable placeholder")
	}
}

// Property 10: Dry run no execution
func TestDryRunNoExecution(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numSteps := rapid.IntRange(1, 4).Draw(rt, "numSteps")
		elements := make([]core.PipelineElement, numSteps)

		for i := 0; i < numSteps; i++ {
			name := fmt.Sprintf("step-%d", i+1)
			elements[i] = core.PipelineElement{Step: &core.Step{Name: name, Command: fmt.Sprintf("echo %d", i)}}
		}

		runner := &mockRunner{}
		e := &engine.Executor{
			Config:   &core.Config{},
			Pipeline: &core.Pipeline{Elements: elements},
			LLM:      &mockEvaluator{},
			DryRun:   true,
			Verbose:  false,
			Runner:   runner,
		}
		code := e.Run()

		if code != 0 {
			rt.Fatalf("expected exit code 0 for dry-run, got %d", code)
		}
		if len(runner.calls) != 0 {
			rt.Fatalf("expected no subprocess calls in dry-run, got %d", len(runner.calls))
		}
	})
}

// Property 11: API key required when pipeline needs LLM
func TestAPIKeyRequired(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENROUTER_MODEL", "")

	rapid.Check(t, func(rt *rapid.T) {
		// Pipeline with a condition
		content := `
steps:
  - name: "step1"
    command: "echo hi"
    condition:
      prompt: "Done?"
`
		f, err := os.CreateTemp("", "pipeline-*.yaml")
		if err != nil {
			rt.Fatalf("temp file: %v", err)
		}
		f.WriteString(content)
		f.Close()
		defer os.Remove(f.Name())

		code := run([]string{"--pipeline", f.Name()})
		if code == 0 {
			rt.Fatalf("expected non-zero exit code when API key missing and conditions present")
		}
	})
}

func TestUsageTextNonEmpty(t *testing.T) {
	text := usageText()
	if text == "" {
		t.Fatal("usageText returned empty string")
	}
	for _, keyword := range []string{"flow", "--dry-run", "--var", "--version"} {
		if !strings.Contains(text, keyword) {
			t.Errorf("usageText does not mention %q", keyword)
		}
	}
}
