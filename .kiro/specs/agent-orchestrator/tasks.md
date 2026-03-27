# Implementation Plan: Agent Orchestrator

## Overview

Build a Go CLI that reads a YAML pipeline definition and sequentially executes `agent-docker` commands with support for LLM-based loops and step conditions. Implementation follows a bottom-up approach: types and parsing first, then execution engine, then LLM integration, then CLI wiring.

## Tasks

- [ ] 1. Initialize Go module and project structure
  - Create `orchestrator/` directory with `go.mod` (module name: `orchestrator`)
  - Add dependencies: `gopkg.in/yaml.v3`, `github.com/joho/godotenv`, `pgregory.net/rapid` (test only)
  - Create `.env.example` with placeholder values for `OPENROUTER_API_KEY` and `OPENROUTER_MODEL`
  - _Requirements: 6.1, 6.2, 5.1, 5.4, 5.5_

- [ ] 2. Implement pipeline types and YAML parsing
  - [ ] 2.1 Create `pipeline.go` with Go types and parsing logic
    - Define `Pipeline`, `PipelineElement`, `Step`, `Loop`, `Condition` structs with YAML tags
    - Implement custom `UnmarshalYAML` on `PipelineElement` to distinguish steps from loops
    - Implement `ParsePipeline(path string) (*Pipeline, error)` that reads file, unmarshals YAML, and runs validation
    - Implement `FlattenStepNames()` and `NeedsLLM()` helper methods
    - Validation: unique step names (across entire pipeline including loops), valid agent types (`kiro`/`claude`) for agent steps, each step is either agent (`agent`+`prompt`) or command (`command`) but not both, required fields, condition prompt required, goto targets exist, max_iterations > 0, loop required fields
    - _Requirements: 1.2, 1.3, 1.5, 1.6, 1.7, 1.8, 1.9, 1.10, 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7, 8.8, 8.9, 8.10, 8.11, 8.12, 8.13, 8.14, 8.15_

  - [ ]* 2.2 Write property test: Pipeline parsing round trip
    - **Property 1: Pipeline parsing round trip**
    - Generate random valid Pipeline structs, serialize to YAML, parse back, assert structural equivalence
    - **Validates: Requirements 1.2, 1.3, 8.2, 8.4, 8.9**

  - [ ]* 2.3 Write property test: Invalid pipelines are rejected
    - **Property 2: Invalid pipelines are rejected**
    - Generate pipelines with random validation violations (invalid agent, duplicate names, missing fields, bad goto, max_iterations ≤ 0, step with both agent and command, step with neither), assert `ParsePipeline` returns error
    - **Validates: Requirements 1.4, 1.5, 1.6, 1.7, 1.8, 8.2, 8.6, 8.7, 8.8, 8.9, 8.11, 8.12, 8.13**

- [ ] 3. Implement configuration loading
  - [ ] 3.1 Create `config.go` with Config struct and loading logic
    - Define `Config` struct with `OpenRouterAPIKey` and `OpenRouterModel` fields
    - Implement `LoadConfig()` that loads `.env` via `godotenv` (silent if missing), reads env vars, applies default model `openai/gpt-4.1-nano`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.7_

  - [ ]* 3.2 Write unit tests for config loading
    - Test `.env` loading when file exists and when it doesn't
    - Test default model is used when `OPENROUTER_MODEL` is not set
    - Test API key is read from environment
    - _Requirements: 5.1, 5.3, 5.5, 5.7_

- [ ] 4. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 5. Implement LLM client
  - [ ] 5.1 Create `llm.go` with OpenRouter API client
    - Define `LLMClient` struct with `APIKey`, `Model`, `BaseURL` fields
    - Define `LLMRequest`, `LLMMessage`, `LLMResponse` types for API serialization
    - Implement `Evaluate(content, conditionPrompt string) (bool, error)` that sends chat completion request with system prompt and user message, parses `true`/`false` from response
    - Implement retry logic: retry once on HTTP error or unparseable response
    - Use `net/http` with 60-second timeout
    - System prompt: English-language instruction to respond with exactly `true` or `false`
    - Define `ConditionEvaluator` interface for testability
    - _Requirements: 9.4, 9.5, 10.3, 10.4, 10.10_

  - [ ]* 5.2 Write property test: LLM response parsing
    - **Property 12: LLM response parsing**
    - Generate random strings, verify `true`/`false` extraction (case-insensitive, trimmed) or unparseable result
    - **Validates: Requirements 9.5, 10.4**

  - [ ]* 5.3 Write property test: LLM client retry on failure
    - **Property 9: LLM client retry on failure**
    - Generate random failure/success sequences, verify retry behavior (retry exactly once, return result on success, error on double failure)
    - **Validates: Requirements 10.10**

