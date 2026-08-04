package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"remote/core"
	"remote/git"
)

// commitHashRe restricts commit hashes to the hexadecimal, 7-40 character
// format git itself produces, so implausible hashes are rejected as 404
// without ever invoking git.
var commitHashRe = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// GitRunner is the minimal interface GitHandler needs to execute git
// operations against a project's repository.
type GitRunner interface {
	Log(repoPath, ref string, limit, offset int) (*git.LogResult, error)
	Show(repoPath, hash string) (*git.CommitDetail, error)
	Status(repoPath string) (*git.StatusResult, error)
	CurrentBranch(repoPath string) (string, error)
	IsRepo(path string) bool
	DiscardFile(repoPath, path string) error
	DiscardAll(repoPath string) error
}

// GitHandler handles git history and working-tree-status REST endpoints.
type GitHandler struct {
	projectsDir string
	git         GitRunner
	logger      *core.Logger
}

// NewGitHandler creates a GitHandler.
func NewGitHandler(projectsDir string, gitRunner GitRunner, logger *core.Logger) *GitHandler {
	return &GitHandler{projectsDir: projectsDir, git: gitRunner, logger: logger}
}

// writeProjectNotFound writes the standard 404 body for an unresolvable project name.
func writeProjectNotFound(w http.ResponseWriter, name string) {
	writeJSON(w, http.StatusNotFound, core.ErrorResponse{
		Error:   "NOT_FOUND",
		Message: fmt.Sprintf("project %q not found", name),
	})
}

// parseIntParam parses an integer query parameter, applying a default when
// absent or unparsable and clamping the result to [min, max].
func parseIntParam(r *http.Request, name string, def, min, max int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// HandleGetCommits handles GET /projects/{name}/commits.
func (h *GitHandler) HandleGetCommits(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repoPath, err := resolveProjectPath(h.projectsDir, h.git.IsRepo, name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	ref := r.URL.Query().Get("ref")
	if ref == "" {
		ref, err = h.git.CurrentBranch(repoPath)
		if err != nil {
			h.logger.Error(r.Context(), "failed to resolve current branch: "+err.Error())
			writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
				Error:   "INTERNAL_ERROR",
				Message: "failed to resolve current branch",
			})
			return
		}
	}

	limit := parseIntParam(r, "limit", 50, 1, 100)
	offset := parseIntParam(r, "offset", 0, 0, math.MaxInt)

	result, err := h.git.Log(repoPath, ref, limit, offset)
	if err != nil {
		if errors.Is(err, git.ErrInvalidRef) {
			writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
				Error:   "VALIDATION_ERROR",
				Message: fmt.Sprintf("ref %q is invalid", ref),
			})
			return
		}
		h.logger.Error(r.Context(), "git log error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to read commit history",
		})
		return
	}

	commits := make([]CommitSummary, len(result.Commits))
	for i, c := range result.Commits {
		commits[i] = CommitSummary{
			Hash:      c.Hash,
			ShortHash: c.ShortHash,
			Message:   c.Message,
			Author:    c.Author,
			Date:      c.Date.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, CommitsResponse{
		Commits: commits,
		Total:   result.Total,
		HasMore: offset+limit < result.Total,
	})
}

// writeCommitNotFound writes the standard 404 body for an unresolvable commit hash.
func writeCommitNotFound(w http.ResponseWriter, hash string) {
	writeJSON(w, http.StatusNotFound, core.ErrorResponse{
		Error:   "NOT_FOUND",
		Message: fmt.Sprintf("commit %q not found", hash),
	})
}

