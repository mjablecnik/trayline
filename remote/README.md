# Agent API Server

HTTP server exposing REST and WebSocket APIs for programmatic interaction with AI agents (Kiro CLI and Claude Code). Manages agent containers via the Docker API, supports one-shot task execution with long polling, and WebSocket-based chat sessions with streaming output.

## Prerequisites

- Docker with the `trayline-net` network and `trayline-proxy` container running
- The `trayline-sandbox` Docker image available locally

## Environment Variables

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `API_TOKEN` | — | Yes | Bearer token for API authentication |
| `WORKSPACE_HOST_DIR` | — | Yes | Host filesystem path to workspace directory (used for Docker volume mounts) |
| `PROJECTS_DIR` | — | Yes | Directory scanned for dashboard projects (must contain git repos as subdirectories) |
| `APP_PORT` | `8080` | No | Server port |
| `MAX_CONCURRENT_TASKS` | `2` | No | Maximum concurrent one-shot task containers (1–32) |
| `MAX_CHAT_SESSIONS` | `4` | No | Maximum concurrent interactive chat sessions (1–32) |
| `WORKSPACE_DIR` | `./workspace` | No | Workspace path inside the server container |
| `SESSION_TIMEOUT` | `24h` | No | Chat session idle timeout before auto-termination |
| `TASK_TIMEOUT` | `10m` | No | Maximum execution time for one-shot tasks |
| `RATE_LIMIT` | `60` | No | Requests per minute per IP address |
| `STATE_DIR` | `/tmp/trayline-server` | No | Directory for persisting server state across restarts |
| `MAX_UPLOAD_SIZE` | `52428800` (50 MB) | No | Maximum size in bytes per uploaded file |
| `MAX_UPLOAD_FILES` | `10` | No | Maximum number of files per request |
| `MAX_PROMPT_LENGTH` | `32000` | No | Maximum prompt length in characters for the `/run` endpoint |
| `DASHBOARD_ORIGIN` | — | No | Allowed CORS origin for the dashboard frontend; empty disables CORS |
| `TRAYLINE_HOME_DIR` | `~/.trayline` | No | Host path mounted read-only into workflow containers at `/home/agent/.trayline` |
| `PIPELINES_DIR` | `TRAYLINE_HOME_DIR/pipelines` | No | Directory used for pipeline discovery and workflow execution |
| `DOCKER_HOST` | — | No | Docker proxy endpoint; set automatically by `start-docker.sh` |
| `KIRO_HOST_DIR` | — | No | Host path to `~/.kiro` (workspace config, steering files) |
| `KIRO_CREDS_HOST_DIR` | — | No | Host path to `~/.local/share/kiro-cli` (auth token) |
| `CLAUDE_HOST_DIR` | — | No | Host path to `~/.claude` (session data) |
| `CLAUDE_CONFIG_HOST_FILE` | — | No | Host path to `~/.claude.json` (global config / token) |

Copy `.env.example` to `.env` and fill in the required values before running.

## Running with Docker

```bash
# Build the image and start the container
bash scripts/start-docker.sh

# Stop and remove the container
bash scripts/stop-docker.sh
```

## Running locally (development)

```bash
cp .env.example .env
# edit .env with your values
go run ./cmd/server
```

## API Reference

### Authentication

All endpoints except `/health` require an `Authorization: Bearer <token>` header.

### Health

```
GET /health
```

Returns `{"status": "ok"}` (HTTP 200) while running, or `{"status": "shutting_down"}` (HTTP 503) during graceful shutdown.

### One-Shot Tasks

#### Submit a task

```
POST /run
Content-Type: application/json

{
  "prompt": "Summarize the files in the workspace",
  "agent": "claude",
  "model": "claude-opus-4-5",     // optional
  "system": "You are a helpful assistant",  // optional
  "output_format": "json"         // optional: "json", "text", or "markdown"
}
```

