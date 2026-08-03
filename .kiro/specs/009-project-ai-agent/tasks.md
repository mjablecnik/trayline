# Implementation Plan: Project AI Agent

## Overview

Implements per-project AI agent chat for the trayline dashboard. The backend extends the existing session infrastructure with project-scoped container binds and a new `ProjectAgentHandler`. The frontend adds an Agent tab with WebSocket-driven chat, session switching, and agent selection. Tasks are ordered: backend foundation → backend handlers → frontend utilities/store → frontend components → page assembly → integration.

## Tasks

- [ ] 1. Extend session store with project scoping
  - [ ] 1.1 Add `Project` field to `Session` struct in `remote/store/session.go`
    - Add `Project string` with JSON tag `json:"project,omitempty"`
    - _Requirements: 1.1, 4.2, 4.3_

  - [ ] 1.2 Implement `ListByProject` method on `SessionStore` in `remote/store/session.go`
    - Filter sessions by `Project == name`
    - Sort by `LastMessageAt` descending
    - Return empty slice (not nil) when no sessions match
    - _Requirements: 4.2, 4.3_

  - [ ]* 1.3 Write property test for `ListByProject` filtering and sorting
    - **Property 6: Session listing is project-filtered and time-sorted**
    - **Validates: Requirements 4.2, 4.3**

- [ ] 2. Add project-scoped container bind methods
  - [ ] 2.1 Implement `BuildProjectContainerBinds` in `remote/docker/container.go`
    - Construct bind string `PROJECTS_DIR/{projectName}:/workspace` as first element
    - Include agent-specific credential binds (kiro: `.kiro` + `.local/share/kiro-cli`; claude: `.claude` + `.claude.json`)
    - _Requirements: 1.1, 1.4_

  - [ ] 2.2 Implement `StartProjectChatContainer` in `remote/docker/container.go`
    - Create container with `WorkingDir: /workspace`
    - Use `BuildProjectContainerBinds` for host config binds
    - Reuse `buildChatCmd`, `buildContainerEnv`, and network config patterns from existing `StartChatContainer`
    - _Requirements: 1.1, 1.4_

  - [ ]* 2.3 Write property test for `BuildProjectContainerBinds` output format
    - **Property 1: Project bind mount is correctly scoped**
    - **Validates: Requirements 1.1**

- [ ] 3. Checkpoint — Backend foundation
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 4. Implement ProjectAgentHandler structure and validation
  - [ ] 4.1 Create `remote/api/project_agent_handler.go` with handler struct and constructor
    - Define `ProjectAgentHandler` struct with `store`, `cm`, `logger`, `config`, `stateMgr` fields
    - Implement `NewProjectAgentHandler` constructor
    - _Requirements: 1.2, 1.5, 1.6_

  - [ ] 4.2 Implement project name validation in `project_agent_handler.go`
    - `validProjectName` regex: `^[a-zA-Z0-9._-]+$`
    - Reject `..` sequences and empty names
    - `projectExists` checks `PROJECTS_DIR/{name}/.git` is a directory
    - Return HTTP 400 (VALIDATION_ERROR) for invalid chars, HTTP 404 (NOT_FOUND) for missing project
    - _Requirements: 1.5, 1.6, 1.7_

  - [ ]* 4.3 Write property test for project name validation
    - **Property 3: Forbidden project name characters are rejected**
    - **Validates: Requirements 1.6**

  - [ ]* 4.4 Write property test for invalid agent string rejection
    - **Property 2: Invalid agent strings are rejected**
    - **Validates: Requirements 1.3, 2.9**

- [ ] 5. Implement ProjectAgentHandler endpoints
  - [ ] 5.1 Implement `HandleProjectChat` WebSocket endpoint
    - Validate project name and existence
    - Validate `agent` query param (must be "kiro" or "claude"), optionally accept `model` and `system`
    - Call `StartProjectChatContainer` with project name
    - Set `sess.Project = name` on created session
    - Send `session_started` message with session ID
    - Stream container output as `output` messages, send `done` on turn completion
    - Update `sess.LastMessageAt` in the streamOutput loop on each agent output (idle timeout resets on both client and agent activity)
    - Handle `interrupt` (SIGINT to container), `terminate` (stop container + close)
    - Handle unknown message types with error response
    - Queue messages while agent is processing
    - _Requirements: 2.1–2.11, 1.2, 1.3, 1.8, 6.1_

  - [ ] 5.2 Implement `HandleProjectChatReconnect` WebSocket endpoint
    - Validate project name and session ID
    - Verify `sess.Project == name` (404 if mismatch)
    - Check for existing active connection (409 if connected)
    - Send `session_resumed` message containing session_id, agent, and model fields
    - Add `Agent string` and `Model string` fields (with `omitempty` JSON tags) to `WSServerMessage` in `session_types.go`
    - Resume stream output and read client loop
    - _Requirements: 3.1–3.5_

  - [ ] 5.3 Implement `HandleProjectSessions` endpoint
    - Validate project name and existence
    - Call `ListByProject(name)` and map to `projectSessionSummary` slice
    - Return JSON array (empty array when no sessions)
    - _Requirements: 4.1–4.5_

  - [ ] 5.4 Implement `HandleTerminateProjectSession` endpoint
    - Validate project name and session ID
    - Verify `sess.Project == name` (404 if mismatch)
    - Stop container with 10s graceful timeout, remove it
    - Send `terminated` to connected WebSocket client before closing
    - Return `{"session_id": "...", "status": "terminated"}`
    - _Requirements: 5.1–5.5_

  - [ ]* 5.5 Write property test for unrecognized WebSocket message types
    - **Property 5: Unrecognized WebSocket message types produce an error**
    - **Validates: Requirements 2.10**

