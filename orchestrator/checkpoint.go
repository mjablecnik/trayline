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

// Checkpoint stores the state of a pipeline run for resume capability.
type Checkpoint struct {
	Pipeline       string            `json:"pipeline"`
	Variables      map[string]string `json:"variables"`
	CompletedSteps []string          `json:"completed_steps"`
	NextStep       string            `json:"next_step"`
	Timestamp      string            `json:"timestamp"`
	RateLimited    bool              `json:"rate_limited"`
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

// IsStepCompleted checks if a step name is in the completed list.
func (cp *Checkpoint) IsStepCompleted(stepName string) bool {
	for _, s := range cp.CompletedSteps {
		if s == stepName {
			return true
		}
	}
	return false
}
