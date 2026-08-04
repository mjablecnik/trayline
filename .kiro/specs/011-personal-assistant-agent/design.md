# Design: Personal Assistant Agent

## Overview

Adds a global personal AI assistant to the trayline dashboard, accessible via `/assistant`. Unlike per-project agents (spec 009), the assistant is not scoped to a single project. It mounts a dedicated assistant folder as `/workspace` (the container CWD where Claude CLI discovers CLAUDE.md), all projects at `/projects/`, and agent credentials at `/home/agent/`. The assistant folder is maintained as a git repository and contains persistent memory, prompts, and a summary file.

The implementation reuses the existing WebSocket chat protocol, `SessionStore`, and `ContainerManager` infrastructure. Sessions are marked with `project=__assistant__` in the store. New backend routes live under `/assistant/` and a new frontend route at `/assistant` provides the chat interface with file browser, starter prompts, and session history on reconnect.

## Architecture

The feature extends the existing backend/frontend split following the same patterns as spec 009:

- **Backend** (`remote/`): A new `AssistantHandler` registers routes under `/assistant/`. It reuses the shared `SessionStore` (with `Project="__assistant__"`) and `ContainerManager` (with a new `StartAssistantChatContainer` method). An `AssistantFolderManager` handles folder initialization, git operations, and file browsing. The existing middleware stack (auth, CORS, rate limiting) applies automatically.
- **Frontend** (`dashboard/`): A new `/assistant` route renders a two-tab interface (Chat + Files). The Chat tab reuses the existing `ChatInterface`, `ChatMessage`, `AgentSelector`, and `SessionList` components. A new `AssistantBrowser` component provides the file browser. A new `assistantStore` manages assistant-specific state.

No new infrastructure is introduced. The idle timeout checker applies to assistant sessions since they share the same `SessionStore`.

### Container Mount Layout

```
┌─────────────────────────────────────────────────┐
│  Assistant Container                            │
│                                                 │
│  /workspace    ← ASSISTANT_DATA_DIR (CWD)       │
│    ├── CLAUDE.md     (personality file)         │
│    ├── memory/       (persistent notes)         │
│    ├── prompts/      (starter prompts)          │
│    └── summary.md    (conversation summary)     │
│                                                 │
│  /projects     ← PROJECTS_DIR (read-write)      │
│    ├── project-a/                               │
│    ├── project-b/                               │
│    └── ...                                      │
│                                                 │
│  /home/agent/  ← credentials only              │
│    ├── .claude-src/  (claude, read-only)        │
│    ├── .claude.json-src (claude, read-only)     │
│    ├── .kiro/        (kiro)                     │
│    └── .local/share/kiro-cli/ (kiro)            │
└─────────────────────────────────────────────────┘
```

Key differences from project agent containers:
- `/workspace` = `ASSISTANT_DATA_DIR` (not a project directory)
- `/projects` = `PROJECTS_DIR` (all projects mounted as sibling)
- No home directory mount — only credentials at `/home/agent/`

## File Structure

```
remote/
├── api/
│   ├── assistant_handler.go        # NEW — /assistant/* HTTP/WS handlers
│   ├── assistant_types.go          # NEW — request/response types
│   ├── assistant_folder.go         # NEW — folder init, file browser, git ops
│   ├── project_agent_handler.go    # EXISTING — reference for patterns
│   ├── session_handler.go          # EXISTING
│   ├── session_types.go            # EXISTING — WSClientMessage, WSServerMessage (reused)
│   └── router.go                   # MODIFIED — register assistant routes
├── docker/
│   └── container.go                # MODIFIED — add BuildAssistantContainerBinds,
│                                   #            StartAssistantChatContainer
├── store/
│   └── session.go                  # EXISTING — ListByProject("__assistant__") works as-is
└── core/
    └── config.go                   # MODIFIED — add AssistantDataDir field

dashboard/
├── src/
│   ├── lib/
│   │   ├── api.ts                  # MODIFIED — add assistant API methods
│   │   ├── components/
│   │   │   ├── ChatInterface.svelte     # EXISTING (reused)
│   │   │   ├── ChatMessage.svelte       # EXISTING (reused)
│   │   │   ├── AgentSelector.svelte     # EXISTING (reused)
│   │   │   ├── SessionList.svelte       # EXISTING (reused)
│   │   │   ├── ConnectionError.svelte   # EXISTING (reused for error handling)
│   │   │   ├── AssistantBrowser.svelte  # NEW — file browser for assistant folder
│   │   │   └── StarterPrompts.svelte    # NEW — prompt selection list
│   │   │   └── Header.svelte           # MODIFIED — add assistant nav link
│   │   ├── i18n/
│   │   │   ├── en.ts              # MODIFIED — add assistant translations
│   │   │   └── cs.ts              # MODIFIED — add assistant translations
│   │   └── stores/
│   │       └── assistant.ts       # NEW — assistant-specific state
│   └── routes/
│       └── assistant/
│           └── +page.svelte       # NEW — /assistant route page
└── ...
```

## Components and Interfaces

| Component | Location | Responsibility |
|-----------|----------|----------------|
| `AssistantHandler` | `remote/api/assistant_handler.go` | HTTP/WS handlers for all `/assistant/*` endpoints |
| `AssistantFolderManager` | `remote/api/assistant_folder.go` | Folder initialization, file browsing, git operations, prompts CRUD |
| `BuildAssistantContainerBinds` | `remote/docker/container.go` | Constructs assistant-scoped Docker volume binds |
| `StartAssistantChatContainer` | `remote/docker/container.go` | Creates container with assistant mount layout |
| `SessionStore.ListByProject` | `remote/store/session.go` | Filters sessions by `__assistant__` (existing, no changes) |
| `assistantStore` | `dashboard/src/lib/stores/assistant.ts` | Client-side state for assistant sessions |
| `AssistantBrowser` | `dashboard/src/lib/components/AssistantBrowser.svelte` | File browser with breadcrumbs, file viewer, git status |
| `StarterPrompts` | `dashboard/src/lib/components/StarterPrompts.svelte` | Prompt selection UI for the chat start screen |

## Data Models

### Configuration (new field in `core/Config`)

```go
// core/config.go — new field added to Config struct

// AssistantDataDir is the host directory mounted as /workspace in assistant containers.
// Read from ASSISTANT_DATA_DIR env var. Defaults to {parent of PROJECTS_DIR}/.assistant.
AssistantDataDir string
```

**No `ASSISTANT_HOME_DIR`** — the previous design's home directory mount is removed. Only `ASSISTANT_DATA_DIR` is needed.

### Configuration Loading Logic

```go
// In LoadConfig():

// ASSISTANT_DATA_DIR (optional, defaults to {parent of PROJECTS_DIR}/.assistant)
cfg.AssistantDataDir = os.Getenv("ASSISTANT_DATA_DIR")
if cfg.AssistantDataDir == "" && cfg.ProjectsDir != "" {
    cfg.AssistantDataDir = filepath.Join(filepath.Dir(cfg.ProjectsDir), ".assistant")
}

// Validate: if set and exists, must be a directory. If doesn't exist, create on first use.
if cfg.AssistantDataDir != "" {
    if info, err := os.Stat(cfg.AssistantDataDir); err == nil {
        if !info.IsDir() {
            return nil, fmt.Errorf("ASSISTANT_DATA_DIR %q exists but is not a directory", cfg.AssistantDataDir)
        }
    }
    // If doesn't exist, creation is deferred to AssistantFolderManager.Init()
}
```

