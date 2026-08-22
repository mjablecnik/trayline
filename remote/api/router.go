package api

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/google/uuid"

	"remote/core"
)

// NewRouter builds and returns the HTTP ServeMux with all routes and middleware applied.
// Middleware chain: recovery → CORS → rate limiter → auth → requestID → mux.
func NewRouter(
	health *HealthHandler,
	taskH *TaskHandler,
	sessionH *SessionHandler,
	gitH *GitHandler,
	envH *EnvHandler,
	projectH *ProjectHandler,
	projectAgentH *ProjectAgentHandler,
	pipelineH *PipelineHandler,
	specH *SpecHandler,
	workflowH *WorkflowHandler,
	assistantH *AssistantHandler,
	openaiH *OpenAIHandler,
	authH *AuthHandler,
	authToken string,
	csrf *csrfStore,
	rl *RateLimiter,
	logger *core.Logger,
	dashboardOrigin string,
) http.Handler {
	mux := http.NewServeMux()

	// Health endpoint — no auth, no rate limiting.
	mux.Handle("GET /health", health)

	// Auth endpoints. Login/logout are exempt from AuthMiddleware (see
	// auth.go) — a caller with no session yet must be able to reach login.
	mux.HandleFunc("POST /auth/login", authH.HandleLogin)
	mux.HandleFunc("POST /auth/logout", authH.HandleLogout)
	mux.HandleFunc("GET /auth/session", authH.HandleSession)

	// Task endpoints.
	mux.HandleFunc("POST /run", taskH.HandlePostRun)
	mux.HandleFunc("GET /run/{id}", taskH.HandleGetRun)
	mux.HandleFunc("GET /runs", taskH.HandleGetRuns)
	mux.HandleFunc("POST /run/{id}/cancel", taskH.HandleCancelRun)

	// Session endpoints.
	mux.HandleFunc("GET /chat", sessionH.HandleChat)
	mux.HandleFunc("GET /chat/{id}", sessionH.HandleChatReconnect)
	mux.HandleFunc("GET /sessions", sessionH.HandleGetSessions)
	mux.HandleFunc("POST /sessions/{id}/terminate", sessionH.HandleTerminateSession)

	// Project endpoints (dashboard).
	mux.HandleFunc("GET /projects", projectH.HandleListProjects)
	mux.HandleFunc("GET /projects/{name}", projectH.HandleGetProject)
	mux.HandleFunc("PUT /projects/{name}/pin", projectH.HandlePinProject)
	mux.HandleFunc("DELETE /projects/{name}/pin", projectH.HandleUnpinProject)
	mux.HandleFunc("GET /projects/{name}/tree/{ref}/{path...}", projectH.HandleGetTree)
	mux.HandleFunc("GET /projects/{name}/blob/{ref}/{path...}", projectH.HandleGetBlob)

	// Git endpoints.
	mux.HandleFunc("GET /projects/{name}/commits", gitH.HandleGetCommits)
	mux.HandleFunc("GET /projects/{name}/commits/{hash}", gitH.HandleGetCommitDetail)
	mux.HandleFunc("GET /projects/{name}/status", gitH.HandleGetStatus)
	mux.HandleFunc("POST /projects/{name}/changes/discard", gitH.HandleDiscardFile)
	mux.HandleFunc("POST /projects/{name}/changes/discard-all", gitH.HandleDiscardAll)

	// Env endpoints.
	mux.HandleFunc("GET /projects/{name}/env", envH.HandleGetEnv)
	mux.HandleFunc("PUT /projects/{name}/env", envH.HandlePutEnv)

	// Project agent endpoints.
	mux.HandleFunc("GET /projects/{name}/chat", projectAgentH.HandleProjectChat)
	mux.HandleFunc("GET /projects/{name}/chat/{id}", projectAgentH.HandleProjectChatReconnect)
	mux.HandleFunc("GET /projects/{name}/sessions", projectAgentH.HandleProjectSessions)
	mux.HandleFunc("POST /projects/{name}/sessions/{id}/terminate", projectAgentH.HandleTerminateProjectSession)

	// Pipeline discovery endpoints.
	mux.HandleFunc("GET /projects/{name}/pipelines", pipelineH.HandleListPipelines)
	mux.HandleFunc("GET /projects/{name}/pipelines/{type}/{pipeline}", pipelineH.HandleGetPipelineDetail)

	// Spec discovery endpoint.
	mux.HandleFunc("GET /projects/{name}/specs", specH.HandleListSpecs)

	// Global workflow overview (cross-project, active only).
	mux.HandleFunc("GET /workflows", workflowH.HandleListAll)

	// Workflow endpoints.
	mux.HandleFunc("POST /projects/{name}/workflows", workflowH.HandleSchedule)
	mux.HandleFunc("GET /projects/{name}/workflows", workflowH.HandleList)
	mux.HandleFunc("GET /projects/{name}/workflows/{id}", workflowH.HandleDetail)
	mux.HandleFunc("PUT /projects/{name}/workflows/{id}", workflowH.HandleEdit)
	mux.HandleFunc("DELETE /projects/{name}/workflows/{id}", workflowH.HandleCancel)
	mux.HandleFunc("POST /projects/{name}/workflows/{id}/retry", workflowH.HandleRetry)
	mux.HandleFunc("GET /projects/{name}/workflows/{id}/logs", workflowH.HandleLogs)

	// Assistant endpoints.
	mux.HandleFunc("GET /assistant/chat", assistantH.HandleAssistantChat)
	mux.HandleFunc("GET /assistant/chat/{id}", assistantH.HandleAssistantChatReconnect)
	mux.HandleFunc("GET /assistant/sessions", assistantH.HandleAssistantSessions)
	mux.HandleFunc("POST /assistant/sessions/{id}/terminate", assistantH.HandleTerminateAssistantSession)
	mux.HandleFunc("GET /assistant/prompts", assistantH.HandleListPrompts)
	mux.HandleFunc("GET /assistant/prompts/{filename}", assistantH.HandleGetPrompt)
	mux.HandleFunc("PUT /assistant/prompts/{filename}", assistantH.HandlePutPrompt)
	mux.HandleFunc("DELETE /assistant/prompts/{filename}", assistantH.HandleDeletePrompt)
	// Note: /assistant/files/commits and /assistant/files/status must be
	// registered before the /assistant/files/{path...} wildcard so the
	// specific paths match first.
	mux.HandleFunc("GET /assistant/files/commits", assistantH.HandleFileCommits)
	mux.HandleFunc("GET /assistant/files/status", assistantH.HandleFileStatus)
	mux.HandleFunc("GET /assistant/files", assistantH.HandleFiles)
	mux.HandleFunc("GET /assistant/files/{path...}", assistantH.HandleFiles)

	// OpenAI-compatible endpoints.
	mux.HandleFunc("POST /v1/chat/completions", openaiH.HandleChatCompletions)
	mux.HandleFunc("GET /v1/models", openaiH.HandleListModels)
	mux.HandleFunc("GET /v1/models/{model_id}", openaiH.HandleGetModel)

	// Apply middleware: recovery → CORS → rate limiter → auth → requestID → mux.
	return recoveryMiddleware(logger, CORSMiddleware(dashboardOrigin)(rl.Middleware(AuthMiddleware(authToken, csrf, requestIDMiddleware(mux)))))
}

// requestIDMiddleware attaches a unique request ID to every request context.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := core.WithRequestID(r.Context(), uuid.NewString())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// recoveryMiddleware catches panics in handlers and returns HTTP 500.
func recoveryMiddleware(logger *core.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				logger.Error(r.Context(), fmt.Sprintf("panic recovered: %v\n%s", rec, stack))
				writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
					Error:   "INTERNAL_ERROR",
					Message: "an unexpected error occurred",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
