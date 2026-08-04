# Requirements Document

## Introduction

Add a global personal AI assistant to the trayline dashboard. Unlike per-project agents (spec 009), this assistant is not scoped to a single project but has access to all projects via a separate mount and persists its own data in a dedicated assistant folder. The container mounts the assistant folder as `/workspace` (the CWD, where Claude CLI finds CLAUDE.md), all projects at `/projects/`, and agent credentials at `/home/agent/`. The assistant is accessible via a dedicated `/assistant` route in the dashboard and reuses the existing WebSocket chat protocol, container management, and session infrastructure.

## Glossary

- **Dashboard**: The SvelteKit SPA frontend for browsing and managing projects
- **Remote_Server**: The existing Go backend (`remote/`) that handles API requests and container management
- **Container_Manager**: The `docker/ContainerManager` responsible for creating, starting, attaching, and stopping Docker containers
- **Session_Store**: The `store/SessionStore` that manages active chat sessions in memory
- **Assistant_Session**: A chat session for the personal assistant (container mounts assistant folder as workspace and projects separately)
- **Assistant_Container**: A Docker container running the personal assistant agent with the assistant folder at `/workspace`, projects at `/projects/`, and credentials at `/home/agent/`
- **Assistant_Folder**: A dedicated host directory (configured via `ASSISTANT_DATA_DIR`, default `{parent of PROJECTS_DIR}/.assistant`) that serves as the assistant's workspace and data root; contains CLAUDE.md at the top level and subdirectories `memory/` and `prompts/` for organized persistent storage; maintained as a git repository
- **CLAUDE_MD**: A markdown file at the root of the Assistant_Folder (`/workspace/CLAUDE.md` inside the container) that defines the assistant's personality and rules; read by the Claude CLI automatically because it is in the working directory
- **Starter_Prompt**: A text file stored in the `prompts/` subdirectory of the Assistant_Folder that contains a pre-written prompt the user can select when starting a session
- **Chat_Interface**: The frontend component for sending messages and displaying streamed agent output
- **WebSocket_Connection**: The bidirectional communication channel between the dashboard and the Remote_Server for real-time chat
- **Summarize_Action**: A special action that instructs the assistant to create a concise summary of the current conversation and save it to a dedicated summary file; the summary can later be used as the initial message when starting a new session after reset
- **Summary_File**: A file at `/workspace/summary.md` (i.e. `ASSISTANT_DATA_DIR/summary.md` on the host) that stores the latest conversation summary; overwritten on each Summarize action; readable via the file browser
- **Assistant_Browser**: A tab in the assistant UI that displays the contents of the Assistant_Folder, allowing the user to browse files and read their content
- **Assistant_Git_Repository**: The Assistant_Folder is maintained as a git repository; the CLAUDE_MD instructs the agent to commit changes after every modification

## Requirements

### Requirement 1: Assistant Container Mount Layout

**User Story:** As a user, I want the personal assistant container to have a clear mount layout separating workspace, projects, and credentials, so that the assistant can access its data, all projects, and agent tools without mounting the entire home directory.

#### Acceptance Criteria

1. WHEN an assistant session is created, THE Container_Manager SHALL create a Docker container with three volume binds: `ASSISTANT_DATA_DIR:/workspace` (read-write), `PROJECTS_DIR:/projects` (read-write), and agent credential directories at `/home/agent/` (read-only, following existing credential mount patterns per agent type)
2. WHEN an assistant container is created, THE Container_Manager SHALL set the container working directory to `/workspace` so that the Claude CLI automatically discovers CLAUDE.md in the current directory
3. WHEN an assistant container is created, THE Container_Manager SHALL set the container name with prefix `trayline-assistant-` followed by a UUID v4 identifier so the container is clearly distinguishable from project agent containers
4. THE Remote_Server SHALL read the assistant folder host path from the `ASSISTANT_DATA_DIR` environment variable during startup
5. IF the `ASSISTANT_DATA_DIR` environment variable is not set, THEN THE Remote_Server SHALL default to `{parent directory of PROJECTS_DIR}/.assistant`
6. IF the resolved `ASSISTANT_DATA_DIR` path does not exist on the host at server startup, THEN THE Remote_Server SHALL create it with standard directory permissions (0755) along with subdirectories `memory/` and `prompts/`
7. IF the resolved `ASSISTANT_DATA_DIR` path exists but is not a directory, THEN THE Remote_Server SHALL fail startup with an error message indicating the path is not a directory
8. IF the resolved `ASSISTANT_DATA_DIR` path exists but is missing any of the required subdirectories (`memory/`, `prompts/`), THEN THE Remote_Server SHALL create the missing subdirectories during startup

