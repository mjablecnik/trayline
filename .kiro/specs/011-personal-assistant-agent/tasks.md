# Implementation Plan: Personal Assistant Agent

## Overview

Implements a global personal AI assistant accessible at `/assistant` in the trayline dashboard. The backend extends config, container manager, and session infrastructure with a new `AssistantHandler` and `AssistantFolderManager`. The frontend adds a two-tab route (Chat + Files) reusing existing chat components with new assistant-specific components (StarterPrompts, AssistantBrowser) and store. Container mounts: `ASSISTANT_DATA_DIR:/workspace` (CWD), `PROJECTS_DIR:/projects`, credentials at `/home/agent/`. Only `ASSISTANT_DATA_DIR` env var is used (no home dir mount). Tasks are ordered: backend config → container manager → folder manager → handler/routes → frontend API/store → frontend components → page assembly → integration.

## Tasks

- [ ] 1. Backend configuration and container manager
  - [ ] 1.1 Add assistant config field to `remote/core/config.go`
    - Add `AssistantDataDir string` field to Config struct
    - In `LoadConfig()`: read `ASSISTANT_DATA_DIR` env var
    - If not set, default to `filepath.Join(filepath.Dir(cfg.ProjectsDir), ".assistant")`
    - Validate: if path exists but is not a directory, return startup error
    - Update `remote/.env.example` with `ASSISTANT_DATA_DIR` entry (commented out, with default note)
    - _Requirements: 1.4, 1.5, 1.7_

  - [ ] 1.2 Implement `BuildAssistantContainerBinds` in `remote/docker/container.go`
    - First bind: `config.AssistantDataDir + ":/workspace"` (read-write)
    - Second bind: `config.ProjectsDir + ":/projects"` (read-write)
    - Agent-specific credential mounts at `/home/agent/` (kiro: `.kiro` + `.local/share/kiro-cli`; claude: `.claude-src:ro` + `.claude.json-src:ro`)
    - No home directory mount — only dedicated credential paths
    - _Requirements: 1.1_

  - [ ] 1.3 Implement `StartAssistantChatContainer` in `remote/docker/container.go`
    - Create container with `WorkingDir: "/workspace"` so Claude CLI discovers CLAUDE.md from CWD
    - Use `BuildAssistantContainerBinds` for volume binds
    - Use `buildChatCmd`, `buildContainerEnv`, sandbox network config from existing patterns
    - Do NOT pass CLAUDE.md content as system prompt — agent reads it from CWD
    - _Requirements: 1.1, 1.2, 2.2, 2.3_

  - [ ] 1.4 Implement `resolveAssistantContainerName` in `remote/docker/container.go`
    - Return `trayline-assistant-{first 8 chars of sessionID}`
    - On naming conflict (container exists), append `-2` through `-6` until unused name found
    - Pass computed name to Docker `ContainerCreate` API
    - _Requirements: 1.3, 5.1, 5.2, 5.3_

  - [ ]* 1.5 Write property test for `BuildAssistantContainerBinds` output format
    - **Property 1: Assistant container binds are correctly constructed**
    - **Validates: Requirements 1.1**

  - [ ]* 1.6 Write property test for `resolveAssistantContainerName` prefix format
    - **Property 2: Assistant container name follows prefix format**
    - **Validates: Requirements 1.3, 5.1**

  - [ ]* 1.7 Write property test for container name conflict resolution
    - **Property 3: Container name conflict resolution appends numeric suffix**
    - **Validates: Requirements 5.2**