### .env.example Addition

```env
# Assistant Configuration
# ASSISTANT_DATA_DIR=/path/to/.assistant  # defaults to {parent of PROJECTS_DIR}/.assistant
```

### WebSocket Message Protocol (reuses existing types)

Client → Server (identical to project agent):
```json
{"type": "auth", "token": "..."}
{"type": "message", "prompt": "text"}
{"type": "interrupt"}
{"type": "terminate"}
```

Server → Client (identical to project agent):
```json
{"type": "session_started", "session_id": "uuid"}
{"type": "session_resumed", "session_id": "uuid", "agent": "...", "model": "..."}
{"type": "history", "messages": [{"role": "user", "content": "...", "complete": true}, ...]}
{"type": "output", "data": "text chunk"}
{"type": "done"}
{"type": "error", "message": "description"}
{"type": "terminated"}
{"type": "context_compacted"}
{"type": "file_uploaded", "data": "filename"}
```

The `history` message is sent after `session_resumed` on reconnect, containing the full transcript. Uses the existing `WSServerMessage.Messages` field and `HistoryMessage` type (already implemented in spec 009).

### REST Response Types

```go
// api/assistant_types.go

type assistantSessionSummary struct {
    SessionID     string    `json:"session_id"`
    Agent         string    `json:"agent"`
    Model         string    `json:"model,omitempty"`
    IsAssistant   bool      `json:"is_assistant"`
    CreatedAt     time.Time `json:"created_at"`
    LastMessageAt time.Time `json:"last_message_at"`
}

type starterPrompt struct {
    Filename    string `json:"filename"`
    DisplayName string `json:"display_name"`
    Content     string `json:"content"`
}

type putPromptRequest struct {
    Content string `json:"content"`
}

type fileEntry struct {
    Name string `json:"name"`
    Type string `json:"type"` // "file" or "directory"
    Size int64  `json:"size"`
}

type directoryResponse struct {
    Path    string      `json:"path"`
    Entries []fileEntry `json:"entries"`
}

type fileContentResponse struct {
    Path      string  `json:"path"`
    Filename  string  `json:"filename"`
    Size      int64   `json:"size"`
    Content   *string `json:"content"`   // null if truncated
    Truncated bool    `json:"truncated"`
}

type gitCommitEntry struct {
    Hash      string `json:"hash"`
    ShortHash string `json:"short_hash"`
    Message   string `json:"message"`
    Date      string `json:"date"`
}

type gitStatusResponse struct {
    Clean   bool             `json:"clean"`
    Files   []gitStatusFile  `json:"files"`
    Summary gitStatusSummary `json:"summary"`
}

type gitStatusFile struct {
    Path   string `json:"path"`
    Status string `json:"status"` // "modified", "untracked", "deleted", "added"
}

type gitStatusSummary struct {
    FilesChanged int `json:"files_changed"`
    Insertions   int `json:"insertions"`
    Deletions    int `json:"deletions"`
}
```

## Backend: Container Manager — Assistant-Scoped Binds

### BuildAssistantContainerBinds

Constructs volume binds for the assistant container with the new layout: `ASSISTANT_DATA_DIR:/workspace`, `PROJECTS_DIR:/projects`, and credentials at `/home/agent/`.

```go
// docker/container.go — new method

// BuildAssistantContainerBinds constructs volume binds for an assistant container.
// Mounts:
//   - ASSISTANT_DATA_DIR as /workspace (CWD — Claude CLI finds CLAUDE.md here)
//   - PROJECTS_DIR as /projects (all projects, read-write)
//   - Agent credentials at /home/agent/ (per agent type)
func (m *ContainerManager) BuildAssistantContainerBinds(agent string) []string {
    const agentHome = "/home/agent"

    binds := []string{
        m.config.AssistantDataDir + ":" + workspaceMount,       // /workspace
        m.config.ProjectsDir + ":" + "/projects",               // /projects
    }

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
            binds = append(binds, m.config.ClaudeHostDir+":"+agentHome+"/.claude-src:ro")
        }
        if m.config.ClaudeConfigHostFile != "" {
            binds = append(binds, m.config.ClaudeConfigHostFile+":"+agentHome+"/.claude.json-src:ro")
        }
    }

    return binds
}
```

### StartAssistantChatContainer

```go
// docker/container.go — new method

// StartAssistantChatContainer creates a persistent interactive container for the
// personal assistant. Mounts ASSISTANT_DATA_DIR at /workspace (CWD) and PROJECTS_DIR
// at /projects. Sets container name with "trayline-assistant-" prefix.
// Does NOT start the container — caller must attach then start.
func (m *ContainerManager) StartAssistantChatContainer(
    ctx context.Context, agent, model, system, sessionID string,
) (string, error) {
    cmd := buildChatCmd(agent, model, system)
    useTTY := agent != "claude"

    containerName := m.resolveAssistantContainerName(ctx, sessionID)

    cfg := &container.Config{
        Image:       SandboxImage,
        Cmd:         cmd,
        Env:         m.buildContainerEnv(),
        Tty:         useTTY,
        AttachStdin: true,
        OpenStdin:   true,
        StdinOnce:   false,
        WorkingDir:  workspaceMount, // /workspace = ASSISTANT_DATA_DIR
    }

    hostCfg := &container.HostConfig{
        Binds:      m.BuildAssistantContainerBinds(agent),
        AutoRemove: false,
    }

    netCfg := &network.NetworkingConfig{
        EndpointsConfig: map[string]*network.EndpointSettings{
            sandboxNetwork: {},
        },
    }

    resp, err := m.client.ContainerCreate(ctx, cfg, hostCfg, netCfg, containerName)
    if err != nil {
        return "", fmt.Errorf("failed to create assistant container: %w", err)
    }
    return resp.ID, nil
}
```

### Container Name Resolution

```go
// resolveAssistantContainerName returns "trayline-assistant-{short}" where short
// is the first 8 chars of sessionID. On naming conflict, appends -2, -3, etc.
// up to 5 attempts.
func (m *ContainerManager) resolveAssistantContainerName(
    ctx context.Context, sessionID string,
) string {
    short := sessionID
    if len(short) > 8 {
        short = short[:8]
    }
    base := "trayline-assistant-" + short
    name := base

    for i := 2; i <= 6; i++ {
        _, err := m.client.ContainerInspect(ctx, name)
        if err != nil {
            return name // container does not exist, name is free
        }
        name = fmt.Sprintf("%s-%d", base, i)
    }
    return name // best effort — Docker will error if still conflicting
}
```

## Backend: Assistant Folder Manager

`api/assistant_folder.go` — manages the assistant data folder lifecycle, file browsing, prompts, and git operations.

### Initialization (called at server startup)

