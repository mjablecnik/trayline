# Trayline Server API

## Overview

Trayline Server is an HTTP + WebSocket API for programmatic interaction with AI agents (Kiro and Claude Code). It provides two operational modes:

- **One-Shot (REST)** — Submit a prompt, get a result. Stateless, ephemeral container per task.
- **Chat (WebSocket)** — Open an interactive session, exchange multiple messages with streaming output. Stateful, persistent container per session.

## Base URL

Production: `https://trayline-relay.fly.dev`

## Authentication

All endpoints (except `/health`) require a Bearer token in the `Authorization` header:

```
Authorization: Bearer <API_TOKEN>
```

WebSocket connections also pass the token in the Upgrade request headers.

Responses on auth failure:

```json
HTTP 401
{
  "error": "UNAUTHORIZED",
  "message": "missing or invalid Authorization header"
}
```

```json
HTTP 401
{
  "error": "UNAUTHORIZED",
  "message": "invalid token"
}
```

## Error Response Format

All API errors return a consistent JSON envelope:

```json
{
  "error": "ERROR_CODE",
  "message": "Human-readable description"
}
```

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `VALIDATION_ERROR` | 400 | Invalid request body or parameters |
| `UNAUTHORIZED` | 401 | Missing or invalid token |
| `NOT_FOUND` | 404 | Resource not found |
| `CONFLICT` | 409 | Resource state conflict |
| `RATE_LIMITED` | 429 | Too many requests (includes `Retry-After` header) |
| `SERVICE_UNAVAILABLE` | 503 | Server at capacity or shutting down |
| `INTERNAL_ERROR` | 500 | Unexpected server error |

## Rate Limiting

Per-IP token bucket rate limiter. Default: 60 requests/minute.

When rate-limited, the response includes a `Retry-After` header (seconds):

```json
HTTP 429
Retry-After: 3

{
  "error": "RATE_LIMITED",
  "message": "too many requests, retry after 3 seconds"
}
```

The `/health` endpoint is exempt from rate limiting.

---

## REST Endpoints

### GET /health

Health check. No auth required, no rate limiting.

**Response 200:**

```json
{
  "status": "ok"
}
```

**Response 503** (server shutting down):

```json
{
  "status": "shutting_down"
}
```

---

### POST /run

Submit a one-shot task to an AI agent. The server long-polls for up to 30 seconds. If the task completes within that window, you get the result immediately. Otherwise, you receive a 202 Accepted with a task ID to poll later.

#### JSON Request

```
Content-Type: application/json
```

```json
{
  "prompt": "Refactor the login function to use async/await",
  "agent": "kiro",
  "model": "sonnet",
  "system": "You are a senior Go developer",
  "output_format": "json"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `prompt` | string | Yes | The task prompt. Max 32,000 characters. |
| `agent` | string | Yes | Agent to use: `"kiro"` or `"claude"` |
| `model` | string | No | Model override (agent-specific) |
| `system` | string | No | Custom system prompt |
| `output_format` | string | No | Hint for response format: `"json"`, `"text"`, or `"markdown"` |

#### Multipart Request (with file uploads)

```
Content-Type: multipart/form-data
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `prompt` | form field | Yes | The task prompt |
| `agent` | form field | Yes | `"kiro"` or `"claude"` |
| `model` | form field | No | Model override |
| `system` | form field | No | Custom system prompt |
| `output_format` | form field | No | `"json"`, `"text"`, or `"markdown"` |
| `files` | file(s) | No | Up to 10 files, max 50 MB each |

Uploaded files are placed into the agent's workspace and referenced in the prompt automatically.

#### Response 200 (task completed within 30s)

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "agent": "kiro",
  "result": "Here is the refactored function...",
  "valid": true,
  "created_at": "2026-07-08T10:00:00Z",
  "completed_at": "2026-07-08T10:00:15Z"
}
```

#### Response 202 (task still running after 30s)

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "running"
}
```

After receiving 202, poll `GET /run/{id}` until the task reaches a terminal status.

#### Response 400 (validation error)

```json
{
  "error": "VALIDATION_ERROR",
  "message": "prompt is required and must not be empty"
}
```

#### Response 503 (server at capacity)

```json
{
  "error": "SERVICE_UNAVAILABLE",
  "message": "server is at capacity, try again later"
}
```

---

### GET /run/{id}

Get the status and result of a previously submitted task.

