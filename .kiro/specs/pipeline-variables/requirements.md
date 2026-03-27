# Requirements Document

## Introduction

Pipeline Variables extends the Agent Orchestrator with support for user-defined template variables in pipeline YAML files. Users define key-value pairs in a `variables` section of the Pipeline_File and reference them using `{{variable-name}}` mustache-style placeholders in templatable fields. Variables can be overridden at runtime via the `--var key=value` CLI flag. The Orchestrator resolves all placeholders after YAML parsing but before pipeline validation, failing fast if any placeholder references an undefined variable.

## Glossary

- **Orchestrator**: The Go CLI binary that manages sequential execution of agent steps (existing system)
- **Pipeline_File**: A YAML file that defines the sequence of named steps and loops for the Orchestrator to execute (existing)
- **Variable_Map**: A flat map of string key-value pairs defined in the `variables` section of the Pipeline_File
- **Variable_Placeholder**: A `{{variable-name}}` token embedded in a templatable field, referencing a key in the Variable_Map
- **Template_Substitution**: The process of replacing all Variable_Placeholders in templatable fields with their resolved values
- **Templatable_Field**: A Pipeline_File field that supports Variable_Placeholder resolution: `prompt`, `command`, `project_dir`, `condition.prompt`, and `condition.file`
- **CLI_Variable_Override**: A `--var key=value` flag passed on the command line that sets or overrides a variable value
- **Resolved_Variables**: The final merged Variable_Map after applying CLI_Variable_Overrides on top of YAML-defined variables

## Requirements

### Requirement 1: Variable Definition in Pipeline YAML

**User Story:** As a developer, I want to define variables in my pipeline YAML file, so that I can parameterize prompts and paths without duplicating values across steps.

#### Acceptance Criteria

1. THE Orchestrator SHALL support an optional top-level `variables` key in the Pipeline_File containing a flat map of string key-value pairs
2. WHEN the `variables` section is present, THE Orchestrator SHALL parse each entry as a string key mapped to a string value
3. WHEN the `variables` section is absent from the Pipeline_File, THE Orchestrator SHALL treat the Variable_Map as empty and continue execution
4. WHEN a variable value is an empty string, THE Orchestrator SHALL accept the variable as valid and use the empty string as its resolved value
5. THE Orchestrator SHALL accept variable keys containing lowercase letters, digits, and hyphens

### Requirement 2: CLI Variable Override

**User Story:** As a developer, I want to override pipeline variables from the command line, so that I can customize pipeline behavior per run without editing the YAML file.

#### Acceptance Criteria

1. THE Orchestrator SHALL accept a repeatable `--var` flag in the format `key=value`
2. WHEN one or more `--var` flags are provided, THE Orchestrator SHALL merge CLI_Variable_Overrides into the Variable_Map, with CLI values taking precedence over YAML-defined values
3. WHEN a `--var` flag specifies a key that does not exist in the YAML `variables` section, THE Orchestrator SHALL add the key-value pair to the Resolved_Variables
4. IF a `--var` flag does not contain an `=` separator, THEN THE Orchestrator SHALL exit with a non-zero exit code and print a descriptive error message to stderr
5. WHEN multiple `--var` flags specify the same key, THE Orchestrator SHALL use the value from the last occurrence

### Requirement 3: Template Substitution

**User Story:** As a developer, I want `{{variable-name}}` placeholders in my pipeline fields to be replaced with variable values, so that I can write reusable pipeline templates.

#### Acceptance Criteria

1. THE Orchestrator SHALL replace all Variable_Placeholders in Templatable_Fields with their corresponding values from the Resolved_Variables
2. THE Orchestrator SHALL perform Template_Substitution on the following fields: step `prompt`, step `command`, step `project_dir`, condition `prompt`, and condition `file`
3. THE Orchestrator SHALL perform Template_Substitution after YAML parsing and variable merging but before pipeline validation
4. WHEN a Templatable_Field contains multiple Variable_Placeholders, THE Orchestrator SHALL replace each placeholder independently
5. WHEN a Variable_Placeholder appears within surrounding text, THE Orchestrator SHALL replace only the placeholder portion and preserve the surrounding text
6. THE Orchestrator SHALL apply Template_Substitution to fields in both top-level steps and steps inside loop blocks

### Requirement 4: Undefined Variable Validation

**User Story:** As a developer, I want the orchestrator to fail immediately if a placeholder references an undefined variable, so that I catch configuration errors before execution begins.

#### Acceptance Criteria

1. WHEN a Templatable_Field contains a Variable_Placeholder whose key is not present in the Resolved_Variables, THE Orchestrator SHALL exit with a non-zero exit code and print a descriptive error message to stderr identifying the undefined variable name
2. THE Orchestrator SHALL detect all undefined variables across all Templatable_Fields before reporting the error
3. THE Orchestrator SHALL perform undefined variable validation before pipeline validation and before executing any steps

### Requirement 5: Dry Run with Variables

**User Story:** As a developer, I want the dry-run output to show the resolved variable values, so that I can verify template substitution before running the pipeline.

#### Acceptance Criteria

1. WHEN the `--dry-run` flag is provided, THE Orchestrator SHALL display the Resolved_Variables and their values before printing the pipeline steps
2. WHEN the `--dry-run` flag is provided, THE Orchestrator SHALL display all Templatable_Fields with Variable_Placeholders already resolved to their final values

### Requirement 6: Variable Substitution in Loop Condition Fields

**User Story:** As a developer, I want variables to work in loop condition prompts and file paths, so that I can parameterize iterative workflows.

#### Acceptance Criteria

1. THE Orchestrator SHALL perform Template_Substitution on loop-level condition `prompt` fields
2. THE Orchestrator SHALL perform Template_Substitution on loop-level condition `file` fields