Holds the connection open for up to 30 seconds. Returns HTTP 200 with the full result if the agent finishes within that window, or HTTP 202 with `{"id": "...", "status": "running"}` if it times out.

#### Submit a task with file uploads

```
POST /run
Content-Type: multipart/form-data
```

Form fields mirror the JSON body (`prompt`, `agent`, `model`, `system`, `output_format`). Files are attached under the `files` field (repeatable). Limits: max 10 files, 50 MB per file (configurable via `MAX_UPLOAD_FILES` and `MAX_UPLOAD_SIZE`).

Files are written to `uploads/{taskID}/` inside the workspace and are accessible to the agent at `/workspace/uploads/{taskID}/`. The server prepends a metadata block to the prompt so the agent knows where to find them:

```
[Uploaded Files]
- report.pdf → /workspace/uploads/abc-123/report.pdf
- data.csv → /workspace/uploads/abc-123/data.csv

<original prompt here>
```

Uploaded files are automatically deleted when the task reaches a terminal state.

#### Get task status

```
GET /run/{id}
```

Returns current task status and result (if completed).

#### List tasks

```
GET /runs
```

Returns up to 100 most recent tasks ordered by creation time, descending.

#### Cancel a task

```
POST /run/{id}/cancel
```

Cancels a queued or running task. Returns HTTP 409 if already in a terminal state.

### Chat Sessions (WebSocket)

#### Open a session

```
WS /chat?agent=claude&model=claude-opus-4-5&system=You+are+helpful
```

`agent` is required (`kiro` or `claude`). `model` and `system` are optional.

Upon connection, the server sends:
```json
{"type": "session_started", "sessionId": "<uuid>"}
```

**Client messages (text frames):**
```json
{"type": "message", "prompt": "Hello"}
{"type": "interrupt"}
{"type": "terminate"}
```

**Client messages (binary frames — file upload):**

Binary frames use a simple header format:

```
[4 bytes: filename length, big-endian uint32]
[N bytes: filename, UTF-8]
[remaining bytes: file content]
```

On success, the server responds with:
```json
{"type": "file_uploaded", "data": "report.pdf"}
```

On error:
```json
{"type": "error", "message": "file \"big.zip\" exceeds maximum size of 52428800 bytes"}
```

Files uploaded during a session are stored at `/workspace/uploads/{sessionID}/` and their paths are prepended to the next text message prompt automatically. Uploaded files are deleted when the session terminates.

**Server messages (text frames):**
```json
{"type": "output", "data": "..."}
{"type": "done"}
{"type": "error", "message": "..."}
{"type": "terminated"}
{"type": "context_compacted"}
{"type": "file_uploaded", "data": "<filename>"}
```

#### Reconnect to a session

```
WS /chat/{session_id}
```

Reconnects to an existing session. The server sends `{"type": "session_resumed", "sessionId": "..."}`. Only one client connection per session is allowed.

#### List sessions

```
GET /sessions
```

Returns active sessions ordered by last activity, descending.

#### Terminate a session via REST

```
POST /sessions/{session_id}/terminate
```

Stops the container and removes the session. Returns HTTP 404 if the session does not exist.

## Error Response Format

All errors use:
```json
{
  "error": "ERROR_CODE",
  "message": "Human-readable description"
}
```

| Code | HTTP Status | Meaning |
|------|-------------|---------|
| `VALIDATION_ERROR` | 400 | Invalid request body or parameters |
| `UNAUTHORIZED` | 401 | Missing or invalid bearer token |
| `NOT_FOUND` | 404 | Task or session not found |
| `CONFLICT` | 409 | Cannot cancel terminal task; session already connected |
| `RATE_LIMITED` | 429 | Too many requests from this IP |
| `SERVICE_UNAVAILABLE` | 503 | At capacity or shutting down |
| `INTERNAL_ERROR` | 500 | Unexpected server error |

## Running Tests

```bash
go test ./...                    # all tests
go test ./... -run Property      # property tests only
go test -count=1 -race ./...     # with race detector
```