### Requirement 2: CLAUDE.md Personality Definition

**User Story:** As a user, I want to define the assistant's personality and rules through a CLAUDE.md file in the assistant folder, so that the Claude CLI reads it automatically from the working directory without a system prompt override.

#### Acceptance Criteria

1. THE CLAUDE_MD file SHALL be located at the root of the Assistant_Folder on the host filesystem (at `ASSISTANT_DATA_DIR/CLAUDE.md`), which maps to `/workspace/CLAUDE.md` inside the container
2. WHEN the container working directory is set to `/workspace`, THE Claude CLI SHALL discover and read CLAUDE.md automatically without any CLI flag or system prompt parameter
3. THE Remote_Server SHALL NOT pass the contents of CLAUDE_MD as a `--append-system-prompt` CLI parameter to the agent; the agent SHALL read the file from its working directory naturally
4. WHEN the Assistant_Folder is first created and no CLAUDE_MD file exists, THE Remote_Server SHALL create a default CLAUDE_MD file containing: a role definition identifying the agent as a personal assistant, the workspace layout description (`/workspace` = assistant data, `/projects/` = all projects), descriptions of the subdirectory structure (`memory/` for persistent knowledge and session highlights, `prompts/` for saved starter prompts), and instructions on git auto-commit after changes in `/workspace`
5. IF the CLAUDE_MD file exists but is not readable (permission error or I/O failure), THEN THE Remote_Server SHALL log a warning and proceed with session creation without the personality file (the agent runs with default behavior)

### Requirement 3: Starter Prompts Storage and Retrieval

**User Story:** As a user, I want to store pre-written prompts as files in the assistant folder, so that I can quickly select common prompts when starting a new session.

#### Acceptance Criteria

1. THE Remote_Server SHALL store starter prompts as individual text files in the `prompts/` subdirectory within the Assistant_Folder, creating the subdirectory automatically if it does not exist
2. THE Remote_Server SHALL expose `GET /assistant/prompts` to list all available starter prompts, returning for each prompt: filename, display name (filename without extension, with hyphens and underscores replaced by spaces), and content
3. WHEN listing prompts, THE Remote_Server SHALL read all `.md` and `.txt` files from the `prompts/` subdirectory, sorted alphabetically by filename
4. THE Remote_Server SHALL expose `GET /assistant/prompts/{filename}` to retrieve the content of a single starter prompt
5. THE Remote_Server SHALL expose `PUT /assistant/prompts/{filename}` to create or update a starter prompt file, accepting a request body of up to 10,000 characters
6. THE Remote_Server SHALL expose `DELETE /assistant/prompts/{filename}` to delete a starter prompt file
7. THE Remote_Server SHALL validate prompt filenames: only alphanumeric characters, hyphens, underscores, and dots are allowed; maximum length is 100 characters including extension; path separators and dot-dot sequences are rejected with HTTP 400
8. THE assistant agent SHALL have access to the prompts directory at `/workspace/prompts/` and MAY create or edit prompt files directly
9. IF a requested prompt file does not exist, THEN THE Remote_Server SHALL return HTTP 404 with an error response indicating the file was not found

### Requirement 4: Starter Prompts UI

**User Story:** As a user, I want to see and select starter prompts in the chat interface, so that I can quickly start conversations with pre-written prompts.

#### Acceptance Criteria

1. WHEN no active session exists or after a session reset, THE Chat_Interface SHALL display up to 10 starter prompts fetched from `GET /assistant/prompts`, showing each prompt's title and a truncated preview of its content (maximum 100 characters with ellipsis if longer)
2. WHEN the user clicks a starter prompt, THE Chat_Interface SHALL insert the prompt's full content into the message input field without sending it automatically
3. THE Chat_Interface SHALL allow the user to edit the inserted prompt text before sending
4. WHEN the starter prompt list is empty, THE Chat_Interface SHALL display the agent selector and start button without a prompt section
5. WHEN the user navigates to the prompts section of the dashboard, THE Chat_Interface SHALL display all prompts with their full content in a dedicated list view
6. IF fetching starter prompts fails, THEN THE Chat_Interface SHALL display the agent selector without the prompts section and show an inline warning indicating the prompts could not be loaded

