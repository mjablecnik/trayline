package main

import (
	"os"
	"testing"
)

// --- parseFlowArgs tests ---

func TestParseFlowArgs_TwoSegments(t *testing.T) {
	segments, globalFlags := parseFlowArgs([]string{"proc/p1", "--then", "proc/p2"})
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d: %v", len(segments), segments)
	}
	if len(globalFlags) != 0 {
		t.Errorf("expected no global flags, got %v", globalFlags)
	}
	if len(segments[0]) != 1 || segments[0][0] != "proc/p1" {
		t.Errorf("unexpected segment[0]: %v", segments[0])
	}
	if len(segments[1]) != 1 || segments[1][0] != "proc/p2" {
		t.Errorf("unexpected segment[1]: %v", segments[1])
	}
}

func TestParseFlowArgs_SingleSegment(t *testing.T) {
	segments, globalFlags := parseFlowArgs([]string{"proc/p1"})
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if len(globalFlags) != 0 {
		t.Errorf("expected no global flags, got %v", globalFlags)
	}
	if segments[0][0] != "proc/p1" {
		t.Errorf("unexpected segment: %v", segments[0])
	}
}

func TestParseFlowArgs_ExtractsGlobalFlags(t *testing.T) {
	args := []string{"--dry-run", "proc/p1", "--then", "--verbose", "proc/p2"}
	segments, globalFlags := parseFlowArgs(args)

	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d: %v", len(segments), segments)
	}

	foundDryRun, foundVerbose := false, false
	for _, f := range globalFlags {
		if f == "--dry-run" {
			foundDryRun = true
		}
		if f == "--verbose" {
			foundVerbose = true
		}
	}
	if !foundDryRun {
		t.Errorf("expected --dry-run in global flags %v", globalFlags)
	}
	if !foundVerbose {
		t.Errorf("expected --verbose in global flags %v", globalFlags)
	}
	// Segments should not contain global flags.
	for i, seg := range segments {
		for _, arg := range seg {
			if arg == "--dry-run" || arg == "--verbose" {
				t.Errorf("segment[%d] contains global flag %q: %v", i, arg, seg)
			}
		}
	}
}

func TestParseFlowArgs_GlobalFlagDeduplication(t *testing.T) {
	_, globalFlags := parseFlowArgs([]string{"--dry-run", "proc/p1", "--then", "--dry-run", "proc/p2"})
	count := 0
	for _, f := range globalFlags {
		if f == "--dry-run" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected --dry-run to appear once in global flags, got %d", count)
	}
}

func TestParseFlowArgs_ThenAtStart(t *testing.T) {
	// --then at start: leading empty segment is dropped
	segments, _ := parseFlowArgs([]string{"--then", "proc/p1"})
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment (leading --then dropped), got %d: %v", len(segments), segments)
	}
	if segments[0][0] != "proc/p1" {
		t.Errorf("unexpected segment: %v", segments[0])
	}
}

func TestParseFlowArgs_ThenAtEnd(t *testing.T) {
	// --then at end: trailing empty segment is dropped
	segments, _ := parseFlowArgs([]string{"proc/p1", "--then"})
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment (trailing --then dropped), got %d: %v", len(segments), segments)
	}
}

func TestParseFlowArgs_ConsecutiveThen(t *testing.T) {
	// consecutive --then: empty intermediate segment is dropped
	segments, _ := parseFlowArgs([]string{"proc/p1", "--then", "--then", "proc/p2"})
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments (consecutive --then handled), got %d: %v", len(segments), segments)
	}
}

func TestParseFlowArgs_AllGlobalFlagTypes(t *testing.T) {
	args := []string{
		"--dry-run", "--verbose", "--log-llm", "--no-lifecycle", "--restart",
		"proc/p1",
	}
	segments, globalFlags := parseFlowArgs(args)
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if len(globalFlags) != 5 {
		t.Errorf("expected 5 global flags, got %d: %v", len(globalFlags), globalFlags)
	}
}

func TestParseFlowArgs_VarFlagsRemainInSegment(t *testing.T) {
	// --var is NOT a global flag, so it stays in the segment.
	args := []string{"--var", "a=1", "proc/p1", "--then", "proc/p2"}
	segments, globalFlags := parseFlowArgs(args)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	if len(globalFlags) != 0 {
		t.Errorf("expected no global flags, got %v", globalFlags)
	}
	// --var and a=1 should remain in the first segment.
	if len(segments[0]) < 3 {
		t.Errorf("expected --var a=1 to remain in segment[0]: %v", segments[0])
	}
}