**Response 200:**

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "agent": "kiro",
  "result": "The refactored code...",
  "valid": true,
  "created_at": "2026-07-08T10:00:00Z",
  "completed_at": "2026-07-08T10:00:15Z"
}
```

The `status` field transitions through: `queued` → `running` → `completed` | `failed` | `cancelled`

| Field | Type | Present | Description |
|-------|------|---------|-------------|
| `id` | string | Always | UUID of the task |
| `status` | string | Always | One of: `queued`, `running`, `completed`, `failed`, `cancelled` |
| `agent` | string | Always | Agent used: `"kiro"` or `"claude"` |
| `result` | string | On success | Agent output text |
| `error` | string | On failure | Error description |
| `valid` | boolean | Optional | Whether the result passed output validation |
| `created_at` | ISO 8601 | Always | Task creation time |
| `completed_at` | ISO 8601 | On terminal | Task completion time |

**Response 404:**

```json
{
  "error": "NOT_FOUND",
  "message": "task \"abc123\" not found"
}
```

---

### GET /runs

List all recent tasks (max 100, newest first).

**Response 200:**

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "completed",
    "agent": "kiro",
    "created_at": "2026-07-08T10:00:00Z"
  },
  {
    "id": "660e9500-f39c-51e5-b827-557766551111",
    "status": "running",
    "agent": "claude",
    "created_at": "2026-07-08T09:55:00Z"
  }
]
```

---

### POST /run/{id}/cancel

Cancel a queued or running task.

