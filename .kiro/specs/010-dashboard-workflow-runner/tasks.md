# Implementation Plan: Dashboard Workflow Runner

## Overview

This plan implements a "Workflows" tab in the trayline dashboard, enabling users to schedule, manage, and monitor trayline pipeline executions from the web interface. The implementation follows a backend-first approach: core infrastructure (store, config, ring buffer), then API handlers, then frontend components and wiring.

## Tasks

- [ ] 1. Backend infrastructure — Store, config, and ring buffer
  - [ ] 1.1 Add workflow configuration to `core/Config`
    - Add `TraylineHomeDir`, `PipelinesDir`, and `WorkflowTimeout` fields to `core.Config`
    - Read from `TRAYLINE_HOME_DIR`, `PIPELINES_DIR`, `WORKFLOW_TIMEOUT` environment variables
    - Default `TraylineHomeDir` to `~/.trayline` (expanded), derive `PipelinesDir` as `TraylineHomeDir/pipelines`
    - Default `WorkflowTimeout` to `5h` parsed as `time.Duration`
    - Update `.env.example` with new variables
    - _Requirements: 14.2, 14.5_

  - [ ] 1.2 Implement `store/ringbuffer.go` — Log ring buffer
    - Create `RingBuffer` struct with `mu sync.Mutex`, `buf []byte`, `maxSize int`, `writePos int`, `wrapped bool`
    - Implement `NewRingBuffer(maxSize int)`, `Write(p []byte) (int, error)` (io.Writer), `String() string`, `Wrapped() bool`
    - Maximum size: 1MB (1 * 1024 * 1024 bytes)
    - Thread-safe for concurrent write and read
    - _Requirements: 7.6_

  - [ ]* 1.3 Write property test for ring buffer (Property 9)
    - **Property 9: Ring buffer size invariant**
    - For any sequence of byte writes, stored content never exceeds maxSize; after writing N > maxSize bytes, buffer contains exactly the most recent maxSize bytes and reports truncation
    - Use `testing/quick` or `gopter` with minimum 100 iterations
    - **Validates: Requirements 7.6**

  - [ ] 1.4 Implement `store/workflow.go` — Workflow store
    - Define `WorkflowStatus` type and constants: `WorkflowQueued`, `WorkflowRunning`, `WorkflowCompleted`, `WorkflowFailed`, `WorkflowCancelled`
    - Define `Workflow` struct with all fields (ID, Project, Pipeline, Variables, Status, timestamps, Error, ExitCode, ContainerID, CancelFunc, LogBuffer, LogSubs)
    - Implement `WorkflowStore` with `sync.RWMutex`, `workflows map[string]*Workflow`, `byProject map[string][]string`
    - Methods: `Add`, `Get`, `Update`, `ListByProject` (most recent 20, desc), `NextQueued`, `HasRunning`, `All`, `Evict` (remove oldest terminal workflows beyond 20 per project)
    - Eviction is triggered automatically after a workflow reaches terminal status
    - Follow pattern from `store/task.go`
    - _Requirements: 4.1, 5.1, 5.2, 5.6_

  - [ ]* 1.5 Write property test for sequential execution constraint (Property 4)
    - **Property 4: Sequential execution per project**
    - For any project and any point in time, at most one workflow with status "running" for that project; multiple projects may each have one running workflow
    - Use `gopter` with minimum 100 iterations
    - **Validates: Requirements 4.1, 4.2**

  - [ ]* 1.6 Write property test for workflow listing order and cap (Property 6)
    - **Property 6: Workflow listing is ordered and capped**
    - For any project with N workflows, list returns min(N, 20) sorted by created_at descending
    - Use `gopter` with minimum 100 iterations
    - **Validates: Requirements 5.1**

  - [ ] 1.7 Implement `store/workflow_state.go` — Persistence manager
    - Create `WorkflowStateManager` with `stateDir`, `workflowStore`, `logger`
    - Implement `Save()` with atomic write (write to temp file, then rename)
    - Implement `Load()` to read `STATE_DIR/workflows.json` on startup
    - Implement `Recover()`: set "running" workflows to "failed" with error "server restarted and container was lost", then resume queued
    - Handle corrupt JSON: rename to `workflows.json.corrupt`, initialize empty store
    - Handle missing file: initialize empty store
    - Handle disk write failure: log error, continue operating
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7, 8.8_

  - [ ]* 1.8 Write property test for persistence round-trip (Property 10)
    - **Property 10: Persistence round-trip**
    - For any valid workflow store state, serialize to JSON then deserialize produces equivalent workflows with all persisted fields preserved
    - Use `gopter` with minimum 100 iterations
    - **Validates: Requirements 8.1, 8.4, 8.8**

