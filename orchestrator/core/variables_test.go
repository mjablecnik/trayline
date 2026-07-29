package core

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"pgregory.net/rapid"
)

// --- Unit tests ---

func TestParsePipelineRaw_WithVariables(t *testing.T) {
	content := `
variables:
  project-path: "/tmp/myproject"
  spec-name: "my-spec"
steps:
  - name: "step1"
    command: "echo hello"
`
	path := writeTempPipeline(t, content)
	_, vars, err := ParsePipelineRaw(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vars["project-path"] != "/tmp/myproject" {
		t.Errorf("expected project-path=/tmp/myproject, got %q", vars["project-path"])
	}
	if vars["spec-name"] != "my-spec" {
		t.Errorf("expected spec-name=my-spec, got %q", vars["spec-name"])
	}
}

func TestParsePipelineRaw_WithoutVariables(t *testing.T) {
	content := `
steps:
  - name: "step1"
    command: "echo hello"
`
	path := writeTempPipeline(t, content)
	_, vars, err := ParsePipelineRaw(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vars == nil {
		t.Fatal("expected non-nil empty map, got nil")
	}
	if len(vars) != 0 {
		t.Errorf("expected empty map, got %v", vars)
	}
}

func TestParseCLIVars_Basic(t *testing.T) {
	vars, err := ParseCLIVars([]string{"key=value", "foo=bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vars["key"] != "value" {
		t.Errorf("expected key=value, got %q", vars["key"])
	}
	if vars["foo"] != "bar" {
		t.Errorf("expected foo=bar, got %q", vars["foo"])
	}
}

func TestParseCLIVars_ValueContainsEquals(t *testing.T) {
	vars, err := ParseCLIVars([]string{"key=val=ue"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vars["key"] != "val=ue" {
		t.Errorf("expected key=val=ue, got %q", vars["key"])
	}
}

func TestParseCLIVars_MissingEquals(t *testing.T) {
	_, err := ParseCLIVars([]string{"noequals"})
	if err == nil {
		t.Fatal("expected error for flag without =")
	}
	if !strings.Contains(err.Error(), "key=value format") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseCLIVars_EmptyValue(t *testing.T) {
	vars, err := ParseCLIVars([]string{"key="})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vars["key"] != "" {
		t.Errorf("expected key='', got %q", vars["key"])
	}
}

func TestParseCLIVars_LastOccurrenceWins(t *testing.T) {
	vars, err := ParseCLIVars([]string{"key=first", "key=second"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vars["key"] != "second" {
		t.Errorf("expected last value to win, got %q", vars["key"])
	}
}

func TestMergeVariables_CLIPrecedence(t *testing.T) {
	yaml := map[string]string{"a": "yaml-a", "b": "yaml-b"}
	cli := map[string]string{"b": "cli-b", "c": "cli-c"}
	result := MergeVariables(yaml, cli)
	if result["a"] != "yaml-a" {
		t.Errorf("expected a=yaml-a, got %q", result["a"])
	}
	if result["b"] != "cli-b" {
		t.Errorf("expected b=cli-b (CLI wins), got %q", result["b"])
	}
	if result["c"] != "cli-c" {
		t.Errorf("expected c=cli-c, got %q", result["c"])
	}
}

func TestSubstituteVariables_SinglePlaceholderInPrompt(t *testing.T) {
	p := &Pipeline{Elements: []PipelineElement{
		{Step: &Step{Name: "s1", Agent: "claude", Prompt: "Read {{spec-name}}"}},
	}}
	err := SubstituteVariables(p, map[string]string{"spec-name": "my-spec"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Elements[0].Step.Prompt != "Read my-spec" {
		t.Errorf("expected prompt resolved, got %q", p.Elements[0].Step.Prompt)
	}
}

func TestSubstituteVariables_MultiplePlaceholdersInOneField(t *testing.T) {
	p := &Pipeline{Elements: []PipelineElement{
		{Step: &Step{Name: "s1", Agent: "claude", Prompt: "Read {{spec}} from {{path}}"}},
	}}
	err := SubstituteVariables(p, map[string]string{"spec": "my-spec", "path": "/tmp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Elements[0].Step.Prompt != "Read my-spec from /tmp" {
		t.Errorf("expected prompt fully resolved, got %q", p.Elements[0].Step.Prompt)
	}
}

func TestSubstituteVariables_AllTemplatableFields(t *testing.T) {
	p := &Pipeline{Elements: []PipelineElement{
		{Step: &Step{
			Name:       "s1",
			Agent:      "claude",
			Prompt:     "{{prompt-var}}",
			ProjectDir: "{{dir-var}}",
			Condition: &Condition{
				Prompt: "{{cond-prompt}}",
				File:   "{{cond-file}}",
			},
		}},
		{Step: &Step{
			Name:    "s2",
			Command: "cd {{dir-var}} && echo {{prompt-var}}",
		}},
	}}
	vars := map[string]string{
		"prompt-var":  "do the thing",
		"dir-var":     "/workspace",
		"cond-prompt": "Is it done?",
		"cond-file":   "output.txt",
	}
	if err := SubstituteVariables(p, vars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s1 := p.Elements[0].Step
	if s1.Prompt != "do the thing" {
		t.Errorf("Prompt not resolved: %q", s1.Prompt)
	}
	if s1.ProjectDir != "/workspace" {
		t.Errorf("ProjectDir not resolved: %q", s1.ProjectDir)
	}
	if s1.Condition.Prompt != "Is it done?" {
		t.Errorf("Condition.Prompt not resolved: %q", s1.Condition.Prompt)
	}
	if s1.Condition.File != "output.txt" {
		t.Errorf("Condition.File not resolved: %q", s1.Condition.File)
	}
	if p.Elements[1].Step.Command != "cd /workspace && echo do the thing" {
		t.Errorf("Command not resolved: %q", p.Elements[1].Step.Command)
	}
}

func TestSubstituteVariables_LoopFields(t *testing.T) {
	p := &Pipeline{Elements: []PipelineElement{
		{Loop: &Loop{
			MaxIterations: 2,
			Condition: Condition{
				Prompt: "Continue {{spec}}?",
				File:   "{{path}}/results.txt",
			},
			Elements: []PipelineElement{
				{Step: &Step{Name: "fix", Agent: "claude", Prompt: "Fix in {{path}}"}},
			},
		}},
	}}
	vars := map[string]string{"spec": "my-spec", "path": "/tmp"}
	if err := SubstituteVariables(p, vars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	l := p.Elements[0].Loop
	if l.Condition.Prompt != "Continue my-spec?" {
		t.Errorf("Loop condition.Prompt not resolved: %q", l.Condition.Prompt)
	}
	if l.Condition.File != "/tmp/results.txt" {
		t.Errorf("Loop condition.File not resolved: %q", l.Condition.File)
	}
	if l.Elements[0].Step.Prompt != "Fix in /tmp" {
		t.Errorf("Loop step Prompt not resolved: %q", l.Elements[0].Step.Prompt)
	}
}

func TestSubstituteVariables_UndefinedSingle(t *testing.T) {
	p := &Pipeline{Elements: []PipelineElement{
		{Step: &Step{Name: "s1", Agent: "claude", Prompt: "Read {{undefined-var}}"}},
	}}
	err := SubstituteVariables(p, map[string]string{})
	if err == nil {
		t.Fatal("expected error for undefined variable")
	}
	if !strings.Contains(err.Error(), "undefined-var") {
		t.Errorf("error should name the undefined variable, got: %v", err)
	}
}

func TestSubstituteVariables_UndefinedMultipleAcrossFields(t *testing.T) {
	p := &Pipeline{Elements: []PipelineElement{
		{Step: &Step{Name: "s1", Agent: "claude", Prompt: "{{missing-a}}"}},
		{Step: &Step{Name: "s2", Command: "echo {{missing-b}}"}},
	}}
	err := SubstituteVariables(p, map[string]string{})
	if err == nil {
		t.Fatal("expected error for multiple undefined variables")
	}
	if !strings.Contains(err.Error(), "missing-a") {
		t.Errorf("error should name missing-a, got: %v", err)
	}
	if !strings.Contains(err.Error(), "missing-b") {
		t.Errorf("error should name missing-b, got: %v", err)
	}
}

func TestSubstituteVariables_EmptyStringValue(t *testing.T) {
	p := &Pipeline{Elements: []PipelineElement{
		{Step: &Step{Name: "s1", Agent: "claude", Prompt: "prefix-{{empty}}-suffix"}},
	}}
	if err := SubstituteVariables(p, map[string]string{"empty": ""}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Elements[0].Step.Prompt != "prefix--suffix" {
		t.Errorf("expected empty string substitution, got %q", p.Elements[0].Step.Prompt)
	}
}

func TestSubstituteVariables_SurroundingTextPreserved(t *testing.T) {
	p := &Pipeline{Elements: []PipelineElement{
		{Step: &Step{Name: "s1", Command: "before-{{key}}-after"}},
	}}
	if err := SubstituteVariables(p, map[string]string{"key": "VALUE"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Elements[0].Step.Command != "before-VALUE-after" {
		t.Errorf("surrounding text not preserved, got %q", p.Elements[0].Step.Command)
	}
}

func TestIntegration_VariableSubstitutionBeforeValidation(t *testing.T) {
	// Variables in templatable fields (prompt, command, project_dir, condition.prompt, condition.file)
	// are resolved before ValidatePipeline runs, so validation sees the final resolved values.
	content := `
variables:
  spec-name: "my-spec"
  project-path: "/tmp/proj"
steps:
  - name: "step1"
    agent: "claude"
    prompt: "Read specs from {{spec-name}} and implement in {{project-path}}"
    project_dir: "{{project-path}}"
    condition:
      prompt: "Are there issues in {{spec-name}}?"
      goto: "step1"
  - name: "step2"
    command: "cd {{project-path}} && go test ./..."
`
	path := writeTempPipeline(t, content)
	p, vars, err := ParsePipelineRaw(path)
	if err != nil {
		t.Fatalf("ParsePipelineRaw error: %v", err)
	}
	if err := SubstituteVariables(p, vars); err != nil {
		t.Fatalf("SubstituteVariables error: %v", err)
	}
	if err := ValidatePipeline(p); err != nil {
		t.Fatalf("ValidatePipeline should pass after substitution, got: %v", err)
	}
	// Verify resolved values
	s1 := p.Elements[0].Step
	if s1.Prompt != "Read specs from my-spec and implement in /tmp/proj" {
		t.Errorf("unexpected prompt: %q", s1.Prompt)
	}
	if s1.ProjectDir != "/tmp/proj" {
		t.Errorf("unexpected project_dir: %q", s1.ProjectDir)
	}
	if s1.Condition.Prompt != "Are there issues in my-spec?" {
		t.Errorf("unexpected condition prompt: %q", s1.Condition.Prompt)
	}
}

// --- Property-based tests ---

// Feature: pipeline-variables, Property 1: Variables YAML round trip
func TestVariablesYAMLRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random variable map with valid keys and values
		numVars := rapid.IntRange(0, 5).Draw(rt, "numVars")
		origVars := make(map[string]string, numVars)
		for i := 0; i < numVars; i++ {
			key := genVarKey(rt, fmt.Sprintf("key-%d", i))
			val := rapid.StringMatching(`[a-z0-9/._-]{0,20}`).Draw(rt, fmt.Sprintf("val-%d", i))
			origVars[key] = val
		}

		// Serialize to YAML with a minimal valid pipeline
		raw := rawPipeline{
			Variables: origVars,
			Steps: []PipelineElement{
				{Step: &Step{Name: "s1", Command: "echo hi"}},
			},
		}
		data, err := yaml.Marshal(raw)
		if err != nil {
			rt.Fatalf("marshal error: %v", err)
		}

		// Write to temp file and parse back
		f, err := os.CreateTemp("", "pipeline-vars-*.yaml")
		if err != nil {
			rt.Fatalf("temp file error: %v", err)
		}
		defer os.Remove(f.Name())
		f.Write(data)
		f.Close()

		_, parsedVars, parseErr := ParsePipelineRaw(f.Name())
		if parseErr != nil {
			rt.Fatalf("parse error: %v\nYAML:\n%s", parseErr, data)
		}

		// Verify all original vars are present and equal
		for k, v := range origVars {
			if parsedVars[k] != v {
				rt.Fatalf("variable %q: expected %q, got %q\nYAML:\n%s", k, v, parsedVars[k], data)
			}
		}
		// Verify no extra vars added
		if len(parsedVars) != len(origVars) {
			rt.Fatalf("variable count mismatch: expected %d, got %d", len(origVars), len(parsedVars))
		}
	})
}

// Feature: pipeline-variables, Property 2: CLI variable parsing with last-wins semantics
func TestCLIVarParsingLastWins(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numFlags := rapid.IntRange(1, 10).Draw(rt, "numFlags")
		flags := make([]string, numFlags)
		for i := 0; i < numFlags; i++ {
			key := genVarKey(rt, fmt.Sprintf("flag-key-%d", i))
			val := rapid.StringMatching(`[a-z0-9]{0,10}`).Draw(rt, fmt.Sprintf("flag-val-%d", i))
			flags[i] = key + "=" + val
		}

		result, err := ParseCLIVars(flags)
		if err != nil {
			rt.Fatalf("unexpected error: %v", err)
		}

		// For each key, verify the value is the last occurrence
		lastVals := make(map[string]string)
		for _, flag := range flags {
			idx := strings.Index(flag, "=")
			k := flag[:idx]
			v := flag[idx+1:]
			lastVals[k] = v
		}
		for k, v := range lastVals {
			if result[k] != v {
				rt.Fatalf("key %q: expected last value %q, got %q", k, v, result[k])
			}
		}
	})
}

// Feature: pipeline-variables, Property 3: Variable merge — CLI takes precedence
func TestVariableMergeCLIPrecedence(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numYAML := rapid.IntRange(0, 5).Draw(rt, "numYAML")
		numCLI := rapid.IntRange(0, 5).Draw(rt, "numCLI")

		yamlVars := make(map[string]string, numYAML)
		for i := 0; i < numYAML; i++ {
			k := genVarKey(rt, fmt.Sprintf("yaml-k-%d", i))
			v := rapid.StringMatching(`[a-z]{1,5}`).Draw(rt, fmt.Sprintf("yaml-v-%d", i))
			yamlVars[k] = "yaml-" + v
		}
		cliVars := make(map[string]string, numCLI)
		for i := 0; i < numCLI; i++ {
			k := genVarKey(rt, fmt.Sprintf("cli-k-%d", i))
			v := rapid.StringMatching(`[a-z]{1,5}`).Draw(rt, fmt.Sprintf("cli-v-%d", i))
			cliVars[k] = "cli-" + v
		}

		merged := MergeVariables(yamlVars, cliVars)

		// All yaml keys present
		for k, v := range yamlVars {
			if _, inCLI := cliVars[k]; !inCLI {
				if merged[k] != v {
					rt.Fatalf("yaml-only key %q: expected %q, got %q", k, v, merged[k])
				}
			}
		}
		// All CLI keys present with CLI value
		for k, v := range cliVars {
			if merged[k] != v {
				rt.Fatalf("CLI key %q: expected CLI value %q, got %q", k, v, merged[k])
			}
		}
		// Total key count: union of both maps
		allKeys := make(map[string]bool)
		for k := range yamlVars {
			allKeys[k] = true
		}
		for k := range cliVars {
			allKeys[k] = true
		}
		if len(merged) != len(allKeys) {
			rt.Fatalf("merged map size: expected %d, got %d", len(allKeys), len(merged))
		}
	})
}

// Feature: pipeline-variables, Property 4: Malformed --var flag rejection
func TestMalformedVarFlagRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate strings that do NOT contain '='
		s := rapid.StringMatching(`[a-z0-9]{1,20}`).Draw(rt, "noequals")
		_, err := ParseCLIVars([]string{s})
		if err == nil {
			rt.Fatalf("expected error for flag without '=': %q", s)
		}
	})
}

// Feature: pipeline-variables, Property 5: Template substitution correctness
func TestSubstitutionCorrectness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a variable map
		numVars := rapid.IntRange(1, 4).Draw(rt, "numVars")
		vars := make(map[string]string, numVars)
		var keys []string
		for i := 0; i < numVars; i++ {
			k := fmt.Sprintf("var%d", i)
			v := rapid.StringMatching(`[a-z0-9]{1,10}`).Draw(rt, fmt.Sprintf("varval-%d", i))
			vars[k] = v
			keys = append(keys, k)
		}

		// Build a prompt using one or more of those keys
		chosenKey := rapid.SampledFrom(keys).Draw(rt, "chosenKey")
		prompt := "prefix-{{" + chosenKey + "}}-suffix"
		surrounding := "some text before {{" + chosenKey + "}} some text after"

		p := &Pipeline{Elements: []PipelineElement{
			{Step: &Step{Name: "s1", Agent: "claude", Prompt: prompt}},
			{Step: &Step{Name: "s2", Command: surrounding}},
		}}

		if err := SubstituteVariables(p, vars); err != nil {
			rt.Fatalf("unexpected SubstituteVariables error: %v", err)
		}

		// Placeholder should be replaced
		if strings.Contains(p.Elements[0].Step.Prompt, "{{"+chosenKey+"}}") {
			rt.Fatalf("placeholder not replaced in Prompt: %q", p.Elements[0].Step.Prompt)
		}
		// Value should appear
		if !strings.Contains(p.Elements[0].Step.Prompt, vars[chosenKey]) {
			rt.Fatalf("resolved value not in Prompt: %q (expected %q)", p.Elements[0].Step.Prompt, vars[chosenKey])
		}
		// Surrounding text preserved
		if !strings.HasPrefix(p.Elements[0].Step.Prompt, "prefix-") {
			rt.Fatalf("prefix not preserved: %q", p.Elements[0].Step.Prompt)
		}
		if !strings.HasSuffix(p.Elements[0].Step.Prompt, "-suffix") {
			rt.Fatalf("suffix not preserved: %q", p.Elements[0].Step.Prompt)
		}
	})
}

// Feature: pipeline-variables, Property 6: Undefined variable detection reports all undefined keys
func TestUndefinedVariableDetection(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		numUndefined := rapid.IntRange(1, 3).Draw(rt, "numUndefined")
		undefinedKeys := make([]string, numUndefined)
		for i := 0; i < numUndefined; i++ {
			undefinedKeys[i] = fmt.Sprintf("undef%d", i)
		}

		elements := make([]PipelineElement, numUndefined)
		for i, k := range undefinedKeys {
			elements[i] = PipelineElement{
				Step: &Step{Name: fmt.Sprintf("s%d", i), Agent: "claude", Prompt: "{{" + k + "}}"},
			}
		}
		p := &Pipeline{Elements: elements}

		err := SubstituteVariables(p, map[string]string{})
		if err == nil {
			rt.Fatalf("expected error for undefined variables")
		}
		for _, k := range undefinedKeys {
			if !strings.Contains(err.Error(), k) {
				rt.Fatalf("error should contain %q, got: %v", k, err)
			}
		}
	})
}

// --- Helpers ---

// genVarKey generates a valid variable key matching [a-z0-9-]+
func genVarKey(rt *rapid.T, drawName string) string {
	return rapid.StringMatching(`[a-z][a-z0-9-]{0,9}`).Draw(rt, drawName)
}