- [ ] 6. Create response types and register routes
  - [ ] 6.1 Create `remote/api/project_agent_types.go`
    - Define `projectSessionSummary` struct with JSON tags: `session_id`, `agent`, `model`, `created_at`, `last_message_at`
    - _Requirements: 4.2_

  - [ ] 6.2 Register project agent routes in `remote/api/router.go`
    - Add `projectAgentH *ProjectAgentHandler` parameter to `NewRouter`
    - Register `GET /projects/{name}/chat` → `HandleProjectChat`
    - Register `GET /projects/{name}/chat/{id}` → `HandleProjectChatReconnect`
    - Register `GET /projects/{name}/sessions` → `HandleProjectSessions`
    - Register `POST /projects/{name}/sessions/{id}/terminate` → `HandleTerminateProjectSession`
    - Instantiate `ProjectAgentHandler` in main and pass to router
    - _Requirements: 2.1, 3.1, 4.1, 5.1_

- [ ] 7. Checkpoint — Backend complete
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 8. Add frontend API methods and i18n translations
  - [ ] 8.1 Add agent session API methods to `dashboard/src/lib/api.ts`
    - Define `AgentSession` interface: `session_id`, `agent`, `model?`, `created_at`, `last_message_at`
    - Add `getProjectSessions(name)` method
    - Add `terminateProjectSession(name, sessionId)` method
    - Add `buildWsUrl(projectName, agent, model?, sessionId?)` helper for WebSocket URL construction
    - _Requirements: 10.1, 10.6, 9.4_

  - [ ] 8.2 Add i18n translations for agent tab in `dashboard/src/lib/i18n/en.ts`
    - Add all `tabs.agent` and `agent.*` keys as defined in design
    - _Requirements: 7.3_

  - [ ] 8.3 Add i18n translations for agent tab in `dashboard/src/lib/i18n/cs.ts`
    - Add all Czech translations as defined in design
    - _Requirements: 7.3_

- [ ] 9. Create agent store
  - [ ] 9.1 Create `dashboard/src/lib/stores/agent.ts`
    - Define `ConnectionState`, `ChatMessage`, `AgentSessionState` types
    - Implement `createAgentStore` with methods: `setAgent`, `setModel`, `setConnecting`, `setConnected`, `setDisconnected`, `addUserMessage`, `appendAgentOutput`, `markAgentDone`, `switchToSession`, `reset`
    - Maintain `sessionHistories` map for preserving message history across session switches
    - _Requirements: 10.3, 10.4, 11.5_

  - [ ]* 9.2 Write property test for output chunk accumulation in agent store
    - **Property 9: Output chunks accumulate correctly**
    - **Validates: Requirements 8.4**

  - [ ]* 9.3 Write property test for message history preservation across session switches
    - **Property 10: Client message history survives session switches and connection errors**
    - **Validates: Requirements 10.4, 11.5**

- [ ] 10. Implement ChatMessage component
  - [ ] 10.1 Create `dashboard/src/lib/components/ChatMessage.svelte`
    - Accept `message: ChatMessage` prop
    - User messages: right-aligned, sky-500 background, white text
    - Agent messages: left-aligned, slate-100 dark:slate-800 background
    - Streaming indicator: blinking cursor (▊) when `!message.complete && role === 'agent'`
    - Render agent text with `whitespace-pre-wrap`
    - Show inline error (red text) below bubble if `message.error` is set
    - _Requirements: 8.6_

- [ ] 11. Implement AgentSelector component
  - [ ] 11.1 Create `dashboard/src/lib/components/AgentSelector.svelte`
    - Agent `<select>` with placeholder, options "kiro" and "claude" (no pre-selection)
    - Model `<input type="text">` with i18n placeholder, maxlength=100
    - "Start Agent" button — disabled until agent is selected
    - On start: set connecting state, disable button, show indicator
    - Emit event/callback for parent to initiate WebSocket
    - Display inline error on connection failure, re-enable button
    - _Requirements: 9.1–9.4, 11.2_

- [ ] 12. Implement SessionList component
  - [ ] 12.1 Create `dashboard/src/lib/components/SessionList.svelte`
    - Accept `projectName`, `activeSessionId`, `onSelect`, `onTerminate`, `onNewSession` props
    - Fetch sessions from API on mount and after create/terminate
    - Display each session: agent type, model, relative last_message_at time
    - Highlight active session with distinct background
    - Terminate (✕) button per session
    - "+ New" button to start additional session
    - Show inline error + retry on fetch failure
    - _Requirements: 10.1, 10.2, 10.5, 10.6, 10.7, 10.8_