- [ ] 2. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 3. Backend — Validation helpers and command construction
  - [ ] 3.1 Implement validation helpers
    - Create validation functions for: pipeline type (must be "tasks", "processes", or "workflows"), variable key (regex `^[a-zA-Z0-9_-]{1,100}$`), variable value (max 1000 chars), variables map (max 50 entries), pipeline reference format ("type/name")
    - Place in `api/` or `core/` package as appropriate
    - _Requirements: 1.5, 3.4, 3.5, 3.7_

  - [ ]* 3.2 Write property test for workflow input validation (Property 2)
    - **Property 2: Workflow input validation**
    - For any string as variable key, validation accepts iff matches `^[a-zA-Z0-9_-]{1,100}$`; for values, accepts iff ≤1000 chars; for maps, accepts iff ≤50 entries; for pipeline type, accepts iff in {"tasks","processes","workflows"}
    - Use `testing/quick` with minimum 100 iterations
    - **Validates: Requirements 1.5, 3.4, 3.5, 3.7, 6.2**

  - [ ] 3.3 Implement command construction function
    - Create `buildWorkflowCmd(pipeline string, variables map[string]string) []string`
    - Returns `["trayline", "run", pipeline, "--var", "k1=v1", "--var", "k2=v2", ...]`
    - _Requirements: 4.4_

  - [ ]* 3.4 Write property test for command construction (Property 5)
    - **Property 5: Command construction correctness**
    - For any valid pipeline reference and variables map, constructed command starts with `["trayline", "run", pipeline]` and contains exactly one `--var` flag per variable entry with value `key=value`
    - Use `testing/quick` with minimum 100 iterations
    - **Validates: Requirements 4.4**

- [ ] 4. Backend — Queue manager
  - [ ] 4.1 Implement `WorkflowQueueManager`
    - Create struct with `mu sync.Mutex`, `active map[string]bool`, `notify map[string]chan struct{}`, store/cm/config/logger/stateMgr references
    - Implement `Enqueue(project string)` to signal the processor goroutine
    - Implement `processLoop(project string)` goroutine: check HasRunning → NextQueued → create container → run `trayline run ...` → stream stdout/stderr to ring buffer + broadcast to subscribers → update status → persist → loop
    - Container config: `trayline-sandbox` image, `trayline-net` network, project mount at `/workspace`, `TRAYLINE_HOME_DIR` mount read-only at `/home/agent/.trayline`, `DOCKER_HOST=tcp://trayline-proxy:2375`, `NO_COLOR=1`
    - Apply `WORKFLOW_TIMEOUT` with context deadline; on timeout, kill container and set status to "failed"
    - On non-zero exit: set "failed", capture stderr as error, record exit code
    - On zero exit: set "completed", record completed_at
    - Clean up container (stop + remove) after execution
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 14.1, 14.3, 14.4_

  - [ ]* 4.2 Write property test for queue position preservation (Property 8)
    - **Property 8: Edit preserves queue position**
    - For any set of queued workflows, editing a workflow's variables/pipeline does not change its position relative to other queued workflows
    - Use `gopter` with minimum 100 iterations
    - **Validates: Requirements 6.9**

