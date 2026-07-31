package api

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
}

// NewProjectHandler creates a ProjectHandler.
func NewProjectHandler(projectsDir string, gitRunner *git.Runner, logger *core.Logger) *ProjectHandler {
	return &ProjectHandler{projectsDir: projectsDir, git: gitRunner, logger: logger}
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
		})
	}

	sort.Slice(projects, func(i, j int) bool {
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
