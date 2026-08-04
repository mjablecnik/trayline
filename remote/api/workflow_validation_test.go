package api

import (
	"fmt"
	"regexp"
	"testing"

	"pgregory.net/rapid"
)

// Feature: 010-dashboard-workflow-runner, Property 2: Workflow input validation
func TestWorkflowValidation_VariableKey(t *testing.T) {
	want := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)
	rapid.Check(t, func(t *rapid.T) {
		key := rapid.String().Draw(t, "key")
		got := isValidVariableKey(key)
		expected := want.MatchString(key)
		if got != expected {
			t.Fatalf("isValidVariableKey(%q) = %v, want %v", key, got, expected)
		}
	})
}

func TestWorkflowValidation_VariableValue(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		value := rapid.String().Draw(t, "value")
		got := isValidVariableValue(value)
		expected := len(value) <= 1000
		if got != expected {
			t.Fatalf("isValidVariableValue(len=%d) = %v, want %v", len(value), got, expected)
		}
	})
}

func TestWorkflowValidation_VariablesMapEntryCount(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 60).Draw(t, "n")
		vars := make(map[string]string, n)
		for i := 0; i < n; i++ {
			vars[fmt.Sprintf("key%d", i)] = "v"
		}
		got := isValidVariablesMap(vars)
		expected := len(vars) <= 50
		if got != expected {
			t.Fatalf("isValidVariablesMap(entries=%d) = %v, want %v", len(vars), got, expected)
		}
	})
}

func TestWorkflowValidation_PipelineType(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "type")
		got := isValidPipelineType(s)
		expected := s == "tasks" || s == "processes" || s == "workflows"
		if got != expected {
			t.Fatalf("isValidPipelineType(%q) = %v, want %v", s, got, expected)
		}
	})
}

func TestWorkflowValidation_ParsePipelineRef(t *testing.T) {
	cases := []struct {
		ref     string
		wantOK  bool
		wantTyp string
		wantNm  string
	}{
		{"processes/4-create-code", true, "processes", "4-create-code"},
		{"tasks/foo", true, "tasks", "foo"},
		{"workflows/bar", true, "workflows", "bar"},
		{"invalid/foo", false, "", ""},
		{"noslash", false, "", ""},
		{"processes/", false, "", ""},
		{"/foo", false, "", ""},
		{"tasks/foo/bar", false, "", ""},
	}
	for _, c := range cases {
		typ, name, ok := parsePipelineRef(c.ref)
		if ok != c.wantOK || (ok && (typ != c.wantTyp || name != c.wantNm)) {
			t.Errorf("parsePipelineRef(%q) = (%q, %q, %v), want (%q, %q, %v)", c.ref, typ, name, ok, c.wantTyp, c.wantNm, c.wantOK)
		}
	}
}

// Feature: 010-dashboard-workflow-runner, Property 5: Command construction correctness
func TestBuildWorkflowCmd(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pipeline := rapid.StringMatching(`[a-z]+/[a-z0-9-]+`).Draw(t, "pipeline")
		n := rapid.IntRange(0, 10).Draw(t, "n")
		variables := make(map[string]string, n)
		for i := 0; i < n; i++ {
			key := fmt.Sprintf("k%d", i)
			value := rapid.StringMatching(`[a-zA-Z0-9]{0,10}`).Draw(t, fmt.Sprintf("v%d", i))
			variables[key] = value
		}

		cmd := buildWorkflowCmd(pipeline, variables)

		if len(cmd) < 3 || cmd[0] != "trayline" || cmd[1] != "run" || cmd[2] != pipeline {
			t.Fatalf("buildWorkflowCmd prefix = %v, want [trayline run %s ...]", cmd, pipeline)
		}

		rest := cmd[3:]
		if len(rest) != 2*len(variables) {
			t.Fatalf("expected %d trailing args (2 per variable), got %d: %v", 2*len(variables), len(rest), rest)
		}

		gotFlags := make(map[string]string, len(variables))
		for i := 0; i < len(rest); i += 2 {
			if rest[i] != "--var" {
				t.Fatalf("expected --var flag at index %d, got %q", i, rest[i])
			}
			kv := rest[i+1]
			eq := -1
			for j, r := range kv {
				if r == '=' {
					eq = j
					break
				}
			}
			if eq < 0 {
				t.Fatalf("--var value %q missing '='", kv)
			}
			gotFlags[kv[:eq]] = kv[eq+1:]
		}
		for k, v := range variables {
			if gotFlags[k] != v {
				t.Fatalf("variable %q: got value %q, want %q", k, gotFlags[k], v)
			}
		}
	})
}