// --- parseSegment tests ---

func TestParseSegment_PipelinePathOnly(t *testing.T) {
	seg, err := parseSegment([]string{"proc/my-pipeline"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seg.PipelinePath != "proc/my-pipeline" {
		t.Errorf("expected PipelinePath %q, got %q", "proc/my-pipeline", seg.PipelinePath)
	}
	if len(seg.Vars) != 0 {
		t.Errorf("expected no vars, got %v", seg.Vars)
	}
}

func TestParseSegment_VarsBeforePath(t *testing.T) {
	// Flags must appear before the positional pipeline path for Go's flag parser.
	seg, err := parseSegment([]string{"--var", "key=value", "--var", "env=prod", "proc/p1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seg.PipelinePath != "proc/p1" {
		t.Errorf("wrong path: %q", seg.PipelinePath)
	}
	if seg.Vars["key"] != "value" {
		t.Errorf("expected key=value, got %v", seg.Vars)
	}
	if seg.Vars["env"] != "prod" {
		t.Errorf("expected env=prod, got %v", seg.Vars)
	}
}

func TestParseSegment_EmptyPipelinePath_Error(t *testing.T) {
	// No positional arg → error.
	_, err := parseSegment([]string{"--var", "a=1"})
	if err == nil {
		t.Fatal("expected error for missing pipeline path")
	}
}

func TestParseSegment_MalformedVar_Error(t *testing.T) {
	// --var value without '=' → ParseCLIVars error.
	_, err := parseSegment([]string{"--var", "noequals", "proc/p1"})
	if err == nil {
		t.Fatal("expected error for malformed --var (missing '=')")
	}
}

func TestParseSegment_EmptyArgs_Error(t *testing.T) {
	_, err := parseSegment([]string{})
	if err == nil {
		t.Fatal("expected error for empty args")
	}
}

// --- parseLogTask tests ---

func TestParseLogTask_WithLogTask(t *testing.T) {
	f, err := os.CreateTemp("", "lifecycle-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString("log-task: \"my-log-pipeline\"\nbefore:\n  - name: setup\n")
	f.Close()

	result := parseLogTask(f.Name())
	if result != "my-log-pipeline" {
		t.Errorf("expected %q, got %q", "my-log-pipeline", result)
	}
}

func TestParseLogTask_SingleQuoted(t *testing.T) {
	f, err := os.CreateTemp("", "lifecycle-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString("log-task: 'single-quoted-task'\n")
	f.Close()

	result := parseLogTask(f.Name())
	if result != "single-quoted-task" {
		t.Errorf("expected %q, got %q", "single-quoted-task", result)
	}
}

func TestParseLogTask_NoLogTask(t *testing.T) {
	f, err := os.CreateTemp("", "lifecycle-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString("before:\n  - name: step1\n    command: echo hi\n")
	f.Close()

	result := parseLogTask(f.Name())
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestParseLogTask_MissingFile(t *testing.T) {
	result := parseLogTask("/nonexistent/path/lifecycle.yaml")
	if result != "" {
		t.Errorf("expected empty string for missing file, got %q", result)
	}
}

func TestParseLogTask_UnquotedValue(t *testing.T) {
	f, err := os.CreateTemp("", "lifecycle-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString("log-task: processes/log-it\n")
	f.Close()

	result := parseLogTask(f.Name())
	if result != "processes/log-it" {
		t.Errorf("expected %q, got %q", "processes/log-it", result)
	}
}

// --- flowUsageText / usageText ---

func TestFlowUsageText_NonEmpty(t *testing.T) {
	text := flowUsageText()
	if text == "" {
		t.Error("expected non-empty flowUsageText")
	}
	// Should mention key subcommand/usage markers.
	for _, keyword := range []string{"flow", "--then", "--dry-run", "--var"} {
		found := false
		for i := 0; i+len(keyword) <= len(text); i++ {
			if text[i:i+len(keyword)] == keyword {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in flowUsageText", keyword)
		}
	}
}

func TestUsageText_NonEmpty(t *testing.T) {
	text := usageText()
	if text == "" {
		t.Error("expected non-empty usageText")
	}
	for _, keyword := range []string{"--var", "--dry-run", "--version", "flow"} {
		found := false
		for i := 0; i+len(keyword) <= len(text); i++ {
			if text[i:i+len(keyword)] == keyword {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in usageText", keyword)
		}
	}
}
