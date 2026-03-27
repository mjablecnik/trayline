package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

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
	os.Unsetenv("OPENROUTER_API_KEY")
	// Also clear any .env file by working from a fresh dir
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

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

// Property 10: Dry run no execution
func TestDryRunNoExecution(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numSteps := rapid.IntRange(1, 4).Draw(rt, "numSteps")
		elements := make([]PipelineElement, numSteps)

		for i := 0; i < numSteps; i++ {
			name := fmt.Sprintf("step-%d", i+1)
			elements[i] = PipelineElement{Step: &Step{Name: name, Command: fmt.Sprintf("echo %d", i)}}
		}

		runner := &mockRunner{}
		e := &Executor{
			Config:   &Config{},
			Pipeline: &Pipeline{Elements: elements},
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

		os.Unsetenv("OPENROUTER_API_KEY")
		os.Unsetenv("OPENROUTER_MODEL")

		tmpDir, _ := os.MkdirTemp("", "orchestrator-test-*")
		defer os.RemoveAll(tmpDir)
		origDir, _ := os.Getwd()
		os.Chdir(tmpDir)
		defer os.Chdir(origDir)

		code := run([]string{"--pipeline", f.Name()})
		if code == 0 {
			rt.Fatalf("expected non-zero exit code when API key missing and conditions present")
		}
	})
}
