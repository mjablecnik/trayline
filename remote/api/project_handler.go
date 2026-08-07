package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"remote/core"
	"remote/git"
)

// ProjectHandler serves the /projects family of dashboard REST endpoints:
// discovery, metadata, directory listing, and file content.
type ProjectHandler struct {
	projectsDir string
	git         *git.Runner
	logger      *core.Logger
	pinMu       sync.RWMutex
}

// NewProjectHandler creates a ProjectHandler.
func NewProjectHandler(projectsDir string, gitRunner *git.Runner, logger *core.Logger) *ProjectHandler {
	return &ProjectHandler{projectsDir: projectsDir, git: gitRunner, logger: logger}
}

// pinnedFilePath returns the path to the pinned.json file.
func (h *ProjectHandler) pinnedFilePath() string {
	return filepath.Join(h.projectsDir, ".pinned.json")
}

// loadPinned reads the set of pinned project names from disk.
func (h *ProjectHandler) loadPinned() map[string]bool {
	h.pinMu.RLock()
	defer h.pinMu.RUnlock()

	data, err := os.ReadFile(h.pinnedFilePath())
	if err != nil {
		return map[string]bool{}
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return map[string]bool{}
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// savePinned writes the set of pinned project names to disk.
func (h *ProjectHandler) savePinned(pinned map[string]bool) error {
	h.pinMu.Lock()
	defer h.pinMu.Unlock()

	names := make([]string, 0, len(pinned))
	for n := range pinned {
		names = append(names, n)
	}
	sort.Strings(names)
	data, err := json.MarshalIndent(names, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.pinnedFilePath(), data, 0644)
}

// HandlePinProject handles PUT /projects/{name}/pin.
func (h *ProjectHandler) HandlePinProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	_, err := resolveProjectPath(h.projectsDir, h.git.IsRepo, name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	pinned := h.loadPinned()
	pinned[name] = true
	if err := h.savePinned(pinned); err != nil {
		h.logger.Error(r.Context(), "failed to save pinned state: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to save pin state",
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleUnpinProject handles DELETE /projects/{name}/pin.
func (h *ProjectHandler) HandleUnpinProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	_, err := resolveProjectPath(h.projectsDir, h.git.IsRepo, name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	pinned := h.loadPinned()
	delete(pinned, name)
	if err := h.savePinned(pinned); err != nil {
		h.logger.Error(r.Context(), "failed to save pinned state: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to save pin state",
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// langMap maps file extensions to a syntax-highlighting language hint.
var langMap = map[string]string{
	".go":     "go",
	".ts":     "typescript",
	".tsx":    "typescript",
	".js":     "javascript",
	".jsx":    "javascript",
	".py":     "python",
	".rs":     "rust",
	".yaml":   "yaml",
	".yml":    "yaml",
	".json":   "json",
	".md":     "markdown",
	".sh":     "bash",
	".html":   "html",
	".css":    "css",
	".svelte": "svelte",
	".sql":    "sql",
	".toml":   "toml",
}

// languageForPath returns the syntax-highlighting language hint for a file
// path, based on its extension. Returns "" for unrecognized extensions.
func languageForPath(filePath string) string {
	return langMap[filepath.Ext(filePath)]
}

// isBinary reports whether data looks like binary content, by checking for
// a null byte within the first 8KB.
func isBinary(data []byte) bool {
	check := data
	if len(check) > 8192 {
		check = check[:8192]
	}
	return bytes.Contains(check, []byte{0})
}

// toAPICommit converts a git.Commit to the api response Commit type.
func toAPICommit(c *git.Commit) *Commit {
	if c == nil {
		return nil
	}
	return &Commit{Hash: c.Hash, Message: c.Message, Author: c.Author, Date: c.Date}
}

// HandleListProjects handles GET /projects.
func (h *ProjectHandler) HandleListProjects(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.projectsDir)
	if err != nil {
		h.logger.Error(r.Context(), "failed to read projects dir: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to read projects directory",
		})
		return
	}

	pinned := h.loadPinned()

	projects := []ProjectSummary{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoPath := filepath.Join(h.projectsDir, entry.Name())
		if !h.git.IsRepo(repoPath) {
			continue
		}

		branch, err := h.git.CurrentBranch(repoPath)
		if err != nil {
			h.logger.Warn(r.Context(), fmt.Sprintf("skipping project %q: %v", entry.Name(), err))
			continue
		}
		lastCommit, err := h.git.LastCommit(repoPath)
		if err != nil {
			h.logger.Warn(r.Context(), fmt.Sprintf("skipping project %q: %v", entry.Name(), err))
			continue
		}
		uncommitted, err := h.git.HasUncommittedChanges(repoPath)
		if err != nil {
			h.logger.Warn(r.Context(), fmt.Sprintf("skipping project %q: %v", entry.Name(), err))
			continue
		}

		projects = append(projects, ProjectSummary{
			Name:                  entry.Name(),
			Path:                  repoPath,
			Branch:                branch,
			LastCommit:            toAPICommit(lastCommit),
			HasUncommittedChanges: uncommitted,
			Pinned:                pinned[entry.Name()],
		})
	}

	// Sort: pinned first, then by last commit date (newest first) within each group.
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Pinned != projects[j].Pinned {
			return projects[i].Pinned
		}
		ti, _ := time.Parse(time.RFC3339, projects[i].LastCommit.Date)
		tj, _ := time.Parse(time.RFC3339, projects[j].LastCommit.Date)
		return ti.After(tj)
	})

	writeJSON(w, http.StatusOK, projects)
}

// HandleGetProject handles GET /projects/{name}.
func (h *ProjectHandler) HandleGetProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repoPath, err := resolveProjectPath(h.projectsDir, h.git.IsRepo, name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	branch, err := h.git.CurrentBranch(repoPath)
	if err != nil {
		h.logger.Error(r.Context(), "failed to resolve current branch: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to resolve current branch",
		})
		return
	}
	branches, err := h.git.Branches(repoPath)
	if err != nil {
		h.logger.Error(r.Context(), "failed to list branches: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to list branches",
		})
		return
	}
	remoteURL, err := h.git.RemoteURL(repoPath)
	if err != nil {
		h.logger.Error(r.Context(), "failed to resolve remote url: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to resolve remote url",
		})
		return
	}
	lastCommit, err := h.git.LastCommit(repoPath)
	if err != nil {
		h.logger.Error(r.Context(), "failed to resolve last commit: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to resolve last commit",
		})
		return
	}

	writeJSON(w, http.StatusOK, ProjectDetail{
		Name:       name,
		Branch:     branch,
		Branches:   branches,
		RemoteURL:  remoteURL,
		LastCommit: toAPICommit(lastCommit),
	})
}

