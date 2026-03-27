# Requirements Document

## Introduction

The Agent Orchestrator is a Go CLI program that automates a multi-step AI agent workflow. It sequentially executes `agent-docker` commands and arbitrary shell commands to create code, test it, generate tests, perform code review, and apply fixes. The orchestrator reads a pipeline definition (sequence of named steps — either agent steps or command steps), executes each step as a subprocess, checks the output/exit code after each step, and decides whether to proceed or abort. The pipeline supports two flow-control mechanisms: (1) loops — a group of named steps that repeat up to `max_iterations` times, with an LLM-based condition evaluated after each iteration to decide whether to continue or break; (2) step-level conditions — an LLM evaluation after a step that either acts as a continue/break gate (no goto) or jumps to a named target step (with goto). Conditions can read a specified file or use the step's captured output as input to the LLM. This enables arbitrary iterative refinement workflows (e.g., review-fix cycles) and conditional branching without hardcoding any specific use case. LLM requests are sent via the OpenRouter API. The orchestrator compiles into a single static binary and lives in its own `orchestrator/` directory.

## Glossary

- **Orchestrator**: The Go CLI binary that manages sequential execution of agent steps
- **Agent_Docker**: The existing `agent-docker` bash script that runs AI agents inside sandboxed Docker containers
- **Pipeline**: An ordered sequence of named steps and loops, where each element is either a step or a loop block
- **Step**: A single unit of work in the pipeline; either an agent step (agent type + prompt, executed via agent-docker) or a command step (shell command, executed via `sh -c`)
- **Step_Name**: A unique string identifier for a Step, used as a target for Goto jumps and for referencing within the pipeline
- **Agent_Type**: Either `kiro` or `claude`, selecting which AI agent to run inside the Docker sandbox; required for agent steps, absent for command steps
- **Exit_Code**: The numeric return code from a completed `agent-docker` process, where 0 indicates success
- **Step_Output**: The combined stdout and stderr captured from a single step execution (agent-docker or shell command)
- **Pipeline_File**: A YAML file that defines the sequence of named steps and loops for the orchestrator to execute
- **Loop**: A pipeline element that groups one or more named Steps into a repeatable block, controlled by a Condition and a Max_Iterations safety limit
- **Condition**: An LLM-based evaluation that sends content (from a file or from the previous step's output) with a custom prompt to the LLM; used in loops (to decide whether to continue iterating) and on individual steps (as a continue/break gate or a one-shot goto jump)
- **Condition_File**: An optional file path (relative to the project directory) whose content is sent to the LLM for Condition evaluation; if omitted, the Step_Output of the step the condition is attached to is used instead
- **Condition_Prompt**: A custom English-language prompt string sent to the LLM along with the Condition input; the prompt must be phrased so that a `true` answer means "yes" (continue / take goto) and a `false` answer means "no" (break / continue to next step)
- **Goto**: An optional reference to a Step_Name in a step-level Condition; when present and the LLM returns `true`, the Orchestrator jumps to that step; when absent, `true` means continue to the next step and `false` means stop the pipeline
- **LLM_Decision**: A structured boolean response from the LLM: `true` or `false`
- **Max_Iterations**: A per-Loop upper bound on the number of iterations, defined in the Pipeline_File, preventing infinite loops
- **OpenRouter_API**: The external API service used to send LLM requests, authenticated via the OPENROUTER_API_KEY environment variable

## Requirements

### Requirement 1: Pipeline Definition

**User Story:** As a developer, I want to define a sequence of agent steps in a YAML file, so that I can reuse and modify workflows without recompiling the orchestrator.

#### Acceptance Criteria

1. THE Orchestrator SHALL accept a `--pipeline` flag specifying the path to a Pipeline_File
2. THE Orchestrator SHALL parse the Pipeline_File as YAML containing an ordered list of steps
3. WHEN a Step in the Pipeline_File is parsed, THE Orchestrator SHALL extract the Step_Name and either the Agent_Type and prompt text (agent step) or the command string (command step), plus the optional project directory
4. WHEN the Pipeline_File does not exist or is not valid YAML, THE Orchestrator SHALL exit with a non-zero Exit_Code and print a descriptive error message to stderr
5. THE Orchestrator SHALL validate that each agent step specifies an Agent_Type of either `kiro` or `claude`
6. IF an agent step specifies an invalid Agent_Type, THEN THE Orchestrator SHALL exit with a non-zero Exit_Code and print an error identifying the invalid Step
7. THE Orchestrator SHALL validate that each Step has a unique Step_Name within the Pipeline
8. IF two or more Steps share the same Step_Name, THEN THE Orchestrator SHALL exit with a non-zero Exit_Code and print an error identifying the duplicate name
9. THE Orchestrator SHALL validate that each Step is either an agent step (has `agent` and `prompt`) or a command step (has `command`), but not both
10. IF a Step has both `agent` and `command` fields, or has neither, THEN THE Orchestrator SHALL exit with a non-zero Exit_Code and print a descriptive error message to stderr

### Requirement 2: Sequential Step Execution

**User Story:** As a developer, I want the orchestrator to run each agent step one after another, so that later steps can build on the output of earlier steps.

#### Acceptance Criteria

1. THE Orchestrator SHALL execute Pipeline steps in the order they appear in the Pipeline_File
2. WHEN executing an agent step, THE Orchestrator SHALL invoke Agent_Docker as a shell subprocess with the Step's Agent_Type and prompt as arguments
3. WHEN an agent step specifies a project directory, THE Orchestrator SHALL pass it to Agent_Docker using the `-p` flag
4. WHEN an agent step does not specify a project directory, THE Orchestrator SHALL use the current working directory as the project directory for Agent_Docker
5. THE Orchestrator SHALL wait for each subprocess to complete before starting the next Step
6. WHEN the `--verbose` flag is provided, THE Orchestrator SHALL stream Step_Output to stdout in real time during execution
7. WHEN the `--verbose` flag is not provided, THE Orchestrator SHALL suppress Step_Output from stdout and only print progress log lines
8. THE Orchestrator SHALL capture and retain the full Step_Output of each Step for potential use as Condition input, regardless of the `--verbose` flag
9. WHEN executing a command step, THE Orchestrator SHALL invoke the command string as a shell subprocess via `sh -c`
10. WHEN a command step specifies a project directory, THE Orchestrator SHALL set the working directory of the subprocess to that project directory
11. WHEN a command step does not specify a project directory, THE Orchestrator SHALL use the current working directory as the working directory for the subprocess

### Requirement 3: Output Checking and Error Handling

**User Story:** As a developer, I want the orchestrator to check each step's result before proceeding, so that I don't waste time running subsequent steps on a broken state.

#### Acceptance Criteria

1. WHEN a Step completes, THE Orchestrator SHALL check the Exit_Code of the Agent_Docker subprocess
2. WHEN a Step completes with a non-zero Exit_Code, THE Orchestrator SHALL stop the Pipeline and exit with a non-zero Exit_Code
3. WHEN a Step completes with a non-zero Exit_Code, THE Orchestrator SHALL print an error message identifying which Step failed and the Exit_Code
4. WHEN all Steps complete with Exit_Code 0, THE Orchestrator SHALL exit with Exit_Code 0
5. THE Orchestrator SHALL print a summary line after each Step completes, indicating the Step number, Agent_Type, and whether the Step succeeded or failed

### Requirement 4: Step Logging

**User Story:** As a developer, I want to see clear progress information during pipeline execution, so that I can monitor which step is running and how long each step takes.

#### Acceptance Criteria

1. WHEN a Step begins execution, THE Orchestrator SHALL print the Step number, total step count, and Agent_Type to stdout
2. WHEN a Step completes, THE Orchestrator SHALL print the elapsed time for that Step to stdout
3. WHEN the Pipeline completes, THE Orchestrator SHALL print the total elapsed time for the entire Pipeline to stdout

### Requirement 5: Configuration via Environment Variables

**User Story:** As a developer, I want to configure the orchestrator through environment variables and a .env file, so that I can manage settings without modifying the pipeline file.

#### Acceptance Criteria

1. THE Orchestrator SHALL load environment variables from a `.env` file in the current working directory if the file exists
2. THE Orchestrator SHALL pass all environment variables from the host process to each Agent_Docker subprocess
3. IF the `.env` file does not exist, THEN THE Orchestrator SHALL continue execution using only the existing environment variables
4. THE Orchestrator SHALL read the `OPENROUTER_API_KEY` environment variable for authenticating LLM requests to the OpenRouter_API
5. THE Orchestrator SHALL read the `OPENROUTER_MODEL` environment variable to determine which LLM model to use for Condition evaluation
6. IF `OPENROUTER_API_KEY` is not set and the Pipeline contains a Loop with a Condition or a Step with a Condition, THEN THE Orchestrator SHALL exit with a non-zero Exit_Code and print a descriptive error message to stderr
7. IF `OPENROUTER_MODEL` is not set, THEN THE Orchestrator SHALL use a default model identifier for LLM requests

### Requirement 6: Single Binary Compilation

**User Story:** As a developer, I want the orchestrator to compile into a single Go binary, so that I can deploy and run it without additional dependencies.

#### Acceptance Criteria

1. THE Orchestrator SHALL be compilable into a single statically-linked Go binary using `go build`
2. THE Orchestrator source code SHALL reside in the `orchestrator/` directory at the project root
3. THE Orchestrator SHALL have no runtime dependencies beyond the Go standard library and the `agent-docker` script being available on PATH

### Requirement 7: Dry Run Mode

**User Story:** As a developer, I want to preview the pipeline steps without executing them, so that I can verify the pipeline configuration before running it.

#### Acceptance Criteria

1. WHEN the `--dry-run` flag is provided, THE Orchestrator SHALL print each Step's number, Agent_Type, project directory, and prompt without executing Agent_Docker
2. WHEN the `--dry-run` flag is provided, THE Orchestrator SHALL exit with Exit_Code 0 after printing all Steps

### Requirement 8: Pipeline File Format

**User Story:** As a developer, I want a clear and simple YAML format for defining pipelines with named steps, loops, and conditional goto jumps, so that I can easily create and maintain complex workflow definitions.

#### Acceptance Criteria

1. THE Pipeline_File SHALL use the following YAML structure: a top-level `steps` key containing a list of step objects and loop objects
2. Each step object in the Pipeline_File SHALL be either an agent step or a command step
3. An agent step SHALL contain a `name` field (string: unique Step_Name), an `agent` field (string: `kiro` or `claude`), and a `prompt` field (string)
4. A command step SHALL contain a `name` field (string: unique Step_Name) and a `command` field (string: shell command to execute)
5. Each step object in the Pipeline_File SHALL optionally contain a `project_dir` field (string: path to the project directory)
6. THE Orchestrator SHALL support multi-line prompt strings in the Pipeline_File using YAML block scalar syntax
7. Each step object in the Pipeline_File SHALL optionally contain a `condition` object with the following fields: `prompt` (string: required, the Condition_Prompt for the LLM), `file` (string: optional, path to the Condition_File), and `goto` (string: optional, the Step_Name to jump to if the LLM returns `true`)
8. IF a step-level condition object is present but missing `prompt`, THEN THE Orchestrator SHALL exit with a non-zero Exit_Code and print a descriptive error message to stderr
9. IF a step-level condition specifies a `goto` field, THE Orchestrator SHALL validate that it references an existing Step_Name in the Pipeline
10. IF a condition's `goto` references a non-existent Step_Name, THEN THE Orchestrator SHALL exit with a non-zero Exit_Code and print an error identifying the invalid reference
11. A loop object in the Pipeline_File SHALL contain a `loop` key with the following nested fields: `max_iterations` (integer), `steps` (list of step objects — either agent or command steps), and `condition` (condition object)
12. A loop-level condition object SHALL contain a `prompt` field (string: required, the Condition_Prompt for the LLM, phrased so that `true` means continue the loop and `false` means stop) and an optional `file` field (string: path to the Condition_File)
13. IF a loop object is missing `max_iterations`, `steps`, or `condition`, THEN THE Orchestrator SHALL exit with a non-zero Exit_Code and print a descriptive error message to stderr
14. THE Orchestrator SHALL validate that `max_iterations` is a positive integer greater than zero
15. Steps inside a loop object SHALL also have unique `name` fields, and these names SHALL be unique across the entire Pipeline


### Requirement 9: LLM-Based Loops

**User Story:** As a developer, I want to define loops with LLM-based conditions in my pipeline, so that I can create iterative refinement workflows that repeat until the LLM determines the work is done.

#### Acceptance Criteria

1. WHEN the Orchestrator encounters a Loop in the Pipeline, THE Orchestrator SHALL execute the Loop's Steps sequentially in each iteration
2. WHEN a Loop iteration completes all its Steps, THE Orchestrator SHALL determine the Condition input: if a Condition_File is specified, read its content from the project directory; otherwise, use the Step_Output of the last Step in the Loop iteration
3. IF a Condition_File is specified and does not exist after a Loop iteration, THEN THE Orchestrator SHALL exit with a non-zero Exit_Code and print a descriptive error message to stderr
4. THE Orchestrator SHALL send the Condition input along with the Condition_Prompt to the OpenRouter_API using the configured `OPENROUTER_API_KEY` and `OPENROUTER_MODEL`
5. THE Orchestrator SHALL use an English-language system prompt instructing the LLM to evaluate the provided file content based on the Condition_Prompt and return a structured LLM_Decision as a boolean: `true` (continue the loop) or `false` (stop the loop)
6. WHEN the LLM_Decision is `true`, THE Orchestrator SHALL execute the next Loop iteration
7. WHEN the LLM_Decision is `false`, THE Orchestrator SHALL exit the Loop and proceed with the remaining Pipeline steps
8. WHEN the Loop reaches Max_Iterations without the LLM_Decision being `false`, THE Orchestrator SHALL exit the Loop, print a warning message indicating the maximum iteration count was reached, and continue with any remaining Pipeline steps
9. THE Orchestrator SHALL log each Loop iteration number, the Max_Iterations limit, and the LLM_Decision to stdout
10. WHEN a Step inside a Loop completes with a non-zero Exit_Code, THE Orchestrator SHALL stop the entire Pipeline and exit with a non-zero Exit_Code


### Requirement 10: LLM-Based Step Conditions

**User Story:** As a developer, I want to attach an LLM-based condition to any step, so that I can create conditional branching (with goto) or a continue/break gate (without goto) based on LLM evaluation.

#### Acceptance Criteria

1. WHEN a Step with a Condition completes successfully, THE Orchestrator SHALL determine the Condition input: if a Condition_File is specified, read its content from the project directory; otherwise, use the Step_Output of that Step
2. IF a Condition_File is specified and does not exist after the Step completes, THEN THE Orchestrator SHALL exit with a non-zero Exit_Code and print a descriptive error message to stderr
3. THE Orchestrator SHALL send the Condition input along with the Condition_Prompt to the OpenRouter_API using the configured `OPENROUTER_API_KEY` and `OPENROUTER_MODEL`
4. THE Orchestrator SHALL use an English-language system prompt instructing the LLM to evaluate the provided content based on the Condition_Prompt and return a structured LLM_Decision as a boolean: `true` or `false`
5. WHEN the Condition has a `goto` field and the LLM_Decision is `true`, THE Orchestrator SHALL jump execution to the Step identified by the Goto target Step_Name
6. WHEN the Condition has a `goto` field and the LLM_Decision is `false`, THE Orchestrator SHALL continue to the next step in order
7. WHEN the Condition has no `goto` field and the LLM_Decision is `true`, THE Orchestrator SHALL continue to the next step in order
8. WHEN the Condition has no `goto` field and the LLM_Decision is `false`, THE Orchestrator SHALL stop the Pipeline and exit with Exit_Code 0
9. THE Orchestrator SHALL log each Condition evaluation: the current Step_Name, the LLM_Decision, and the Goto target (if present) to stdout
10. IF the OpenRouter_API request fails or returns an unparseable response, THEN THE Orchestrator SHALL retry the request once, and if the retry also fails, exit with a non-zero Exit_Code and print a descriptive error message to stderr
