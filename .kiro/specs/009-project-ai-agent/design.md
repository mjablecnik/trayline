# Design: 009 — Project AI Agent

## Overview

Adds per-project AI agent chat to the trayline dashboard. Reuses the existing `SessionHandler` and `ContainerManager` infrastructure with a new `ProjectAgentHandler` that scopes container mounts to individual project directories. The frontend gains a new Agent tab with a chat interface, WebSocket management, and session switching.

## File Structure

```
remote/
├── api/
│   ├── project_agent_handler.go   # NEW — /projects/{name}/chat, /sessions endpoints
│   ├── project_agent_types.go     # NEW — request/response types for project agent
│   ├── session_handler.go         # EXISTING — reference for patterns
│   ├── session_types.go           # EXISTING — WSClientMessage, WSServerMessage (reused)
│   └── router.go                  # MODIFIED — register new project agent routes
├── docker/
│   └── container.go               # MODIFIED — add BuildProjectContainerBinds method
├── store/
│   └── session.go                 # MODIFIED — add Project field to Session struct
└── core/
    └── config.go                  # EXISTING — PROJECTS_DIR already configured

dashboard/
├── src/
│   ├── lib/
│   │   ├── api.ts                 # MODIFIED — add session list + terminate API methods
│   │   ├── components/
│   │   │   ├── TabBar.svelte      # MODIFIED — add Agent tab
│   │   │   ├── ChatInterface.svelte    # NEW — main chat UI component
│   │   │   ├── ChatMessage.svelte      # NEW — single message bubble
│   │   │   ├── AgentSelector.svelte    # NEW — agent/model selection + start button
│   │   │   └── SessionList.svelte      # NEW — active session list sidebar
│   │   ├── i18n/
│   │   │   ├── en.ts             # MODIFIED — add agent tab + chat translations
│   │   │   └── cs.ts             # MODIFIED — add agent tab + chat translations
│   │   └── stores/
│   │       └── agent.ts          # NEW — agent session state management
│   └── routes/
│       └── [project]/
│           └── agent/
│               └── +page.svelte  # NEW — Agent tab page
└── ...
```

## Architecture

The feature extends the existing backend/frontend split:

- **Backend** (`remote/`): A new `ProjectAgentHandler` registers under the existing `/projects/{name}/` URL namespace. It uses the shared `SessionStore` (with a new `Project` field for scoping) and `ContainerManager` (with a new project-scoped bind method). The existing middleware stack (auth, CORS, rate limiting) applies automatically.
- **Frontend** (`dashboard/`): A new `agent/` route under `[project]/` renders the chat interface. WebSocket connections are managed by a dedicated `agentStore`. Components follow the same patterns as existing tabs (loading states, error handling, i18n).

No new infrastructure is introduced. The idle timeout checker from `SessionHandler` applies to project sessions as well since they share the same `SessionStore`.

## Components and Interfaces

| Component | Location | Responsibility |
|-----------|----------|----------------|
| `ProjectAgentHandler` | `remote/api/project_agent_handler.go` | HTTP/WS handlers for project agent endpoints |
| `BuildProjectContainerBinds` | `remote/docker/container.go` | Constructs project-scoped Docker volume binds |
| `StartProjectChatContainer` | `remote/docker/container.go` | Creates container with project bind + `/workspace` workdir |
| `SessionStore.ListByProject` | `remote/store/session.go` | Filters and sorts sessions by project |
| `agentStore` | `dashboard/src/lib/stores/agent.ts` | Client-side state for active agent session |
| `ChatInterface` | `dashboard/src/lib/components/ChatInterface.svelte` | WebSocket lifecycle + message display |
| `AgentSelector` | `dashboard/src/lib/components/AgentSelector.svelte` | Agent/model picker + start button |
| `SessionList` | `dashboard/src/lib/components/SessionList.svelte` | Active session list with switch/terminate |
| `ChatMessage` | `dashboard/src/lib/components/ChatMessage.svelte` | Single message bubble rendering |

## Backend: Session Store Changes

Add a `Project` field to the `Session` struct to track which project a session belongs to:

```go
// store/session.go — additions to Session struct
type Session struct {
	// ... existing fields ...
	Project string `json:"project,omitempty"` // project name (empty for global sessions)
}
```

Add a method to list sessions filtered by project:

```go
// ListByProject returns active sessions for a given project, sorted by LastMessageAt desc.
func (s *SessionStore) ListByProject(project string) []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Session
	for _, sess := range s.sessions {
		if sess.Project == project {
			result = append(result, sess)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastMessageAt.After(result[j].LastMessageAt)
	})
	return result
}
```

## Backend: Container Manager — Project-Scoped Binds

Add a method to `ContainerManager` that builds volume binds scoped to a single project directory instead of the full workspace:

```go
// docker/container.go — new method

// BuildProjectContainerBinds constructs volume binds for a project-scoped agent container.
// Mounts PROJECTS_DIR/{projectName} as /workspace instead of the full workspace.
func (m *ContainerManager) BuildProjectContainerBinds(agent, projectName string) []string {
	const agentHome = "/home/agent"
	projectHostPath := filepath.Join(m.config.ProjectsDir, projectName)
	binds := []string{projectHostPath + ":" + workspaceMount}

	switch agent {
	case "kiro":
		if m.config.KiroHostDir != "" {
			binds = append(binds, m.config.KiroHostDir+":"+agentHome+"/.kiro")
		}
		if m.config.KiroCredsHostDir != "" {
			binds = append(binds, m.config.KiroCredsHostDir+":"+agentHome+"/.local/share/kiro-cli")
		}
	case "claude":
		if m.config.ClaudeHostDir != "" {
			binds = append(binds, m.config.ClaudeHostDir+":"+agentHome+"/.claude")
		}
		if m.config.ClaudeConfigHostFile != "" {
			binds = append(binds, m.config.ClaudeConfigHostFile+":"+agentHome+"/.claude.json:ro")
		}
	}

	return binds
}
```

Add a new container creation method that uses project binds and sets the working directory:

```go
// StartProjectChatContainer creates an interactive container scoped to a project.
// Sets working directory to /workspace.
func (m *ContainerManager) StartProjectChatContainer(ctx context.Context, agent, model, system, projectName string) (string, error) {
	cmd := buildChatCmd(agent, model, system)
	useTTY := agent != "claude"

	cfg := &container.Config{
		Image:      SandboxImage,
		Cmd:        cmd,
		Env:        m.buildContainerEnv(),
		Tty:        useTTY,
		AttachStdin: true,
		OpenStdin:   true,
		StdinOnce:   false,
		WorkingDir:  workspaceMount,
	}

	hostCfg := &container.HostConfig{
		Binds:      m.BuildProjectContainerBinds(agent, projectName),
		AutoRemove: false,
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			sandboxNetwork: {},
		},
	}

	resp, err := m.client.ContainerCreate(ctx, cfg, hostCfg, netCfg, "")
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}
	return resp.ID, nil
}
```

## Backend: Project Agent Handler

New handler file `api/project_agent_handler.go` that handles all project-scoped agent endpoints. Reuses the same WebSocket protocol (`WSClientMessage`, `WSServerMessage`), idle timeout checker, and container lifecycle patterns from the existing `SessionHandler`.

### Project Name Validation

```go
// api/project_agent_handler.go

import "regexp"

// validProjectName matches only safe project directory names.
var validProjectName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// validateProjectName checks the project name is safe and exists in PROJECTS_DIR.
// Returns an error response if invalid, or nil if valid.
func (h *ProjectAgentHandler) validateProjectName(name string) *core.ErrorResponse {
	if name == "" || !validProjectName.MatchString(name) || strings.Contains(name, "..") {
		return &core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "project name contains invalid characters",
		}
	}
	return nil
}

// projectExists checks that PROJECTS_DIR/{name} exists and contains .git/.
func (h *ProjectAgentHandler) projectExists(name string) bool {
	projectPath := filepath.Join(h.config.ProjectsDir, name)
	info, err := os.Stat(filepath.Join(projectPath, ".git"))
	return err == nil && info.IsDir()
}
```

### Handler Structure

```go
// ProjectAgentHandler handles project-scoped AI agent endpoints.
type ProjectAgentHandler struct {
	store    *store.SessionStore
	cm       *docker.ContainerManager
	logger   *core.Logger
	config   *core.Config
	stateMgr StateSaver
}

func NewProjectAgentHandler(
	store *store.SessionStore,
	cm *docker.ContainerManager,
	logger *core.Logger,
	config *core.Config,
	stateMgr StateSaver,
) *ProjectAgentHandler {
	return &ProjectAgentHandler{
		store:    store,
		cm:       cm,
		logger:   logger,
		config:   config,
		stateMgr: stateMgr,
	}
}
```

