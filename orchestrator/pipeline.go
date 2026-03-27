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

// Step represents a single pipeline step — either an agent-docker invocation or a shell command.
type Step struct {
	Name       string     `yaml:"name"`
	Agent      string     `yaml:"agent"`
	Prompt     string     `yaml:"prompt"`
	Command    string     `yaml:"command"`
	ProjectDir string     `yaml:"project_dir"`
	Condition  *Condition `yaml:"condition"`
}

// Loop represents a repeatable block of steps with an LLM condition.
type Loop struct {
	MaxIterations int       `yaml:"max_iterations"`
	Steps         []Step    `yaml:"steps"`
	Condition     Condition `yaml:"condition"`
}

// Condition represents an LLM-based evaluation.
type Condition struct {
	Prompt string `yaml:"prompt"`
	File   string `yaml:"file"`
	Goto   string `yaml:"goto"`
}

// rawPipeline is used for YAML marshaling and unmarshaling.
type rawPipeline struct {
	Steps []PipelineElement `yaml:"steps"`
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
	return map[string]interface{}{"steps": steps}, nil
}

// ParsePipeline reads and validates a YAML pipeline file.
func ParsePipeline(path string) (*Pipeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("pipeline file not found: %s", path)
		}
		return nil, fmt.Errorf("reading pipeline file: %w", err)
	}

	var raw rawPipeline
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML in pipeline file: %w", err)
	}

	p := &Pipeline{Elements: raw.Steps}

	if err := validatePipeline(p); err != nil {
		return nil, err
	}

	return p, nil
}

// validatePipeline runs all validation checks on the pipeline.
func validatePipeline(p *Pipeline) error {
	seen := make(map[string]bool)
	allNames := p.FlattenStepNames()

	for _, name := range allNames {
		if seen[name] {
			return fmt.Errorf("duplicate step name %q", name)
		}
		seen[name] = true
	}

	for _, elem := range p.Elements {
		if elem.Step != nil {
			if err := validateStep(elem.Step, allNames); err != nil {
				return err
			}
		}
		if elem.Loop != nil {
			if err := validateLoop(elem.Loop, allNames); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateStep(s *Step, allNames []string) error {
	if s.Name == "" {
		return fmt.Errorf("step missing required field \"name\"")
	}

	hasAgent := s.Agent != "" || s.Prompt != ""
	hasCommand := s.Command != ""

	if hasAgent && hasCommand {
		return fmt.Errorf("step %q: must have either \"agent\"+\"prompt\" or \"command\", not both", s.Name)
	}
	if !hasAgent && !hasCommand {
		return fmt.Errorf("step %q: must have either \"agent\"+\"prompt\" or \"command\"", s.Name)
	}

	if hasAgent {
		if s.Agent == "" {
			return fmt.Errorf("step %q: missing required field \"agent\"", s.Name)
		}
		if s.Agent != "kiro" && s.Agent != "claude" {
			return fmt.Errorf("step %q: invalid agent type %q, must be \"kiro\" or \"claude\"", s.Name, s.Agent)
		}
		if s.Prompt == "" {
			return fmt.Errorf("step %q: missing required field \"prompt\"", s.Name)
		}
	}

	if s.Condition != nil {
		if err := validateCondition(s.Name, s.Condition, allNames); err != nil {
			return err
		}
	}

	return nil
}

func validateCondition(stepName string, c *Condition, allNames []string) error {
	if c.Prompt == "" {
		return fmt.Errorf("step %q: condition requires a \"prompt\" field", stepName)
	}
	if c.Goto != "" {
		found := false
		for _, n := range allNames {
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

func validateLoop(l *Loop, allNames []string) error {
	if l.MaxIterations <= 0 {
		return fmt.Errorf("loop: max_iterations must be a positive integer")
	}
	if len(l.Steps) == 0 {
		return fmt.Errorf("loop: missing required field \"steps\"")
	}
	if l.Condition.Prompt == "" {
		return fmt.Errorf("loop: missing required field \"condition\"")
	}

	for i := range l.Steps {
		if err := validateStep(&l.Steps[i], allNames); err != nil {
			return err
		}
	}

	return nil
}

// FlattenStepNames returns all step names across the pipeline (top-level + inside loops).
func (p *Pipeline) FlattenStepNames() []string {
	var names []string
	for _, elem := range p.Elements {
		if elem.Step != nil {
			names = append(names, elem.Step.Name)
		}
		if elem.Loop != nil {
			for _, s := range elem.Loop.Steps {
				names = append(names, s.Name)
			}
		}
	}
	return names
}

// NeedsLLM returns true if any element in the pipeline has a condition.
func (p *Pipeline) NeedsLLM() bool {
	for _, elem := range p.Elements {
		if elem.Step != nil && elem.Step.Condition != nil {
			return true
		}
		if elem.Loop != nil {
			return true
		}
	}
	return false
}