- [ ] 13. Implement ChatInterface component
  - [ ] 13.1 Create `dashboard/src/lib/components/ChatInterface.svelte`
    - Accept `projectName` prop
    - Manage WebSocket lifecycle: connect, send, receive, disconnect, reconnect
    - Message input: `<textarea>` with auto-grow (max 6 lines)
    - Submit on Enter (Shift+Enter = newline), send button
    - Prevent empty/whitespace-only submission (disable send button)
    - Auto-scroll to bottom on new output unless user scrolled up
    - Show "scroll to bottom" button when user scrolls up
    - Display typing/loading indicator while agent processes
    - Disable input while agent is processing
    - Show "Interrupt" and "Terminate" buttons during active session
    - On terminate: return to agent selector with preserved agent/model values
    - Handle connection errors: show error banner + "Reconnect" button
    - Handle 503: show server busy message
    - Handle reconnect failure (404/timeout >10s): show "session lost" + "Start New Session" button
    - Handle send failure: inline error below message, retain text in input
    - _Requirements: 8.1–8.8, 9.5–9.7, 11.1–11.6_

  - [ ]* 13.2 Write property test for whitespace-only message rejection
    - **Property 8: Whitespace-only messages are rejected**
    - **Validates: Requirements 8.3**

  - [ ]* 13.3 Write property test for message submission behavior
    - **Property 7: Valid message submission updates history and clears input**
    - **Validates: Requirements 8.2**

- [ ] 14. Checkpoint — Frontend components complete
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 15. Assemble Agent tab page and wire TabBar
  - [ ] 15.1 Create `dashboard/src/routes/[project]/agent/+page.svelte`
    - Compose SessionList (sidebar/left) + ChatInterface or AgentSelector (main area)
    - Show AgentSelector when no active session; ChatInterface when connected
    - On mobile: collapse session list into dropdown above chat
    - _Requirements: 7.2, 9.3, 10.2_

  - [ ] 15.2 Add Agent tab to `dashboard/src/lib/components/TabBar.svelte`
    - Add tab link to `/{project}/agent` as last item
    - Do NOT append `?ref=` query parameter
    - Use `tabs.agent` i18n key for label
    - Highlight based on `agent` path segment
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

- [ ] 16. Implement file upload support
  - [ ] 16.1 Add `CopyToContainer` to `ContainerClient` interface in `remote/docker/container.go`
    - Add the method to the interface and implement it in `dockerClientAdapter`
    - _Requirements: 12.1_

  - [ ] 16.2 Implement `CopyFileToContainer` helper in `remote/docker/container.go`
    - Create a tar archive in memory with a single file
    - Call `CopyToContainer` with destination `/tmp/uploads`
    - _Requirements: 12.1_

  - [ ] 16.3 Handle binary WebSocket frames in `HandleProjectChat`
    - Decode binary frame using existing `DecodeBinaryFrame`
    - Validate file size (max 50 MB) and sanitize filename
    - Call `CopyFileToContainer` to write into the running container
    - Track uploaded filenames per session in memory
    - Send `{"type": "file_uploaded", "data": "<filename>"}` on success
    - Send `{"type": "error", "message": "..."}` on failure
    - Prepend upload metadata to next user prompt
    - _Requirements: 12.1–12.5, 12.7_

  - [ ] 16.4 Add file upload UI to `ChatInterface.svelte`
    - Add 📎 button next to message input
    - Implement drag-and-drop on chat area
    - Read file as ArrayBuffer, encode as binary frame, send via WebSocket
    - Display confirmation message in chat on `file_uploaded` response
    - Disable upload button while agent is processing or session is not active
    - _Requirements: 12.8, 12.9_

- [ ] 17. Final checkpoint — Full integration
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- The backend reuses existing `SessionHandler` patterns (WS protocol, idle timeout) — no new infrastructure needed
- Frontend uses fast-check for property tests; backend uses Go's rapid library
- The idle timeout checker from `SessionHandler` applies automatically since project sessions share the `SessionStore`

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "8.1", "8.2", "8.3"] },
    { "id": 1, "tasks": ["1.2", "2.1", "6.1", "9.1"] },
    { "id": 2, "tasks": ["1.3", "2.2", "4.1", "9.2", "9.3"] },
    { "id": 3, "tasks": ["2.3", "4.2", "10.1", "11.1", "12.1"] },
    { "id": 4, "tasks": ["4.3", "4.4", "5.1", "13.1", "16.1"] },
    { "id": 5, "tasks": ["5.2", "5.3", "5.4", "5.5", "13.2", "13.3", "16.2"] },
    { "id": 6, "tasks": ["6.2", "16.3"] },
    { "id": 7, "tasks": ["15.1", "15.2", "16.4"] }
  ]
}
```