- [ ] 2. Backend AssistantFolderManager
  - [ ] 2.1 Create `remote/api/assistant_folder.go` with folder initialization
    - Define `AssistantFolderManager` struct with `dataDir` and `logger` fields
    - Implement `NewAssistantFolderManager` constructor
    - Implement `Init()`: create `ASSISTANT_DATA_DIR` directory if missing (error if path exists but is not a dir), create `memory/` and `prompts/` subdirectories if missing, call `initGitRepo()` and `ensureClaudeMD()`
    - _Requirements: 1.6, 1.8_

  - [ ] 2.2 Implement git repository initialization in `assistant_folder.go`
    - `initGitRepo()`: check for `.git/` directory existence
    - If folder already has `.git/`, skip (do not re-initialize)
    - If folder exists but has no `.git/`, run `git init`
    - If folder is newly created, run `git init`
    - _Requirements: 20.1, 20.2, 20.3_

  - [ ] 2.3 Implement default CLAUDE.md creation in `assistant_folder.go`
    - `ensureClaudeMD()`: create default CLAUDE.md at `ASSISTANT_DATA_DIR/CLAUDE.md` if not present
    - Content: role definition (personal assistant), workspace layout (`/workspace` = assistant data, `/projects/` = all projects), subdirectory descriptions (`memory/`, `prompts/`), git auto-commit instructions after changes in `/workspace/`
    - Commit messages must be concise and descriptive per the instructions
    - If CLAUDE.md exists but is not readable, log warning and proceed (agent runs with defaults)
    - _Requirements: 2.1, 2.4, 2.5, 20.4, 20.5_

  - [ ] 2.4 Implement file browser operations in `assistant_folder.go`
    - `validatePath()`: reject `..`, absolute paths, chars outside `[a-zA-Z0-9._/-]`, return cleaned relative path
    - `ListDirectory()`: return entries excluding `.git/`, sorted dirs-first then alphabetical, with name/type/size
    - `ReadFile()`: return content string (null + truncated=true if > 1MB), with path/filename/size
    - _Requirements: 18.2, 18.3, 18.4, 18.5, 18.6, 18.7_

  - [ ] 2.5 Implement prompts CRUD in `assistant_folder.go`
    - `ListPrompts()`: read `.md`/`.txt` files from `prompts/` dir, build display names (strip extension, replace hyphens/underscores with spaces), sort alphabetically by filename
    - `GetPrompt(filename)`: read single prompt file, return filename/display_name/content
    - `PutPrompt(filename, content)`: write content to prompts dir
    - `DeletePrompt(filename)`: remove prompt file
    - `ValidatePromptFilename()`: enforce `[a-zA-Z0-9._-]` only, max 100 chars, reject `..` and path separators
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9_

  - [ ] 2.6 Implement git operations in `assistant_folder.go`
    - `GetCommits(limit, offset)`: parse `git log --format=%H|%h|%s|%aI` output into commit entries, sorted date descending
    - `GetStatus()`: parse `git status --porcelain` into files list with status (modified/untracked/deleted/added) and summary
    - Return empty results gracefully for uninitialized repos
    - _Requirements: 20.6, 20.8, 21.1_

  - [ ]* 2.7 Write property test for prompt filename validation
    - **Property 5: Prompt filename validation**
    - **Validates: Requirements 3.7**

  - [ ]* 2.8 Write property test for prompt content round-trip
    - **Property 6: Prompt content round-trip**
    - **Validates: Requirements 3.4, 3.5**

  - [ ]* 2.9 Write property test for prompt listing completeness and sort
    - **Property 7: Prompt listing is complete and sorted**
    - **Validates: Requirements 3.2, 3.3**

  - [ ]* 2.10 Write property test for file path validation
    - **Property 8: File path validation rejects traversal and invalid characters**
    - **Validates: Requirements 18.6**

  - [ ]* 2.11 Write property test for file content size threshold
    - **Property 9: File content response respects size threshold**
    - **Validates: Requirements 18.5**

  - [ ]* 2.12 Write property test for directory listing sort order
    - **Property 10: Directory listing is sorted correctly**
    - **Validates: Requirements 18.2, 18.4**