### HandleProjectChat — WebSocket Session Creation

`GET /projects/{name}/chat?agent=kiro|claude&model=...&system=...`

Follows the same flow as `SessionHandler.HandleChat` but:
1. Validates project name (regex + exists check)
2. Uses `StartProjectChatContainer` instead of `StartChatContainer`
3. Sets `sess.Project = name` on the created session

The stream output, read client, and idle timeout logic are shared via helper methods extracted from `SessionHandler` or by embedding shared logic. For simplicity, the handler reuses the same `streamOutput` and `readClient` patterns inline (copy the pattern, not import — keeps the two handlers independent).

**Idle timeout reset on agent output:** Unlike the existing `SessionHandler` (which only resets the idle timer on client messages), the project agent handler SHALL also update `sess.LastMessageAt` inside the `streamOutput` loop whenever agent output is received. This ensures the idle timeout resets on both client activity and agent activity, matching Requirement 6.1.

### HandleProjectChatReconnect

`GET /projects/{name}/chat/{id}`

Same as `SessionHandler.HandleChatReconnect` with additional checks:
- If `sess.Project != name` → return 404
- The `session_resumed` message includes `session_id`, `agent`, and `model` fields:

```go
h.writeWS(conn, WSServerMessage{
	Type:      "session_resumed",
	SessionID: id,
	Agent:     sess.Agent,
	Model:     sess.Model,
})
```

This requires extending `WSServerMessage` with optional `Agent` and `Model` fields (or using a map for this specific message). The simplest approach is to add `Agent string` and `Model string` fields with `omitempty` JSON tags to `WSServerMessage`.

### HandleProjectSessions — List Sessions

`GET /projects/{name}/sessions`

```go
func (h *ProjectAgentHandler) HandleProjectSessions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.validateProjectName(name); err != nil {
		writeJSON(w, http.StatusBadRequest, err)
		return
	}
	if !h.projectExists(name) {
		writeJSON(w, http.StatusNotFound, core.ErrorResponse{
			Error: "NOT_FOUND", Message: "project not found",
		})
		return
	}

	sessions := h.store.ListByProject(name)
	result := make([]projectSessionSummary, len(sessions))
	for i, s := range sessions {
		result[i] = projectSessionSummary{
			SessionID:     s.ID,
			Agent:         s.Agent,
			Model:         s.Model,
			CreatedAt:     s.CreatedAt,
			LastMessageAt: s.LastMessageAt,
		}
	}
	writeJSON(w, http.StatusOK, result)
}
```

### HandleTerminateProjectSession

`POST /projects/{name}/sessions/{id}/terminate`

Same logic as `SessionHandler.HandleTerminateSession` with the additional project ownership check: if `sess.Project != name` → 404.

## Backend: Response Types

```go
// api/project_agent_types.go

type projectSessionSummary struct {
	SessionID     string    `json:"session_id"`
	Agent         string    `json:"agent"`
	Model         string    `json:"model,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	LastMessageAt time.Time `json:"last_message_at"`
}
```

The WebSocket messages reuse the existing `WSClientMessage` and `WSServerMessage` types unchanged.

## Backend: Route Registration

Add routes to `router.go`:

```go
// Project agent endpoints (in NewRouter)
mux.HandleFunc("GET /projects/{name}/chat", projectAgentH.HandleProjectChat)
mux.HandleFunc("GET /projects/{name}/chat/{id}", projectAgentH.HandleProjectChatReconnect)
mux.HandleFunc("GET /projects/{name}/sessions", projectAgentH.HandleProjectSessions)
mux.HandleFunc("POST /projects/{name}/sessions/{id}/terminate", projectAgentH.HandleTerminateProjectSession)
```

The `NewRouter` function signature gains a `projectAgentH *ProjectAgentHandler` parameter.

## Frontend: TabBar Modification

Add the Agent tab as the last item in `TabBar.svelte`. Unlike other tabs, it does not append `?ref=` since agent chat is not branch-specific:

```svelte
<a href={resolve(`/[project]/agent`, { project })} class={tabClass('agent')}>
	{$t('tabs.agent')}
</a>
```

## Frontend: i18n Additions

English (`en.ts`):
```typescript
'tabs.agent': 'Agent',

