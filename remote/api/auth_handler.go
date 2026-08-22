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
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(token string, cookieSecure bool) *AuthHandler {
	return &AuthHandler{token: token, cookieSecure: cookieSecure}
}

type loginRequest struct {
	Token string `json:"token"`
}

// HandleLogin handles POST /auth/login. It validates the submitted token
// against the server's configured API token and, on success, sets it as an
// HttpOnly session cookie. This endpoint is intentionally exempt from
// AuthMiddleware (see router.go) — the caller has no session yet.
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

	setSessionCookie(w, req.Token, h.cookieSecure)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
// caller's header or cookie was valid — used by the dashboard on load to
// discover whether it already has a valid session, since it cannot read the
// HttpOnly cookie itself to check locally.
func (h *AuthHandler) HandleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
