package api

import (
	"regexp"
	"strings"
)

// validPipelineTypes are the subdirectory categories under the pipelines directory.
var validPipelineTypes = map[string]bool{
	"tasks":     true,
	"processes": true,
	"workflows": true,
}

// variableKeyRe restricts workflow variable keys to a safe character set,
// matching Requirements 3.4/3.7 and design.md's Property 2.
var variableKeyRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

const maxVariableValueLen = 1000
const maxVariablesEntries = 50

// isValidPipelineType reports whether t is one of "tasks", "processes", or "workflows".
func isValidPipelineType(t string) bool {
	return validPipelineTypes[t]
}

// isValidVariableKey reports whether key matches ^[a-zA-Z0-9_-]{1,100}$.
func isValidVariableKey(key string) bool {
	return variableKeyRe.MatchString(key)
}

// isValidVariableValue reports whether value does not exceed 1000 characters.
func isValidVariableValue(value string) bool {
	return len(value) <= maxVariableValueLen
}

// isValidVariablesMap reports whether vars contains at most 50 entries and
// every key/value pair is individually valid.
func isValidVariablesMap(vars map[string]string) bool {
	if len(vars) > maxVariablesEntries {
		return false
	}
	for k, v := range vars {
		if !isValidVariableKey(k) || !isValidVariableValue(v) {
			return false
		}
	}
	return true
}

// parsePipelineRef splits a pipeline reference of the form "type/name" into
// its type and name components. It returns ok=false if ref does not contain
// exactly one "/" separator, either component is empty, or the type is not
// one of the valid pipeline types.
func parsePipelineRef(ref string) (pipelineType, name string, ok bool) {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	pipelineType, name = parts[0], parts[1]
	if pipelineType == "" || name == "" || strings.Contains(name, "/") {
		return "", "", false
	}
	if !isValidPipelineType(pipelineType) {
		return "", "", false
	}
	return pipelineType, name, true
}

// buildWorkflowCmd constructs the container command for running a pipeline:
// ["trayline", "run", pipeline, "--var", "k1=v1", "--var", "k2=v2", ...].
// Iteration order over variables is unspecified since --var flags are
// order-independent.
func buildWorkflowCmd(pipeline string, variables map[string]string) []string {
	cmd := []string{"trayline", "run", pipeline}
	for key, value := range variables {
		cmd = append(cmd, "--var", key+"="+value)
	}
	return cmd
}
