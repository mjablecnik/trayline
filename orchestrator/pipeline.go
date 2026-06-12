package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Pipeline is the top-level structure parsed from the YAML file.
type Pipeline struct {
	Elements []PipelineElement
}

// PipelineElement is either a Step or a Loop.
type PipelineElement struct {
	Step *Step
	Loop *Loop
}

// MarshalYAML serializes a PipelineElement back to YAML.
// Steps are serialized as flat objects; loops are wrapped under a "loop" key.
func (pe PipelineElement) MarshalYAML() (interface{}, error) {
	if pe.Loop != nil {
		return map[string]interface{}{"loop": pe.Loop}, nil
	}
	return pe.Step, nil
}

// UnmarshalYAML distinguishes between step objects and loop objects.
func (pe *PipelineElement) UnmarshalYAML(value *yaml.Node) error {
	// Check if it has a "loop" key
	for i := 0; i < len(value.Content)-1; i += 2 {
		if value.Content[i].Value == "loop" {
			var wrapper struct {
				Loop Loop `yaml:"loop"`
			}
			if err := value.Decode(&wrapper); err != nil {
				return err
			}
			pe.Loop = &wrapper.Loop
			return nil
		}
	}
	// Otherwise parse as Step
	var step Step
	if err := value.Decode(&step); err != nil {
		return err
	}
	pe.Step = &step
	return nil
}

// Step represents a single pipeline step — either a trayline-agent invocation or a shell command.
type Step struct {
	Name       string     `yaml:"name"`
	Agent      string     `yaml:"agent"`
	Model      string     `yaml:"model"`
	Prompt     string     `yaml:"prompt"`
	Command    string     `yaml:"command"`
	ProjectDir string     `yaml:"project_dir"`
	Verbose    bool       `yaml:"verbose,omitempty"`
	Condition  *Condition `yaml:"condition"`
}

// Loop represents a repeatable block of steps (and nested loops) with an optional condition.
type Loop struct {
	MaxIterations int              `yaml:"max_iterations"`
	Elements      []PipelineElement `yaml:"steps"`
	Condition     Condition         `yaml:"condition"`
}

// Condition represents an evaluation — either LLM-based (prompt), string match (contains),
// or negated string match (not_contains).
type Condition struct {
	Prompt      string `yaml:"prompt"`
	File        string `yaml:"file"`
	Goto        string `yaml:"goto"`
	Contains    string `yaml:"contains"`
	NotContains string `yaml:"not_contains"`
}

// rawPipeline is used for YAML marshaling and unmarshaling.
type rawPipeline struct {
	Variables map[string]string `yaml:"variables"`
	Steps     []PipelineElement `yaml:"steps"`
}

// MarshalYAML ensures each PipelineElement is serialized correctly:
// steps as flat objects (no wrapper), loops as {loop: ...} objects.
// This is needed because yaml.v3 does not reliably call MarshalYAML
// on concrete struct values inside slices.
func (r rawPipeline) MarshalYAML() (interface{}, error) {
	steps := make([]interface{}, 0, len(r.Steps))
	for _, elem := range r.Steps {
		if elem.Loop != nil {
			steps = append(steps, map[string]interface{}{"loop": elem.Loop})
		} else {
			steps = append(steps, elem.Step)
		}
	}
	out := map[string]interface{}{"steps": steps}
	if len(r.Variables) > 0 {
		out["variables"] = r.Variables
	}
	return out, nil
}

// ParsePipelineRaw reads a YAML pipeline file and returns the pipeline and its variable map
// without running validation. Returns an empty (non-nil) map when no variables section is present.
func ParsePipelineRaw(path string) (*Pipeline, map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("pipeline file not found: %s", path)
		}
		return nil, nil, fmt.Errorf("reading pipeline file: %w", err)
	}

	var raw rawPipeline
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("invalid YAML in pipeline file: %w", err)
	}

	vars := raw.Variables
	if vars == nil {
		vars = make(map[string]string)
	}

	return &Pipeline{Elements: raw.Steps}, vars, nil
}

// ParsePipeline reads and validates a YAML pipeline file.
func ParsePipeline(path string) (*Pipeline, error) {
	p, _, err := ParsePipelineRaw(path)
	if err != nil {
		return nil, err
	}
	if err := ValidatePipeline(p); err != nil {
		return nil, err
	}
	return p, nil
}