**Response 200 (success):**

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "cancelled"
}
```

**Response 404 (task not found):**

```json
{
  "error": "NOT_FOUND",
  "message": "task \"abc123\" not found"
}
```

**Response 409 (already terminal):**

```json
{
  "error": "CONFLICT",
  "message": "task is already in a terminal status and cannot be cancelled"
}
```

---

### GET /sessions

List all active chat sessions.

**Response 200:**

```json
[
  {
    "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "agent": "kiro",
    "model": "sonnet",
    "created_at": "2026-07-08T08:00:00Z",
    "last_message_at": "2026-07-08T10:15:00Z"
  }
]
```

---

### POST /sessions/{id}/terminate

Terminate an active chat session via REST. Stops the agent container and closes any connected WebSocket client.

**Response 200:**

```json
{
  "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "status": "terminated"
}
```

**Response 404:**

```json
{
  "error": "NOT_FOUND",
  "message": "session \"abc123\" not found"
}
```

---

## WebSocket Endpoints

### GET /chat — New Session

Upgrades to WebSocket. Starts a new interactive chat session with an AI agent.

**Query Parameters:**

| Param | Required | Description |
|-------|----------|-------------|
| `agent` | Yes | `"kiro"` or `"claude"` |
| `model` | No | Model override |
| `system` | No | Custom system prompt |

**Connection URL example:**

```
wss://trayline-relay.fly.dev/chat?agent=kiro&model=sonnet
```

**Headers:**

```
Authorization: Bearer <API_TOKEN>
```

**Possible HTTP errors before upgrade:**

| Status | Condition |
|--------|-----------|
| 400 | Invalid/missing `agent` parameter |
| 401 | Invalid or missing token |
| 503 | Server at capacity (all agent slots occupied) |

Once upgraded, the server sends:

```json
{"type": "session_started", "sessionId": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"}
```

---

### GET /chat/{id} — Reconnect to Existing Session

Upgrades to WebSocket. Reconnects to a previously established session (e.g., after a network disconnect).

**Headers:**

```
Authorization: Bearer <API_TOKEN>
```

**Possible HTTP errors before upgrade:**

| Status | Condition |
|--------|-----------|
| 404 | Session not found or no longer active |
| 409 | Session already has an active connection |

Once upgraded, the server sends:

```json
{"type": "session_resumed", "sessionId": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"}
```

---

## WebSocket Message Protocol

### Client → Server (Text Messages)

All client messages are JSON:

```json
{"type": "message", "prompt": "Explain the auth flow"}
```

| Type | Description |
|------|-------------|
| `message` | Send a prompt to the agent. Field: `prompt` (string) |
| `interrupt` | Send SIGINT to the agent container (interrupts current operation) |
| `terminate` | End the session. Server responds with `terminated` and closes connection |

### Client → Server (Binary Messages — File Upload)

File uploads use a binary frame format:

```
[4 bytes: filename length (big-endian uint32)]
[N bytes: filename (UTF-8)]
[remaining bytes: file content]
```

Max file size: 50 MB. After successful upload, the server responds with:

```json
{"type": "file_uploaded", "data": "original-filename.txt"}
```

Uploaded files are available to the agent in subsequent prompts.

### Server → Client

All server messages are JSON with a `type` field:

```json
{"type": "output", "data": "Here is the agent's response text..."}
```

| Type | Fields | Description |
|------|--------|-------------|
| `session_started` | `sessionId` | New session created, agent container is ready |
| `session_resumed` | `sessionId` | Reconnected to existing session |
| `output` | `data` | Agent output chunk (streaming). Accumulate these. |
| `done` | — | Agent finished its turn. Response is complete. |
| `error` | `message` | An error occurred (non-fatal, session continues) |
| `terminated` | — | Session ended (timeout, user-initiated, or server shutdown) |
| `context_compacted` | — | Agent's context was compacted (informational) |
| `file_uploaded` | `data` | Filename of the successfully uploaded file |

### Typical Message Flow

```
Client                          Server
  |                                |
  |--- WS Upgrade GET /chat ------>|
  |<-- session_started ------------|
  |                                |
  |--- {"type":"message",          |
  |     "prompt":"Hello"} -------->|
  |<-- {"type":"output","data":"Hi |
  |     there! How can I..."} ----|
  |<-- {"type":"output","data":"   |
  |     help you today?"} --------|
  |<-- {"type":"done"} -----------|
  |                                |
  |--- {"type":"message",          |
  |     "prompt":"Refactor X"} -->|
  |<-- {"type":"output",...} ------|
  |<-- {"type":"output",...} ------|
  |<-- {"type":"done"} -----------|
  |                                |
  |--- {"type":"terminate"} ------>|
  |<-- {"type":"terminated"} -----|
  |--- [connection closed] ------->|
```

### Reconnection Flow

If the WebSocket disconnects (network issue, client crash), the session remains active on the server for up to 24 hours (configurable). Reconnect using `GET /chat/{sessionId}`:

```
Client                          Server
  |                                |
  |--- WS Upgrade                  |
  |    GET /chat/{id} ------------>|
  |<-- session_resumed ------------|
  |                                |
  |--- {"type":"message",...} ---->|
  |<-- {"type":"output",...} ------|
  ...
```

If another client is already connected to the session, the reconnect fails with HTTP 409.

---

## Implementation Notes

### Polling Pattern for Long Tasks

When `POST /run` returns 202, implement polling:

```
1. POST /run → 202 {id, status: "running"}
2. Wait 2-3 seconds
3. GET /run/{id} → {status: "running"}
4. Wait 2-3 seconds
5. GET /run/{id} → {status: "completed", result: "..."}
```

Suggested polling interval: 2–5 seconds. Stop polling once status is `completed`, `failed`, or `cancelled`.

### Concurrency Limits

The server has a configurable maximum number of concurrent agent containers (default: 2). This limit is shared between one-shot tasks and chat sessions. When all slots are occupied:

- `POST /run` returns 503 `SERVICE_UNAVAILABLE`
- `GET /chat` (WebSocket upgrade) returns 503 `SERVICE_UNAVAILABLE`

### Session Idle Timeout

Chat sessions are automatically terminated after 24 hours of inactivity (no messages sent). When terminated by timeout, the server sends `{"type": "terminated"}` and closes the WebSocket.

### Supported Agents

| Agent | Description |
|-------|-------------|
| `kiro` | Kiro CLI agent |
| `claude` | Claude Code CLI agent |

### Output Format Hint

The `output_format` field in `POST /run` instructs the agent to format its response:

| Value | Effect |
|-------|--------|
| `json` | Agent is instructed to respond with valid JSON only |
| `text` | Agent responds with plain text |
| `markdown` | Agent responds with markdown |
| (empty) | No format constraint |

This is a hint appended to the prompt — the agent may not always comply perfectly.

---

## Full Example: One-Shot Task (curl)

```bash
# Submit a task
curl -X POST https://trayline-relay.fly.dev/run \
  -H "Authorization: Bearer your-token" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "List all TODO comments in the codebase",
    "agent": "kiro"
  }'

# Response (completed quickly):
# HTTP 200
# {"id":"...","status":"completed","agent":"kiro","result":"Found 3 TODOs...","created_at":"...","completed_at":"..."}

# Response (still running):
# HTTP 202
# {"id":"abc-123","status":"running"}

# Poll for result:
curl https://trayline-relay.fly.dev/run/abc-123 \
  -H "Authorization: Bearer your-token"
```

## Full Example: One-Shot Task with File Upload (curl)

```bash
curl -X POST https://trayline-relay.fly.dev/run \
  -H "Authorization: Bearer your-token" \
  -F "prompt=Review this code for bugs" \
  -F "agent=claude" \
  -F "files=@./src/main.go" \
  -F "files=@./src/handler.go"
```

## Full Example: WebSocket Chat (JavaScript)

```javascript
const ws = new WebSocket(
  "wss://trayline-relay.fly.dev/chat?agent=kiro&model=sonnet",
  { headers: { "Authorization": "Bearer your-token" } }
);

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  switch (msg.type) {
    case "session_started":
      console.log("Session:", msg.sessionId);
      // Send first message
      ws.send(JSON.stringify({ type: "message", prompt: "Hello!" }));
      break;
    case "output":
      process.stdout.write(msg.data);
      break;
    case "done":
      console.log("\n--- Agent turn complete ---");
      break;
    case "error":
      console.error("Error:", msg.message);
      break;
    case "terminated":
      console.log("Session ended");
      ws.close();
      break;
  }
};

// To interrupt the agent:
ws.send(JSON.stringify({ type: "interrupt" }));

// To upload a file (binary frame):
function uploadFile(filename, arrayBuffer) {
  const nameBytes = new TextEncoder().encode(filename);
  const frame = new Uint8Array(4 + nameBytes.length + arrayBuffer.byteLength);
  new DataView(frame.buffer).setUint32(0, nameBytes.length);
  frame.set(nameBytes, 4);
  frame.set(new Uint8Array(arrayBuffer), 4 + nameBytes.length);
  ws.send(frame);
}

// To end the session:
ws.send(JSON.stringify({ type: "terminate" }));
```