'agent.selectAgent': 'Select agent',
'agent.selectModel': 'Model (optional)',
'agent.start': 'Start Agent',
'agent.interrupt': 'Interrupt',
'agent.terminate': 'Terminate',
'agent.reconnect': 'Reconnect',
'agent.newSession': 'Start New Session',
'agent.connecting': 'Connecting...',
'agent.serverBusy': 'Server is at capacity. Try again later.',
'agent.sessionLost': 'Session is no longer available.',
'agent.connectionError': 'Connection lost.',
'agent.sendError': 'Failed to send message.',
'agent.sessionsError': 'Could not load sessions.',
'agent.noSessions': 'No active sessions.',
'agent.scrollToBottom': 'Scroll to bottom',
```

Czech (`cs.ts`):
```typescript
'tabs.agent': 'Agent',

'agent.selectAgent': 'Vyberte agenta',
'agent.selectModel': 'Model (volitelné)',
'agent.start': 'Spustit agenta',
'agent.interrupt': 'Přerušit',
'agent.terminate': 'Ukončit',
'agent.reconnect': 'Znovu připojit',
'agent.newSession': 'Nová relace',
'agent.connecting': 'Připojování...',
'agent.serverBusy': 'Server je vytížený. Zkuste to později.',
'agent.sessionLost': 'Relace již není dostupná.',
'agent.connectionError': 'Spojení přerušeno.',
'agent.sendError': 'Nepodařilo se odeslat zprávu.',
'agent.sessionsError': 'Relace se nepodařilo načíst.',
'agent.noSessions': 'Žádné aktivní relace.',
'agent.scrollToBottom': 'Posunout dolů',
```

## Frontend: API Client Additions

Add methods to `api.ts`:

```typescript
export interface AgentSession {
	session_id: string;
	agent: string;
	model?: string;
	created_at: string;
	last_message_at: string;
}

export const api = {
	// ... existing methods ...

	getProjectSessions: (name: string) =>
		request<AgentSession[]>('GET', `/projects/${encodeURIComponent(name)}/sessions`),

	terminateProjectSession: (name: string, sessionId: string) =>
		request<{ session_id: string; status: string }>(
			'POST',
			`/projects/${encodeURIComponent(name)}/sessions/${encodeURIComponent(sessionId)}/terminate`
		),
};
```

### WebSocket URL Construction

WebSocket connections are opened directly (not via the `api` helper). The URL is built from `PUBLIC_API_URL`:

```typescript
function buildWsUrl(projectName: string, agent: string, model?: string, sessionId?: string): string {
	const base = (import.meta.env.PUBLIC_API_URL as string).replace(/^http/, 'ws');
	const encoded = encodeURIComponent(projectName);
	if (sessionId) {
		return `${base}/projects/${encoded}/chat/${encodeURIComponent(sessionId)}`;
	}
	const params = new URLSearchParams({ agent });
	if (model) params.set('model', model);
	return `${base}/projects/${encoded}/chat?${params}`;
}
```

The `Authorization` header is passed via the WebSocket protocol field or as a query parameter `?token=...` since the `WebSocket` API does not support custom headers. The backend should accept the token from `Sec-WebSocket-Protocol` or from an `Authorization` query parameter for WebSocket upgrades.

Alternative (simpler): The existing `SessionHandler` WebSocket upgrader already has `CheckOrigin: func(r *http.Request) bool { return true }` and the auth middleware runs before the mux, so the bearer token in the initial HTTP upgrade request's `Authorization` header is validated by the existing `AuthMiddleware`. The frontend passes it via the `protocols` parameter trick or uses a sub-protocol approach. Since the existing `/chat` endpoint works the same way, follow the same pattern the existing client uses.

## Frontend: Agent Store

`dashboard/src/lib/stores/agent.ts` — manages client-side agent state:

```typescript
import { writable } from 'svelte/store';

export type ConnectionState = 'disconnected' | 'connecting' | 'connected';

export interface ChatMessage {
	id: string;
	role: 'user' | 'agent';
	content: string;
	complete: boolean;       // false while agent is still streaming
	error?: string;          // inline error for failed sends
}

export interface AgentSessionState {
	sessionId: string | null;
	agent: string;           // "kiro" | "claude" | ""
	model: string;
	connectionState: ConnectionState;
	messages: ChatMessage[];
}

