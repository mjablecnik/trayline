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
| `APP_PORT` | `8080` | No | Server port |
| `MAX_CONCURRENT_TASKS` | `2` | No | Maximum concurrent agent containers (1–32) |
| `WORKSPACE_DIR` | `./workspace` | No | Workspace path inside the server container |
| `SESSION_TIMEOUT` | `24h` | No | Chat session idle timeout before auto-termination |
| `TASK_TIMEOUT` | `10m` | No | Maximum execution time for one-shot tasks |
| `RATE_LIMIT` | `60` | No | Requests per minute per IP address |
| `STATE_DIR` | `/tmp/trayline-server` | No | Directory for persisting server state across restarts |

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
cd server
cp .env.example .env
# edit .env with your values
go run .
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

**Client messages:**
```json
{"type": "message", "prompt": "Hello"}
{"type": "interrupt"}
{"type": "terminate"}
```

**Server messages:**
```json
{"type": "output", "data": "..."}
{"type": "done"}
{"type": "error", "message": "..."}
{"type": "terminated"}
{"type": "context_compacted"}
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
cd server
go test ./...                    # all tests
go test ./... -run Property      # property tests only
go test -count=1 -race ./...     # with race detector
```
