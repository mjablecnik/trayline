# Requirements Document

## Introduction

Add per-project AI agent chat capability to the trayline dashboard. Each project gains a new "Agent" tab where the user can create an AI agent session (running in a Docker container via the existing trayline sandbox) scoped to that specific project's directory. The user communicates with the agent through a real-time chat interface using WebSocket, reusing the existing session infrastructure with project-scoped container mounting.

Source of truth: `dashboard/SPEC.md` (architecture, auth, CORS patterns)
Related existing code: `remote/api/session_handler.go`, `remote/docker/container.go`, `remote/store/session.go`

## Glossary

- **Dashboard**: The SvelteKit SPA frontend for browsing and managing projects
- **Remote_Server**: The existing Go backend (`remote/`) that handles API requests and container management
- **Container_Manager**: The `docker/ContainerManager` responsible for creating, starting, attaching, and stopping Docker containers
- **Session_Store**: The `store/SessionStore` that manages active chat sessions in memory
- **Project_Agent_Session**: A chat session scoped to a specific project directory (container mounts only that project's folder)
- **Agent_Tab**: A new tab in the project detail view that provides the chat interface
- **Chat_Interface**: The frontend component for sending messages and displaying streamed agent output
- **WebSocket_Connection**: The bidirectional communication channel between the dashboard and the Remote_Server for real-time chat

## Requirements

### Requirement 1: Project-Scoped Agent Session Creation

**User Story:** As a developer, I want to create an AI agent session scoped to a specific project, so that the agent operates only within that project's directory.

#### Acceptance Criteria

1. WHEN a project agent session is requested, THE Remote_Server SHALL create a Docker container with the workspace volume bind set to `PROJECTS_DIR/{project_name}:/workspace` so that only that project's subdirectory is accessible inside the container
2. WHEN a project agent session is requested with a valid agent type, THE Remote_Server SHALL accept agent values "kiro" and "claude"
3. IF the `agent` query parameter is missing or is not one of "kiro" or "claude", THEN THE Remote_Server SHALL return HTTP 400 with error code "VALIDATION_ERROR" and a message specifying the allowed values
4. WHEN the container is created for a project agent session, THE Remote_Server SHALL set the container working directory to `/workspace`
5. WHEN a project agent session is requested, THE Remote_Server SHALL validate the project name by checking it exists as a directory containing a `.git/` subdirectory under PROJECTS_DIR
6. THE Remote_Server SHALL reject project names containing path separators (`/`, `\`), dot-dot sequences (`..`), or characters outside `[a-zA-Z0-9._-]` with HTTP 400
7. IF the specified project does not exist in PROJECTS_DIR, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"
8. IF the server has no available concurrency slots, THEN THE Remote_Server SHALL return HTTP 503 with error code "SERVICE_UNAVAILABLE" and message "server is at capacity, try again later"

### Requirement 2: WebSocket Chat Endpoint for Project Agents

**User Story:** As a developer, I want a WebSocket endpoint to communicate with a project-scoped agent, so that I can send prompts and receive streamed responses in real time.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose a WebSocket endpoint at `GET /projects/{name}/chat` that creates a new project agent session
2. WHEN a WebSocket connection is established, THE Remote_Server SHALL send a JSON message `{"type": "session_started", "session_id": "<uuid>"}` as the first message
3. WHEN the client sends a JSON message `{"type": "message", "prompt": "<text>"}`, THE Remote_Server SHALL forward the prompt text to the agent container's stdin
4. WHEN the agent container produces output, THE Remote_Server SHALL stream the output to the client as JSON messages `{"type": "output", "data": "<text>"}`
5. WHEN the agent completes a response turn, THE Remote_Server SHALL send a JSON message `{"type": "done"}`
6. WHEN the client sends a JSON message `{"type": "interrupt"}`, THE Remote_Server SHALL send SIGINT to the agent container process
7. WHEN the client sends a JSON message `{"type": "terminate"}`, THE Remote_Server SHALL stop the container, close the session, and send `{"type": "terminated"}` before closing the WebSocket
8. THE Remote_Server SHALL require the `agent` query parameter and optionally accept `model` and `system` query parameters
9. IF the `agent` query parameter is missing or invalid, THEN THE Remote_Server SHALL reject the WebSocket upgrade with HTTP 400
10. IF the client sends a message with an unrecognized `type` field, THEN THE Remote_Server SHALL respond with `{"type": "error", "message": "unknown message type"}`
11. IF the client sends a message while the agent is still processing a previous prompt, THE Remote_Server SHALL queue and forward the message to stdin without blocking

### Requirement 3: Project Agent Session Reconnection

**User Story:** As a developer, I want to reconnect to an existing project agent session after a temporary disconnection, so that I do not lose the agent's state.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose a WebSocket endpoint at `GET /projects/{name}/chat/{id}` for reconnecting to an existing session
2. WHEN a client reconnects to an active session, THE Remote_Server SHALL send a `session_resumed` message containing the session ID, agent type, and model
3. IF the session ID does not exist or has been terminated or timed out, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"
4. IF the session already has an active WebSocket connection, THEN THE Remote_Server SHALL return HTTP 409 with error code "CONFLICT"
5. IF the session ID exists but does not belong to the project specified in the URL path, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"

### Requirement 4: Project Agent Session Listing

**User Story:** As a developer, I want to see active agent sessions for a project, so that I can reconnect to an existing session or know what is running.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose `GET /projects/{name}/sessions` to list active agent sessions for a specific project
2. WHEN sessions are listed, THE Remote_Server SHALL return for each session: session_id, agent, model, created_at, last_message_at, sorted by last_message_at descending (most recently active first), returning an empty array when no sessions exist
3. THE Remote_Server SHALL return only sessions belonging to the specified project
4. WHEN a session is terminated or times out, THE Remote_Server SHALL remove the session from the listing
5. IF the specified project does not exist, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"

### Requirement 5: Project Agent Session Termination via REST

**User Story:** As a developer, I want to terminate a project agent session via a REST endpoint, so that I can clean up sessions without needing a WebSocket connection.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose `POST /projects/{name}/sessions/{id}/terminate` to terminate a specific session
2. WHEN a session is terminated, THE Remote_Server SHALL stop the Docker container with a 10-second graceful timeout and then remove it
3. WHEN a session is terminated, THE Remote_Server SHALL send a `{"type": "terminated"}` message to any connected WebSocket client before closing the connection
4. IF the session does not exist or does not belong to the specified project, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"
5. WHEN a session is successfully terminated, THE Remote_Server SHALL return HTTP 200 with `{"session_id": "<id>", "status": "terminated"}`

### Requirement 6: Agent Session Idle Timeout

**User Story:** As a developer, I want agent sessions to be automatically terminated after a period of inactivity, so that resources are not wasted.

#### Acceptance Criteria

1. WHILE a project agent session has no client messages and no agent output for the configured SESSION_TIMEOUT duration (default: 30 minutes), THE Remote_Server SHALL automatically terminate the session; the idle timer resets on any client message, agent output, or successful WebSocket reconnection
2. WHEN an idle session is terminated, THE Remote_Server SHALL send a `{"type": "terminated"}` message to any connected WebSocket client before closing; if WebSocket delivery fails, termination proceeds regardless
3. WHEN an idle session is terminated, THE Remote_Server SHALL stop and remove the associated Docker container

### Requirement 7: Agent Tab in Dashboard

**User Story:** As a developer, I want an "Agent" tab on the project detail page, so that I can access the chat interface for the project's AI agent.

#### Acceptance Criteria

1. THE Dashboard SHALL display an "Agent" tab in the project detail TabBar as the last tab after the existing tabs (Files, Commits, Changes, Environment)
2. WHEN the Agent tab is selected, THE Dashboard SHALL navigate to `/{project}/agent` and highlight the Agent tab as active based on the `agent` URL path segment
3. THE Dashboard SHALL display the Agent tab label using the `tabs.agent` translation key with the value "Agent" in English and "Agent" in Czech
4. THE Dashboard SHALL render the Agent tab without appending a `ref` query parameter to its URL, since the agent chat is not branch-specific

### Requirement 8: Chat Interface Component

**User Story:** As a developer, I want a chat interface in the Agent tab, so that I can send messages to the agent and see its responses in real time.

#### Acceptance Criteria

1. THE Chat_Interface SHALL display a message input area at the bottom and a scrollable message history area above it
2. WHEN the user submits a message (via Enter key or send button), THE Chat_Interface SHALL send the message over the WebSocket connection, display the message in the history, and clear the input field
3. THE Chat_Interface SHALL prevent submitting empty or whitespace-only messages (send button disabled and Enter key does nothing)
4. WHEN streamed output is received from the agent (messages of type "output"), THE Chat_Interface SHALL append the output text to the current agent response in real time
5. WHEN a "done" message is received, THE Chat_Interface SHALL mark the agent response as complete and re-enable the message input
6. THE Chat_Interface SHALL display user messages and agent responses with distinct visual styling (different background colors and alignment)
7. THE Chat_Interface SHALL auto-scroll to the latest message when new output is received, unless the user has manually scrolled up; if the user scrolls up, a "scroll to bottom" indicator appears
8. WHILE the agent is processing (between sending a message and receiving "done"), THE Chat_Interface SHALL display a typing/loading indicator and disable the message input

### Requirement 9: Agent Selection and Session Management UI

**User Story:** As a developer, I want to choose which AI agent and model to use, manage multiple sessions, and control the session lifecycle from the UI.

#### Acceptance Criteria

1. THE Chat_Interface SHALL provide an agent selector with options "kiro" and "claude", with no option pre-selected by default
2. THE Chat_Interface SHALL provide a model text input field (maximum 100 characters) where the user can specify the AI model to use, which remains optional and uses the agent default if left empty
3. WHEN no active session exists, THE Chat_Interface SHALL display a "Start Agent" button that is enabled only when an agent is selected from the selector
4. WHEN the user clicks "Start Agent", THE Dashboard SHALL disable the "Start Agent" button, display a connecting indicator, and establish a WebSocket connection to `GET /projects/{name}/chat?agent={agent}&model={model}`
5. WHILE an active session exists, THE Chat_Interface SHALL display an "Interrupt" button that sends a WebSocket message of type "interrupt" when clicked
6. WHILE an active session exists, THE Chat_Interface SHALL display a "Terminate" button that sends a WebSocket message of type "terminate" when clicked
7. WHEN a session is terminated (server sends a "terminated" message or the WebSocket closes after a terminate request), THE Chat_Interface SHALL return to the initial state displaying the agent selector and "Start Agent" button with the previously selected agent and model values preserved

### Requirement 10: Session Switching

**User Story:** As a developer, I want to switch between multiple active agent sessions for a project, so that I can run parallel tasks and switch context.

#### Acceptance Criteria

1. WHEN the Agent tab is opened, THE Dashboard SHALL fetch the list of active sessions for the project from `GET /projects/{name}/sessions`
2. IF active sessions exist, THEN THE Chat_Interface SHALL display a session list showing each session's agent type, model, and last activity time, with the currently connected session visually highlighted
3. WHEN the user selects a different session from the list, THE Dashboard SHALL disconnect from the current WebSocket and reconnect to the selected session via `GET /projects/{name}/chat/{id}`
4. WHEN the user switches to a different session, THE Chat_Interface SHALL retain the message history of all previously viewed sessions in client memory so the user can switch back without losing context
5. THE Chat_Interface SHALL allow the user to start a new additional session while other sessions are active
6. THE Chat_Interface SHALL allow the user to terminate any individual session from the session list
7. WHEN a session is created or terminated, THE Chat_Interface SHALL refresh the session list to reflect the current server state
8. IF the session list fetch fails, THEN THE Chat_Interface SHALL display an inline error message and allow the user to retry

### Requirement 11: Chat Interface Error Handling

**User Story:** As a developer, I want the chat interface to handle errors gracefully, so that I am informed when something goes wrong without losing context.

#### Acceptance Criteria

1. IF the WebSocket connection closes without a client-initiated disconnect, THEN THE Chat_Interface SHALL display a connection error message and a "Reconnect" button that initiates a single reconnection attempt when clicked
2. IF the server returns an error during session creation, THEN THE Chat_Interface SHALL display the error message inline near the session creation controls without navigating away from the Agent tab
3. IF the server returns a 503 (at capacity) error, THEN THE Chat_Interface SHALL display a message indicating the server is busy and to try again later
4. IF a reconnection attempt fails (server returns 404 or the connection cannot be established within 10 seconds), THEN THE Chat_Interface SHALL display a message indicating the session is no longer available and offer a "Start New Session" button to return to the agent selection state
5. IF a connection error occurs, THEN THE Chat_Interface SHALL preserve the displayed message history and display the error message without replacing or clearing the conversation view
6. IF sending a message fails due to a WebSocket error during an active session, THEN THE Chat_Interface SHALL display an inline error below the failed message and retain the message text in the input area

### Requirement 12: File Upload to Agent Container

**User Story:** As a developer, I want to upload files to the agent's container via the chat interface, so that the agent can reference or process them without polluting the project directory.

#### Acceptance Criteria

1. WHEN the client sends a WebSocket binary frame during an active session, THE Remote_Server SHALL decode the frame (4-byte filename length + filename + content) and write the file into the running container at `/tmp/uploads/{sanitized_filename}` using `docker cp`
2. WHEN a file is successfully uploaded, THE Remote_Server SHALL respond with `{"type": "file_uploaded", "data": "<original_filename>"}`
3. WHEN the next user message is sent after file uploads, THE Remote_Server SHALL prepend upload metadata to the prompt indicating file locations: `[Uploaded Files]\n- filename → /tmp/uploads/filename\n`
4. THE Remote_Server SHALL validate uploaded files: maximum 50 MB per file, filename sanitized (path traversal characters removed)
5. IF a file upload fails (container not running, size exceeded), THEN THE Remote_Server SHALL respond with `{"type": "error", "message": "<description>"}`
6. WHEN the session is terminated and the Docker container is removed, THE uploaded files SHALL be automatically deleted (they live only inside the container filesystem)
7. THE agent MAY copy uploaded files from `/tmp/uploads/` to `/workspace/` (the project directory) if the user instructs it to do so; this is not automatic
8. THE Chat_Interface SHALL provide a file upload button (or drag-and-drop area) that sends binary WebSocket frames to the server
9. WHEN a file is uploaded, THE Chat_Interface SHALL display a confirmation message in the chat showing the uploaded filename