// Per-session message history keyed by session ID
// Kept in memory so switching sessions preserves context
const sessionHistories = new Map<string, ChatMessage[]>();

function createAgentStore() {
	const { subscribe, set, update } = writable<AgentSessionState>({
		sessionId: null,
		agent: '',
		model: '',
		connectionState: 'disconnected',
		messages: [],
	});

	return {
		subscribe,
		setAgent(agent: string) { update(s => ({ ...s, agent })); },
		setModel(model: string) { update(s => ({ ...s, model })); },
		setConnecting() { update(s => ({ ...s, connectionState: 'connecting' })); },
		setConnected(sessionId: string) {
			update(s => ({
				...s,
				sessionId,
				connectionState: 'connected',
				messages: sessionHistories.get(sessionId) ?? [],
			}));
		},
		setDisconnected() {
			update(s => {
				if (s.sessionId) sessionHistories.set(s.sessionId, s.messages);
				return { ...s, sessionId: null, connectionState: 'disconnected' };
			});
		},
		addUserMessage(content: string) {
			const id = crypto.randomUUID();
			update(s => ({
				...s,
				messages: [...s.messages, { id, role: 'user', content, complete: true }],
			}));
		},
		appendAgentOutput(text: string) {
			update(s => {
				const msgs = [...s.messages];
				const last = msgs[msgs.length - 1];
				if (last && last.role === 'agent' && !last.complete) {
					msgs[msgs.length - 1] = { ...last, content: last.content + text };
				} else {
					msgs.push({ id: crypto.randomUUID(), role: 'agent', content: text, complete: false });
				}
				return { ...s, messages: msgs };
			});
		},
		markAgentDone() {
			update(s => {
				const msgs = [...s.messages];
				const last = msgs[msgs.length - 1];
				if (last && last.role === 'agent') {
					msgs[msgs.length - 1] = { ...last, complete: true };
				}
				return { ...s, messages: msgs };
			});
		},
		switchToSession(sessionId: string) {
			update(s => {
				if (s.sessionId) sessionHistories.set(s.sessionId, s.messages);
				return {
					...s,
					sessionId,
					messages: sessionHistories.get(sessionId) ?? [],
				};
			});
		},
		reset() {
			set({ sessionId: null, agent: '', model: '', connectionState: 'disconnected', messages: [] });
		},
	};
}

export const agentStore = createAgentStore();
```

## Frontend: Agent Tab Page

`dashboard/src/routes/[project]/agent/+page.svelte`

Top-level page that composes the agent UI components:

```
┌─────────────────────────────────────────────────────────────────┐
│ ┌───────────────┐  ┌─────────────────────────────────────────┐  │
│ │ Session List   │  │  Chat Area                              │  │
│ │               │  │                                         │  │
│ │ ● session-1   │  │  ┌─────────────────────────────────┐    │  │
│ │   claude/opus │  │  │ User: How do I fix the bug?     │    │  │
│ │   2min ago    │  │  └─────────────────────────────────┘    │  │
│ │               │  │  ┌─────────────────────────────────┐    │  │
│ │   session-2   │  │  │ Agent: Looking at the code...   │    │  │
│ │   kiro        │  │  │ ▊ (streaming)                   │    │  │
│ │   5min ago    │  │  └─────────────────────────────────┘    │  │
│ │               │  │                                         │  │
│ │ [+ New]       │  ├─────────────────────────────────────────┤  │
│ │               │  │  [Interrupt] [Terminate]                │  │
│ └───────────────┘  │  ┌─────────────────────────────────┐    │  │
│                    │  │ Type a message...          [Send]│    │  │
│                    │  └─────────────────────────────────┘    │  │
│                    └─────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

When no session is active (initial state):

