# Tasks: 003 — Dashboard API Environment Variables Module

## Task 1: Create env package — parser
- [x] Create `remote/env/parser.go`
- [x] Implement `Parse(path string) (*EnvFile, error)` — read and parse .env file
- [x] Handle: empty lines (skip), comments (preserve), key=value pairs
- [x] Support quoted values (strip surrounding `"` or `'`)
- [x] Preserve original key order
- [x] Return structured `EnvFile` with variables + comments

## Task 2: Create env package — discovery
- [x] Implement `Discover(projectPath string) ([]string, error)`
- [x] List project root directory
- [x] Filter files matching `^\.env(\..+)?$` regex
- [x] Return sorted filenames
- [x] Return empty slice (not error) if no .env files exist

## Task 3: Create env package — writer
- [x] Create `remote/env/writer.go`
- [x] Implement `Write(path string, variables []Variable, comments []string) error`
- [x] Write to temp file first (atomic write via rename)
- [x] Write preserved comments at top of file
- [x] Write key=value pairs (quote values containing spaces or special chars)
- [x] Preserve file permissions of original (if exists)

## Task 4: Create env handler and types
- [x] Create `remote/api/env_handler.go` with `EnvHandler` struct
- [x] Create `remote/api/env_types.go` with request/response types
- [x] Wire handler with projectsDir and logger

## Task 5: Implement GET /projects/{name}/env
- [x] Validate project name
- [x] Call env.Discover to find .env files
- [x] Parse each file with env.Parse
- [x] Return EnvListResponse with all files and their variables
- [x] Return empty files array if no .env files (not 404)

## Task 6: Implement PUT /projects/{name}/env
- [x] Validate project name
- [x] Parse request body (filename + variables array)
- [x] Run validation: key format, no empty keys, no duplicates, filename regex
- [x] Verify resolved path is within project root
- [x] Read existing file to preserve comments (if file exists)
- [x] Write via env.Write (atomic)
- [x] Return 200 with updated content
- [x] Log write operation at INFO level

## Task 7: Register routes
- [x] Add env routes to router.go: GET and PUT /projects/{name}/env
- [x] Verify auth middleware applies to both endpoints