- [ ] 5. Backend — API handlers
  - [ ] 5.1 Implement `api/pipeline_handler.go` — Pipeline discovery
    - Create `PipelineHandler` struct with `config` and `logger`
    - `HandleListPipelines`: Read YAML files from `config.PipelinesDir/{tasks,processes,workflows}/`, return JSON with keys "tasks", "processes", "workflows" each containing array of `{name, type, display_name}`
    - `HandleGetPipelineDetail`: Parse `variables` section from specific pipeline YAML, return `{name, type, variables}`
    - Validate pipeline type, return 400 for invalid, 404 for missing file, 404 for missing project
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8_

  - [ ]* 5.2 Write property test for pipeline name transformation (Property 1)
    - **Property 1: Pipeline name transformation**
    - For any valid YAML filename, name equals filename without `.yaml`, display_name equals name with hyphens replaced by spaces
    - Use `testing/quick` with minimum 100 iterations
    - **Validates: Requirements 1.2**

  - [ ] 5.3 Implement `api/spec_handler.go` — Spec discovery
    - Create `SpecHandler` struct with `config` and `logger`
    - `HandleListSpecs`: Scan `PROJECTS_DIR/{name}/.kiro/specs/` directories, filter to specs with `tasks.md` containing `- [ ]`, return `{name, created_at}` sorted by created_at desc
    - Return empty array if no specs found, 404 for missing project
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

  - [ ] 5.4 Implement `api/workflow_handler.go` — Workflow CRUD
    - Create `WorkflowHandler` struct with store, cm, config, logger, stateMgr, queues
    - `HandleSchedule`: Validate pipeline + variables, create workflow (UUID, status=queued, created_at), persist, enqueue, return 201
    - `HandleList`: Return most recent 20 workflows for project, desc by created_at
    - `HandleDetail`: Return single workflow with all fields + log output
    - `HandleEdit`: Validate status==queued (409 otherwise), validate new pipeline/variables, update, persist, return 200
    - `HandleCancel`: For queued: set cancelled, remove from queue, return 200. For running: SIGTERM → wait 10s → SIGKILL, set cancelled, return 200. For terminal: return 409
    - _Requirements: 3.1–3.7, 5.1–5.5, 6.1–6.9_

  - [ ]* 5.5 Write property test for workflow creation invariants (Property 3)
    - **Property 3: Workflow creation invariants**
    - For any valid creation request, resulting workflow has non-empty UUID, status "queued", non-zero created_at not in the future
    - Use `gopter` with minimum 100 iterations
    - **Validates: Requirements 3.3**

  - [ ]* 5.6 Write property test for state machine constraints (Property 7)
    - **Property 7: State machine edit/cancel constraints**
    - For any workflow in non-queued status, edit is rejected with 409. For any workflow in terminal status, cancel is rejected with 409
    - Use `gopter` with minimum 100 iterations
    - **Validates: Requirements 6.3, 6.7**

  - [ ] 5.7 Implement WebSocket log streaming in `workflow_handler.go`
    - `HandleLogs`: Authenticate Bearer token, upgrade to WebSocket
    - For running: stream container output as `{"type":"output","data":"..."}`, send `{"type":"finished",...}` on completion, close connection
    - For queued: send `{"type":"waiting"}`, begin streaming once started
    - For terminal: send stored log as single output message + finished message, close
    - Reject upgrade with 404 if workflow not found, 401 if auth fails
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.7, 7.8_

  - [ ] 5.8 Register all new routes in `api/router.go`
    - Add pipeline routes: `GET /projects/{name}/pipelines`, `GET /projects/{name}/pipelines/{type}/{pipeline}`
    - Add spec route: `GET /projects/{name}/specs`
    - Add workflow routes: `POST`, `GET` (list), `GET` (detail), `PUT`, `DELETE`, `GET` (logs WebSocket)
    - Initialize handlers with dependencies
    - Update CORS middleware in `api/cors.go` to allow `POST, DELETE` methods (current: `GET, PUT, OPTIONS` → new: `GET, POST, PUT, DELETE, OPTIONS`)
    - _Requirements: 1.1, 2.1, 3.1, 5.1, 5.3, 6.1, 6.4, 7.1_

- [ ] 6. Checkpoint — Ensure all backend tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Frontend — API client and store
  - [ ] 7.1 Extend `src/lib/api.ts` with new types and methods
    - Add interfaces: `Pipeline`, `PipelinesResponse`, `PipelineDetail`, `Spec`, `Workflow`
    - Add methods: `getPipelines`, `getPipelineDetail`, `getSpecs`, `getWorkflows`, `getWorkflow`, `createWorkflow`, `updateWorkflow`, `cancelWorkflow`
    - Add `buildWorkflowLogWsUrl(projectName, workflowId)` function
    - Follow existing `request<T>()` pattern with `enc()` for URL encoding
    - _Requirements: 10.1, 11.2, 11.3, 11.5, 12.3, 12.6_

  - [ ] 7.2 Create `src/lib/stores/workflow.ts` — Workflow store
    - Create Svelte writable store managing workflow list state
    - Implement 5-second polling with `setInterval` + `document.hidden` check
    - Start polling on mount, stop on unmount or tab hidden
    - Follow pattern from `stores/agent.ts`
    - _Requirements: 10.5_

- [ ] 8. Frontend — i18n and tab integration
  - [ ] 8.1 Add i18n keys for Workflows tab
    - Add `tabs.workflows` key with value "Workflows" in `src/lib/i18n/en.ts`
    - Add `tabs.workflows` key with value "Workflows" in `src/lib/i18n/cs.ts`
    - Add all workflow-related translation keys (status labels, form labels, buttons, empty state, errors)
    - _Requirements: 9.3_

  - [ ] 8.2 Add "Workflows" tab to `TabBar.svelte`
    - Add tab after "Env" and before "Agent"
    - Navigate to `/{project}/workflows` using `resolve('/[project]/workflows', { project })`
    - Active when `page.url.pathname.split('/')[2]` equals `'workflows'`
    - No `ref` query parameter appended
    - _Requirements: 9.1, 9.2, 9.3, 9.4_