```go
type AssistantFolderManager struct {
    dataDir string
    logger  *core.Logger
}

func NewAssistantFolderManager(dataDir string, logger *core.Logger) *AssistantFolderManager {
    return &AssistantFolderManager{dataDir: dataDir, logger: logger}
}

// Init ensures the assistant folder exists with required structure.
// Called once during server startup. Returns error only on fatal conditions.
func (m *AssistantFolderManager) Init() error {
    info, err := os.Stat(m.dataDir)
    if err == nil {
        if !info.IsDir() {
            return fmt.Errorf("assistant data path %q exists but is not a directory", m.dataDir)
        }
    } else if os.IsNotExist(err) {
        if err := os.MkdirAll(m.dataDir, 0755); err != nil {
            return fmt.Errorf("failed to create assistant folder: %w", err)
        }
    } else {
        return fmt.Errorf("failed to stat assistant folder: %w", err)
    }

    // Ensure subdirectories exist (create missing ones)
    for _, sub := range []string{"memory", "prompts"} {
        subPath := filepath.Join(m.dataDir, sub)
        if err := os.MkdirAll(subPath, 0755); err != nil {
            return fmt.Errorf("failed to create %s/ subdirectory: %w", sub, err)
        }
    }

    // Initialize git repo if needed
    m.initGitRepo()

    // Create default CLAUDE.md if missing
    m.ensureClaudeMD()

    return nil
}
```

### Git Repository Initialization

```go
func (m *AssistantFolderManager) initGitRepo() {
    gitDir := filepath.Join(m.dataDir, ".git")
    if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
        return // already a git repo
    }
    cmd := exec.Command("git", "init")
    cmd.Dir = m.dataDir
    if out, err := cmd.CombinedOutput(); err != nil {
        m.logger.Warn(context.Background(),
            fmt.Sprintf("git init in assistant folder failed: %v: %s", err, out))
    }
}
```

### Default CLAUDE.md Content

The CLAUDE.md content reflects the updated mount layout (`/workspace` = assistant data, `/projects/` = all projects):

```go
const defaultClaudeMD = `# Personal Assistant

You are a personal assistant with access to all projects and a persistent workspace.

## Workspace Layout

- /workspace — your assistant data directory (this is your CWD)
  - CLAUDE.md — this file (your personality and rules)
  - memory/ — persistent knowledge, session highlights, and notes
  - prompts/ — saved starter prompts for quick session starts
  - summary.md — latest conversation summary (created via Summarize action)
- /projects/ — all user projects (read-write access)
  - Each subdirectory is a separate project repository

## Rules

1. After creating or modifying any file in /workspace/, always run:
   git add -A && git commit -m "<descriptive message>"
   in the /workspace/ directory.

2. Commit messages must be concise and descriptive (e.g., "Add session notes from programming discussion", "Update task list with new items").

3. When asked to summarize a conversation, create a concise summary covering key topics, decisions, and action items, and save it to /workspace/summary.md (overwriting any previous summary).

4. You can browse and modify files in /projects/ when the user asks about specific projects.

5. Store persistent notes and session context in /workspace/memory/ for future reference.
`

func (m *AssistantFolderManager) ensureClaudeMD() {
    path := filepath.Join(m.dataDir, "CLAUDE.md")
    if _, err := os.Stat(path); err == nil {
        return // already exists
    }
    if err := os.WriteFile(path, []byte(defaultClaudeMD), 0644); err != nil {
        m.logger.Warn(context.Background(),
            "failed to create default CLAUDE.md: "+err.Error())
    }
}
```

### File Browser Operations

```go
var validAssistantPath = regexp.MustCompile(`^[a-zA-Z0-9._/\-]+$`)

// validatePath checks for path traversal and invalid characters.
func (m *AssistantFolderManager) validatePath(path string) (string, error) {
    if path == "" || path == "/" {
        return ".", nil
    }
    if filepath.IsAbs(path) || strings.Contains(path, "..") ||
        !validAssistantPath.MatchString(path) {
        return "", fmt.Errorf("invalid path")
    }
    return filepath.Clean(path), nil
}

// ListDirectory returns entries for a directory within the assistant folder.
func (m *AssistantFolderManager) ListDirectory(relPath string) ([]fileEntry, error) {
    absPath := filepath.Join(m.dataDir, relPath)
    entries, err := os.ReadDir(absPath)
    if err != nil {
        return nil, err
    }

    result := make([]fileEntry, 0, len(entries))
    for _, e := range entries {
        if e.Name() == ".git" {
            continue
        }
        info, _ := e.Info()
        entry := fileEntry{Name: e.Name(), Type: "file"}
        if e.IsDir() {
            entry.Type = "directory"
        }
        if info != nil {
            entry.Size = info.Size()
        }
        result = append(result, entry)
    }

    // Sort: directories first, then alphabetical
    sort.Slice(result, func(i, j int) bool {
        if result[i].Type != result[j].Type {
            return result[i].Type == "directory"
        }
        return result[i].Name < result[j].Name
    })
    return result, nil
}

// ReadFile returns file content. Files > 1MB return nil content + truncated=true.
func (m *AssistantFolderManager) ReadFile(relPath string) (*fileContentResponse, error) {
    absPath := filepath.Join(m.dataDir, relPath)
    info, err := os.Stat(absPath)
    if err != nil {
        return nil, err
    }
    resp := &fileContentResponse{
        Path: relPath, Filename: filepath.Base(relPath), Size: info.Size(),
    }
    const maxFileSize = 1 * 1024 * 1024
    if info.Size() > maxFileSize {
        resp.Truncated = true
        return resp, nil
    }
    data, err := os.ReadFile(absPath)
    if err != nil {
        return nil, err
    }
    content := string(data)
    resp.Content = &content
    return resp, nil
}
```

### Prompts CRUD Operations

```go
var validPromptFilename = regexp.MustCompile(`^[a-zA-Z0-9._\-]+$`)
const maxPromptFilenameLen = 100
const maxPromptContentLen = 10000

func (m *AssistantFolderManager) ListPrompts() ([]starterPrompt, error) {
    promptsDir := filepath.Join(m.dataDir, "prompts")
    entries, err := os.ReadDir(promptsDir)
    if err != nil {
        if os.IsNotExist(err) {
            return []starterPrompt{}, nil
        }
        return nil, err
    }

    var prompts []starterPrompt
    for _, e := range entries {
        if e.IsDir() { continue }
        ext := strings.ToLower(filepath.Ext(e.Name()))
        if ext != ".md" && ext != ".txt" { continue }
        content, err := os.ReadFile(filepath.Join(promptsDir, e.Name()))
        if err != nil { continue }
        displayName := strings.TrimSuffix(e.Name(), ext)
        displayName = strings.ReplaceAll(displayName, "-", " ")
        displayName = strings.ReplaceAll(displayName, "_", " ")
        prompts = append(prompts, starterPrompt{
            Filename: e.Name(), DisplayName: displayName, Content: string(content),
        })
    }
    sort.Slice(prompts, func(i, j int) bool {
        return prompts[i].Filename < prompts[j].Filename
    })
    return prompts, nil
}

func (m *AssistantFolderManager) GetPrompt(filename string) (*starterPrompt, error) {
    path := filepath.Join(m.dataDir, "prompts", filename)
    content, err := os.ReadFile(path)
    if err != nil { return nil, err }
    ext := filepath.Ext(filename)
    displayName := strings.TrimSuffix(filename, ext)
    displayName = strings.ReplaceAll(displayName, "-", " ")
    displayName = strings.ReplaceAll(displayName, "_", " ")
    return &starterPrompt{
        Filename: filename, DisplayName: displayName, Content: string(content),
    }, nil
}

func (m *AssistantFolderManager) PutPrompt(filename, content string) error {
    path := filepath.Join(m.dataDir, "prompts", filename)
    return os.WriteFile(path, []byte(content), 0644)
}

func (m *AssistantFolderManager) DeletePrompt(filename string) error {
    return os.Remove(filepath.Join(m.dataDir, "prompts", filename))
}

func ValidatePromptFilename(filename string) *core.ErrorResponse {
    if filename == "" || len(filename) > maxPromptFilenameLen {
        return &core.ErrorResponse{Error: "VALIDATION_ERROR", Message: "filename must be 1-100 characters"}
    }
    if !validPromptFilename.MatchString(filename) || strings.Contains(filename, "..") {
        return &core.ErrorResponse{Error: "VALIDATION_ERROR", Message: "filename contains invalid characters"}
    }
    return nil
}
```