```
┌─────────────────────────────────────────────────────────────────┐
│ ┌───────────────┐  ┌─────────────────────────────────────────┐  │
│ │ Session List   │  │  Agent Setup                            │  │
│ │               │  │                                         │  │
│ │ (no sessions) │  │  Agent:  [▾ Select agent ]              │  │
│ │               │  │  Model:  [                 ]            │  │
│ │               │  │                                         │  │
│ │               │  │  [ Start Agent ]                        │  │
│ │               │  │                                         │  │
│ └───────────────┘  └─────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

On mobile, the session list collapses into a dropdown/selector above the chat area.

## Frontend: ChatInterface Component

`dashboard/src/lib/components/ChatInterface.svelte`

Props:
```typescript
interface ChatInterfaceProps {
	projectName: string;
}
```

Manages the WebSocket connection lifecycle:

1. **Connect** — opens WebSocket, waits for `session_started`, updates store
2. **Send** — validates non-empty, sends `{"type":"message","prompt":"..."}`, adds to history
3. **Receive** — handles `output` (append), `done` (mark complete), `error`, `terminated`
4. **Disconnect** — closes WebSocket, preserves history in store
5. **Reconnect** — opens WebSocket to `/projects/{name}/chat/{id}`, waits for `session_resumed`

### Auto-scroll behavior

Track a `userScrolledUp` flag. On each `output` message:
- If `!userScrolledUp` → scroll container to bottom
- If scrolled up → show "scroll to bottom" button

Set `userScrolledUp = true` when user scrolls the container such that `scrollTop + clientHeight < scrollHeight - 50`.

### Input handling

- Enter (without Shift) submits the message
- Shift+Enter inserts a newline
- Input is a `<textarea>` that auto-grows to fit content (max 6 lines)
- Send button disabled when: input is whitespace-only OR agent is processing (between send and `done`)
- On send failure (WS error): show inline error below the failed message, keep text in textarea

## Frontend: AgentSelector Component

`dashboard/src/lib/components/AgentSelector.svelte`

Shown when no active session exists. Contains:
- `<select>` for agent type: placeholder "Select agent", options "kiro", "claude"
- `<input type="text">` for model (placeholder from i18n, maxlength=100)
- "Start Agent" button — disabled until agent is selected

On "Start Agent" click:
1. Set `connectionState = 'connecting'`
2. Disable button, show connecting indicator
3. Build WebSocket URL and open connection
4. On `session_started` → transition to chat view
5. On error/failure → show inline error, re-enable button

## Frontend: SessionList Component

`dashboard/src/lib/components/SessionList.svelte`

Props:
```typescript
interface SessionListProps {
	projectName: string;
	activeSessionId: string | null;
	onSelect: (sessionId: string) => void;
	onTerminate: (sessionId: string) => void;
	onNewSession: () => void;
}
```

- Fetches sessions from `GET /projects/{name}/sessions` on mount and after create/terminate
- Displays each session: agent icon, model name, relative time of last activity
- Active session highlighted with distinct background
- Each session has a terminate (✕) button
- "+ New" button at the bottom to start a new session
- Error state: inline error + retry button

## Frontend: ChatMessage Component

`dashboard/src/lib/components/ChatMessage.svelte`

Props:
```typescript
interface ChatMessageProps {
	message: ChatMessage;
}
```

Styling:
- User messages: right-aligned, sky-500 background, white text
- Agent messages: left-aligned, slate-100 dark:slate-800 background
- Streaming indicator: blinking cursor (▊) appended when `!message.complete && role === 'agent'`
- Agent text rendered as plain text with preserved whitespace (`whitespace-pre-wrap`)
- Inline error (if `message.error`): red text below the message bubble

## Data Models

### WebSocket Message Protocol (reuses existing types)

Client → Server:
```json
{"type": "message", "prompt": "text"}
{"type": "interrupt"}
{"type": "terminate"}
```

Server → Client:
```json
{"type": "session_started", "sessionId": "uuid"}
{"type": "session_resumed", "sessionId": "uuid"}
{"type": "output", "data": "text chunk"}
{"type": "done"}
{"type": "error", "message": "description"}
{"type": "terminated"}
{"type": "context_compacted"}
```

### Session Summary (REST response)

```json
{
	"session_id": "uuid",
	"agent": "kiro" | "claude",
	"model": "string or empty",
	"created_at": "ISO 8601",
	"last_message_at": "ISO 8601"
}
```

## Backend: File Upload via Docker CP

For project agent sessions, uploaded files are written directly into the running container's filesystem (not onto any host-mounted volume). This keeps the project directory clean and ensures files are automatically deleted when the container is removed.

### Flow

1. Client sends a WebSocket binary frame: `[4 bytes: filename length][filename bytes][file content bytes]`
2. Server decodes using existing `DecodeBinaryFrame` (from `upload.go`)
3. Server validates: file size ≤ 50 MB, filename sanitized via `sanitizeFilename`
4. Server writes the file into the container using Docker `CopyToContainer` API:
   - Destination path: `/tmp/uploads/{sanitized_filename}`
   - Creates a tar archive in memory containing the single file
   - Calls `docker.CopyToContainer(ctx, containerID, "/tmp/uploads", tarReader, ...)`
5. Server sends `{"type": "file_uploaded", "data": "<original_filename>"}` to client
6. Server tracks uploaded filenames in memory (per session)
7. On next user message, prepends metadata to prompt: `[Uploaded Files]\n- file.txt → /tmp/uploads/file.txt\n`

### Container Client Extension

```go
// Add to ContainerClient interface
CopyToContainer(ctx context.Context, containerID string, dstPath string, content io.Reader, options dockertypes.CopyToContainerOptions) error
```

### Helper Function

```go
// CopyFileToContainer writes a single file into a running container.
func (m *ContainerManager) CopyFileToContainer(ctx context.Context, containerID, dstDir, filename string, data []byte) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name: filename,
		Mode: 0644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	tw.Close()

	return m.client.CopyToContainer(ctx, containerID, dstDir, &buf, dockertypes.CopyToContainerOptions{})
}
```

### Why `/tmp/uploads/` Instead of a Volume

- Files live only in container filesystem — no host cleanup needed
- Container removal = automatic file deletion
- No extra bind mounts or host directories to manage
- Agent can copy files to `/workspace/` (project) if the user asks — this is an agent decision, not automatic
- Consistent with the ephemeral nature of chat sessions

## Frontend: File Upload in Chat Interface

The `ChatInterface` component includes a file upload mechanism:

- A 📎 (paperclip) button next to the message input
- Drag-and-drop onto the chat area
- On file selection: read as ArrayBuffer, encode as binary frame (`EncodeBinaryFrame` format), send via WebSocket
- On `file_uploaded` response: display a system message in chat: "📁 file.txt uploaded"
- File upload button disabled while agent is processing or session is not active

```typescript
function sendFile(file: File) {
	const reader = new FileReader();
	reader.onload = () => {
		const data = new Uint8Array(reader.result as ArrayBuffer);
		const nameBytes = new TextEncoder().encode(file.name);
		const frame = new Uint8Array(4 + nameBytes.length + data.length);
		new DataView(frame.buffer).setUint32(0, nameBytes.length);
		frame.set(nameBytes, 4);
		frame.set(data, 4 + nameBytes.length);
		ws.send(frame.buffer);
	};
	reader.readAsArrayBuffer(file);
}
```

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system — essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

### Property 1: Project bind mount is correctly scoped

*For any* valid project name, the `BuildProjectContainerBinds` function SHALL produce a bind string whose first element is exactly `PROJECTS_DIR/{projectName}:/workspace`, ensuring no other host paths are exposed as the workspace.

**Validates: Requirements 1.1**

### Property 2: Invalid agent strings are rejected

*For any* string that is not exactly "kiro" or "claude", the project chat endpoint SHALL reject the request with HTTP 400 and error code "VALIDATION_ERROR".

**Validates: Requirements 1.3, 2.9**

### Property 3: Forbidden project name characters are rejected

*For any* string containing path separators (`/`, `\`), dot-dot sequences (`..`), or characters outside `[a-zA-Z0-9._-]`, the project name validation SHALL reject the input.

**Validates: Requirements 1.6**

### Property 4: Message round-trip preserves content

*For any* non-empty prompt string sent by the client, the server SHALL forward exactly that string to the container's stdin. *For any* output text produced by the container, the server SHALL deliver it to the client as one or more "output" messages whose concatenated `data` fields equal the original text.

**Validates: Requirements 2.3, 2.4**

### Property 5: Unrecognized WebSocket message types produce an error

*For any* JSON message with a `type` field that is not one of "message", "interrupt", or "terminate", the server SHALL respond with `{"type": "error", "message": "unknown message type"}`.

**Validates: Requirements 2.10**

### Property 6: Session listing is project-filtered and time-sorted

*For any* set of active sessions across multiple projects, `GET /projects/{name}/sessions` SHALL return only sessions where `session.Project == name`, and the returned list SHALL be sorted by `last_message_at` in descending order.

**Validates: Requirements 4.2, 4.3**

### Property 7: Valid message submission updates history and clears input

*For any* non-whitespace message string, submitting via the chat interface SHALL: (a) add the message to the displayed history, (b) send the message over the WebSocket, and (c) clear the input field.

**Validates: Requirements 8.2**

### Property 8: Whitespace-only messages are rejected

*For any* string composed entirely of whitespace characters (spaces, tabs, newlines), the chat interface SHALL prevent submission — the send button remains disabled and pressing Enter does nothing.

**Validates: Requirements 8.3**

### Property 9: Output chunks accumulate correctly

*For any* sequence of "output" messages received from the server, the displayed agent response text SHALL equal the concatenation of all `data` fields in the order received.

**Validates: Requirements 8.4**

### Property 10: Client message history survives session switches and connection errors

*For any* session with accumulated message history, switching to another session and back, or experiencing a connection error, SHALL preserve the original message history without loss or reordering.

**Validates: Requirements 10.4, 11.5**

## Error Handling

### Backend Errors

| Condition | HTTP | Code | Message |
|-----------|------|------|---------|
| Invalid project name characters | 400 | VALIDATION_ERROR | project name contains invalid characters |
| Invalid/missing agent param | 400 | VALIDATION_ERROR | agent query parameter must be "kiro" or "claude" |
| Project not found | 404 | NOT_FOUND | project not found |
| Session not found | 404 | NOT_FOUND | session not found or is no longer active |
| Session belongs to different project | 404 | NOT_FOUND | session not found or is no longer active |
| Session already has active connection | 409 | CONFLICT | session already has an active connection |
| No concurrency slots available | 503 | SERVICE_UNAVAILABLE | server is at capacity, try again later |
| Container creation failure | WS error msg | — | failed to create agent container: {detail} |
| Container attach failure | WS error msg | — | failed to attach to container: {detail} |

### Frontend Error States

1. **Connection error** (WS closes unexpectedly): Show "Connection lost" banner + Reconnect button. Message history preserved.
2. **Session creation error** (HTTP error on upgrade): Show inline error near start button. Don't navigate away.
3. **503 capacity error**: Show "Server is at capacity" message. Allow retry.
4. **Reconnect failure** (404 or timeout > 10s): Show "Session no longer available" + "Start New Session" button.
5. **Send failure** (WS in error state): Show inline error below the failed message. Keep message text in input.
6. **Session list fetch failure**: Show inline error with retry button in session list panel.

## Testing Strategy

### Unit Tests (Go)

- `project_agent_handler_test.go`: Test project name validation (regex, `..` sequences, path separators)
- `project_agent_handler_test.go`: Test project existence check with temp directories
- `container_test.go`: Test `BuildProjectContainerBinds` produces correct bind strings
- `session_store_test.go`: Test `ListByProject` filtering and sorting

### Unit Tests (TypeScript)

- `agent.test.ts`: Test agent store state transitions (connect, disconnect, switch, message accumulation)
- Component tests for `AgentSelector`, `SessionList`, `ChatMessage`

### Property-Based Tests

Use **fast-check** (TypeScript) for frontend properties and Go's **rapid** library for backend properties.

Each property test runs a minimum of 100 iterations. Each test is tagged with a comment referencing the design property:

```typescript
// Feature: project-ai-agent, Property 3: Forbidden project name characters are rejected
```

**Backend properties (Go + rapid):**
- Property 1: `BuildProjectContainerBinds` output format
- Property 2: Invalid agent string rejection
- Property 3: `validateProjectName` rejects forbidden characters
- Property 5: Unrecognized message type handling
- Property 6: `ListByProject` filtering and sort order

**Frontend properties (TypeScript + fast-check):**
- Property 7: Message submission → history + WS + clear
- Property 8: Whitespace rejection
- Property 9: Output chunk accumulation
- Property 10: History preservation across switches/errors

**Property 4 (message round-trip)** is tested as an integration test with a mock container since it spans the full WebSocket lifecycle.

### Integration Tests

- Full WebSocket session lifecycle: connect → send message → receive output → done → terminate
- Reconnection flow: connect → disconnect → reconnect → verify session_resumed
- Session listing reflects create/terminate operations
- Idle timeout terminates sessions after configured duration
- Project ownership enforcement (session from project A not accessible via project B)

### Configuration

- Property tests: minimum 100 iterations each
- Integration tests: 1-3 representative scenarios per flow
- All tests run without Docker dependency (mock `ContainerClient` interface)
