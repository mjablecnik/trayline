package api

import (
	"encoding/json"
	"net/http"

	"remote/core"
)

// AuthHandler handles the login/logout/session-check endpoints that let the
// dashboard exchange the shared API token for an HttpOnly session cookie,
// instead of holding the token itself in JS-readable storage (see
// dashboard/src/lib/auth.ts and code-security.md's Token Storage rule).
type AuthHandler struct {
	token        string
	cookieSecure bool
	csrf         *csrfStore
}

// NewAuthHandler creates an AuthHandler. csrf must be the same *csrfStore
// instance passed to AuthMiddleware (see router.go) — tokens issued here
// must validate there.
func NewAuthHandler(token string, cookieSecure bool, csrf *csrfStore) *AuthHandler {
	return &AuthHandler{token: token, cookieSecure: cookieSecure, csrf: csrf}
}

type loginRequest struct {
	Token string `json:"token"`
}

// sessionResponse is returned by both HandleLogin and HandleSession. csrfToken
// must be read by the frontend and attached as the X-CSRF-Token header on
// every subsequent state-changing request (see AuthMiddleware) — it is
// deliberately NOT delivered as a cookie, since a cookie set by the API's
// origin could never be read via JS from the dashboard's different origin
// anyway (cross-origin document.cookie access is blocked by the browser).
type sessionResponse struct {
	OK        bool   `json:"ok"`
	CSRFToken string `json:"csrfToken"`
}

// HandleLogin handles POST /auth/login. It validates the submitted token
// against the server's configured API token and, on success, sets it as an
// HttpOnly session cookie and returns a fresh CSRF token. This endpoint is
// intentionally exempt from AuthMiddleware (see router.go) — the caller has
// no session yet.
func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: "token is required",
		})
		return
	}

	if !ValidateWSToken(req.Token, h.token) {
		writeJSON(w, http.StatusUnauthorized, core.ErrorResponse{
			Error:   "UNAUTHORIZED",
			Message: "invalid token",
		})
		return
	}

	csrfToken, err := h.csrf.issue()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to issue CSRF token",
		})
		return
	}

	setSessionCookie(w, req.Token, h.cookieSecure)
	writeJSON(w, http.StatusOK, sessionResponse{OK: true, CSRFToken: csrfToken})
}

// HandleLogout handles POST /auth/logout, clearing the session cookie.
// Exempt from AuthMiddleware like HandleLogin — clearing a cookie that may
// already be invalid or absent should never itself require a valid session.
func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, h.cookieSecure)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleSession handles GET /auth/session. It runs behind the normal
// AuthMiddleware chain, so simply reaching this handler at all means the
// caller's header or cookie was valid — used by the dashboard on page load
// to discover whether it already has a valid session (it cannot read the
// HttpOnly cookie itself to check locally) and to obtain a fresh CSRF token,
// since the one from login isn't persisted anywhere across a page reload.
func (h *AuthHandler) HandleSession(w http.ResponseWriter, r *http.Request) {
	csrfToken, err := h.csrf.issue()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to issue CSRF token",
		})
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{OK: true, CSRFToken: csrfToken})
}
