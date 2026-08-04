# Requirements Document

## Introduction

Add a "Workflows" tab to the trayline dashboard project detail view, enabling users to schedule, manage, and monitor trayline pipeline executions directly from the web interface. This replicates the `trayline run ...` CLI experience within the dashboard, including a per-project workflow queue, a form for configuring pipeline variables, live log streaming via WebSocket, and queue persistence across server restarts.

Source of truth: `dashboard/SPEC.md` (architecture, auth, CORS patterns), `pipelines/PIPELINES.md` (pipeline structure and semantics)
Related existing code: `remote/store/state.go` (persistence pattern), `remote/docker/container.go` (container execution), `remote/api/task_handler.go` (one-shot task pattern), `dashboard/src/lib/api.ts` (API client)

## Glossary

- **Dashboard**: The SvelteKit SPA frontend for browsing and managing projects
- **Remote_Server**: The existing Go backend (`remote/`) that handles API requests and container management
- **Container_Manager**: The `docker/ContainerManager` responsible for creating, starting, and stopping Docker containers
- **Workflow**: A scheduled pipeline execution instance with a unique ID, pipeline reference, variables, status, and log output
- **Workflow_Store**: A thread-safe in-memory store for workflows with JSON file persistence
- **Workflow_Queue**: The per-project sequential execution queue that processes workflows one at a time within a project
- **Pipeline**: A YAML file from `~/.trayline/pipelines/` defining variables and steps (tasks, processes, or workflows)
- **Pipeline_Type**: One of "tasks", "processes", or "workflows" — the subdirectory category of a pipeline
- **Workflow_Status**: One of "queued", "running", "completed", "failed", or "cancelled"
- **Workflow_Tab**: The new tab in the project detail view providing the workflow management interface
- **Log_Stream**: A WebSocket connection that delivers real-time container output for a running workflow
- **Skip_Flag**: A workflow variable prefixed with `skip-` that acts as a boolean toggle to bypass specific steps

## Requirements

### Requirement 1: Pipeline Discovery API

**User Story:** As a developer, I want to list available pipelines and inspect their variables, so that I can choose which pipeline to run and configure it properly.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose `GET /projects/{name}/pipelines` that returns a JSON object with keys "tasks", "processes", and "workflows", each containing an array of pipeline objects found in the corresponding subdirectory of the mounted pipelines directory
2. WHEN listing pipelines, THE Remote_Server SHALL return for each pipeline: name (filename without `.yaml` extension), type (tasks/processes/workflows), and display_name (name with hyphens replaced by spaces)
3. THE Remote_Server SHALL expose `GET /projects/{name}/pipelines/{type}/{pipeline}` that returns the parsed variables section of a specific pipeline YAML file
4. WHEN a pipeline detail is requested, THE Remote_Server SHALL return all variables with their default values as defined in the YAML file; IF the YAML file contains no `variables` key, THEN THE Remote_Server SHALL return an empty object
5. IF a requested pipeline type is not one of "tasks", "processes", or "workflows", THEN THE Remote_Server SHALL return HTTP 400 with error code "VALIDATION_ERROR"
6. IF a requested pipeline YAML file does not exist, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"
7. THE Remote_Server SHALL read pipelines from the `PIPELINES_DIR` configuration path (default: `~/.trayline/pipelines` on the host), which is mounted read-only into the server container
8. IF the project specified by `{name}` does not exist, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"

### Requirement 2: Spec Discovery API

**User Story:** As a developer, I want to see available specs with uncompleted tasks for a project, so that I can select one when configuring a workflow that requires a `specs-name` variable.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose `GET /projects/{name}/specs` that returns a JSON array of specs from the `.kiro/specs/` directory of the specified project
2. WHEN listing specs, THE Remote_Server SHALL include only specs that have a `tasks.md` file containing at least one unchecked task (matching pattern `- [ ]`)
3. WHEN listing specs, THE Remote_Server SHALL return for each spec: `name` (directory name) and `created_at` (directory modification time as ISO 8601 timestamp with timezone), sorted by `created_at` descending (newest first)
4. IF the project has no `.kiro/specs/` directory or no specs contain unchecked tasks, THEN THE Remote_Server SHALL return an empty JSON array
5. IF the specified project does not exist, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"

### Requirement 3: Workflow Scheduling