- [ ] 3. Checkpoint — Backend foundation
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 4. Backend AssistantHandler, types, and file upload
  - [ ] 4.1 Create `remote/api/assistant_types.go` with request/response types
    - Define `assistantSessionSummary` (session_id, agent, model, is_assistant, created_at, last_message_at)
    - Define `starterPrompt` (filename, display_name, content)
    - Define `putPromptRequest` (content string)
    - Define `fileEntry`, `directoryResponse`, `fileContentResponse`
    - Define `gitCommitEntry`, `gitStatusResponse`, `gitStatusFile`, `gitStatusSummary`
    - _Requirements: 6.2, 18.2, 18.5, 19.3, 20.6, 21.1_

  - [ ] 4.2 Create `remote/api/assistant_handler.go` with handler struct and constructor
    - Define `AssistantHandler` struct with `store`, `cm`, `logger`, `config`, `stateMgr`, `folderMgr` fields
    - Implement `NewAssistantHandler` constructor
    - Define `assistantProject = "__assistant__"` constant
    - _Requirements: 19.2_

  - [ ] 4.3 Implement `HandleAssistantChat` WebSocket endpoint (`GET /assistant/chat`)
    - Validate `ASSISTANT_DATA_DIR` is configured and accessible
    - Validate `agent` query param (must be "kiro" or "claude"), optional `model` (max 100 chars), optional `system`
    - Reject with HTTP 400 VALIDATION_ERROR if agent invalid
    - Reject with HTTP 503 SERVICE_UNAVAILABLE if at max session capacity
    - Call `StartAssistantChatContainer` with agent, model, system, sessionID
    - Set `sess.Project = "__assistant__"` in SessionStore
    - Send `{"type": "session_started", "session_id": "<uuid>"}` as first message
    - Stream output, handle client messages (message/interrupt/terminate), idle timeout refresh
    - Forward messages to stdin without blocking when agent is processing
    - Respond with error for unrecognized message types
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 6.1, 6.3, 6.4, 14.1_

  - [ ] 4.4 Implement `HandleAssistantChatReconnect` WebSocket endpoint (`GET /assistant/chat/{id}`)
    - Validate session exists and `sess.Project == "__assistant__"`, return 404 if not
    - Return 409 CONFLICT if session already has active WebSocket connection
    - Send `{"type": "session_resumed", "session_id": "<id>", "agent": "<agent>", "model": "<model>"}` as first message
    - Send `{"type": "history", "messages": [...]}` containing full transcript after session_resumed
    - History messages contain: role (user/assistant), content, timestamp (ISO 8601)
    - Maintain message transcript in SessionStore (append user prompts and completed agent responses)
    - Stream output from existing container
    - _Requirements: 10.8, 10.9, 10.10, 10.11, 11.1, 11.2, 11.3, 11.4_

  - [ ] 4.5 Implement `HandleAssistantSessions` — list sessions (`GET /assistant/sessions`)
    - Call `store.ListByProject("__assistant__")`
    - Map to `assistantSessionSummary` with `is_assistant: true`
    - Sort by `last_message_at` descending
    - _Requirements: 6.2, 19.2, 19.3_

  - [ ] 4.6 Implement `HandleTerminateAssistantSession` (`POST /assistant/sessions/{id}/terminate`)
    - Validate session exists and `sess.Project == "__assistant__"`, return 404 if not
    - Send `{"type": "terminated"}` to connected WebSocket client before closing connection
    - Stop container with 10-second graceful timeout, then remove it
    - Remove session from SessionStore (no longer in listings)
    - Return HTTP 200 with `{"session_id": "<id>", "status": "terminated"}`
    - Discard stored message transcript along with the session
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5, 13.6, 11.4_

  - [ ] 4.7 Implement prompts endpoints (list, get, put, delete)
    - `HandleListPrompts` (`GET /assistant/prompts`): return all prompts with filename/display_name/content
    - `HandleGetPrompt` (`GET /assistant/prompts/{filename}`): return single prompt, 404 if missing
    - `HandlePutPrompt` (`PUT /assistant/prompts/{filename}`): validate filename + content (max 10,000 chars), create/update
    - `HandleDeletePrompt` (`DELETE /assistant/prompts/{filename}`): remove file, 404 if missing
    - _Requirements: 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.9_

  - [ ] 4.8 Implement file browser endpoints
    - `HandleFiles` (`GET /assistant/files` and `GET /assistant/files/{path...}`): route to dir listing or file content based on path type; validate path (400 on traversal/invalid chars), 404 on not found
    - `HandleFileCommits` (`GET /assistant/files/commits?limit=20&offset=0`): return git log
    - `HandleFileStatus` (`GET /assistant/files/status`): return git status response
    - _Requirements: 18.2, 18.3, 18.4, 18.5, 18.6, 18.7, 20.6, 21.1_

  - [ ] 4.9 Implement file upload via binary WebSocket frames
    - Decode binary frame: first 4 bytes = filename length, then filename bytes, then file content
    - Validate: max 50 MB per file, sanitize filename (remove path traversal characters)
    - Write file into running container at `/tmp/uploads/{sanitized_filename}` using `docker cp`
    - Send `{"type": "file_uploaded", "data": "<original_filename>"}` on success
    - Send `{"type": "error", "message": "<description>"}` on failure (container not running, size exceeded)
    - On next user message, prepend upload metadata: `[Uploaded Files]\n- filename → /tmp/uploads/filename\n`
    - Uploaded files live only inside container, deleted on session termination
    - _Requirements: 17.1, 17.2, 17.3, 17.4, 17.5, 17.6, 17.7_

  - [ ] 4.10 Implement idle timeout for assistant sessions
    - Reuse existing idle timeout checker that iterates SessionStore
    - Assistant sessions (`Project == "__assistant__"`) included automatically
    - Timer resets on client message, agent output, or successful reconnection
    - On timeout: send `{"type": "terminated"}` to connected WS client, stop container with 10s timeout, remove session
    - Default SESSION_TIMEOUT: 24 hours
    - _Requirements: 14.1, 14.2, 14.3_

  - [ ] 4.11 Ensure cleanup logic does not affect assistant containers inappropriately
    - `RunOneShot` removal targets specific container IDs — assistant containers safe by default
    - `StopAndRemoveContainer` only called by terminate endpoint or idle timeout
    - Containers with `trayline-assistant-*` prefix NOT terminated by generic cleanup unless through terminate endpoint or idle timeout
    - _Requirements: 5.4_

  - [ ]* 4.12 Write property test for invalid agent string rejection
    - **Property 4: Invalid agent strings are rejected**
    - **Validates: Requirements 10.3, 12.4**

  - [ ]* 4.13 Write property test for upload metadata construction
    - **Property 13: Upload metadata construction**
    - **Validates: Requirements 17.3**

  - [ ]* 4.14 Write property test for file upload size validation
    - **Property 14: File upload size validation**
    - **Validates: Requirements 17.4**