### Git Operations

```go
func (m *AssistantFolderManager) GetCommits(limit, offset int) ([]gitCommitEntry, error) {
    if limit <= 0 { limit = 20 }
    args := []string{
        "log", "--format=%H|%h|%s|%aI",
        fmt.Sprintf("--skip=%d", offset),
        fmt.Sprintf("-n%d", limit),
    }
    cmd := exec.Command("git", args...)
    cmd.Dir = m.dataDir
    out, err := cmd.Output()
    if err != nil {
        return []gitCommitEntry{}, nil // empty repo or not initialized
    }
    lines := strings.Split(strings.TrimSpace(string(out)), "\n")
    var commits []gitCommitEntry
    for _, line := range lines {
        if line == "" { continue }
        parts := strings.SplitN(line, "|", 4)
        if len(parts) < 4 { continue }
        commits = append(commits, gitCommitEntry{
            Hash: parts[0], ShortHash: parts[1], Message: parts[2], Date: parts[3],
        })
    }
    return commits, nil
}

func (m *AssistantFolderManager) GetStatus() (*gitStatusResponse, error) {
    cmd := exec.Command("git", "status", "--porcelain")
    cmd.Dir = m.dataDir
    out, err := cmd.Output()
    if err != nil {
        return &gitStatusResponse{Clean: true}, nil
    }
    lines := strings.Split(strings.TrimSpace(string(out)), "\n")
    resp := &gitStatusResponse{Clean: true}
    for _, line := range lines {
        if len(line) < 4 { continue }
        resp.Clean = false
        statusCode := strings.TrimSpace(line[:2])
        filePath := strings.TrimSpace(line[3:])
        status := "modified"
        switch {
        case statusCode == "??":
            status = "untracked"
        case strings.Contains(statusCode, "D"):
            status = "deleted"
        case strings.Contains(statusCode, "A"):
            status = "added"
        }
        resp.Files = append(resp.Files, gitStatusFile{Path: filePath, Status: status})
        resp.Summary.FilesChanged++
    }
    return resp, nil
}
```

## Backend: Assistant Handler

`api/assistant_handler.go` — handles all `/assistant/*` endpoints.

### Handler Structure

```go
type AssistantHandler struct {
    store     *store.SessionStore
    cm        *docker.ContainerManager
    logger    *core.Logger
    config    *core.Config
    stateMgr  StateSaver
    folderMgr *AssistantFolderManager
}

func NewAssistantHandler(
    store *store.SessionStore,
    cm *docker.ContainerManager,
    logger *core.Logger,
    config *core.Config,
    stateMgr StateSaver,
    folderMgr *AssistantFolderManager,
) *AssistantHandler {
    return &AssistantHandler{
        store: store, cm: cm, logger: logger,
        config: config, stateMgr: stateMgr, folderMgr: folderMgr,
    }
}

const assistantProject = "__assistant__"
```

### HandleAssistantChat — WebSocket Session Creation

`GET /assistant/chat?agent=kiro|claude&model=...&system=...`

Follows the same flow as `ProjectAgentHandler.HandleProjectChat` with these differences:

1. Validates `agent` query parameter (same validation: must be "kiro" or "claude")
2. Validates `ASSISTANT_DATA_DIR` is configured and accessible
3. Uses `StartAssistantChatContainer` instead of `StartProjectChatContainer`
4. Sets `sess.Project = "__assistant__"` on the created session
5. Does NOT pass CLAUDE.md content as system prompt — the agent reads it from `/workspace/CLAUDE.md` (its CWD)

The `streamOutput`, `readClient`, idle timeout, file upload, and auth logic are identical to the project agent handler (same helper pattern).

### HandleAssistantChatReconnect — Session Reconnection with History

`GET /assistant/chat/{id}`

Same as `ProjectAgentHandler.HandleProjectChatReconnect` with:
- If `sess.Project != "__assistant__"` → return 404
- After sending `session_resumed`, sends the `history` message with the full transcript

```go
// After sending session_resumed:
h.writeWS(conn, WSServerMessage{Type: "history", Messages: toHistoryMessages(sess.Messages)})
```

The `toHistoryMessages` helper (already exists in project_agent_handler.go) converts `[]store.ChatMessage` to `[]HistoryMessage`.

### HandleAssistantSessions — List Sessions

`GET /assistant/sessions`

```go
func (h *AssistantHandler) HandleAssistantSessions(w http.ResponseWriter, r *http.Request) {
    sessions := h.store.ListByProject(assistantProject)
    result := make([]assistantSessionSummary, len(sessions))
    for i, s := range sessions {
        result[i] = assistantSessionSummary{
            SessionID: s.ID, Agent: s.Agent, Model: s.Model,
            IsAssistant: true, CreatedAt: s.CreatedAt, LastMessageAt: s.LastMessageAt,
        }
    }
    writeJSON(w, http.StatusOK, result)
}
```

### HandleTerminateAssistantSession

`POST /assistant/sessions/{id}/terminate`

Same logic as project agent termination with the ownership check: if `sess.Project != "__assistant__"` → 404. Sends `{"type": "terminated"}` to connected WebSocket, stops container with 10s timeout, removes from store.

### Prompts Endpoints

- `GET /assistant/prompts` — list all starter prompts
- `GET /assistant/prompts/{filename}` — get single prompt content
- `PUT /assistant/prompts/{filename}` — create or update prompt (max 10,000 chars)
- `DELETE /assistant/prompts/{filename}` — delete prompt

All validate filename using `ValidatePromptFilename`. PUT validates content length. 404 on missing files.

### File Browser Endpoints

```go
// GET /assistant/files — list top-level assistant folder contents
// GET /assistant/files/{path...} — list directory or read file at path
func (h *AssistantHandler) HandleFiles(w http.ResponseWriter, r *http.Request) {
    rawPath := r.PathValue("path")
    relPath, err := h.folderMgr.validatePath(rawPath)
    if err != nil {
        writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
            Error: "VALIDATION_ERROR", Message: "path contains invalid characters or traversal",
        })
        return
    }
    absPath := filepath.Join(h.folderMgr.dataDir, relPath)
    info, err := os.Stat(absPath)
    if err != nil {
        if os.IsNotExist(err) {
            writeJSON(w, http.StatusNotFound, core.ErrorResponse{Error: "NOT_FOUND", Message: "path not found"})
        } else {
            writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{Error: "INTERNAL_ERROR", Message: "failed to access path"})
        }
        return
    }
    if info.IsDir() {
        entries, _ := h.folderMgr.ListDirectory(relPath)
        writeJSON(w, http.StatusOK, directoryResponse{Path: relPath, Entries: entries})
    } else {
        fileResp, _ := h.folderMgr.ReadFile(relPath)
        writeJSON(w, http.StatusOK, fileResp)
    }
}

// GET /assistant/files/commits?limit=20&offset=0
// GET /assistant/files/status
```

