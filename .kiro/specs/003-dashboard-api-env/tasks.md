# Tasks: 003 — Dashboard API Environment Variables Module

## Task 1: Create env package — parser
- [ ] Create `remote/env/parser.go`
- [ ] Implement `Parse(path string) (*EnvFile, error)` — read and parse .env file
- [ ] Handle: empty lines (skip), comments (preserve), key=value pairs
- [ ] Support quoted values (strip surrounding `"` or `'`)
- [ ] Preserve original key order
- [ ] Return structured `EnvFile` with variables + comments

## Task 2: Create env package — discovery
- [ ] Implement `Discover(projectPath string) ([]string, error)`
- [ ] List project root directory
- [ ] Filter files matching `^\.env(\..+)?$` regex
- [ ] Return sorted filenames
- [ ] Return empty slice (not error) if no .env files exist

## Task 3: Create env package — writer
- [ ] Create `remote/env/writer.go`
- [ ] Implement `Write(path string, variables []Variable, comments []string) error`
- [ ] Write to temp file first (atomic write via rename)
- [ ] Write preserved comments at top of file
- [ ] Write key=value pairs (quote values containing spaces or special chars)
- [ ] Preserve file permissions of original (if exists)

## Task 4: Create env handler and types
- [ ] Create `remote/api/env_handler.go` with `EnvHandler` struct
- [ ] Create `remote/api/env_types.go` with request/response types
- [ ] Wire handler with projectsDir and logger

## Task 5: Implement GET /projects/{name}/env
- [ ] Validate project name
- [ ] Call env.Discover to find .env files
- [ ] Parse each file with env.Parse
- [ ] Return EnvListResponse with all files and their variables
- [ ] Return empty files array if no .env files (not 404)

## Task 6: Implement PUT /projects/{name}/env
- [ ] Validate project name
- [ ] Parse request body (filename + variables array)
- [ ] Run validation: key format, no empty keys, no duplicates, filename regex
- [ ] Verify resolved path is within project root
- [ ] Read existing file to preserve comments (if file exists)
- [ ] Write via env.Write (atomic)
- [ ] Return 200 with updated content
- [ ] Log write operation at INFO level

## Task 7: Register routes
- [ ] Add env routes to router.go: GET and PUT /projects/{name}/env
- [ ] Verify auth middleware applies to both endpoints