- [ ] 9. Frontend — Page and list component
  - [ ] 9.1 Create `src/routes/[project]/workflows/+page.svelte`
    - Main workflows page with list view and expandable details
    - Include "New Workflow" button above the list
    - Handle empty state with message and call-to-action
    - Handle fetch errors with inline message and retry button
    - _Requirements: 10.1, 10.4, 10.7, 10.8_

  - [ ] 9.2 Create `src/lib/components/WorkflowList.svelte`
    - Display each workflow with: pipeline display_name, status badge (color-coded pill), created_at as relative time, key variable summary (specs-name, path)
    - Status badges: queued (gray/slate), running (blue + pulsing dot), completed (green), failed (red), cancelled (muted)
    - Click to expand inline showing full details (variables table + log viewer)
    - Show Edit/Cancel buttons for queued, Cancel-only for running, no buttons for terminal
    - _Requirements: 10.2, 10.3, 10.5, 10.6, 12.1, 12.8, 12.9_

- [ ] 10. Frontend — Form component
  - [ ] 10.1 Create `src/lib/components/WorkflowForm.svelte`
    - Pipeline selector dropdown grouped by type (optgroup), default "processes/4-create-code"
    - Fetch pipeline details on selection, display variables with defaults pre-filled
    - `specs-name` variable: autocomplete dropdown from `GET /projects/{name}/specs` (newest first)
    - `skip-*` variables: toggle switches mapping to "true"/"false"
    - `path` variable: editable text input with pipeline YAML default
    - Validate required variables (empty defaults, non-skip) before submission
    - Submit via `POST /projects/{name}/workflows`, disable button during request
    - On success: close form, refresh list, show 3-second auto-dismiss notification
    - On failure: keep form open, preserve input, show inline error
    - Support pre-fill mode for editing (pipeline + variables from existing workflow)
    - On edit submit: `PUT /projects/{name}/workflows/{id}`, handle 409 (close form, show error, refresh)
    - _Requirements: 11.1–11.10, 12.2, 12.3, 12.4_

- [ ] 11. Frontend — Log viewer and cancel dialog
  - [ ] 11.1 Create `src/lib/components/WorkflowLogViewer.svelte`
    - Terminal-styled viewer: monospace font, dark background, fixed max height with overflow-y scroll
    - For running workflows: WebSocket connection, stream output preserving line breaks
    - Auto-scroll to bottom unless user scrolled up > 50px
    - On "finished" message: display final status and exit code at bottom
    - For completed/failed: display stored log output (no WebSocket)
    - On unexpected disconnect: show reconnection notice, retry once within 10s, show persistent error on failure
    - Truncation notice if buffer wrapped (>1MB)
    - Close WebSocket on collapse
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5, 13.6, 13.7_

  - [ ] 11.2 Implement cancel confirmation dialog
    - Display workflow pipeline name, ask to confirm
    - "Cancel Workflow" destructive button + "Keep" dismiss button
    - On confirm: `DELETE /projects/{name}/workflows/{id}`, refresh list on success
    - Handle 409: dismiss dialog, show error, refresh list
    - _Requirements: 12.5, 12.6, 12.7_

- [ ] 12. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document (Properties 1–10)
- Unit tests validate specific examples and edge cases
- Backend uses Go with `testing/quick` and `gopter` for PBT
- Frontend uses SvelteKit with TypeScript, following existing patterns in `api.ts`, `stores/agent.ts`, `TabBar.svelte`
- The ring buffer, workflow store, state manager, and queue manager are core backend components implemented first
- Frontend follows backend to ensure API contracts are stable before building UI

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["1.3", "1.4", "3.1"] },
    { "id": 2, "tasks": ["1.5", "1.6", "1.7", "3.2", "3.3"] },
    { "id": 3, "tasks": ["1.8", "3.4", "4.1"] },
    { "id": 4, "tasks": ["4.2", "5.1", "5.3"] },
    { "id": 5, "tasks": ["5.2", "5.4"] },
    { "id": 6, "tasks": ["5.5", "5.6", "5.7"] },
    { "id": 7, "tasks": ["5.8"] },
    { "id": 8, "tasks": ["7.1", "8.1"] },
    { "id": 9, "tasks": ["7.2", "8.2"] },
    { "id": 10, "tasks": ["9.1", "9.2"] },
    { "id": 11, "tasks": ["10.1"] },
    { "id": 12, "tasks": ["11.1", "11.2"] }
  ]
}
```
