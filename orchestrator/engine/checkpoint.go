package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const checkpointDir = ".agents/checkpoints"
const flowCheckpointFile = ".agents/checkpoints/.flow-checkpoint"
const stopFile = ".agents/checkpoints/.stop"

// checkpointPath returns the checkpoint file path for a given pipeline name.
// Each pipeline gets its own checkpoint file to avoid conflicts between
// nested pipeline executions (e.g., workflow calling sub-processes).
func checkpointPath(pipelineName string) string {
	// Create a safe filename from the pipeline name
	// e.g., "/home/martin/.trayline/pipelines/processes/3-ui-refactor.yaml" -> "processes--3-ui-refactor"
	name := pipelineName
	// Strip common prefix and .yaml extension
	name = strings.TrimSuffix(name, ".yaml")
	name = strings.TrimSuffix(name, ".yml")
	// Get last two path components (category/name) for readability
	parts := strings.Split(filepath.ToSlash(name), "/")
	if len(parts) >= 2 {
		name = parts[len(parts)-2] + "--" + parts[len(parts)-1]
	} else if len(parts) == 1 {
		name = parts[0]
	}
	// Sanitize: replace any remaining slashes or special chars
	name = strings.ReplaceAll(name, "/", "--")
	name = strings.ReplaceAll(name, "\\", "--")
	return filepath.Join(checkpointDir, "checkpoint-"+name)
}

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
	Segments          []FlowSegmentState `json:"segments"`
	CompletedSegments int                `json:"completed_segments"`
	Timestamp         string             `json:"timestamp"`
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
	"session limit",
	"overloaded",
}

// ansiRegex matches ANSI escape sequences (colors, cursor movement, etc.).
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes all ANSI escape sequences from a string.
func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// IsRateLimitError checks if the output contains rate limit indicators.
// Strips ANSI escape codes before matching to handle colored agent output.
func IsRateLimitError(output string) bool {
	lower := strings.ToLower(stripANSI(output))
	for _, pattern := range rateLimitPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// ParseResetTime extracts a reset time from rate limit output.
// Supports patterns like "resets 2am (UTC)", "resets 3:30pm (UTC)", "resets 14:00 (UTC)".
// Returns zero time if no reset time is found.
func ParseResetTime(output string) time.Time {
	lower := strings.ToLower(stripANSI(output))

	// Pattern: "resets <time> (utc)" or "resets <time> utc"
	idx := strings.Index(lower, "resets ")
	if idx == -1 {
		// Try "reset at <time>"
		idx = strings.Index(lower, "reset at ")
		if idx != -1 {
			idx += len("reset at ")
		}
	} else {
		idx += len("resets ")
	}

	if idx == -1 {
		return time.Time{}
	}

	// Extract the time portion (up to next parenthesis, comma, period, or end)
	rest := strings.TrimSpace(lower[idx:])
	// Remove surrounding context — take until we hit something non-time-like
	var timeStr string
	for i, ch := range rest {
		if ch == '(' || ch == ',' || ch == '.' || ch == '\n' || ch == ';' {
			timeStr = strings.TrimSpace(rest[:i])
			break
		}
		if i > 20 {
			// Safety limit
			timeStr = strings.TrimSpace(rest[:i])
			break
		}
	}
	if timeStr == "" {
		timeStr = strings.TrimSpace(rest)
		if len(timeStr) > 20 {
			timeStr = timeStr[:20]
		}
	}

	// Remove "utc" suffix if present (when not in parentheses)
	timeStr = strings.TrimSuffix(timeStr, "utc")
	timeStr = strings.TrimSpace(timeStr)

	// Try parsing various formats
	now := time.Now().UTC()
	var parsed time.Time

	formats := []string{
		"3pm",
		"3:04pm",
		"3 pm",
		"3:04 pm",
		"3am",
		"3:04am",
		"3 am",
		"3:04 am",
		"15:04",
		"15",
	}

	for _, format := range formats {
		t, err := time.Parse(format, timeStr)
		if err == nil {
			// Build the target time today in UTC
			parsed = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
			// If parsed time is in the past, it means tomorrow
			if parsed.Before(now) {
				parsed = parsed.Add(24 * time.Hour)
			}
			return parsed
		}
	}

	return time.Time{}
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

	cpPath := checkpointPath(pipelineName)
	dir := filepath.Dir(cpPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating checkpoint directory: %w", err)
	}

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling checkpoint: %w", err)
	}

	return os.WriteFile(cpPath, data, 0o644)
}

// LoadCheckpoint reads the checkpoint file if it exists.
// Returns nil if no checkpoint exists or if pipeline/variables don't match.
func LoadCheckpoint(pipelineName string, variables map[string]string) *Checkpoint {
	cpPath := checkpointPath(pipelineName)
	data, err := os.ReadFile(cpPath)
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

// ClearCheckpoint removes the checkpoint file for a specific pipeline.
func ClearCheckpoint(pipelineName string) {
	cpPath := checkpointPath(pipelineName)
	os.Remove(cpPath)
}

// ClearAllCheckpoints removes all checkpoint files (used on fresh flow start).
func ClearAllCheckpoints() {
	entries, err := os.ReadDir(checkpointDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "checkpoint-") {
			os.Remove(filepath.Join(checkpointDir, e.Name()))
		}
	}
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

// ShouldGracefulStop checks whether a graceful stop has been requested
// by checking for the presence of the stop file.
func ShouldGracefulStop() bool {
	_, err := os.Stat(stopFile)
	return err == nil
}

// ClearGracefulStop removes the stop file after it has been acknowledged.
func ClearGracefulStop() {
	os.Remove(stopFile)
}

// RequestGracefulStop creates the stop file to signal the orchestrator
// to finish the current step and then stop gracefully.
func RequestGracefulStop() error {
	dir := filepath.Dir(stopFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating stop file directory: %w", err)
	}
	return os.WriteFile(stopFile, []byte("stop\n"), 0o644)
}

// LoadAllCheckpoints reads all existing checkpoint files and returns them.
// This is used by workflow executors to detect if a sub-pipeline has an active checkpoint,
// allowing the workflow to skip steps that come before the checkpointed sub-pipeline.
func LoadAllCheckpoints() []*Checkpoint {
	var checkpoints []*Checkpoint

	// Read from new checkpoint directory
	entries, err := os.ReadDir(checkpointDir)
	if err == nil {
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "checkpoint-") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(checkpointDir, e.Name()))
			if err != nil {
				continue
			}
			var cp Checkpoint
			if err := json.Unmarshal(data, &cp); err != nil {
				continue
			}
			checkpoints = append(checkpoints, &cp)
		}
	}

	return checkpoints
}
