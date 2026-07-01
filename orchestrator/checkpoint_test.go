package main

import (
	"os"
	"testing"
)

// withTempCheckpointDir changes the working directory to a temporary directory
// so that checkpoint file functions (which use relative paths) write to an isolated location.
// The original directory is restored in t.Cleanup.
func withTempCheckpointDir(t *testing.T) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir to temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.Chdir(orig)
	})
}

// --- SaveCheckpoint / LoadCheckpoint round-trip ---

func TestCheckpoint_SaveLoadRoundTrip(t *testing.T) {
	withTempCheckpointDir(t)

	pipeline := "test-pipeline-roundtrip"
	vars := map[string]string{"key": "value", "env": "prod"}
	steps := []string{"step-a", "step-b"}
	next := "step-c"

	if err := SaveCheckpoint(pipeline, vars, steps, next, false); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	cp := LoadCheckpoint(pipeline, vars)
	if cp == nil {
		t.Fatal("expected non-nil checkpoint after save")
	}
	if cp.Pipeline != pipeline {
		t.Errorf("pipeline mismatch: want %q, got %q", pipeline, cp.Pipeline)
	}
	if cp.NextStep != next {
		t.Errorf("next step mismatch: want %q, got %q", next, cp.NextStep)
	}
	if cp.RateLimited {
		t.Error("expected RateLimited=false")
	}
	if len(cp.CompletedSteps) != 2 {
		t.Fatalf("expected 2 completed steps, got %d", len(cp.CompletedSteps))
	}
	if cp.CompletedSteps[0] != "step-a" || cp.CompletedSteps[1] != "step-b" {
		t.Errorf("unexpected completed steps: %v", cp.CompletedSteps)
	}
	if cp.Variables["key"] != "value" || cp.Variables["env"] != "prod" {
		t.Errorf("unexpected variables: %v", cp.Variables)
	}
}

func TestCheckpoint_RateLimitedPreserved(t *testing.T) {
	withTempCheckpointDir(t)

	pipeline := "test-pipeline-ratelimited"
	if err := SaveCheckpoint(pipeline, nil, nil, "resume-step", true); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	cp := LoadCheckpoint(pipeline, nil)
	if cp == nil {
		t.Fatal("expected non-nil checkpoint")
	}
	if !cp.RateLimited {
		t.Error("expected RateLimited=true")
	}
}

// --- LoadCheckpoint mismatch rejection ---

func TestCheckpoint_PipelineNameMismatch(t *testing.T) {
	withTempCheckpointDir(t)

	if err := SaveCheckpoint("pipeline-a", nil, nil, "", false); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	// Load with a different pipeline name — should return nil.
	cp := LoadCheckpoint("pipeline-b", nil)
	if cp != nil {
		t.Error("expected nil for pipeline name mismatch")
	}
}

func TestCheckpoint_VariableMismatch(t *testing.T) {
	withTempCheckpointDir(t)

	pipeline := "test-pipeline-varmatch"
	saved := map[string]string{"x": "1"}
	if err := SaveCheckpoint(pipeline, saved, nil, "", false); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Different variable value.
	cp := LoadCheckpoint(pipeline, map[string]string{"x": "2"})
	if cp != nil {
		t.Error("expected nil for variable value mismatch")
	}

	// Extra variable.
	cp = LoadCheckpoint(pipeline, map[string]string{"x": "1", "y": "2"})
	if cp != nil {
		t.Error("expected nil for extra variable")
	}

	// Correct variables.
	cp = LoadCheckpoint(pipeline, map[string]string{"x": "1"})
	if cp == nil {
		t.Error("expected non-nil for matching variables")
	}
}

// --- IsStepCompleted ---

func TestIsStepCompleted(t *testing.T) {
	withTempCheckpointDir(t)

	pipeline := "test-pipeline-steps"
	steps := []string{"step-1", "step-2", "step-3"}
	if err := SaveCheckpoint(pipeline, nil, steps, "step-4", false); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	cp := LoadCheckpoint(pipeline, nil)
	if cp == nil {
		t.Fatal("expected non-nil checkpoint")
	}

	for _, s := range steps {
		if !cp.IsStepCompleted(s) {
			t.Errorf("expected step %q to be completed", s)
		}
	}
	if cp.IsStepCompleted("step-4") {
		t.Error("expected step-4 to not be completed")
	}
	if cp.IsStepCompleted("") {
		t.Error("expected empty string to not be completed")
	}
}

// --- ClearCheckpoint ---

