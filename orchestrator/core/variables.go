package core

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// placeholderRegex matches {{variable-name}} placeholders in templatable fields.
var placeholderRegex = regexp.MustCompile(`\{\{([a-z0-9-]+)\}\}`)

// validKeyRegex matches valid variable key names: lowercase letters, digits, and hyphens.
var validKeyRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

// ParseCLIVars parses --var flag values (each in "key=value" format) into a map.
// Last occurrence of a key wins. Returns an error if any value lacks an '=' separator
// or if any key does not match [a-z0-9-]+.
func ParseCLIVars(flags []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, flag := range flags {
		idx := strings.Index(flag, "=")
		if idx == -1 {
			return nil, fmt.Errorf("invalid --var flag %q: must be in key=value format", flag)
		}
		key := flag[:idx]
		val := flag[idx+1:]
		if !validKeyRegex.MatchString(key) {
			return nil, fmt.Errorf("invalid --var key %q: must contain only lowercase letters, digits, and hyphens", key)
		}
		result[key] = val
	}
	return result, nil
}

// MergeVariables merges YAML-defined variables with CLI overrides.
// CLI values take precedence over YAML values. Keys from both maps are included.
func MergeVariables(yamlVars, cliVars map[string]string) map[string]string {
	result := make(map[string]string, len(yamlVars)+len(cliVars))
	for k, v := range yamlVars {
		result[k] = v
	}
	for k, v := range cliVars {
		result[k] = v
	}
	return result
}

// FindPlaceholders returns all unique {{variable-name}} placeholder keys found in s.
func FindPlaceholders(s string) []string {
	matches := placeholderRegex.FindAllStringSubmatch(s, -1)
	seen := make(map[string]bool)
	var keys []string
	for _, m := range matches {
		key := m[1]
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// ResolveString replaces all {{key}} placeholders in s with values from vars.
// Placeholders whose keys are not in vars are left unchanged.
func ResolveString(s string, vars map[string]string) string {
	return placeholderRegex.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-2]
		if val, ok := vars[key]; ok {
			return val
		}
		return match
	})
}

// ResolveCommand is like ResolveString, but single-quotes each substituted
// value using POSIX shell quoting rules before splicing it in. It must be
// used for the step Command field specifically, since Command is the only
// templatable field whose resolved value is later executed via `sh -c`
// (see engine/executor.go RunCommand) — a variable value containing shell
// metacharacters (";", "|", "`", "$(", ...) would otherwise break out of its
// intended position and run as an independent command. Prompt/Skip/Condition
// fields are never passed to a shell, so they keep using plain ResolveString.
func ResolveCommand(s string, vars map[string]string) string {
	return placeholderRegex.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-2]
		if val, ok := vars[key]; ok {
			return shellQuote(val)
		}
		return match
	})
}

// shellQuote wraps s in single quotes, escaping any embedded single quote as
// '\'' (close quote, escaped literal quote, reopen quote) — the standard
// POSIX-safe way to make an arbitrary string immune to shell interpretation.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SubstituteVariables replaces all {{variable-name}} placeholders in the
// templatable fields of the pipeline with values from vars. Templatable fields
// are: step Prompt, Command, ProjectDir, condition Prompt, condition File —
// applied to both top-level steps and steps/conditions inside loop blocks (recursively).
//
// Returns an error listing all undefined variable names if any placeholder
// references a key not present in vars. Returns nil if all placeholders resolved.
func SubstituteVariables(p *Pipeline, vars map[string]string) error {
	var undefined []string
	seen := make(map[string]bool)

	// collectUndefined records keys referenced in s that are absent from vars.
	collectUndefined := func(s string) {
		for _, key := range FindPlaceholders(s) {
			if _, ok := vars[key]; !ok && !seen[key] {
				seen[key] = true
				undefined = append(undefined, key)
			}
		}
	}

	// resolveField collects undefined keys then replaces defined ones in-place.
	resolveField := func(s *string) {
		collectUndefined(*s)
		*s = ResolveString(*s, vars)
	}

	// resolveCommandField is resolveField's counterpart for the Command field,
	// which reaches a shell (see ResolveCommand) — substituted values must be
	// shell-quoted, not spliced in raw.
	resolveCommandField := func(s *string) {
		collectUndefined(*s)
		*s = ResolveCommand(*s, vars)
	}

	var substituteElements func(elements []PipelineElement)
	substituteElements = func(elements []PipelineElement) {
		for i := range elements {
			elem := &elements[i]
			if elem.Step != nil {
				s := elem.Step
				resolveField(&s.Prompt)
				resolveCommandField(&s.Command)
				resolveField(&s.ProjectDir)
				resolveField(&s.Skip)
				if s.Condition != nil {
					resolveField(&s.Condition.Prompt)
					resolveField(&s.Condition.File)
					resolveField(&s.Condition.Matches)
					resolveField(&s.Condition.NotMatches)
				}
			}
			if elem.Loop != nil {
				l := elem.Loop
				resolveField(&l.Condition.Prompt)
				resolveField(&l.Condition.File)
				resolveField(&l.Condition.Matches)
				resolveField(&l.Condition.NotMatches)
				substituteElements(l.Elements)
			}
		}
	}

	substituteElements(p.Elements)

	if len(undefined) > 0 {
		sort.Strings(undefined)
		return fmt.Errorf("undefined variables: %s", strings.Join(undefined, ", "))
	}
	return nil
}
