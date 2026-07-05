# Implementation Tasks: File Upload with Prompt

## Task 1: Create upload handler module

- [ ] 1.1 Create `server/api/upload.go` with `UploadedFile` struct, `SaveUploadedFiles`, `SaveSingleFile`, `BuildUploadMetadata`, `CleanupUploadDir`, and `sanitizeFilename` functions as specified in the design
- [ ] 1.2 Create `server/api/upload_test.go` with property-based tests using `pgregory.net/rapid`:
  - Property 1: SaveUploadedFiles stores correct files (1-10 files, valid sizes, content matches)
  - Property 2: BuildUploadMetadata format correctness (N entries for N files, correct paths)
  - Property 4: Count validation rejects and leaves no files on disk
  - Property 5: Size validation rejects and leaves no files on disk
  - Property 6: sanitizeFilename prevents path traversal for any input
  - Property 8: Binary frame encode/decode round-trip
- [ ] 1.3 Add binary frame encoding/decoding functions (`EncodeBinaryFrame`, `DecodeBinaryFrame`) to `upload.go` for the WebSocket file protocol

## Task 2: Modify server task handler for multipart support

- [ ] 2.1 Modify `HandlePostRun` in `server/api/task_handler.go` to detect `Content-Type` header and branch between multipart and JSON parsing
- [ ] 2.2 Implement multipart parsing path: extract `prompt`, `agent`, `model`, `system`, `output_format` from form fields and files from `files` field
- [ ] 2.3 Call `SaveUploadedFiles` with the task ID as subdirectory, then prepend metadata to prompt using `BuildUploadMetadata`
- [ ] 2.4 Add cleanup call (`CleanupUploadDir`) in `executeTask` after task reaches terminal status
- [ ] 2.5 Add example-based tests in `server/api/task_handler_test.go` for multipart POST /run (happy path, missing prompt, backward-compatible JSON)

## Task 3: Modify server session handler for WebSocket file uploads

- [ ] 3.1 Modify the WebSocket read loop in `session_handler.go` to handle binary frames (detect `websocket.BinaryMessage` type)
- [ ] 3.2 On binary frame: decode using `DecodeBinaryFrame`, call `SaveSingleFile` with session ID as subdirectory, send `file_uploaded` ack or `error` message
- [ ] 3.3 Track uploaded files per session (in-memory slice on Session or SessionHandler) so metadata can be prepended to subsequent prompts
- [ ] 3.4 Prepend `BuildUploadMetadata` for all session files when forwarding a text message prompt to the agent
- [ ] 3.5 Add cleanup call (`CleanupUploadDir`) in session termination/cleanup path
- [ ] 3.6 Add `"file_uploaded"` to the WSServerMessage type documentation comment in `session_types.go`

## Task 4: Add configuration for upload limits

- [ ] 4.1 Add `MAX_UPLOAD_SIZE` and `MAX_UPLOAD_FILES` environment variables to `server/core/config.go` with defaults (50 MB, 10 files)
- [ ] 4.2 Update `server/.env.example` with the new optional variables and comments
- [ ] 4.3 Pass config values to upload functions (replace hardcoded constants with config-driven values)

## Task 5: Modify client for REST file upload

- [ ] 5.1 Add `--file` flag (repeatable) to the `run` command in `client/run.go`
- [ ] 5.2 Add file existence/readability validation before sending (exit code 2 on failure)
- [ ] 5.3 Add `PostRunMultipart` method to `client/api.go` that constructs a `multipart/form-data` request with form fields and file parts
- [ ] 5.4 Modify `handleRun` to call `PostRunMultipart` when `--file` flags are present, `PostRun` otherwise
- [ ] 5.5 Add tests in `client/run_test.go` for the new flag parsing and file validation logic

## Task 6: Modify client for WebSocket file upload

- [ ] 6.1 Add `/file <path>` command handling in the chat input loop in `client/chat.go`
- [ ] 6.2 Implement binary frame construction using `EncodeBinaryFrame` equivalent (or inline encoding) and send via `conn.WriteMessage(websocket.BinaryMessage, ...)`
- [ ] 6.3 Handle `file_uploaded` server message type in the WebSocket read loop — print confirmation to stderr
- [ ] 6.4 Handle file read errors gracefully (print to stderr, do not disconnect)
- [ ] 6.5 Add tests in `client/chat_test.go` for `/file` command parsing and error handling

## Task 7: Update documentation and completions

- [ ] 7.1 Update client `--help` text for the `run` command to document the `--file` flag
- [ ] 7.2 Update zsh completion script `client/completions/_trayline-client` to include `--file` flag with file path completion
- [ ] 7.3 Update server README or DOCS if they exist to document the multipart upload API and WebSocket binary frame protocol