**User Story:** As a developer, I want to schedule a new workflow execution for a project, so that a pipeline runs with my chosen variables without me needing CLI access.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose `POST /projects/{name}/workflows` to schedule a new workflow execution
2. WHEN a workflow is scheduled, THE Remote_Server SHALL accept a JSON body containing: pipeline (required, string in format "type/name", e.g. "processes/4-create-code"), and variables (required, object with key-value string pairs, maximum 50 entries)
3. WHEN a workflow is created, THE Remote_Server SHALL assign a UUID, set status to "queued", record created_at timestamp, and return the workflow object with HTTP 201
4. WHEN a workflow scheduling request is received, THE Remote_Server SHALL validate that the pipeline type is one of "tasks", "processes", or "workflows", that the pipeline name matches an existing YAML file in the corresponding subdirectory of the pipelines directory, and that variable keys contain only characters matching `[a-zA-Z0-9_-]` with a maximum key length of 100 characters and values do not exceed 1000 characters
5. IF the pipeline field is missing, the pipeline type is not one of "tasks", "processes", or "workflows", or the referenced pipeline file does not exist, THEN THE Remote_Server SHALL return HTTP 400 with error code "VALIDATION_ERROR"
6. IF the specified project does not exist, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"
7. IF any variable key contains characters outside `[a-zA-Z0-9_-]`, a key exceeds 100 characters, a value exceeds 1000 characters, or the variables object contains more than 50 entries, THEN THE Remote_Server SHALL return HTTP 400 with error code "VALIDATION_ERROR"

### Requirement 4: Workflow Queue Execution

**User Story:** As a developer, I want workflows for a project to execute sequentially in queue order, so that pipeline runs do not interfere with each other within the same project.

#### Acceptance Criteria

1. THE Remote_Server SHALL execute queued workflows for a given project sequentially — only one workflow per project runs at a time
2. THE Remote_Server SHALL allow workflows from different projects to execute in parallel
3. WHEN a workflow reaches the front of the queue, THE Remote_Server SHALL create a Docker container using the `trayline-sandbox` image with the project directory mounted at `/workspace`, the trayline home directory mounted read-only at `/home/agent/.trayline`, the environment variable `DOCKER_HOST=tcp://trayline-proxy:2375`, and the container connected to the `trayline-net` network
4. WHEN executing a workflow, THE Remote_Server SHALL run the command `trayline run {pipeline} --var key=value ...` inside the container, constructing `--var` flags from the workflow's variables, with a configurable timeout (via `WORKFLOW_TIMEOUT` env var, default 5 hours) after which the container is forcefully terminated
5. WHEN a workflow container exits with code 0, THE Remote_Server SHALL set the workflow status to "completed", record completed_at timestamp, and clean up the container (stop and remove)
6. IF a workflow container exits with a non-zero code, THEN THE Remote_Server SHALL set the workflow status to "failed", record the exit code and captured stderr as the error message, clean up the container, and proceed to the next queued workflow
7. WHEN a workflow starts running, THE Remote_Server SHALL update its status to "running", record started_at timestamp, and record the container ID

### Requirement 5: Workflow Listing and Detail

**User Story:** As a developer, I want to see all workflows for a project with their current status, so that I can monitor execution progress and history.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose `GET /projects/{name}/workflows` that returns the most recent workflows for the project (up to 20), ordered by created_at descending (newest first)
2. WHEN listing workflows, THE Remote_Server SHALL return for each workflow: id, pipeline, variables, status, created_at, started_at, completed_at, and error (if failed)
3. THE Remote_Server SHALL expose `GET /projects/{name}/workflows/{id}` that returns full detail of a single workflow including all fields from the list response plus the captured log output
4. IF the workflow does not exist, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"
5. IF the specified project does not exist, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"
6. THE Remote_Server SHALL automatically evict the oldest terminal workflows (completed, failed, cancelled) when a project exceeds 20 stored workflows, keeping only the most recent 20 per project

### Requirement 6: Workflow Edit and Cancel