- [ ] 5. Backend route registration and wiring
  - [ ] 5.1 Register assistant routes in `remote/api/router.go`
    - Add `assistantH *AssistantHandler` parameter to `NewRouter`
    - Register all `/assistant/*` routes in correct order
    - `GET /assistant/files/commits` and `GET /assistant/files/status` MUST be registered BEFORE `GET /assistant/files/{path...}`
    - Wire `AssistantFolderManager` creation and `Init()` call at server startup in `main.go`
    - _Requirements: 10.1, 10.8, 3.2, 13.1, 18.2, 20.6, 21.1_

- [ ] 6. Checkpoint — Backend complete
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Frontend API client and store
  - [ ] 7.1 Add assistant types and API methods to `dashboard/src/lib/api.ts`
    - Add TypeScript interfaces: `StarterPrompt`, `AssistantFileEntry`, `AssistantDirectoryResponse`, `AssistantFileContentResponse`, `AssistantSession`, `GitCommitEntry`, `GitStatusFile`, `GitStatusResponse`
    - Add API methods: `getAssistantSessions`, `terminateAssistantSession`, `getAssistantPrompts`, `getAssistantPrompt`, `putAssistantPrompt`, `deleteAssistantPrompt`, `getAssistantFiles`, `getAssistantFileCommits`, `getAssistantFileStatus`, `getAssistantSummary`
    - Add `buildAssistantWsUrl(agent, model?, sessionId?)` function for WebSocket URL construction
    - _Requirements: 6.2, 3.2, 18.2, 20.6, 21.1_

  - [ ] 7.2 Create `dashboard/src/lib/stores/assistant.ts`
    - Implement `assistantStore` with state: sessionId, agent, model, connectionState, messages, activeTab, summarizeInProgress, summaryContent, selectedPrompt
    - Implement session history map (`Map<string, ChatMessage[]>`) for preserving messages across session switches
    - Actions: setAgent, setModel, setTab, setConnecting, setConnected, setDisconnected, addUserMessage, appendAgentOutput, markAgentDone, setHistory (for reconnect), setSummarizeInProgress, setSummaryContent, selectPrompt, switchToSession, clearSessionHistory, reset
    - `switchToSession`: save current messages to map, load target session messages from map
    - `setHistory`: replace local messages with server-provided history on reconnect
    - `clearSessionHistory`: remove specific session from map (used after reset)
    - _Requirements: 6.5, 6.6, 8.6, 11.5_

  - [ ]* 7.3 Write property test for message history preservation (fast-check)
    - **Property 11: Session message history is preserved across state transitions**
    - **Validates: Requirements 6.6, 8.6, 16.5**

  - [ ]* 7.4 Write property test for history on reconnect (fast-check)
    - **Property 12: History on reconnect contains full transcript**
    - **Validates: Requirements 11.1, 11.2**