### Requirement 5: Assistant Container Naming

**User Story:** As a user, I want the assistant container to have a distinctive name prefix, so that it is clearly identifiable and will not be accidentally terminated by cleanup scripts.

#### Acceptance Criteria

1. WHEN an assistant container is created, THE Container_Manager SHALL assign a container name following the pattern `trayline-assistant-{session_id_short}` where `session_id_short` is the first 8 characters of the session UUID
2. IF a container with the computed name already exists (Docker returns a naming conflict), THEN THE Container_Manager SHALL append a numeric suffix starting at `-2` and incrementing until an unused name is found, up to a maximum of 5 attempts, after which it SHALL return an error
3. THE Container_Manager SHALL pass the computed container name to Docker's `ContainerCreate` API as the `containerName` parameter during container creation
4. THE existing container cleanup logic (RunOneShot post-execution removal and StopAndRemoveContainer) SHALL NOT terminate containers whose name starts with the `trayline-assistant-` prefix unless the removal is initiated through the assistant session termination endpoint (`POST /assistant/sessions/{id}/terminate`) or idle timeout auto-termination

### Requirement 6: Multiple Assistant Sessions

**User Story:** As a user, I want to run multiple assistant sessions simultaneously, so that I can maintain separate conversation contexts for different topics.

#### Acceptance Criteria

1. THE Remote_Server SHALL allow multiple concurrent assistant sessions, up to the `MAX_CHAT_SESSIONS` limit (configurable between 1 and 32, default 4); this limit is shared globally across assistant and project agent sessions
2. THE Remote_Server SHALL expose `GET /assistant/sessions` to list all active assistant sessions, returning for each: session_id, agent type, model, created_at, and last_message_at, sorted by last_message_at descending
3. WHEN the user starts a new assistant session while other sessions are active and the total active session count is below `MAX_CHAT_SESSIONS`, THE Remote_Server SHALL create a new session without terminating existing ones
4. IF the user starts a new assistant session and the total active session count has reached `MAX_CHAT_SESSIONS`, THEN THE Remote_Server SHALL return HTTP 503 with error code "SERVICE_UNAVAILABLE"
5. THE Chat_Interface SHALL display a session list panel showing all active assistant sessions, allowing the user to switch between them by selecting a session entry
6. WHEN the user switches sessions, THE Chat_Interface SHALL retain message history of previously viewed sessions in client memory

### Requirement 7: Summarize Action

**User Story:** As a user, I want a button that creates a summary of the current conversation and saves it to a file, so that I can use it as context for a new session after reset without keeping the full conversation history.

#### Acceptance Criteria

1. WHILE an active assistant session exists AND the agent is not processing a response, THE Chat_Interface SHALL display an enabled "Summarize" button; WHILE the agent is processing (between sending any message and receiving "done"), THE Chat_Interface SHALL disable the Summarize button
2. WHEN the user clicks "Summarize", THE Chat_Interface SHALL send a predefined prompt as a regular user message instructing the assistant to create a concise summary of the entire conversation covering: key topics discussed, decisions made, important information shared, and any pending action items; and save it to `/workspace/summary.md`
3. WHEN the user clicks "Summarize", THE Chat_Interface SHALL display the predefined prompt text in the message history as a user message so the user can see what was sent
4. THE predefined Summarize prompt SHALL instruct the assistant to output the summary content in its response so the user can read and verify it
5. WHILE the Summarize action is processing, THE Chat_Interface SHALL disable the Summarize button and display the same typing/loading indicator used for regular agent responses
6. WHEN a "done" WebSocket message is received after a Summarize prompt, THE Chat_Interface SHALL re-enable the Summarize button and mark the agent response as complete
7. THE summary SHALL be stored at the root of the Assistant_Folder as `summary.md` (`/workspace/summary.md` inside the container), overwriting any previous summary file
8. THE user SHALL be able to read the summary file via the file browser (Files tab) at the path `summary.md`