func TestClearCheckpoint(t *testing.T) {
	withTempCheckpointDir(t)

	pipeline := "test-pipeline-clear"
	if err := SaveCheckpoint(pipeline, nil, nil, "step-x", false); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	cp := LoadCheckpoint(pipeline, nil)
	if cp == nil {
		t.Fatal("expected checkpoint to exist before clear")
	}

	ClearCheckpoint(pipeline)

	cp = LoadCheckpoint(pipeline, nil)
	if cp != nil {
		t.Error("expected nil checkpoint after clear")
	}
}

// --- Missing/corrupt file → nil, no panic ---

func TestLoadCheckpoint_MissingFile(t *testing.T) {
	withTempCheckpointDir(t)

	cp := LoadCheckpoint("nonexistent-pipeline", nil)
	if cp != nil {
		t.Error("expected nil for missing checkpoint file")
	}
}

func TestLoadCheckpoint_CorruptFile(t *testing.T) {
	withTempCheckpointDir(t)

	pipeline := "test-pipeline-corrupt"
	path := checkpointPath(pipeline)
	if err := os.MkdirAll(checkpointDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("not valid json {{{"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	cp := LoadCheckpoint(pipeline, nil)
	if cp != nil {
		t.Error("expected nil for corrupt checkpoint file")
	}
}

// --- Flow checkpoint round-trip ---

func TestFlowCheckpoint_SaveLoadRoundTrip(t *testing.T) {
	withTempCheckpointDir(t)

	segments := []*FlowSegment{
		{PipelinePath: "proc/p1", Vars: map[string]string{"a": "1"}},
		{PipelinePath: "proc/p2", Vars: map[string]string{"b": "2"}},
	}

	if err := SaveFlowCheckpoint(segments, 1); err != nil {
		t.Fatalf("SaveFlowCheckpoint: %v", err)
	}

	fcp := LoadFlowCheckpoint(segments)
	if fcp == nil {
		t.Fatal("expected non-nil flow checkpoint")
	}
	if fcp.CompletedSegments != 1 {
		t.Errorf("expected CompletedSegments=1, got %d", fcp.CompletedSegments)
	}
	if len(fcp.Segments) != 2 {
		t.Errorf("expected 2 segments, got %d", len(fcp.Segments))
	}
}

func TestFlowCheckpoint_SegmentMismatchRejected(t *testing.T) {
	withTempCheckpointDir(t)

	segments := []*FlowSegment{
		{PipelinePath: "proc/p1", Vars: nil},
		{PipelinePath: "proc/p2", Vars: nil},
	}
	if err := SaveFlowCheckpoint(segments, 1); err != nil {
		t.Fatalf("SaveFlowCheckpoint: %v", err)
	}

	// Different pipeline path in segment.
	mismatch := []*FlowSegment{
		{PipelinePath: "proc/p1", Vars: nil},
		{PipelinePath: "proc/DIFFERENT", Vars: nil},
	}
	fcp := LoadFlowCheckpoint(mismatch)
	if fcp != nil {
		t.Error("expected nil for segment path mismatch")
	}

	// Different count.
	fewer := []*FlowSegment{{PipelinePath: "proc/p1", Vars: nil}}
	fcp = LoadFlowCheckpoint(fewer)
	if fcp != nil {
		t.Error("expected nil for segment count mismatch")
	}
}

func TestClearFlowCheckpoint(t *testing.T) {
	withTempCheckpointDir(t)

	segments := []*FlowSegment{{PipelinePath: "proc/p1", Vars: nil}}
	if err := SaveFlowCheckpoint(segments, 0); err != nil {
		t.Fatalf("SaveFlowCheckpoint: %v", err)
	}

	fcp := LoadFlowCheckpoint(segments)
	if fcp == nil {
		t.Fatal("expected flow checkpoint before clear")
	}

	ClearFlowCheckpoint()

	fcp = LoadFlowCheckpoint(segments)
	if fcp != nil {
		t.Error("expected nil flow checkpoint after clear")
	}
}

func TestLoadFlowCheckpoint_MissingFile(t *testing.T) {
	withTempCheckpointDir(t)

	segments := []*FlowSegment{{PipelinePath: "proc/p1", Vars: nil}}
	fcp := LoadFlowCheckpoint(segments)
	if fcp != nil {
		t.Error("expected nil for missing flow checkpoint file")
	}
}

func TestLoadFlowCheckpoint_CorruptFile(t *testing.T) {
	withTempCheckpointDir(t)

	dir := checkpointDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(flowCheckpointFile, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	segments := []*FlowSegment{{PipelinePath: "proc/p1", Vars: nil}}
	fcp := LoadFlowCheckpoint(segments)
	if fcp != nil {
		t.Error("expected nil for corrupt flow checkpoint file")
	}
}