- [ ] 8. Frontend i18n and navigation
  - [ ] 8.1 Add assistant translations to `dashboard/src/lib/i18n/en.ts` and `cs.ts`
    - English keys: `nav.assistant` = "Assistant", `assistant.chatTab` = "Chat", `assistant.filesTab` = "Files", `assistant.summarize` = "Summarize", `assistant.reset` = "Reset", `assistant.resetDialog`, `assistant.resetWithSummary`, `assistant.resetWithoutSummary`, `assistant.noSummaryWarning`, `assistant.prompts`, `assistant.promptsEmpty`, `assistant.promptsError`, `assistant.filesEmpty`, `assistant.filesError`, `assistant.history`, `assistant.historyEmpty`, `assistant.statusClean`, `assistant.statusDirty`, `assistant.refresh`, `assistant.breadcrumbRoot`, `assistant.connectionError`, `assistant.reconnect`, `assistant.sessionLost`, `assistant.newSession`, `assistant.serverBusy`, `assistant.sendError`, `assistant.fileUploaded`, `assistant.uploadError`
    - Czech translations for all above keys
    - _Requirements: 9.3_

  - [ ] 8.2 Add assistant navigation link to `dashboard/src/lib/components/Header.svelte`
    - Add `/assistant` link in both desktop and mobile navigation
    - Use `$t('nav.assistant')` for label ("Assistant" / "Asistent")
    - Add active state detection based on `page.url.pathname.startsWith('/assistant')`
    - Position after existing navigation items
    - _Requirements: 9.1, 9.2, 9.3_

- [ ] 9. Frontend components
  - [ ] 9.1 Create `dashboard/src/lib/components/StarterPrompts.svelte`
    - Props: prompts list, selectedFilename, onSelect callback
    - Display up to 10 prompts as clickable cards with display_name as title and truncated preview (max 100 chars + ellipsis)
    - Click to select (highlight), click again to deselect
    - When empty: render nothing (no prompts section shown)
    - On fetch failure: show inline warning, agent selector remains usable
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.6, 15.1, 15.6_

  - [ ] 9.2 Create `dashboard/src/lib/components/AssistantBrowser.svelte`
    - File browser with breadcrumb navigation for `/workspace` contents
    - Directory listing: navigable list, directories first then files with sizes
    - File viewing: display content with syntax highlighting (markdown) or plain text (whitespace-pre-wrap), read-only
    - Git status badge next to "Files" tab label when `status.clean === false`
    - History section: show recent commits (short_hash, message, relative date)
    - Uncommitted changes section: list changed files with status badges (modified/added/deleted/untracked)
    - If clean: show "All changes committed" message
    - Refresh button: re-fetch directory, status, and commits
    - Read-only: no create, edit, or delete UI (modifications via agent chat or prompts API)
    - _Requirements: 18.1, 18.8, 18.9, 18.10, 20.7, 21.2, 21.3, 21.4, 21.5_

  - [ ] 9.3 Implement file upload UI in chat interface
    - Add file upload button (📎 icon) and drag-and-drop area
    - On file select: read as ArrayBuffer, encode binary WebSocket frame (4-byte filename length + filename + content)
    - Send binary frame via WebSocket
    - On `file_uploaded` response: display confirmation in chat ("📁 filename uploaded")
    - On `error` response for upload: display error in chat
    - Disable upload button while agent processing or session not active
    - _Requirements: 17.8, 17.9_