### Requirement 8: Reset Session with Summary Option

**User Story:** As a user, I want a button to reset the current assistant session with the option to carry over the summary, so that I can start fresh with reduced context while preserving key information.

#### Acceptance Criteria

1. WHILE an active assistant session exists, THE Chat_Interface SHALL display a "Reset" button
2. WHEN the user clicks "Reset", THE Chat_Interface SHALL display a confirmation dialog asking whether to send the last summary as the first message of the new session, with options: "With Summary" and "Without Summary"
3. IF the user selects "With Summary" AND a summary file exists (`summary.md` in the Assistant_Folder), THEN THE Chat_Interface SHALL terminate the current session, start a new session with the same agent and model, and insert the content of `summary.md` into the message input field without sending it
4. IF the user selects "With Summary" AND no summary file exists, THEN THE Chat_Interface SHALL display an inline warning that no summary is available and proceed as "Without Summary"
5. IF the user selects "Without Summary", THEN THE Chat_Interface SHALL terminate the current session and return to the initial state showing the agent selector, model input, and starter prompts
6. WHEN a session is reset, THE Chat_Interface SHALL clear the message history for that session from client memory while preserving any other sessions' histories in the session list
7. WHEN a session is reset, THE Container_Manager SHALL stop the associated Docker container with a 10-second graceful timeout and then remove it
8. IF the user clicks "Reset" while the WebSocket connection is closed or in an error state, THEN THE Chat_Interface SHALL skip the dialog and clear the local session state returning to the initial state without sending a WebSocket message

### Requirement 9: Dashboard Route and Navigation

**User Story:** As a user, I want a dedicated route in the dashboard for the personal assistant, so that I can access it separately from project-specific views.

#### Acceptance Criteria

1. THE Dashboard SHALL expose the assistant chat interface at the route `/assistant`
2. THE Dashboard SHALL display a navigation link to the assistant page in the main header navigation area, positioned after the existing navigation items, visible from all authenticated pages including both desktop and mobile navigation
3. THE Dashboard SHALL display the navigation link label using the translation key `nav.assistant` with value "Assistant" in English and "Asistent" in Czech
4. WHEN the user navigates to `/assistant`, THE Dashboard SHALL render the assistant chat interface with session list, agent selector, and starter prompts
5. THE assistant route SHALL NOT require a project context; it operates independently of any selected project and passes no project identifier to the chat interface or session list

### Requirement 10: WebSocket Chat Endpoint for Assistant

**User Story:** As a user, I want a WebSocket endpoint for the assistant chat, so that I can communicate with the assistant in real time using the same protocol as project agents.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose a WebSocket endpoint at `GET /assistant/chat` that upgrades the connection and creates a new assistant session
2. THE Remote_Server SHALL accept query parameters: `agent` (required, "kiro" or "claude"), `model` (optional, maximum 100 characters), and `system` (optional)
3. IF the `agent` query parameter is missing or is not one of "kiro" or "claude", THEN THE Remote_Server SHALL reject the WebSocket upgrade with HTTP 400 and error code "VALIDATION_ERROR"
4. WHEN a WebSocket connection is established, THE Remote_Server SHALL send `{"type": "session_started", "session_id": "<uuid>"}` as the first message
5. THE Remote_Server SHALL support the same WebSocket message protocol as the project agent endpoint: client-to-server types `message`, `interrupt`, and `terminate`; server-to-client types `output`, `done`, `error`, and `terminated`
6. IF the client sends a message with an unrecognized `type` field, THEN THE Remote_Server SHALL respond with `{"type": "error", "message": "unknown message type"}`
7. IF the client sends a message while the agent is still processing a previous prompt, THE Remote_Server SHALL queue and forward the message to stdin without blocking
8. THE Remote_Server SHALL expose a WebSocket endpoint at `GET /assistant/chat/{id}` for reconnecting to an existing assistant session
9. WHEN a client reconnects to an active assistant session, THE Remote_Server SHALL send `{"type": "session_resumed", "session_id": "<id>", "agent": "<agent>", "model": "<model>"}` as the first message
10. IF the session ID does not exist or has timed out, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"
11. IF the session already has an active WebSocket connection, THEN THE Remote_Server SHALL return HTTP 409 with error code "CONFLICT"