### File Upload via Docker CP

Identical to project agent (spec 009): WebSocket binary frames are decoded, validated (max 50MB, sanitized filename), and written to `/tmp/uploads/{filename}` inside the running container using `docker cp`. On next user message, upload metadata is prepended to the prompt.

## Backend: Route Registration

Add routes to `router.go`:

```go
// Assistant endpoints (in NewRouter — new parameter: assistantH *AssistantHandler)
mux.HandleFunc("GET /assistant/chat", assistantH.HandleAssistantChat)
mux.HandleFunc("GET /assistant/chat/{id}", assistantH.HandleAssistantChatReconnect)
mux.HandleFunc("GET /assistant/sessions", assistantH.HandleAssistantSessions)
mux.HandleFunc("POST /assistant/sessions/{id}/terminate", assistantH.HandleTerminateAssistantSession)
mux.HandleFunc("GET /assistant/prompts", assistantH.HandleListPrompts)
mux.HandleFunc("GET /assistant/prompts/{filename}", assistantH.HandleGetPrompt)
mux.HandleFunc("PUT /assistant/prompts/{filename}", assistantH.HandlePutPrompt)
mux.HandleFunc("DELETE /assistant/prompts/{filename}", assistantH.HandleDeletePrompt)
mux.HandleFunc("GET /assistant/files", assistantH.HandleFiles)
mux.HandleFunc("GET /assistant/files/commits", assistantH.HandleFileCommits)
mux.HandleFunc("GET /assistant/files/status", assistantH.HandleFileStatus)
mux.HandleFunc("GET /assistant/files/{path...}", assistantH.HandleFiles)
```

**Route ordering note:** `GET /assistant/files/commits` and `GET /assistant/files/status` must be registered before `GET /assistant/files/{path...}` so the specific paths match first.

## Backend: Idle Timeout and Cleanup

The existing idle timeout checker iterates over all sessions in the `SessionStore`. Assistant sessions (`Project == "__assistant__"`) are included automatically. The `SessionTimeout` config (default 24h) applies uniformly.

The assistant handler's `streamOutput` refreshes `sess.LastMessageAt` on agent output (same as project agent), so the idle timer resets on both client and agent activity.

**Container cleanup protection:** The existing `RunOneShot` removal targets specific container IDs — assistant containers are safe. The `StopAndRemoveContainer` method is called explicitly by session termination and idle timeout, both aware of which session they're terminating. The requirement that cleanup logic SHALL NOT terminate `trayline-assistant-*` containers unless initiated through the terminate endpoint or idle timeout is naturally satisfied.

## Frontend: Header Navigation

Add assistant link to `Header.svelte`:

```svelte
<a href="/assistant" class={navLinkClass(isAssistantActive)}>{$t('nav.assistant')}</a>
```

With derived state:
```typescript
let isAssistantActive = $derived(page.url.pathname.startsWith('/assistant'));
```

Positioned after existing navigation items, visible in both desktop and mobile nav.

## Frontend: i18n Additions

English (`en.ts`):
```typescript
'nav.assistant': 'Assistant',

'assistant.chatTab': 'Chat',
'assistant.filesTab': 'Files',
'assistant.summarize': 'Summarize',
'assistant.reset': 'Reset',
'assistant.resetDialog': 'Start new session:',
'assistant.resetWithSummary': 'With Summary',
'assistant.resetWithoutSummary': 'Without Summary',
'assistant.noSummaryWarning': 'No summary file found. Proceeding without summary.',
'assistant.prompts': 'Prompts',
'assistant.promptsEmpty': 'No starter prompts found.',
'assistant.promptsError': 'Could not load prompts.',
'assistant.filesEmpty': 'No files in assistant folder.',
'assistant.filesError': 'Could not load files.',
'assistant.history': 'History',
'assistant.historyEmpty': 'No commits yet.',
'assistant.statusClean': 'All changes committed.',
'assistant.statusDirty': 'Uncommitted changes',
'assistant.refresh': 'Refresh',
'assistant.breadcrumbRoot': 'Assistant',
'assistant.connectionError': 'Connection lost.',
'assistant.reconnect': 'Reconnect',
'assistant.sessionLost': 'Session is no longer available.',
'assistant.newSession': 'Start New Session',
'assistant.serverBusy': 'Server is at capacity. Try again later.',
'assistant.sendError': 'Failed to send message.',
'assistant.fileUploaded': 'File uploaded',
'assistant.uploadError': 'Upload failed',
```

Czech (`cs.ts`):
```typescript
'nav.assistant': 'Asistent',

'assistant.chatTab': 'Chat',
'assistant.filesTab': 'Soubory',
'assistant.summarize': 'Sumarizovat',
'assistant.reset': 'Reset',
'assistant.resetDialog': 'Začít novou relaci:',
'assistant.resetWithSummary': 'Se shrnutím',
'assistant.resetWithoutSummary': 'Bez shrnutí',
'assistant.noSummaryWarning': 'Soubor shrnutí nebyl nalezen. Pokračuji bez shrnutí.',
'assistant.prompts': 'Rychlé prompty',
'assistant.promptsEmpty': 'Žádné uložené prompty.',
'assistant.promptsError': 'Prompty se nepodařilo načíst.',
'assistant.filesEmpty': 'Složka asistenta je prázdná.',
'assistant.filesError': 'Soubory se nepodařilo načíst.',
'assistant.history': 'Historie',
'assistant.historyEmpty': 'Zatím žádné commity.',
'assistant.statusClean': 'Vše commitnuto.',
'assistant.statusDirty': 'Necommitnuté změny',
'assistant.refresh': 'Obnovit',
'assistant.breadcrumbRoot': 'Asistent',
'assistant.connectionError': 'Spojení přerušeno.',
'assistant.reconnect': 'Znovu připojit',
'assistant.sessionLost': 'Relace již není dostupná.',
'assistant.newSession': 'Nová relace',
'assistant.serverBusy': 'Server je vytížený. Zkuste to později.',
'assistant.sendError': 'Nepodařilo se odeslat zprávu.',
'assistant.fileUploaded': 'Soubor nahrán',
'assistant.uploadError': 'Nahrávání selhalo',
```

## Frontend: API Client Additions

Add methods to `api.ts`:

```typescript
export interface StarterPrompt {
    filename: string;
    display_name: string;
    content: string;
}

export interface AssistantFileEntry {
    name: string;
    type: 'file' | 'directory';
    size: number;
}

export interface AssistantDirectoryResponse {
    path: string;
    entries: AssistantFileEntry[];
}

export interface AssistantFileContentResponse {
    path: string;
    filename: string;
    size: number;
    content: string | null;
    truncated: boolean;
}

export interface AssistantSession {
    session_id: string;
    agent: string;
    model?: string;
    is_assistant: boolean;
    created_at: string;
    last_message_at: string;
}

export interface GitCommitEntry {
    hash: string;
    short_hash: string;
    message: string;
    date: string;
}

export interface GitStatusResponse {
    clean: boolean;
    files: { path: string; status: string }[];
    summary: { files_changed: number; insertions: number; deletions: number };
}

export const api = {
    // ... existing methods ...

    // Assistant sessions
    getAssistantSessions: () =>
        request<AssistantSession[]>('GET', '/assistant/sessions'),
    terminateAssistantSession: (sessionId: string) =>
        request<{ session_id: string; status: string }>(
            'POST', `/assistant/sessions/${encodeURIComponent(sessionId)}/terminate`),

    // Starter prompts
    getAssistantPrompts: () =>
        request<StarterPrompt[]>('GET', '/assistant/prompts'),
    getAssistantPrompt: (filename: string) =>
        request<StarterPrompt>('GET', `/assistant/prompts/${encodeURIComponent(filename)}`),
    putAssistantPrompt: (filename: string, content: string) =>
        request<{ status: string }>('PUT', `/assistant/prompts/${encodeURIComponent(filename)}`, { content }),
    deleteAssistantPrompt: (filename: string) =>
        request<{ status: string }>('DELETE', `/assistant/prompts/${encodeURIComponent(filename)}`),

    // File browser
    getAssistantFiles: (path?: string) =>
        request<AssistantDirectoryResponse | AssistantFileContentResponse>(
            'GET', `/assistant/files${path ? '/' + encodePath(path) : ''}`),
    getAssistantFileCommits: (limit = 20, offset = 0) =>
        request<GitCommitEntry[]>('GET', `/assistant/files/commits?limit=${limit}&offset=${offset}`),
    getAssistantFileStatus: () =>
        request<GitStatusResponse>('GET', '/assistant/files/status'),

    // Summary file
    getAssistantSummary: () =>
        request<AssistantFileContentResponse>('GET', '/assistant/files/summary.md'),
};
```

### WebSocket URL Construction

```typescript
export function buildAssistantWsUrl(agent: string, model?: string, sessionId?: string): string {
    const base = (import.meta.env.PUBLIC_API_URL as string).replace(/^http/, 'ws');
    if (sessionId) {
        return `${base}/assistant/chat/${encodeURIComponent(sessionId)}`;
    }
    const params = new URLSearchParams({ agent });
    if (model) params.set('model', model);
    return `${base}/assistant/chat?${params}`;
}
```

## Frontend: Assistant Store

`dashboard/src/lib/stores/assistant.ts`:

```typescript
import { writable } from 'svelte/store';
import type { ChatMessage, ConnectionState } from './agent';

export type AssistantTab = 'chat' | 'files';

export interface AssistantState {
    sessionId: string | null;
    agent: string;
    model: string;
    connectionState: ConnectionState;
    messages: ChatMessage[];
    activeTab: AssistantTab;
    summarizeInProgress: boolean;
    summaryContent: string | null;
    selectedPrompt: string | null;
}

const sessionHistories = new Map<string, ChatMessage[]>();

function createAssistantStore() {
    const { subscribe, set, update } = writable<AssistantState>({
        sessionId: null, agent: '', model: '',
        connectionState: 'disconnected', messages: [],
        activeTab: 'chat', summarizeInProgress: false,
        summaryContent: null, selectedPrompt: null,
    });

    return {
        subscribe,
        setAgent(agent: string) { update(s => ({ ...s, agent })); },
        setModel(model: string) { update(s => ({ ...s, model })); },
        setTab(tab: AssistantTab) { update(s => ({ ...s, activeTab: tab })); },
        setConnecting() { update(s => ({ ...s, connectionState: 'connecting' })); },
        setConnected(sessionId: string) {
            update(s => ({
                ...s, sessionId, connectionState: 'connected',
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
            update(s => ({ ...s, messages: [...s.messages, { id, role: 'user', content, complete: true }] }));
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
                return { ...s, messages: msgs, summarizeInProgress: false };
            });
        },
        setHistory(messages: { role: string; content: string; complete: boolean }[]) {
            update(s => ({
                ...s, messages: messages.map(m => ({
                    id: crypto.randomUUID(), role: m.role as 'user' | 'agent',
                    content: m.content, complete: m.complete,
                })),
            }));
        },
        setSummarizeInProgress() { update(s => ({ ...s, summarizeInProgress: true })); },
        setSummaryContent(content: string | null) { update(s => ({ ...s, summaryContent: content })); },
        selectPrompt(filename: string | null) { update(s => ({ ...s, selectedPrompt: filename })); },
        switchToSession(sessionId: string) {
            update(s => {
                if (s.sessionId) sessionHistories.set(s.sessionId, s.messages);
                return { ...s, sessionId, messages: sessionHistories.get(sessionId) ?? [] };
            });
        },
        clearSessionHistory(sessionId: string) { sessionHistories.delete(sessionId); },
        reset() {
            set({
                sessionId: null, agent: '', model: '',
                connectionState: 'disconnected', messages: [],
                activeTab: 'chat', summarizeInProgress: false,
                summaryContent: null, selectedPrompt: null,
            });
        },
    };
}

export const assistantStore = createAssistantStore();
```

## Frontend: Assistant Page Layout

`dashboard/src/routes/assistant/+page.svelte`

Two-tab layout (Chat | Files) with a session list sidebar:

```
┌─────────────────────────────────────────────────────────────────┐
│  [Chat]  [Files]                                                │
├─────────────────────────────────────────────────────────────────┤
│ ┌───────────────┐  ┌─────────────────────────────────────────┐  │
│ │ Sessions      │  │  Chat Area (initial state)              │  │
│ │               │  │                                         │  │
│ │ (no sessions) │  │  ┌── Starter Prompts ──────────────┐    │  │
│ │               │  │  │ ▸ Daily review                  │    │  │
│ │               │  │  │ ▸ Code review helper            │    │  │
│ │               │  │  └─────────────────────────────────┘    │  │
│ │               │  │                                         │  │
│ │               │  │  ┌─ Agent Selector ────────────────┐    │  │
│ │               │  │  │ Agent: [▾ Select ]              │    │  │
│ │               │  │  │ Model: [          ]             │    │  │
│ │ [+ New]       │  │  │ [Start Agent]                   │    │  │
│ │               │  │  └─────────────────────────────────┘    │  │
│ └───────────────┘  └─────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

When a session is active:

```
┌─────────────────────────────────────────────────────────────────┐
│  [Chat]  [Files]                                                │
├─────────────────────────────────────────────────────────────────┤
│ ┌───────────────┐  ┌─────────────────────────────────────────┐  │
│ │ Sessions      │  │  ┌─────────────────────────────────┐    │  │
│ │               │  │  │ User: Summarize today's work    │    │  │
│ │ ★ session-1   │  │  └─────────────────────────────────┘    │  │
│ │   (active)    │  │  ┌─────────────────────────────────┐    │  │
│ │               │  │  │ Agent: I'll review and save...  │    │  │
│ │ [+ New]       │  │  │ ▊ (streaming)                   │    │  │
│ │               │  │  └─────────────────────────────────┘    │  │
│ └───────────────┘  ├─────────────────────────────────────────┤  │
│                    │ [Summarize] [Reset]   [Interrupt]       │  │
│                    │ ┌─────────────────────────────┐ [📎][▶] │  │
│                    │ │ Type a message...            │         │  │
│                    │ └─────────────────────────────┘         │  │
│                    └─────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

Files tab:

```
┌─────────────────────────────────────────────────────────────────┐
│  [Chat]  [Files]                  ● Uncommitted changes (3)     │
├─────────────────────────────────────────────────────────────────┤
│  Assistant / memory /                                [Refresh]   │
│  ─────────────────────────────────────────────────────────────  │
│  📁 ..                                                          │
│  📄 session-notes.md                            2.4 KB          │
│  📄 project-decisions.md                        1.1 KB          │
│                                                                  │
│  ─────── History ─────────────────────────────────────────────  │
│  a1b2c3d  Add session notes from discussion      2 min ago      │
│  d4e5f6g  Update task list with new items        1 hour ago     │
│  ─────── Uncommitted ─────────────────────────────────────────  │
│  M  memory/scratch.md                                           │
│  ?  memory/new-idea.md                                          │
└─────────────────────────────────────────────────────────────────┘
```

## Frontend: Chat Interface Error Handling

The chat interface handles errors following the same patterns as spec 009:

1. **Connection closed without client disconnect:** Display connection error message with "Reconnect" button. Clicking triggers a single reconnect attempt to `GET /assistant/chat/{id}`.
2. **Session creation error:** Display inline error near agent selector controls without navigating away.
3. **503 at capacity:** Display "Server is busy, try again later" message.
4. **Reconnect failure (404 or timeout >10s):** Display "Session is no longer available" with "Start New Session" button returning to agent selection state.
5. **Connection error preserves messages:** Error messages are displayed without replacing or clearing the conversation view.
6. **Send failure:** Display inline error below the failed message, retain text in input area.

The existing `ConnectionError.svelte` component is reused for error display states.

## Frontend: StarterPrompts Component

`dashboard/src/lib/components/StarterPrompts.svelte`

Props:
```typescript
interface StarterPromptsProps {
    prompts: StarterPrompt[];
    selectedFilename: string | null;
    onSelect: (filename: string | null) => void;
}
```

Behavior:
- Displays up to 10 prompts as clickable cards
- Each card shows: display_name as title, first 100 chars of content as preview (with ellipsis)
- Clicking a prompt selects it (highlighted). Clicking again deselects.
- Selected prompt's full content is inserted into the message input field on session start (not auto-sent)
- If prompts list is empty: component renders nothing
- If fetching fails: shows inline warning, agent selector remains usable

## Frontend: AssistantBrowser Component

`dashboard/src/lib/components/AssistantBrowser.svelte`

A read-only file browser for the assistant folder with breadcrumb navigation, git history, and uncommitted changes indicator.

State:
```typescript
interface BrowserState {
    currentPath: string[];
    entries: AssistantFileEntry[];
    fileContent: AssistantFileContentResponse | null;
    commits: GitCommitEntry[];
    status: GitStatusResponse | null;
    loading: boolean;
    error: string | null;
}
```

Behavior:
- On mount: fetches root directory listing, git status, and recent commits
- Directory navigation: clicking a directory entry fetches its contents, updates breadcrumbs
- File viewing: clicking a file entry fetches its content, displays with syntax highlighting (markdown) or plain text (whitespace-pre-wrap)
- Breadcrumb navigation: clicking a breadcrumb segment navigates back to that level
- Git status badge: shown next to "Files" tab label when `status.clean === false`
- History section: shows 5 most recent commits
- Uncommitted changes section: shows list of changed files with status badges
- Refresh button: re-fetches current directory, status, and commits
- Read-only: no create, edit, or delete actions in the browser UI

## Frontend: Summarize Button

Appears in the chat toolbar when a session is active.

Behavior:
1. Enabled when: session connected AND agent not processing (last message `complete === true`)
2. On click: sends a predefined prompt as a regular user message:

```
Summarize this entire conversation concisely, covering: key topics discussed, decisions made, important information shared, and any pending action items. Save the summary to /workspace/summary.md (overwrite any existing content). Output the summary content in your response so I can review it.
```

3. The prompt appears in chat history as a regular user message
4. While processing: button disabled, normal streaming indicator shown
5. On `done`: button re-enabled via `summarizeInProgress = false` in `markAgentDone()`

## Frontend: Reset Button

Behavior:
1. Visible when: session is active
2. On click (WS connected): display confirmation dialog with "With Summary" and "Without Summary"
3. **"With Summary":** fetch `summary.md` via `api.getAssistantSummary()`. If exists and has content: terminate current session, start new session (same agent/model), insert summary into input field without sending. If 404 or empty: show warning, proceed as "Without Summary".
4. **"Without Summary":** terminate current session, return to initial state (agent selector + prompts)
5. On click (WS disconnected/error): skip dialog, clear local state, return to initial state
6. After reset: clear message history for terminated session, preserve other sessions' histories

## Frontend: File Upload

The chat interface includes a file upload button (📎) and drag-and-drop:

1. File selected → read as ArrayBuffer → encode as binary WebSocket frame
2. Frame format: `[4 bytes: filename length][filename bytes][file content bytes]`
3. On `file_uploaded` response: display confirmation in chat ("📁 file.txt uploaded")
4. Upload button disabled while agent is processing or session not active
5. Max file size: 50 MB (validated server-side)

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

## Frontend: Session History on Reconnect

When the client receives a `history` message after reconnecting:

1. Parse the `messages` array from the server
2. Call `assistantStore.setHistory(messages)` to replace any stale local state
3. Messages are rendered in the chat area immediately
4. The history replaces whatever was in the local message buffer for that session

This ensures the user sees the full conversation transcript even if they navigated away and came back.

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system — essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

### Property 1: Assistant container binds are correctly constructed

*For any* valid `AssistantDataDir` path D, `ProjectsDir` path P, and agent type (kiro or claude), calling `BuildAssistantContainerBinds(agent)` SHALL produce a bind list whose first element is `D:/workspace`, second element is `P:/projects`, and remaining elements are the correct agent-specific credential mounts (kiro: .kiro + kiro-cli; claude: .claude-src:ro + .claude.json-src:ro).

**Validates: Requirements 1.1**

### Property 2: Assistant container name follows prefix format

*For any* valid UUID session ID, `resolveAssistantContainerName(sessionID)` SHALL return a string starting with `trayline-assistant-` followed by the first 8 characters of the session ID, when no naming conflict exists.

**Validates: Requirements 1.3, 5.1**

### Property 3: Container name conflict resolution appends numeric suffix

*For any* session ID where the base name `trayline-assistant-{first8}` already exists as a container, and N containers with suffixes -2 through -(N+1) also exist, `resolveAssistantContainerName` SHALL return the first name with suffix -(N+2) that is unused, up to a maximum of 5 attempts.

**Validates: Requirements 5.2**

### Property 4: Invalid agent strings are rejected

*For any* string that is not exactly "kiro" or "claude", the assistant chat endpoint SHALL reject the request with HTTP 400 and error code "VALIDATION_ERROR". *For any* of exactly "kiro" or "claude", the request SHALL be accepted.

**Validates: Requirements 10.3, 12.4**

### Property 5: Prompt filename validation

*For any* string containing path separators, dot-dot sequences, characters outside `[a-zA-Z0-9._-]`, empty strings, or strings longer than 100 characters, `ValidatePromptFilename` SHALL return a validation error. *For any* string of 1–100 characters composed solely of `[a-zA-Z0-9._-]`, it SHALL return nil.

**Validates: Requirements 3.7**

### Property 6: Prompt content round-trip