// ValidatePipeline runs all validation checks on the pipeline.
func ValidatePipeline(p *Pipeline) error {
	seen := make(map[string]bool)
	allNames := p.FlattenStepNames()
	topLevelNames := p.TopLevelStepNames()

	for _, name := range allNames {
		if seen[name] {
			return fmt.Errorf("duplicate step name %q", name)
		}
		seen[name] = true
	}

	for _, elem := range p.Elements {
		if elem.Step != nil {
			if err := validateStep(elem.Step, topLevelNames); err != nil {
				return err
			}
		}
		if elem.Loop != nil {
			if err := validateLoop(elem.Loop, topLevelNames); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateStep validates a single step. topLevelNames is used for goto target validation;
// pass nil for steps inside loops (which cannot have conditions).
func validateStep(s *Step, topLevelNames []string) error {
	if s.Name == "" {
		return fmt.Errorf("step missing required field \"name\"")
	}

	isAgentStep := s.Agent != ""
	hasCommand := s.Command != ""

	// Reject if both agent-related fields (agent or orphan prompt) AND command are present.
	if (isAgentStep || s.Prompt != "") && hasCommand {
		return fmt.Errorf("step %q: must have either \"agent\"+\"prompt\" or \"command\", not both", s.Name)
	}
	// Reject if neither agent nor command — also catches "prompt only" (no agent, no command).
	if !isAgentStep && !hasCommand {
		return fmt.Errorf("step %q: must have either \"agent\"+\"prompt\" or \"command\"", s.Name)
	}

	if isAgentStep {
		if s.Agent != "kiro" && s.Agent != "claude" {
			return fmt.Errorf("step %q: invalid agent type %q, must be \"kiro\" or \"claude\"", s.Name, s.Agent)
		}
		if s.Prompt == "" {
			return fmt.Errorf("step %q: missing required field \"prompt\"", s.Name)
		}
	}

	if s.Condition != nil {
		if err := validateCondition(s.Name, s.Condition, topLevelNames); err != nil {
			return err
		}
	}

	return nil
}

// validateCondition validates a condition object. targetNames should be the top-level step names
// (goto can only target top-level steps, not steps inside loops).
func validateCondition(stepName string, c *Condition, targetNames []string) error {
	modes := 0
	if c.Prompt != "" {
		modes++
	}
	if c.Contains != "" {
		modes++
	}
	if c.NotContains != "" {
		modes++
	}
	if modes == 0 {
		return fmt.Errorf("step %q: condition requires one of \"prompt\", \"contains\", or \"not_contains\"", stepName)
	}
	if modes > 1 {
		return fmt.Errorf("step %q: condition must have exactly one of \"prompt\", \"contains\", or \"not_contains\"", stepName)
	}
	if c.Goto != "" {
		found := false
		for _, n := range targetNames {
			if n == c.Goto {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("step %q: goto target %q not found", stepName, c.Goto)
		}
	}
	return nil
}

// validateLoop validates a loop block. Loop-level condition is optional when
// at least one step inside the loop has a condition. Goto inside loop step
// conditions is not supported. Nested loops are allowed.
func validateLoop(l *Loop, topLevelNames []string) error {
	if l.MaxIterations <= 0 {
		return fmt.Errorf("loop: max_iterations must be a positive integer")
	}
	if len(l.Elements) == 0 {
		return fmt.Errorf("loop: missing required field \"steps\"")
	}

	hasStepCondition := false
	for i := range l.Elements {
		elem := &l.Elements[i]
		if elem.Step != nil {
			if elem.Step.Condition != nil {
				hasStepCondition = true
				if elem.Step.Condition.Goto != "" {
					return fmt.Errorf("loop: step %q: goto inside loop step conditions is not supported", elem.Step.Name)
				}
			}
			if err := validateStep(elem.Step, nil); err != nil {
				return err
			}
		}
		if elem.Loop != nil {
			if err := validateLoop(elem.Loop, topLevelNames); err != nil {
				return err
			}
		}
	}

	// Loop-level condition is required unless at least one step has a condition.
	if l.Condition.Prompt == "" && l.Condition.Contains == "" && l.Condition.NotContains == "" && !hasStepCondition {
		return fmt.Errorf("loop: missing required field \"condition\"")
	}

	return nil
}

// TopLevelStepNames returns only the names of top-level steps (not steps inside loops).
// Goto targets must reference top-level steps only.
func (p *Pipeline) TopLevelStepNames() []string {
	var names []string
	for _, elem := range p.Elements {
		if elem.Step != nil {
			names = append(names, elem.Step.Name)
		}
	}
	return names
}

// FlattenStepNames returns all step names across the pipeline (top-level + inside loops, recursively).
func (p *Pipeline) FlattenStepNames() []string {
	var names []string
	flattenElements(p.Elements, &names)
	return names
}

// flattenElements recursively collects step names from a slice of PipelineElements.
func flattenElements(elements []PipelineElement, names *[]string) {
	for _, elem := range elements {
		if elem.Step != nil {
			*names = append(*names, elem.Step.Name)
		}
		if elem.Loop != nil {
			flattenElements(elem.Loop.Elements, names)
		}
	}
}

// NeedsLLM returns true if any element in the pipeline uses an LLM-based condition (prompt).
func (p *Pipeline) NeedsLLM() bool {
	return elementsNeedLLM(p.Elements)
}

// elementsNeedLLM recursively checks if any element uses an LLM-based condition.
func elementsNeedLLM(elements []PipelineElement) bool {
	for _, elem := range elements {
		if elem.Step != nil && elem.Step.Condition != nil && elem.Step.Condition.Prompt != "" {
			return true
		}
		if elem.Loop != nil {
			if elem.Loop.Condition.Prompt != "" {
				return true
			}
			if elementsNeedLLM(elem.Loop.Elements) {
				return true
			}
		}
	}
	return false
}