### Requirement 11: Session History on Reconnect

**User Story:** As a user, I want to see past messages when I reconnect to an existing session, so that I have full context of the conversation without losing history.

#### Acceptance Criteria

1. WHEN a client reconnects to an existing session via `GET /assistant/chat/{id}`, THE Remote_Server SHALL send a `{"type": "history", "messages": [...]}` message after the `session_resumed` message containing the full message transcript
2. THE history messages array SHALL contain objects with fields: `role` ("user" or "assistant"), `content` (the message text), and `timestamp` (ISO 8601 format)
3. THE Remote_Server SHALL maintain the message transcript for each active session in the Session_Store, appending each user prompt and completed agent response
4. WHEN a session is terminated or times out, THE Remote_Server SHALL discard the stored message transcript along with the session
5. THE Chat_Interface SHALL render the received history messages in the message area upon reconnection, replacing any stale local state for that session

### Requirement 12: Agent Type Selection

**User Story:** As a user, I want to choose between "kiro" and "claude" agent types when starting an assistant session, so that I can use my preferred AI agent.

#### Acceptance Criteria

1. THE Chat_Interface SHALL provide an agent selector with exactly two options: "kiro" and "claude", with no option pre-selected by default
2. THE Chat_Interface SHALL provide an optional model text input field (maximum 100 characters) that accepts free-form text
3. WHEN the user selects an agent, THE Chat_Interface SHALL enable the "Start" button; WHILE no agent is selected, THE Chat_Interface SHALL keep the "Start" button disabled
4. IF the agent query parameter is missing or has a value other than "kiro" or "claude", THEN THE Remote_Server SHALL reject the WebSocket upgrade with HTTP 400

### Requirement 13: Session Termination via REST

**User Story:** As a user, I want to terminate an assistant session via a REST endpoint, so that I can clean up sessions without an active WebSocket connection.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose `POST /assistant/sessions/{id}/terminate` to terminate a specific assistant session
2. WHEN a session is terminated, THE Remote_Server SHALL stop the Docker container with a 10-second graceful timeout and then remove it
3. WHEN a session is terminated, THE Remote_Server SHALL send `{"type": "terminated"}` to any connected WebSocket client before closing the connection
4. IF the session does not exist, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"
5. WHEN a session is successfully terminated, THE Remote_Server SHALL return HTTP 200 with `{"session_id": "<id>", "status": "terminated"}`
6. WHEN a session is terminated, THE Remote_Server SHALL remove the session from the Session_Store so it no longer appears in session listings

### Requirement 14: Assistant Session Idle Timeout

**User Story:** As a user, I want assistant sessions to be automatically terminated after inactivity, so that resources are not wasted.

#### Acceptance Criteria

1. WHILE an assistant session has no client messages and no agent output for the configured SESSION_TIMEOUT duration (default: 24 hours), THE Remote_Server SHALL automatically terminate the session; the idle timer resets on any client message, agent output, or successful WebSocket reconnection
2. WHEN an idle session is terminated, THE Remote_Server SHALL send a `{"type": "terminated"}` message to any connected WebSocket client before closing; if WebSocket delivery fails, termination proceeds regardless
3. WHEN an idle session is terminated, THE Remote_Server SHALL stop the associated Docker container with a 10-second graceful timeout and then remove it

### Requirement 15: Initial Prompt on Session Start

**User Story:** As a user, I want to optionally select a starter prompt before starting a session, so that the first message is pre-filled and ready to send or edit.

#### Acceptance Criteria

1. WHEN no active session exists, THE Chat_Interface SHALL display a list of starter prompts within the agent selector panel, each shown as a selectable option displaying the prompt title and a truncated preview
2. WHEN the user selects a starter prompt and clicks "Start Agent", THE Chat_Interface SHALL establish the WebSocket connection and insert the selected prompt text into the message input field without sending it
3. THE Chat_Interface SHALL NOT auto-send the starter prompt; the user must explicitly send it via Enter key or send button
4. THE Chat_Interface SHALL allow the user to start a session without selecting any starter prompt, leaving the message input field empty after connection
5. WHEN a session becomes active (connection state changes to "connected"), THE Chat_Interface SHALL hide the starter prompt list and display the chat interface
6. WHEN the user has selected a starter prompt, THE Chat_Interface SHALL allow the user to deselect it by clicking it again before starting the session, so that no prompt is pre-filled

