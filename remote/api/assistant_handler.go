package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"remote/core"
	"remote/docker"
	"remote/store"
)

// assistantProject is the synthetic project name used to tag assistant
// sessions in the SessionStore, distinguishing them from project-scoped
// sessions.
const assistantProject = "__assistant__"

// AssistantHandler handles personal assistant agent endpoints: chat,
// sessions, starter prompts, and the assistant folder file browser.
type AssistantHandler struct {
	store     *store.SessionStore
	cm        *docker.ContainerManager
	logger    *core.Logger
	config    *core.Config
	stateMgr  StateSaver
	folderMgr *AssistantFolderManager
}

// NewAssistantHandler creates an AssistantHandler.
func NewAssistantHandler(
	store *store.SessionStore,
	cm *docker.ContainerManager,
	logger *core.Logger,
	config *core.Config,
	stateMgr StateSaver,
	folderMgr *AssistantFolderManager,
) *AssistantHandler {
	return &AssistantHandler{
		store:     store,
		cm:        cm,
		logger:    logger,
		config:    config,
		stateMgr:  stateMgr,
		folderMgr: folderMgr,
	}
}

// HandleAssistantSessions handles GET /assistant/sessions, listing all active
// assistant sessions ordered by last_message_at descending.
func (h *AssistantHandler) HandleAssistantSessions(w http.ResponseWriter, r *http.Request) {
	sessions := h.store.ListByProject(assistantProject)
	result := make([]assistantSessionSummary, len(sessions))
	for i, s := range sessions {
		result[i] = assistantSessionSummary{
			SessionID:     s.ID,
			Agent:         s.Agent,
			Model:         s.Model,
			IsAssistant:   true,
			CreatedAt:     s.CreatedAt,
			LastMessageAt: s.LastMessageAt,
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// HandleTerminateAssistantSession handles POST /assistant/sessions/{id}/terminate.
func (h *AssistantHandler) HandleTerminateAssistantSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := h.store.Get(id)
	if sess == nil || sess.Project != assistantProject {
		writeJSON(w, http.StatusNotFound, core.ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("session %q not found", id),
		})
		return
	}

	h.logger.Info(r.Context(), fmt.Sprintf("assistant session %s terminated: user-initiated", id))

	sess.ConnMu.Lock()
	if sess.Conn != nil {
		h.writeWS(sess.Conn, WSServerMessage{Type: "terminated"})
		sess.Conn.Close()
		sess.Conn = nil
	}
	sess.ConnMu.Unlock()

	h.terminateSessionImmediately(id)

	if sess.CancelFunc != nil {
		sess.CancelFunc()
	}

	h.saveState(r.Context())

	writeJSON(w, http.StatusOK, map[string]string{
		"session_id": id,
		"status":     "terminated",
	})
}

// HandleListPrompts handles GET /assistant/prompts, listing all starter
// prompts from the assistant folder's prompts/ subdirectory.
func (h *AssistantHandler) HandleListPrompts(w http.ResponseWriter, r *http.Request) {
	prompts, err := h.folderMgr.ListPrompts()
	if err != nil {
		h.logger.Error(r.Context(), "list prompts error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to list prompts",
		})
		return
	}
	writeJSON(w, http.StatusOK, prompts)
}

// HandleGetPrompt handles GET /assistant/prompts/{filename}.
func (h *AssistantHandler) HandleGetPrompt(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if err := ValidatePromptFilename(filename); err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: err.Error(),
		})
		return
	}

	prompt, err := h.folderMgr.GetPrompt(filename)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, core.ErrorResponse{
				Error:   "NOT_FOUND",
				Message: fmt.Sprintf("prompt %q not found", filename),
			})
			return
		}
		h.logger.Error(r.Context(), "get prompt error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to read prompt",
		})
		return
	}
	writeJSON(w, http.StatusOK, prompt)
}

// HandlePutPrompt handles PUT /assistant/prompts/{filename}, creating or
// updating a starter prompt file.
func (h *AssistantHandler) HandlePutPrompt(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if err := ValidatePromptFilename(filename); err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: err.Error(),
		})
		return
	}

	var req putPromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "invalid JSON body",
		})
		return
	}
	if len(req.Content) > maxPromptContentLen {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: fmt.Sprintf("content must be at most %d characters", maxPromptContentLen),
		})
		return
	}

	if err := h.folderMgr.PutPrompt(filename, req.Content); err != nil {
		h.logger.Error(r.Context(), "put prompt error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to write prompt",
		})
		return
	}

	writeJSON(w, http.StatusOK, starterPrompt{
		Filename:    filename,
		DisplayName: promptDisplayName(filename),
		Content:     req.Content,
	})
}

// HandleDeletePrompt handles DELETE /assistant/prompts/{filename}.
func (h *AssistantHandler) HandleDeletePrompt(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if err := ValidatePromptFilename(filename); err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: err.Error(),
		})
		return
	}

	if err := h.folderMgr.DeletePrompt(filename); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, core.ErrorResponse{
				Error:   "NOT_FOUND",
				Message: fmt.Sprintf("prompt %q not found", filename),
			})
			return
		}
		h.logger.Error(r.Context(), "delete prompt error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to delete prompt",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"filename": filename,
		"status":   "deleted",
	})
}

// HandleFiles handles GET /assistant/files and GET /assistant/files/{path...},
// returning a directory listing or file content depending on the path's
// type.
func (h *AssistantHandler) HandleFiles(w http.ResponseWriter, r *http.Request) {
	rawPath := r.PathValue("path")
	relPath, err := h.folderMgr.validatePath(rawPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "path contains invalid characters or traversal",
		})
		return
	}

	absPath := filepath.Join(h.folderMgr.dataDir, relPath)
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, core.ErrorResponse{
				Error:   "NOT_FOUND",
				Message: "path not found",
			})
			return
		}
		h.logger.Error(r.Context(), "stat assistant path error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to access path",
		})
		return
	}

	if info.IsDir() {
		entries, err := h.folderMgr.ListDirectory(relPath)
		if err != nil {
			h.logger.Error(r.Context(), "list directory error: "+err.Error())
			writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
				Error:   "INTERNAL_ERROR",
				Message: "failed to list directory",
			})
			return
		}
		writeJSON(w, http.StatusOK, directoryResponse{Path: relPath, Entries: entries})
		return
	}

	fileResp, err := h.folderMgr.ReadFile(relPath)
	if err != nil {
		h.logger.Error(r.Context(), "read file error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to read file",
		})
		return
	}
	writeJSON(w, http.StatusOK, fileResp)
}

// HandleFileCommits handles GET /assistant/files/commits?limit=20&offset=0,
// returning a page of the assistant folder's git history.
func (h *AssistantHandler) HandleFileCommits(w http.ResponseWriter, r *http.Request) {
	limit := defaultCommitLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	commits, err := h.folderMgr.GetCommits(limit, offset)
	if err != nil {
		h.logger.Error(r.Context(), "get commits error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to read git history",
		})
		return
	}
	writeJSON(w, http.StatusOK, commits)
}

// HandleFileStatus handles GET /assistant/files/status, returning the
// working tree status of the assistant folder.
func (h *AssistantHandler) HandleFileStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.folderMgr.GetStatus()
	if err != nil {
		h.logger.Error(r.Context(), "get status error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to read git status",
		})
		return
	}
	writeJSON(w, http.StatusOK, status)
}