- [ ] 6. Implement execution engine
  - [ ] 6.1 Create `executor.go` with Executor struct and step execution
    - Define `Executor` struct with `Config`, `Pipeline`, `LLM` (as `ConditionEvaluator`), `DryRun`, `Verbose`, and `Runner` (as `CommandRunner`) fields
    - Define `CommandRunner` interface for subprocess abstraction (with `RunAgent` and `RunCommand` methods)
    - Implement `executeStep()`: for agent steps, build `agent-docker` command with agent type, prompt, project dir (default to cwd); for command steps, build `sh -c` command with working directory set to project dir (default to cwd); capture output; stream to stdout when verbose via `io.MultiWriter`; return output, exit code
    - Implement real `CommandRunner` using `os/exec` that passes all host env vars to subprocess
    - Print step start log (step number, total count, step type), completion log (elapsed time, success/failure)
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 2.10, 2.11, 3.1, 3.2, 3.3, 3.5, 4.1, 4.2, 5.2_

  - [ ]* 6.2 Write property test: Command construction correctness
    - **Property 3: Command construction correctness**
    - Generate random steps (both agent and command types) with various agent types, prompts, commands, and optional project dirs; verify constructed command contains correct arguments
    - **Validates: Requirements 2.2, 2.3, 2.4, 2.9, 2.10, 2.11**

  - [ ] 6.3 Implement `Run()` method with sequential execution, error handling, and condition evaluation
    - Implement main `Run()` loop iterating over pipeline elements in order
    - On non-zero exit code: stop pipeline, print error identifying step and exit code, return non-zero
    - On all steps success: return exit code 0
    - Implement `evaluateCondition()`: determine input (read condition file or use step output), call `ConditionEvaluator`
    - Handle condition file not found: exit with error
    - Print total pipeline elapsed time at completion
    - _Requirements: 2.1, 3.1, 3.2, 3.3, 3.4, 3.5, 4.3, 9.2, 9.3, 10.1, 10.2_

  - [ ]* 6.4 Write property test: Sequential execution order
    - **Property 4: Sequential execution order**
    - Generate random pipelines (no conditions), mock executor, verify call order matches definition order and total invocations equal step count
    - **Validates: Requirements 2.1, 9.1**

  - [ ]* 6.5 Write property test: Failure stops pipeline
    - **Property 5: Failure stops pipeline**
    - Generate pipelines with a random failing step at index K, verify only steps 0..K execute and pipeline exits non-zero
    - **Validates: Requirements 3.2, 3.3, 3.4, 9.10**

  - [ ]* 6.6 Write property test: Condition input selection
    - **Property 6: Condition input selection**
    - Generate conditions with/without file field, verify correct input (file content vs step output) is passed to evaluator
    - **Validates: Requirements 2.7, 9.2, 10.1**

- [ ] 7. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 8. Implement loop execution and step condition routing
  - [ ] 8.1 Implement `executeLoop()` in `executor.go`
    - Execute loop steps sequentially each iteration
    - After each iteration: determine condition input (file or last step output), evaluate via LLM
    - On LLM `true`: continue to next iteration
    - On LLM `false`: exit loop, proceed with remaining pipeline
    - On reaching max_iterations: exit loop with warning, continue pipeline
    - Log iteration number, max_iterations, and LLM decision
    - On step failure inside loop: stop entire pipeline
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 9.7, 9.8, 9.9, 9.10_

  - [ ]* 8.2 Write property test: Loop iteration control
    - **Property 7: Loop iteration control**
    - Generate loops with random max_iterations and LLM decision sequences, verify correct iteration count
    - **Validates: Requirements 9.6, 9.7, 9.8**

  - [ ] 8.3 Implement step condition routing with goto support in `Run()`
    - After successful step with condition: evaluate condition
    - With goto + true: jump to target step (set next execution index)
    - With goto + false: continue to next step
    - Without goto + true: continue to next step
    - Without goto + false: stop pipeline with exit code 0
    - Log condition evaluation: step name, LLM decision, goto target
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 10.9_

  - [ ]* 8.4 Write property test: Step condition routing
    - **Property 8: Step condition routing**
    - Generate steps with conditions (with/without goto, true/false decisions), verify correct next step selection
    - **Validates: Requirements 10.5, 10.6, 10.7, 10.8**

- [ ] 9. Implement CLI entry point and dry-run mode
  - [ ] 9.1 Create `main.go` with flag parsing and orchestration
    - Parse flags: `--pipeline` (required), `--dry-run`, `--verbose`, `--version`, `--help`
    - Version string set via `-ldflags` at build time
    - Custom help output with description, flags, and examples
    - On missing/invalid flags: print usage to stderr, exit 1
    - Load config, parse pipeline, validate API key if pipeline needs LLM
    - Create executor and run pipeline
    - _Requirements: 1.1, 1.4, 5.6, 6.1, 7.1, 7.2_

  - [ ] 9.2 Implement dry-run mode in executor
    - When `--dry-run`: print each step's number, agent type, project directory, and prompt without executing
    - Include loop steps with loop context
    - Exit with code 0 after printing all steps
    - _Requirements: 7.1, 7.2_

  - [ ]* 9.3 Write property test: Dry run prints all steps without execution
    - **Property 10: Dry run no execution**
    - Generate random pipelines, run in dry-run mode, verify output contains all step info and no subprocess spawned
    - **Validates: Requirements 7.1, 7.2**

  - [ ]* 9.4 Write property test: API key required when pipeline needs LLM
    - **Property 11: API key required when pipeline needs LLM**
    - Generate pipelines with conditions, unset API key, verify error before any execution
    - **Validates: Requirements 5.6**

- [ ] 10. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Property tests use `pgregory.net/rapid` with minimum 100 iterations
- `CommandRunner` and `ConditionEvaluator` interfaces enable test isolation without real subprocesses or LLM calls
- All source files live flat in `orchestrator/` per project structure rules
- Tests live alongside source: `pipeline_test.go`, `executor_test.go`, `llm_test.go`, `main_test.go`