### Requirement 16: Chat Interface Error Handling

**User Story:** As a developer, I want the chat interface to handle errors gracefully, so that I am informed when something goes wrong without losing context.

#### Acceptance Criteria

1. IF the WebSocket connection closes without a client-initiated disconnect, THEN THE Chat_Interface SHALL display a connection error message and a "Reconnect" button that initiates a single reconnection attempt when clicked
2. IF the server returns an error during session creation, THEN THE Chat_Interface SHALL display the error message inline near the session creation controls without navigating away from the Agent tab
3. IF the server returns a 503 (at capacity) error, THEN THE Chat_Interface SHALL display a message indicating the server is busy and to try again later
4. IF a reconnection attempt fails (server returns 404 or the connection cannot be established within 10 seconds), THEN THE Chat_Interface SHALL display a message indicating the session is no longer available and offer a "Start New Session" button to return to the agent selection state
5. IF a connection error occurs, THEN THE Chat_Interface SHALL preserve the displayed message history and display the error message without replacing or clearing the conversation view
6. IF sending a message fails due to a WebSocket error during an active session, THEN THE Chat_Interface SHALL display an inline error below the failed message and retain the message text in the input area

### Requirement 17: File Upload to Agent Container

**User Story:** As a user, I want to upload files to the agent's container via the chat interface, so that the agent can reference or process them without polluting the assistant workspace.

#### Acceptance Criteria

1. WHEN the client sends a WebSocket binary frame during an active session, THE Remote_Server SHALL decode the frame (4-byte filename length + filename + content) and write the file into the running container at `/tmp/uploads/{sanitized_filename}` using `docker cp`
2. WHEN a file is successfully uploaded, THE Remote_Server SHALL respond with `{"type": "file_uploaded", "data": "<original_filename>"}`
3. WHEN the next user message is sent after file uploads, THE Remote_Server SHALL prepend upload metadata to the prompt indicating file locations: `[Uploaded Files]\n- filename → /tmp/uploads/filename\n`
4. THE Remote_Server SHALL validate uploaded files: maximum 50 MB per file, filename sanitized (path traversal characters removed)
5. IF a file upload fails (container not running, size exceeded), THEN THE Remote_Server SHALL respond with `{"type": "error", "message": "<description>"}`
6. WHEN the session is terminated and the Docker container is removed, THE uploaded files SHALL be automatically deleted (they live only inside the container filesystem)
7. THE agent MAY copy uploaded files from `/tmp/uploads/` to `/workspace/` or `/projects/` if the user instructs it to do so; this is not automatic
8. THE Chat_Interface SHALL provide a file upload button (or drag-and-drop area) that sends binary WebSocket frames to the server
9. WHEN a file is uploaded, THE Chat_Interface SHALL display a confirmation message in the chat showing the uploaded filename

### Requirement 18: Assistant Folder File Browser

**User Story:** As a user, I want a file browser tab in the assistant view that shows the contents of the assistant folder, so that I can browse notes, memory, prompts, and the CLAUDE.md file without switching to a terminal.

#### Acceptance Criteria

1. THE Dashboard SHALL display a "Files" tab in the assistant view alongside the "Chat" tab, using the translation key `assistant.filesTab` with value "Files" in English and "Soubory" in Czech
2. THE Remote_Server SHALL expose `GET /assistant/files` to list the top-level contents of the Assistant_Folder, returning for each entry: name, type (file or directory), and size in bytes, sorted alphabetically with directories listed first
3. THE Remote_Server SHALL expose `GET /assistant/files/{path...}` to list directory contents or return file content for a given path within the Assistant_Folder
4. WHEN the path points to a directory, THE Remote_Server SHALL return a directory listing with entries containing name, type, and size
5. WHEN the path points to a file, THE Remote_Server SHALL return the file content as a string along with filename, size, and path; files larger than 1 MB SHALL return `"content": null` with `"truncated": true`
6. THE Remote_Server SHALL validate all paths: reject path traversal sequences (`..`), absolute paths, and characters outside `[a-zA-Z0-9._/-]` with HTTP 400 and error code "VALIDATION_ERROR"
7. IF a requested path does not exist within the Assistant_Folder, THEN THE Remote_Server SHALL return HTTP 404 with error code "NOT_FOUND"
8. THE Assistant_Browser SHALL display directory contents as a navigable list with breadcrumb navigation, allowing the user to click into subdirectories and back
9. WHEN a file is selected in the Assistant_Browser, THE Assistant_Browser SHALL display the file content with syntax highlighting (for markdown files) or plain text with preserved whitespace (read-only, no editing capability)
10. THE Assistant_Browser SHALL display the file browser as a read-only view; no create, edit, or delete actions are exposed through the file browser UI (file modifications happen through the agent chat or the REST prompts endpoints)