**User Story:** As a developer, I want to edit variables of a queued workflow or cancel it before it starts, so that I can correct mistakes without losing my queue position.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose `PUT /projects/{name}/workflows/{id}` to update the variables of a queued workflow
2. WHEN a workflow is edited, THE Remote_Server SHALL accept a JSON body containing: pipeline (optional, to change pipeline) and variables (object with updated key-value pairs), where the variables object fully replaces the existing variables (not merged) and the same validation rules from scheduling apply (pipeline must exist, variable keys match `[a-zA-Z0-9_-]`, values do not exceed 1000 characters)
3. IF the workflow status is not "queued", THEN THE Remote_Server SHALL return HTTP 409 with error code "CONFLICT" and message indicating only queued workflows can be edited
4. THE Remote_Server SHALL expose `DELETE /projects/{name}/workflows/{id}` to cancel a workflow
5. WHEN a queued workflow is cancelled via DELETE, THE Remote_Server SHALL set its status to "cancelled", remove it from the execution queue, and return HTTP 200 with the updated workflow object
6. WHEN a running workflow is cancelled via DELETE, THE Remote_Server SHALL send SIGTERM to the container, wait up to 10 seconds for exit, send SIGKILL if still running, set status to "cancelled", and proceed to the next queued workflow
7. IF the workflow status is already terminal (completed, failed, cancelled), THEN THE Remote_Server SHALL return HTTP 409 with error code "CONFLICT"
8. IF the workflow does not exist, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"
9. WHEN a workflow is successfully edited via PUT, THE Remote_Server SHALL return HTTP 200 with the updated workflow object reflecting the new pipeline and/or variables while preserving the workflow's queue position

### Requirement 7: Live Log Streaming

**User Story:** As a developer, I want to see real-time log output of a running workflow, so that I can monitor execution progress like I would in a terminal.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose `GET /projects/{name}/workflows/{id}/logs` as a WebSocket endpoint for live log streaming, requiring a valid Bearer token via the same authentication mechanism used by the existing chat WebSocket
2. WHEN a client connects to the log WebSocket for a running workflow, THE Remote_Server SHALL stream container stdout and stderr output as JSON messages `{"type": "output", "data": "<text>"}`
3. WHEN the workflow completes or fails, THE Remote_Server SHALL send `{"type": "finished", "status": "<completed|failed>", "exit_code": <number>}` and close the WebSocket connection immediately after the message is sent
4. IF the workflow is already in a terminal status (completed, failed, or cancelled) when the client connects, THEN THE Remote_Server SHALL send the stored log output as a single `{"type": "output", "data": "<text>"}` message followed by a `{"type": "finished", ...}` message, then close the connection
5. IF the workflow is in "queued" status when the client connects, THEN THE Remote_Server SHALL send `{"type": "waiting"}` and begin streaming output once the workflow starts running
6. THE Remote_Server SHALL capture and buffer workflow log output up to 1 MB so it remains available after the workflow completes; WHEN the buffer reaches 1 MB, THE Remote_Server SHALL discard the oldest output and continue capturing new output, retaining only the most recent 1 MB
7. IF the workflow does not exist, THEN THE Remote_Server SHALL reject the WebSocket upgrade with HTTP 404
8. IF a WebSocket connection is attempted without a valid authentication token, THEN THE Remote_Server SHALL reject the WebSocket upgrade with HTTP 401

### Requirement 8: Workflow Persistence

**User Story:** As a developer, I want the workflow queue to survive server restarts, so that scheduled workflows are not lost.

#### Acceptance Criteria

1. THE Remote_Server SHALL persist workflow state to a JSON file at `STATE_DIR/workflows.json` using atomic write (write to temp file, then rename)
2. WHEN a workflow is created, started, completed, failed, or cancelled, THE Remote_Server SHALL persist the updated state to disk within the same operation before returning a response to the client
3. WHEN the server starts and `STATE_DIR/workflows.json` does not exist, THE Remote_Server SHALL initialize an empty Workflow_Store and continue startup without error
4. WHEN the server starts and `STATE_DIR/workflows.json` exists, THE Remote_Server SHALL load persisted workflows from the JSON file and restore the Workflow_Store
5. IF the persisted JSON file exists but cannot be parsed as valid JSON, THEN THE Remote_Server SHALL log an error, rename the corrupted file to `workflows.json.corrupt`, initialize an empty Workflow_Store, and continue startup
6. WHEN the server starts and restores workflows, THE Remote_Server SHALL first set any workflow with "running" status to "failed" with the error message "server restarted and container was lost", then resume executing queued workflows in their original creation order
7. IF a disk write fails during persistence, THEN THE Remote_Server SHALL log an error and continue operating with the in-memory state without crashing the server
8. THE Remote_Server SHALL persist for each workflow: id, pipeline, variables, status, created_at, started_at, completed_at, error, and captured log output

### Requirement 9: Workflows Tab in Dashboard

**User Story:** As a developer, I want a "Workflows" tab on the project detail page, so that I can access workflow management from the dashboard.

#### Acceptance Criteria

