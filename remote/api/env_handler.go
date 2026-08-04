package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"remote/core"
	"remote/env"
)

// validKeyRegex matches valid shell variable names.
var validKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validFilenameRegex matches valid .env filenames (".env", ".env.example", ...).
var validFilenameRegex = regexp.MustCompile(`^\.env(\..+)?$`)

// EnvHandler handles the .env read/write REST endpoints.
type EnvHandler struct {
	projectsDir string
	logger      *core.Logger
}

// NewEnvHandler creates an EnvHandler.
func NewEnvHandler(projectsDir string, logger *core.Logger) *EnvHandler {
	return &EnvHandler{projectsDir: projectsDir, logger: logger}
}

// resolveProjectPath validates name and resolves it to an existing project
// directory under projectsDir. Mirrors GitHandler.resolveProjectPath — spec
// 001's shared ProjectHandler/resolveProjectPath doesn't exist yet (see
// .agents/MEMORY.md), so each handler that needs project resolution keeps its
// own minimal copy for now.
func (h *EnvHandler) resolveProjectPath(name string) (string, error) {
	if name == "" || !projectNameRe.MatchString(name) {
		return "", os.ErrNotExist
	}
	projectPath := filepath.Join(h.projectsDir, name)
	info, err := os.Stat(projectPath)
	if err != nil || !info.IsDir() {
		return "", os.ErrNotExist
	}
	return projectPath, nil
}

// HandleGetEnv handles GET /projects/{name}/env.
func (h *EnvHandler) HandleGetEnv(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	projectPath, err := h.resolveProjectPath(name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	relPaths, err := env.Discover(projectPath)
	if err != nil {
		h.logger.Error(r.Context(), "env discover error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to discover env files",
		})
		return
	}

	files := make([]EnvFileResponse, 0, len(relPaths))
	for _, relPath := range relPaths {
		ef, err := env.Parse(filepath.Join(projectPath, filepath.FromSlash(relPath)))
		if err != nil {
			h.logger.Error(r.Context(), "env parse error: "+err.Error())
			writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
				Error:   "INTERNAL_ERROR",
				Message: "failed to parse env file",
			})
			return
		}

		vars := make([]EnvVarResponse, 0, len(ef.Variables))
		for _, v := range ef.Variables {
			vars = append(vars, EnvVarResponse{Key: v.Key, Value: v.Value})
		}
		files = append(files, EnvFileResponse{Path: relPath, Variables: vars})
	}

	writeJSON(w, http.StatusOK, EnvListResponse{Files: files})
}

// HandlePutEnv handles PUT /projects/{name}/env.
func (h *EnvHandler) HandlePutEnv(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	projectPath, err := h.resolveProjectPath(name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	var req PutEnvRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "invalid JSON body",
		})
		return
	}

	relPath, err := validateEnvPath(projectPath, req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: fmt.Sprintf("path %q must be a .env file within the project, with no \"..\" segments", req.Path),
		})
		return
	}

	if msg := validatePutEnvRequest(req); msg != "" {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: msg,
		})
		return
	}

	filePath := filepath.Join(projectPath, filepath.FromSlash(relPath))

	var comments []string
	if existing, err := env.Parse(filePath); err == nil {
		comments = existing.Comments
	} else if !os.IsNotExist(err) {
		h.logger.Error(r.Context(), "env read existing error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to read existing env file",
		})
		return
	}

	variables := make([]env.Variable, 0, len(req.Variables))
	for _, v := range req.Variables {
		variables = append(variables, env.Variable{Key: v.Key, Value: v.Value})
	}

	if err := env.Write(filePath, variables, comments); err != nil {
		h.logger.Error(r.Context(), "env write error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to write env file",
		})
		return
	}

	h.logger.Info(r.Context(), fmt.Sprintf(
		"env file updated project=%s path=%s variables=%d", name, relPath, len(req.Variables)))

	respVars := make([]EnvVarResponse, 0, len(req.Variables))
	for _, v := range req.Variables {
		respVars = append(respVars, EnvVarResponse{Key: v.Key, Value: v.Value})
	}
	writeJSON(w, http.StatusOK, EnvFileResponse{Path: relPath, Variables: respVars})
}

// validateEnvPath validates a client-supplied relative .env file path: no
// ".." segments, must resolve within projectPath (mirrors the tree/blob
// handlers' own path validation), and its final segment must look like a
// .env file. Returns the cleaned, "/"-joined relative path.
func validateEnvPath(projectPath, path string) (string, error) {
	cleaned, err := validateSubPath(projectPath, path)
	if err != nil {
		return "", err
	}
	if cleaned == "" || !validFilenameRegex.MatchString(filepath.Base(cleaned)) {
		return "", errInvalidPath
	}
	return cleaned, nil
}

// validatePutEnvRequest validates a PutEnvRequest's variables and returns a
// descriptive error message, or "" if they're valid. The path itself is
// validated separately by validateEnvPath.
func validatePutEnvRequest(req PutEnvRequest) string {
	seen := make(map[string]struct{}, len(req.Variables))
	for _, v := range req.Variables {
		if v.Key == "" {
			return "key must not be empty"
		}
		if !validKeyRegex.MatchString(v.Key) {
			return fmt.Sprintf("invalid key %q: must match ^[A-Za-z_][A-Za-z0-9_]*$", v.Key)
		}
		if _, dup := seen[v.Key]; dup {
			return fmt.Sprintf("duplicate key %q", v.Key)
		}
		seen[v.Key] = struct{}{}
	}
	return ""
}
