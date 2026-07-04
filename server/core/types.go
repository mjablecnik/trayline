package core

// ErrorResponse is the standard error body for all API error responses.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
