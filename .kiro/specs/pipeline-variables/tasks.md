# Implementation Plan: Pipeline Variables

## Overview

Extend the Agent Orchestrator with user-defined template variables in pipeline YAML files. Implementation adds a new `variables.go` file for all variable logic, modifies `pipeline.go` to split parsing from validation, updates `main.go` for `--var` CLI flags, and updates `executor.go` for dry-run variable display. All changes are in Go within the existing `orchestrator/` directory.

## Tasks

- [x] 1. Add `variables` field to rawPipeline and split ParsePipeline into parse + validate phases
  - [x] 1.1 Add `Variables map[string]string` field with `yaml:"variables"` tag to `rawPipeline` struct in `pipeline.go`
    - _Requirements: 1.1, 1.2_
  - [x] 1.2 Extract a new exported `ParsePipelineRaw(path string) (*Pipeline, map[string]string, error)` function that parses YAML and returns the pipeline and variable map without running validation
    - Return empty map (not nil) when `variables` section is absent
    - _Requirements: 1.3_
  - [x] 1.3 Export validation as `ValidatePipeline(p *Pipeline) error` (rename existing `validatePipeline`)
    - Update all internal callers to use the new name
    - _Requirements: 3.3_
  - [x] 1.4 Update `ParsePipeline` to call `ParsePipelineRaw` then `ValidatePipeline` to preserve backward compatibility
    - _Requirements: 1.1, 1.2, 1.3_

- [x] 2. Create `variables.go` with core variable functions
  - [x] 2.1 Implement `ParseCLIVars(flags []string) (map[string]string, error)`
    - Split each flag on the first `=`; error if no `=` found
    - Last occurrence of a key wins
    - _Requirements: 2.1, 2.4, 2.5_
  - [x] 2.2 Implement `MergeVariables(yamlVars, cliVars map[string]string) map[string]string`
    - CLI values take precedence over YAML values
    - Keys from both maps are included in the result
    - _Requirements: 2.2, 2.3_
  - [x] 2.3 Implement `FindPlaceholders(s string) []string`
    - Use regex `\{\{([a-z0-9-]+)\}\}` to extract placeholder keys
    - Return unique keys found in the string
    - _Requirements: 3.1_
  - [x] 2.4 Implement `ResolveString(s string, vars map[string]string) string`
    - Replace all `{{key}}` placeholders with corresponding values from vars
    - Preserve surrounding text
    - _Requirements: 3.4, 3.5_
  - [x] 2.5 Implement `SubstituteVariables(p *Pipeline, vars map[string]string) error`
    - Walk all templatable fields: step `Prompt`, `Command`, `ProjectDir`, condition `Prompt`, condition `File`
    - Apply to both top-level steps and steps/conditions inside loop blocks
    - Collect all undefined variable references across all fields
    - Return error listing all undefined variables, or nil if all resolved
    - _Requirements: 3.1, 3.2, 3.6, 4.1, 4.2, 4.3, 6.1, 6.2_
  - [x]* 2.6 Write property test for CLI variable parsing (Property 2: CLI variable parsing with last-wins semantics)
    - **Property 2: CLI variable parsing with last-wins semantics**
    - **Validates: Requirements 2.1, 2.5**
  - [x]* 2.7 Write property test for malformed --var flag rejection (Property 4: Malformed --var flag rejection)
    - **Property 4: Malformed --var flag rejection**
    - **Validates: Requirements 2.4**
  - [x]* 2.8 Write property test for variable merge precedence (Property 3: Variable merge — CLI takes precedence)
    - **Property 3: Variable merge — CLI takes precedence**
    - **Validates: Requirements 2.2, 2.3**
  - [x]* 2.9 Write property test for template substitution correctness (Property 5: Template substitution correctness)
    - **Property 5: Template substitution correctness**
    - **Validates: Requirements 3.1, 3.2, 3.4, 3.5, 3.6, 6.1, 6.2**
  - [x]* 2.10 Write property test for undefined variable detection (Property 6: Undefined variable detection reports all undefined keys)
    - **Property 6: Undefined variable detection reports all undefined keys**
    - **Validates: Requirements 4.1, 4.2**

- [x] 3. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Integrate `--var` flag into `main.go` and wire the resolution flow
  - [x] 4.1 Add a custom `varFlags` type implementing `flag.Value` and register `--var` flag in `run()`
    - Update `usageText()` to document the `--var` flag
    - _Requirements: 2.1_
  - [x] 4.2 Replace `ParsePipeline` call with `ParsePipelineRaw` + `ParseCLIVars` + `MergeVariables` + `SubstituteVariables` + `ValidatePipeline` sequence
    - Exit with code 1 and descriptive stderr message on any variable error
    - Pass resolved variables to the Executor for dry-run display
    - _Requirements: 1.1, 1.2, 1.3, 2.1, 2.2, 2.3, 2.4, 3.3, 4.1, 4.2, 4.3_
  - [x] 4.3 Add `ResolvedVars map[string]string` field to `Executor` struct in `executor.go`
    - _Requirements: 5.1_

- [x] 5. Update dry-run output to display resolved variables
  - [x] 5.1 Modify `printDryRun()` in `executor.go` to print a "Variables" section before steps, listing each resolved variable key and value
    - Only print the section when `ResolvedVars` is non-empty
    - Templatable fields are already resolved at this point, so step output shows final values
    - _Requirements: 5.1, 5.2_
  - [x]* 5.2 Write property test for dry-run resolved values display (Property 7: Dry-run shows resolved values, not placeholders)
    - **Property 7: Dry-run shows resolved values, not placeholders**
    - **Validates: Requirements 5.1, 5.2**

- [x] 6. Add YAML round-trip property test and unit tests
  - [x]* 6.1 Write property test for variables YAML round trip (Property 1: Variables YAML round trip)
    - **Property 1: Variables YAML round trip**
    - **Validates: Requirements 1.1, 1.2**
  - [x]* 6.2 Write unit tests for variable edge cases in `variables_test.go`
    - Parse pipeline YAML with and without `variables` section
    - Parse `--var key=val=ue` (value contains `=`)
    - Empty string variable value resolves to empty string
    - Multiple placeholders in one field
    - Substitution in all templatable field types including loop condition fields
    - Multiple undefined variables across different fields reported in single error
    - _Requirements: 1.3, 1.4, 1.5, 2.5, 3.4, 3.5, 4.1, 4.2, 6.1, 6.2_

- [x] 7. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties using `pgregory.net/rapid`
- Unit tests validate specific examples and edge cases
- All new code goes in `orchestrator/` directory, keeping the flat structure under the 8-file threshold