*For any* valid filename and content string (up to 10,000 characters), writing via `PutPrompt(filename, content)` then reading via `GetPrompt(filename)` SHALL return a prompt whose Content field equals the original string.

**Validates: Requirements 3.4, 3.5**

### Property 7: Prompt listing is complete and sorted

*For any* set of `.md` and `.txt` files placed in the prompts/ subdirectory, `ListPrompts()` SHALL return exactly all of them with correct filenames, display names (filename without extension, hyphens/underscores replaced by spaces), and content — sorted alphabetically by filename.

**Validates: Requirements 3.2, 3.3**

### Property 8: File path validation rejects traversal and invalid characters

*For any* path string containing `..`, starting with `/`, or containing characters outside `[a-zA-Z0-9._/-]`, `validatePath` SHALL return an error. *For any* path composed solely of valid characters without `..` or leading `/`, it SHALL return a cleaned relative path.

**Validates: Requirements 18.6**

### Property 9: File content response respects size threshold

*For any* file in the assistant folder with size ≤ 1 MB, `ReadFile` SHALL return non-nil Content with Truncated=false. *For any* file > 1 MB, it SHALL return nil Content with Truncated=true.

**Validates: Requirements 18.5**

### Property 10: Directory listing is sorted correctly

*For any* directory in the assistant folder containing a mix of files and subdirectories, `ListDirectory` SHALL return entries sorted with directories first, then alphabetically within each group, and SHALL exclude the `.git` directory.

**Validates: Requirements 18.2, 18.4**

### Property 11: Session message history is preserved across state transitions

*For any* sequence of messages accumulated in a session, switching away from the session and switching back (or encountering a connection error) SHALL preserve the full message array unchanged. Resetting a session SHALL clear only that session's history while preserving other sessions'.

**Validates: Requirements 6.6, 8.6, 16.5**

### Property 12: History on reconnect contains full transcript

*For any* session that has accumulated N messages (user + agent), reconnecting to that session SHALL produce a history message containing exactly those N messages in chronological order with correct role, content, and complete fields.

**Validates: Requirements 11.1, 11.2**

### Property 13: Upload metadata construction

*For any* non-empty set of uploaded files with original and sanitized names, `buildProjectUploadMetadata` SHALL produce a string that starts with `[Uploaded Files]\n`, contains one line per file in format `- {original} → /tmp/uploads/{safe}\n`, and ends with a blank line.

**Validates: Requirements 17.3**

### Property 14: File upload size validation

*For any* file with size ≤ 50 MB, the upload handler SHALL accept it and write to the container. *For any* file with size > 50 MB, it SHALL reject with an error message indicating size exceeded.

**Validates: Requirements 17.4**

## Error Handling

### Backend Errors

| Scenario | HTTP Code | Error Code | Behavior |
|----------|-----------|------------|----------|
| Invalid agent param | 400 | VALIDATION_ERROR | Reject before WS upgrade |
| ASSISTANT_DATA_DIR misconfigured | 500 | CONFIGURATION_ERROR | Reject session creation |
| At capacity (all chat slots taken) | 503 | SERVICE_UNAVAILABLE | Reject before WS upgrade |
| Session not found (reconnect/terminate) | 404 | NOT_FOUND | JSON error response |
| Session already connected (reconnect) | 409 | CONFLICT | JSON error response |
| Invalid prompt filename | 400 | VALIDATION_ERROR | JSON error response |
| Prompt content > 10,000 chars | 400 | VALIDATION_ERROR | JSON error response |
| Prompt file not found | 404 | NOT_FOUND | JSON error response |
| Invalid file path (traversal, bad chars) | 400 | VALIDATION_ERROR | JSON error response |
| File/directory not found | 404 | NOT_FOUND | JSON error response |
| Container creation failure | WS error | — | Send `{"type":"error"}` then close |
| Container attach failure | WS error | — | Send `{"type":"error"}` then close |
| File upload too large | WS error | — | Send `{"type":"error", "message":"...exceeds..."}` |
| File upload container not running | WS error | — | Send `{"type":"error"}` |
| Stdin write failure | WS error | — | Send `{"type":"error", "message":"failed to send message to agent"}` |

### Frontend Error States

| Scenario | UI Behavior |
|----------|-------------|
| WS connection lost (not client-initiated) | Show "Connection lost" + Reconnect button, preserve messages |
| Session creation error | Inline error near agent selector, stay on Agent tab |
| 503 at capacity | "Server is busy, try again later" message |
| Reconnect fails (404 or >10s timeout) | "Session no longer available" + Start New Session button |
| Send failure during active session | Inline error below failed message, retain text in input |
| Prompts fetch failure | Show agent selector without prompts section + inline warning |
| File browser fetch failure | Show error message with retry button |

### CLAUDE.md Not Readable

If CLAUDE.md exists but is not readable (permission error), the server logs a warning and proceeds with session creation. The agent runs without the personality file (default behavior).

## Testing Strategy

### Dual Testing Approach

**Property-based tests** (using Go's `rapid` library for backend, `fast-check` for frontend):
- Minimum 100 iterations per property test
- Each test references its design property
- Tag format: `Feature: personal-assistant-agent, Property {N}: {title}`

**Unit tests** (example-based):
- Specific examples and edge cases from acceptance criteria
- Integration points between components
- Error conditions and boundary values

### Backend Test Plan

| Area | Type | Description |
|------|------|-------------|
| `BuildAssistantContainerBinds` | Property | Verify bind list correctness for random configs and agent types |
| `resolveAssistantContainerName` | Property | Verify name format for random UUIDs, conflict resolution |
| `ValidatePromptFilename` | Property | Verify accept/reject for random strings |
| `PutPrompt` / `GetPrompt` | Property | Round-trip correctness for random content |
| `ListPrompts` | Property | Completeness and sort order for random file sets |
| `validatePath` | Property | Accept/reject for random path strings |
| `ReadFile` | Property | Size threshold behavior for random file sizes |
| `ListDirectory` | Property | Sort order and .git exclusion |
| `buildProjectUploadMetadata` | Property | Metadata format for random file lists |
| Config loading | Unit | Default derivation, env var override |
| Folder initialization | Unit | Create dirs, handle edge cases (file exists, missing subdirs) |
| Git operations | Unit | Commit parsing, status parsing |
| Handler integration | Unit | WS lifecycle, auth, session creation/termination |
| File upload flow | Unit | Binary frame decode, size validation, CopyToContainer call |

### Frontend Test Plan

| Area | Type | Description |
|------|------|-------------|
| `assistantStore` | Property | History preservation across state transitions |
| `assistantStore` | Unit | State transitions, reset, session switch |
| WebSocket handling | Unit | Message type routing, reconnect with history |
| StarterPrompts | Unit | Selection, deselection, empty state |
| AssistantBrowser | Unit | Navigation, breadcrumbs, file viewing |
| Summarize flow | Unit | Predefined prompt text, button state |
| Reset flow | Unit | Dialog options, summary fetch, state cleanup |
| Error handling | Unit | Connection errors, send failures, capacity errors |
| File upload | Unit | Binary frame encoding, confirmation display |

### Integration Tests

- End-to-end: create session → send message → receive output → terminate
- Reconnect flow: create → disconnect → reconnect → verify history
- File upload: upload → send message → verify metadata prepended
- Summarize: send summarize prompt → verify file created at /workspace/summary.md
- Reset with summary: summarize → reset with summary → verify input pre-filled