### Requirement 19: Session Visual Identification

**User Story:** As a user, I want assistant sessions to be visually distinct from project agent sessions, so that I can quickly identify which is my personal assistant in a list of sessions.

#### Acceptance Criteria

1. WHEN listing assistant sessions in the session list panel, THE Chat_Interface SHALL display a distinct icon (star or user icon) and the label "Assistant" next to each session entry to differentiate them from project agent sessions
2. THE Remote_Server SHALL mark assistant sessions in the Session_Store with a special project value of `__assistant__` so that they can be distinguished from project-scoped sessions
3. WHEN the GET /assistant/sessions endpoint returns session data, THE Remote_Server SHALL include an `"is_assistant": true` field in each session summary object
4. THE Chat_Interface SHALL use the `is_assistant` field or the `__assistant__` project marker to apply assistant-specific styling (distinct icon, label, or color accent) when rendering session list items

### Requirement 20: Assistant Folder Git Repository

**User Story:** As a user, I want the assistant folder to be a git repository with automatic commits, so that I have a history of all changes the assistant makes and can revert if needed.

#### Acceptance Criteria

1. WHEN the Assistant_Folder is created for the first time during server startup, THE Remote_Server SHALL initialize it as a git repository by running `git init` in the directory
2. IF the Assistant_Folder already exists and already contains a `.git/` subdirectory, THEN THE Remote_Server SHALL NOT re-initialize it
3. IF the Assistant_Folder already exists but does not contain a `.git/` subdirectory, THEN THE Remote_Server SHALL initialize it as a git repository during startup
4. THE default CLAUDE_MD file created by the Remote_Server SHALL include instructions telling the agent to run `git add -A && git commit -m "<descriptive message>"` in the `/workspace/` directory after every file creation or modification within the assistant folder
5. THE CLAUDE_MD instruction SHALL specify that commit messages must be concise and descriptive of what was changed (e.g., "Add session notes from programming discussion", "Update task list with new items")
6. THE Remote_Server SHALL expose `GET /assistant/files/commits?limit=20&offset=0` to return the git commit history of the Assistant_Folder, with each entry containing: hash, short_hash, message, date; sorted by date descending
7. WHEN the user views the Files tab, THE Dashboard SHALL display a "History" section or button that shows recent commits to the assistant repository
8. THE assistant agent SHALL have access to the `git` command inside the container at the `/workspace/` path, allowing it to execute git operations

### Requirement 21: Assistant Folder Uncommitted Changes Indicator

**User Story:** As a user, I want to see in the dashboard whether the assistant folder has uncommitted changes, so that I can tell if the agent is properly committing after modifications.

#### Acceptance Criteria

1. THE Remote_Server SHALL expose `GET /assistant/files/status` to return the git status of the Assistant_Folder, including: whether the working tree is clean, a list of changed files with their status (modified, untracked, deleted), and a summary of insertions and deletions
2. WHEN the user views the Files tab of the assistant, THE Dashboard SHALL display an indicator showing whether the Assistant_Folder has uncommitted changes (e.g., a badge or colored dot next to the Files tab label)
3. IF uncommitted changes exist, THEN THE Dashboard SHALL display the list of changed files with status badges (modified, added, deleted, untracked) similar to the project Changes tab
4. IF the working tree is clean (no uncommitted changes), THEN THE Dashboard SHALL display a message indicating all changes are committed
5. THE Dashboard SHALL refresh the uncommitted changes status when the user switches to the Files tab or clicks a refresh button