1. THE Dashboard SHALL display a "Workflows" tab in the project detail TabBar, positioned after the "Env" tab and before the "Agent" tab
2. WHEN the Workflows tab is selected, THE Dashboard SHALL navigate to `/{project}/workflows` using `resolve('/[project]/workflows', { project })` and highlight the tab as active when `page.url.pathname.split('/')[2]` equals `'workflows'`
3. THE Dashboard SHALL display the Workflows tab label using the `tabs.workflows` translation key with value "Workflows" in English and "Workflows" in Czech
4. THE Dashboard SHALL render the Workflows tab without appending a `ref` query parameter to its URL, since workflows are not branch-specific

### Requirement 10: Workflow List View

**User Story:** As a developer, I want to see the workflow queue and execution history for a project, so that I can monitor what is scheduled, running, and completed.

#### Acceptance Criteria

1. WHEN the Workflows tab is opened, THE Dashboard SHALL fetch workflows from `GET /projects/{name}/workflows` and display them in a list
2. THE Dashboard SHALL display each workflow with: pipeline name (formatted as display_name), status badge (rounded pill-style, color-coded), created_at as relative time (e.g. "2 minutes ago"), and a summary of key variables (specs-name, path) when present
3. THE Dashboard SHALL visually distinguish workflows by status: queued (gray/slate badge), running (blue badge with a pulsing dot animation), completed (green badge), failed (red badge), cancelled (muted/dimmed badge)
4. THE Dashboard SHALL display a "New Workflow" button above the list that opens the workflow creation form
5. THE Dashboard SHALL auto-refresh the workflow list every 5 seconds while the tab is visible (document not hidden) and the component is mounted; THE Dashboard SHALL stop polling when the tab becomes hidden or the user navigates away
6. WHEN the user clicks a workflow in the list, THE Dashboard SHALL expand it inline to show full details including all variables as a key-value table and the log output viewer
7. IF the workflow list is empty, THEN THE Dashboard SHALL display an empty state message with text indicating no workflows have been scheduled yet and a call-to-action pointing to the "New Workflow" button
8. IF the workflow list fetch fails, THEN THE Dashboard SHALL display an inline error message and a retry button

### Requirement 11: Workflow Creation Form

**User Story:** As a developer, I want a form to configure and schedule a new workflow, so that I can select a pipeline, set variables, and add it to the queue.

#### Acceptance Criteria

1. THE Dashboard SHALL display a pipeline selector dropdown grouped by Pipeline_Type (tasks, processes, workflows) using optgroup-style grouping, with "processes/4-create-code" selected by default
2. WHEN a pipeline is selected, THE Dashboard SHALL fetch pipeline details from `GET /projects/{name}/pipelines/{type}/{pipeline}` and display all variables with their default values pre-filled
3. WHEN the selected pipeline has a `specs-name` variable, THE Dashboard SHALL display it as an autocomplete dropdown populated from `GET /projects/{name}/specs`, showing only specs with unchecked tasks sorted by creation date (newest first)
4. WHEN the selected pipeline has variables prefixed with `skip-`, THE Dashboard SHALL display them as toggle switches (on/off) that map to string values "true" and "false" respectively, with the initial state reflecting the pipeline YAML default value
5. WHEN the user clicks the "Schedule" button, THE Dashboard SHALL submit the form via `POST /projects/{name}/workflows` with the pipeline reference (format "type/name") and all variables as key-value string pairs, and disable the button during the request to prevent duplicate submissions
6. WHEN the form is submitted successfully, THE Dashboard SHALL close the form, refresh the workflow list, and display a success notification that auto-dismisses after 3 seconds
7. THE Dashboard SHALL validate that required variables (those with empty string "" defaults that are not skip flags) are filled before allowing submission, and display inline error messages next to each unfilled required field
8. THE Dashboard SHALL allow the user to change the `path` variable value, defaulting to the value from the pipeline YAML
9. IF the pipeline details fetch fails, THEN THE Dashboard SHALL display an error message within the form indicating the pipeline details could not be loaded and disable the "Schedule" button until a pipeline is successfully loaded
10. IF the workflow submission request fails, THEN THE Dashboard SHALL keep the form open with all user input preserved and display an inline error message indicating the submission failed

### Requirement 12: Workflow Edit and Cancel Controls

**User Story:** As a developer, I want to edit or cancel queued workflows from the dashboard, so that I can correct configuration mistakes or remove unnecessary work.

#### Acceptance Criteria