// HandleGetTree handles GET /projects/{name}/tree/{ref}/{path...}.
func (h *ProjectHandler) HandleGetTree(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repoPath, err := resolveProjectPath(h.projectsDir, h.git.IsRepo, name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	ref := r.PathValue("ref")
	subPath, err := validateSubPath(repoPath, r.PathValue("path"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "invalid path",
		})
		return
	}

	gitEntries, err := h.git.Tree(repoPath, ref, subPath)
	if err != nil {
		if errors.Is(err, git.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, core.ErrorResponse{
				Error:   "NOT_FOUND",
				Message: fmt.Sprintf("path %q not found at ref %q", subPath, ref),
			})
			return
		}
		h.logger.Error(r.Context(), "git tree error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to list directory",
		})
		return
	}

	entries := make([]TreeEntry, len(gitEntries))
	for i, e := range gitEntries {
		entries[i] = TreeEntry{Name: e.Name, Type: e.Type, Size: e.Size}
	}

	writeJSON(w, http.StatusOK, TreeResponse{
		Type:    "directory",
		Path:    subPath,
		Entries: entries,
	})
}

// maxInlineBlobSize is the largest file size (in bytes) returned inline as
// content. Larger files are returned with content:null, truncated:true.
const maxInlineBlobSize = 1 << 20 // 1 MB

// HandleGetBlob handles GET /projects/{name}/blob/{ref}/{path...}.
func (h *ProjectHandler) HandleGetBlob(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repoPath, err := resolveProjectPath(h.projectsDir, h.git.IsRepo, name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	ref := r.PathValue("ref")
	subPath, err := validateSubPath(repoPath, r.PathValue("path"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "invalid path",
		})
		return
	}

	data, err := h.git.Blob(repoPath, ref, subPath)
	if err != nil {
		if errors.Is(err, git.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, core.ErrorResponse{
				Error:   "NOT_FOUND",
				Message: fmt.Sprintf("path %q not found at ref %q", subPath, ref),
			})
			return
		}
		h.logger.Error(r.Context(), "git blob error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to read file",
		})
		return
	}

	resp := BlobResponse{
		Type:     "file",
		Path:     subPath,
		Size:     int64(len(data)),
		Language: languageForPath(subPath),
	}

	switch {
	case isBinary(data):
		resp.Binary = true
	case len(data) > maxInlineBlobSize:
		resp.Truncated = true
	default:
		content := string(data)
		resp.Content = &content
	}

	writeJSON(w, http.StatusOK, resp)
}