- [ ] 10. Frontend assistant page
  - [ ] 10.1 Create `dashboard/src/routes/assistant/+page.svelte` — layout and agent selection
    - Two-tab layout (Chat | Files) using existing TabBar component
    - Chat tab: session list sidebar + chat area
    - Agent selector with "kiro"/"claude" options (no pre-selection), optional model text input (max 100 chars)
    - Start button enabled only when agent selected
    - No project context — operates independently of any selected project
    - _Requirements: 9.1, 9.4, 9.5, 12.1, 12.2, 12.3_

  - [ ] 10.2 Implement session management and WebSocket connection in assistant page
    - On "Start Agent": establish WebSocket connection via `buildAssistantWsUrl(agent, model)`
    - Handle `session_started` message → update store with sessionId, set connected state
    - Handle `session_resumed` + `history` messages on reconnect → call `setHistory()` to render transcript
    - Handle `output` → `appendAgentOutput`, `done` → `markAgentDone`, `error` → display inline
    - Handle `terminated` → set disconnected state
    - Handle `context_compacted` message
    - Session list: display all active sessions with assistant icon/label, allow switching
    - Session switching retains message history in client memory via store
    - _Requirements: 6.2, 6.3, 6.5, 6.6, 10.4, 10.5, 11.5, 19.1, 19.4_

  - [ ] 10.3 Implement starter prompts integration in assistant page
    - Fetch prompts from `GET /assistant/prompts` on mount (when no active session)
    - Display StarterPrompts component in initial state
    - On session start with selected prompt: insert prompt content into message input field (don't auto-send)
    - Allow user to edit inserted text before sending
    - Allow session start without any prompt selected (empty input)
    - On deselect: clear pre-fill
    - Hide prompts when session becomes active (connected state)
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 15.1, 15.2, 15.3, 15.4, 15.5, 15.6_

  - [ ] 10.4 Implement Summarize button in assistant page
    - Display when session active AND agent not processing (last message complete)
    - On click: send predefined summarize prompt as regular user message, display in chat history
    - Predefined prompt instructs: summarize entire conversation (key topics, decisions, info, action items), save to `/workspace/summary.md`, output in response
    - Disable button while processing, re-enable on `done` message
    - Summary stored at `/workspace/summary.md` (overwriting previous), readable via Files tab
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8_

  - [ ] 10.5 Implement Reset button with summary dialog in assistant page
    - Display when session active
    - On click (WS connected): show dialog with "With Summary" / "Without Summary" options
    - "With Summary": fetch `summary.md` via API, terminate session, start new session (same agent/model), insert summary content into input field (don't send)
    - "With Summary" but no summary exists (404/empty): show warning, proceed as "Without Summary"
    - "Without Summary": terminate session, return to initial state (agent selector + prompts)
    - On click (WS disconnected/error): skip dialog, clear local state, return to initial state
    - Clear terminated session's history from map, preserve other sessions
    - Terminate = stop container with 10s timeout + remove
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7, 8.8_

  - [ ] 10.6 Implement error handling in assistant chat interface
    - WS connection closed (not client-initiated): display "Connection lost" message + "Reconnect" button (single attempt)
    - Session creation error: inline error near agent selector, stay on Chat tab
    - 503 at capacity: display "Server is busy, try again later"
    - Reconnect failure (404 or >10s timeout): "Session no longer available" + "Start New Session" button
    - Connection error preserves displayed message history (no clearing)
    - Send failure during active session: inline error below failed message, retain text in input
    - _Requirements: 16.1, 16.2, 16.3, 16.4, 16.5, 16.6_

  - [ ] 10.7 Wire Files tab with AssistantBrowser component
    - Render AssistantBrowser when Files tab selected
    - Show uncommitted changes badge on Files tab label when status.clean === false
    - Refresh status when switching to Files tab
    - _Requirements: 18.1, 21.2, 21.5_

- [ ] 11. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- Backend property tests use Go's `rapid` library; frontend property tests use `fast-check`
- The design uses Go (backend) and TypeScript/Svelte (frontend) — no language decision needed
- The WebSocket chat handler reuses the same streaming/idle-timeout patterns from the project agent handler (spec 009)
- Route ordering is critical: specific paths (`/assistant/files/commits`, `/assistant/files/status`) must be registered before the wildcard (`/assistant/files/{path...}`)
- Container mounts: `/workspace` = ASSISTANT_DATA_DIR (CWD), `/projects` = PROJECTS_DIR — no home directory mount
- Only `ASSISTANT_DATA_DIR` env var is needed (defaults to `{parent of PROJECTS_DIR}/.assistant`)
- File upload uses binary WebSocket frames with `docker cp` to write to `/tmp/uploads/` inside the container
- Session history is sent as `{"type": "history"}` message on reconnect, rendered by frontend replacing stale local state

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "1.3", "1.4", "2.1"] },
    { "id": 2, "tasks": ["1.5", "1.6", "1.7", "2.2", "2.3", "2.4", "2.5", "2.6"] },
    { "id": 3, "tasks": ["2.7", "2.8", "2.9", "2.10", "2.11", "2.12", "4.1"] },
    { "id": 4, "tasks": ["4.2", "4.3", "4.4", "4.5", "4.6", "4.7", "4.8", "4.9", "4.10", "4.11"] },
    { "id": 5, "tasks": ["4.12", "4.13", "4.14", "5.1"] },
    { "id": 6, "tasks": ["7.1", "8.1", "8.2"] },
    { "id": 7, "tasks": ["7.2", "9.1", "9.2", "9.3"] },
    { "id": 8, "tasks": ["7.3", "7.4", "10.1"] },
    { "id": 9, "tasks": ["10.2", "10.3", "10.7"] },
    { "id": 10, "tasks": ["10.4", "10.5", "10.6"] }
  ]
}
```