1. WHILE a workflow is in "queued" status, THE Dashboard SHALL display "Edit" and "Cancel" action buttons on the workflow entry
2. WHEN the user clicks "Edit" on a queued workflow, THE Dashboard SHALL open the workflow creation form pre-filled with the workflow's current pipeline and variable values
3. WHEN the user submits an edit, THE Dashboard SHALL send `PUT /projects/{name}/workflows/{id}` with the updated values and, on success, close the form and refresh the workflow list
4. IF the edit submission returns HTTP 409 (workflow is no longer queued), THEN THE Dashboard SHALL close the form, display an inline error message indicating the workflow can no longer be edited, and refresh the workflow list to reflect the current status
5. WHEN the user clicks "Cancel" on a queued or running workflow, THE Dashboard SHALL display a confirmation dialog stating the workflow pipeline name and asking the user to confirm cancellation, with a destructive "Cancel Workflow" button and a neutral "Keep" dismiss button
6. WHEN the user confirms cancellation, THE Dashboard SHALL send `DELETE /projects/{name}/workflows/{id}` and, on success, refresh the workflow list
7. IF the cancel request returns HTTP 409 (workflow already in terminal status), THEN THE Dashboard SHALL dismiss the confirmation dialog, display an inline error message indicating the workflow cannot be cancelled, and refresh the workflow list
8. WHILE a workflow is in "running" status, THE Dashboard SHALL display only a "Cancel" button (not "Edit")
9. WHILE a workflow is in a terminal status (completed, failed, cancelled), THE Dashboard SHALL display no action buttons

### Requirement 13: Live Log Viewer

**User Story:** As a developer, I want a terminal-like log viewer that shows real-time output of a running workflow, so that I can monitor execution as if running the pipeline in my terminal.

#### Acceptance Criteria

1. WHEN the user expands a running workflow, THE Dashboard SHALL establish a WebSocket connection to `GET /projects/{name}/workflows/{id}/logs` and display output in a terminal-styled viewer with a monospace font, dark background, and a fixed maximum height with vertical scrolling
2. THE Dashboard SHALL render log output preserving line breaks and whitespace, and auto-scroll the log viewer to the bottom as new output arrives, unless the user has manually scrolled up more than 50 pixels from the bottom
3. WHEN the workflow finishes (receives "finished" message), THE Dashboard SHALL display the final status and exit code at the bottom of the log viewer
4. WHEN the user expands a completed or failed workflow, THE Dashboard SHALL display the stored log output without establishing a WebSocket connection
5. IF the WebSocket connection is lost unexpectedly while streaming logs, THEN THE Dashboard SHALL display a reconnection notice in the log viewer and attempt to reconnect once within 10 seconds; if the reconnection attempt fails, THE Dashboard SHALL display a persistent disconnection error indicating logs are unavailable
6. IF the log output exceeds the 1 MB buffer, THE Dashboard SHALL display a notice at the top of the viewer that older output was truncated
7. WHEN the user collapses an expanded workflow entry that has an active WebSocket connection, THE Dashboard SHALL close the WebSocket connection

### Requirement 14: Trayline Home Directory Mount

**User Story:** As a developer, I want the workflow container to have access to trayline pipeline files, so that `trayline run` can find and execute pipeline definitions.

#### Acceptance Criteria

1. WHEN creating a workflow container, THE Remote_Server SHALL mount the configured `TRAYLINE_HOME_DIR` host path to `/home/agent/.trayline` inside the container as read-only
2. THE Remote_Server SHALL read `TRAYLINE_HOME_DIR` from the `TRAYLINE_HOME_DIR` environment variable (default: the absolute path of `~/.trayline` on the host, resolved at config load time using the running user's home directory)
3. IF `TRAYLINE_HOME_DIR` is set but the directory does not exist on the host when a workflow execution is attempted, THEN THE Remote_Server SHALL return HTTP 500 with error code "CONFIGURATION_ERROR" and a message indicating the configured trayline home directory is missing
4. IF `TRAYLINE_HOME_DIR` is empty or not set and the default path (`~/.trayline` expanded) does not exist, THEN THE Remote_Server SHALL return HTTP 500 with error code "CONFIGURATION_ERROR" when a workflow execution is attempted
5. THE Remote_Server SHALL derive `PIPELINES_DIR` as `TRAYLINE_HOME_DIR/pipelines` by default, or from the `PIPELINES_DIR` environment variable if explicitly set, and use this path for the pipeline discovery endpoints (Requirement 1)
6. IF `PIPELINES_DIR` does not exist or is not a directory when a pipeline listing or workflow execution is attempted, THEN THE Remote_Server SHALL return HTTP 500 with error code "CONFIGURATION_ERROR" and a message indicating the pipelines directory is missing
