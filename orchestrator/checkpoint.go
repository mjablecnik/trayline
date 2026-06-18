package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const checkpointFile = ".agents/tmp/.checkpoint"
const flowCheckpointFile = ".agents/tmp/.flow-checkpoint"

// Checkpoint stores the state of a pipeline run for resume capability.
type Checkpoint struct {
	Pipeline       string            `json:"pipeline"`
	Variables      map[string]string `json:"variables"`
	CompletedSteps []string          `json:"completed_steps"`
	NextStep       string            `json:"next_step"`
	Timestamp      string            `json:"timestamp"`
	RateLimited    bool              `json:"rate_limited"`
}

// FlowCheckpoint stores the state of a flow (multi-pipeline) run for resume capability.
type FlowCheckpoint struct {
	Segments           []FlowSegmentState `json:"segments"`
	CompletedSegments  int                `json:"completed_segments"`
	Timestamp          string             `json:"timestamp"`
}

// FlowSegmentState stores the identity of a flow segment for matching.
type FlowSegmentState struct {
	PipelinePath string            `json:"pipeline_path"`
	Vars         map[string]string `json:"vars"`
}

// rateLimitPatterns are strings that indicate a rate limit error in agent output.
var rateLimitPatterns = []string{
	"rate limit",
	"rate_limit",
	"too many requests",
	"429",
	"quota exceeded",
	"token limit",
	"usage limit",
	"request limit",
	"overloaded",
}

// IsRateLimitError checks if the output contains rate limit indicators.
func IsRateLimitError(output string) bool {
	lower := strings.ToLower(output)
	for _, pattern := range rateLimitPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// SaveCheckpoint writes the current pipeline state to disk.
func SaveCheckpoint(pipelineName string, variables map[string]string, completedSteps []string, nextStep string, rateLimited bool) error {
	cp := Checkpoint{
		Pipeline:       pipelineName,
		Variables:      variables,
		CompletedSteps: completedSteps,
		NextStep:       nextStep,
		Timestamp:      time.Now().Format(time.RFC3339),
		RateLimited:    rateLimited,
	}

	dir := filepath.Dir(checkpointFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating checkpoint directory: %w", err)
	}

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling checkpoint: %w", err)
	}

	return os.WriteFile(checkpointFile, data, 0o644)
}

// LoadCheckpoint reads the checkpoint file if it exists.
// Returns nil if no checkpoint exists or if pipeline/variables don't match.
func LoadCheckpoint(pipelineName string, variables map[string]string) *Checkpoint {
	data, err := os.ReadFile(checkpointFile)
	if err != nil {
		return nil
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil
	}

	// Only return checkpoint if it's for the same pipeline
	if cp.Pipeline != pipelineName {
		return nil
	}

	// Check that variables match
	if len(cp.Variables) != len(variables) {
		return nil
	}
	for k, v := range cp.Variables {
		if variables[k] != v {
			return nil
		}
	}

	return &cp
}

// ClearCheckpoint removes the checkpoint file.
func ClearCheckpoint() {
	os.Remove(checkpointFile)
}

// SaveFlowCheckpoint writes the current flow state to disk.
func SaveFlowCheckpoint(segments []*FlowSegment, completedSegments int) error {
	var segStates []FlowSegmentState
	for _, seg := range segments {
		segStates = append(segStates, FlowSegmentState{
			PipelinePath: seg.PipelinePath,
			Vars:         seg.Vars,
		})
	}

	fcp := FlowCheckpoint{
		Segments:          segStates,
		CompletedSegments: completedSegments,
		Timestamp:         time.Now().Format(time.RFC3339),
	}

	dir := filepath.Dir(flowCheckpointFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating flow checkpoint directory: %w", err)
	}

	data, err := json.MarshalIndent(fcp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling flow checkpoint: %w", err)
	}

	return os.WriteFile(flowCheckpointFile, data, 0o644)
}

// LoadFlowCheckpoint reads the flow checkpoint file if it exists.
// Returns nil if no checkpoint exists or if segments don't match.
func LoadFlowCheckpoint(segments []*FlowSegment) *FlowCheckpoint {
	data, err := os.ReadFile(flowCheckpointFile)
	if err != nil {
		return nil
	}

	var fcp FlowCheckpoint
	if err := json.Unmarshal(data, &fcp); err != nil {
		return nil
	}

	// Verify segments match
	if len(fcp.Segments) != len(segments) {
		return nil
	}
	for i, seg := range segments {
		if fcp.Segments[i].PipelinePath != seg.PipelinePath {
			return nil
		}
		if len(fcp.Segments[i].Vars) != len(seg.Vars) {
			return nil
		}
		for k, v := range fcp.Segments[i].Vars {
			if seg.Vars[k] != v {
				return nil
			}
		}
	}

	return &fcp
}

// ClearFlowCheckpoint removes the flow checkpoint file.
func ClearFlowCheckpoint() {
	os.Remove(flowCheckpointFile)
}

// IsStepCompleted checks if a step name is in the completed list.
func (cp *Checkpoint) IsStepCompleted(stepName string) bool {
	for _, s := range cp.CompletedSteps {
		if s == stepName {
			return true
		}
	}
	return false
}