// HandleGetCommitDetail handles GET /projects/{name}/commits/{hash}.
func (h *GitHandler) HandleGetCommitDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repoPath, err := resolveProjectPath(h.projectsDir, h.git.IsRepo, name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	hash := r.PathValue("hash")
	if !commitHashRe.MatchString(hash) {
		writeCommitNotFound(w, hash)
		return
	}

	detail, err := h.git.Show(repoPath, hash)
	if err != nil {
		if errors.Is(err, git.ErrNotFound) {
			writeCommitNotFound(w, hash)
			return
		}
		h.logger.Error(r.Context(), "git show error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to read commit detail",
		})
		return
	}

	writeJSON(w, http.StatusOK, CommitDetailResponse{
		Hash:         detail.Hash,
		ShortHash:    detail.ShortHash,
		Message:      detail.Message,
		Author:       detail.Author,
		Date:         detail.Date.Format(time.RFC3339),
		FilesChanged: detail.FilesChanged,
		Insertions:   detail.Insertions,
		Deletions:    detail.Deletions,
		Diff:         detail.Diff,
	})
}

// HandleGetStatus handles GET /projects/{name}/status.
func (h *GitHandler) HandleGetStatus(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repoPath, err := resolveProjectPath(h.projectsDir, h.git.IsRepo, name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	result, err := h.git.Status(repoPath)
	if err != nil {
		h.logger.Error(r.Context(), "git status error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to read working tree status",
		})
		return
	}

	files := make([]FileStatus, len(result.Files))
	for i, f := range result.Files {
		files[i] = FileStatus{
			Path:       f.Path,
			Status:     f.Status,
			Insertions: f.Insertions,
			Deletions:  f.Deletions,
			Diff:       f.Diff,
		}
	}

	writeJSON(w, http.StatusOK, StatusResponse{
		Clean: result.Clean,
		Files: files,
		Summary: StatusSummary{
			FilesChanged: result.Summary.FilesChanged,
			Insertions:   result.Summary.Insertions,
			Deletions:    result.Summary.Deletions,
		},
	})
}

// HandleDiscardFile handles POST /projects/{name}/changes/discard. It
// irreversibly discards all uncommitted changes to a single working-tree
// path, reverting it to HEAD (or removing it, if HEAD has never seen it).
func (h *GitHandler) HandleDiscardFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repoPath, err := resolveProjectPath(h.projectsDir, h.git.IsRepo, name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	var req DiscardFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "invalid JSON body",
		})
		return
	}

	subPath, err := validateSubPath(repoPath, req.Path)
	if err != nil || subPath == "" {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "invalid path",
		})
		return
	}

	if err := h.git.DiscardFile(repoPath, subPath); err != nil {
		h.logger.Error(r.Context(), "git discard file error: "+err.Error())
		writeDiscardError(w, err)
		return
	}

	h.logger.Info(r.Context(), fmt.Sprintf("discarded changes to %q in project %q", subPath, name))
	w.WriteHeader(http.StatusNoContent)
}

// writeDiscardError writes the appropriate error response for a failed
// discard operation, giving git.ErrIndexLocked a specific, actionable
// message instead of the generic internal-error fallback.
func writeDiscardError(w http.ResponseWriter, err error) {
	if errors.Is(err, git.ErrIndexLocked) {
		writeJSON(w, http.StatusConflict, core.ErrorResponse{
			Error: "GIT_INDEX_LOCKED",
			Message: "Another git process appears to be using this repository right now, or a previous " +
				"one crashed and left a lock file behind (.git/index.lock). Resolve this outside the " +
				"dashboard before retrying.",
		})
		return
	}
	writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
		Error:   "INTERNAL_ERROR",
		Message: "failed to discard changes",
	})
}

// HandleDiscardAll handles POST /projects/{name}/changes/discard-all. It
// irreversibly discards every uncommitted change in the project, reverting
// the working tree to a clean HEAD.
func (h *GitHandler) HandleDiscardAll(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repoPath, err := resolveProjectPath(h.projectsDir, h.git.IsRepo, name)
	if err != nil {
		writeProjectNotFound(w, name)
		return
	}

	if err := h.git.DiscardAll(repoPath); err != nil {
		h.logger.Error(r.Context(), "git discard all error: "+err.Error())
		writeDiscardError(w, err)
		return
	}

	h.logger.Info(r.Context(), fmt.Sprintf("discarded all changes in project %q", name))
	w.WriteHeader(http.StatusNoContent)
}
