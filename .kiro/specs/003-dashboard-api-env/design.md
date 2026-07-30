# Design: 003 — Dashboard API Environment Variables Module

## Overview

Adds endpoints for reading and writing `.env` files within projects. Simple file I/O with parsing and validation.

## File Structure

```
remote/
├── api/
│   ├── env_handler.go          # NEW — /env endpoints
│   ├── env_types.go            # NEW — request/response types
│   └── router.go               # MODIFIED — register new routes
├── env/
│   ├── parser.go               # NEW — .env file parser
│   └── writer.go               # NEW — .env file writer (atomic)
└── ...
```

## Component Design

### Env Package (`env/`)

```go
// env/parser.go
type Variable struct {
    Key   string
    Value string
}

type EnvFile struct {
    Filename  string
    Variables []Variable
    Comments  []string // preserved comment lines from original file
}

func Parse(path string) (*EnvFile, error)
func Discover(projectPath string) ([]string, error)
```

**Parse** logic:
- Read file line by line
- Skip empty lines
- Lines starting with `#` are stored as comments (preserved for write-back)
- Other lines split on first `=`: left is key, right is value
- Value may be quoted (strip surrounding `"` or `'`)
- Preserve original key order

**Discover** logic:
- List project root directory
- Filter files matching regex `^\.env(\..+)?$`
- Return sorted filenames

```go
// env/writer.go
func Write(path string, variables []Variable, comments []string) error
```

**Write** logic:
- Write to temp file (`path + ".tmp"`)
- Write preserved comments at top
- Write each key=value pair (quote value if it contains spaces or special chars)
- Rename temp → target (atomic on same filesystem)
- Preserve original file permissions

### Env Handler (`api/env_handler.go`)

```go
type EnvHandler struct {
    projectsDir string
    logger      *core.Logger
}

func (h *EnvHandler) HandleGetEnv(w http.ResponseWriter, r *http.Request)
func (h *EnvHandler) HandlePutEnv(w http.ResponseWriter, r *http.Request)
```

### Validation (`HandlePutEnv`)

```go
var validKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var validFilenameRegex = regexp.MustCompile(`^\.env(\..+)?$`)

func validateEnvRequest(req PutEnvRequest) error {
    // 1. Validate filename matches pattern
    // 2. Validate each key is non-empty and matches regex
    // 3. Check for duplicate keys
    // 4. Return first validation error found
}
```

### Security Checks

Before writing:
1. Validate filename against regex
2. Construct path: `filepath.Join(projectPath, filename)`
3. Verify resolved path is within project directory (no `../`)
4. Verify it's in the root (no `/` in filename after the `.env` prefix)

## Route Registration

```go
mux.HandleFunc("GET /projects/{name}/env", envH.HandleGetEnv)
mux.HandleFunc("PUT /projects/{name}/env", envH.HandlePutEnv)
```

## Response Types (`api/env_types.go`)

```go
type EnvListResponse struct {
    Files []EnvFileResponse `json:"files"`
}

type EnvFileResponse struct {
    Filename  string            `json:"filename"`
    Variables []EnvVarResponse  `json:"variables"`
}

type EnvVarResponse struct {
    Key   string `json:"key"`
    Value string `json:"value"`
}

type PutEnvRequest struct {
    Filename  string           `json:"filename"`
    Variables []EnvVarRequest  `json:"variables"`
}

type EnvVarRequest struct {
    Key   string `json:"key"`
    Value string `json:"value"`
}
```

## Error Responses

| Condition | Code | Error |
|-----------|------|-------|
| Project not found | 404 | NOT_FOUND |
| Invalid filename | 400 | VALIDATION_ERROR |
| Empty key | 400 | VALIDATION_ERROR |
| Invalid key format | 400 | VALIDATION_ERROR |
| Duplicate key | 400 | VALIDATION_ERROR |
| Path traversal attempt | 400 | VALIDATION_ERROR |
| File write failure | 500 | INTERNAL_ERROR |

## Logging

All PUT operations logged at INFO level:
```
"env file updated" project=my-app filename=.env variables=5
```
