package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
)

// APIError represents a structured error from the server or the HTTP transport.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return e.Message
}

// APIClient wraps net/http.Client with auth header injection and optional verbose logging.
type APIClient struct {
	serverURL  string
	token      string
	verboseOut io.Writer // nil disables verbose output; os.Stderr when enabled
	httpClient *http.Client
}

// NewAPIClient creates an APIClient. Verbose output goes to os.Stderr when cfg.Verbose is true.
func NewAPIClient(cfg *Config) *APIClient {
	var verboseOut io.Writer
	if cfg.Verbose {
		verboseOut = os.Stderr
	}
	return &APIClient{
		serverURL:  cfg.ServerURL,
		token:      cfg.Token,
		verboseOut: verboseOut,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// logVerbose writes a verbose line when verbose output is enabled.
func (c *APIClient) logVerbose(format string, args ...any) {
	if c.verboseOut != nil {
		fmt.Fprintf(c.verboseOut, format, args...)
	}
}

// do executes an HTTP request with auth header, logs if verbose, and returns the response body.
func (c *APIClient) do(method, path string, body io.Reader) (*http.Response, []byte, error) {
	reqURL := c.serverURL + path
	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, nil, &APIError{Message: fmt.Sprintf("Error: Failed to build request: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return nil, nil, &APIError{Message: fmt.Sprintf("Error: Server unreachable at %s. Check that the server is running.", c.serverURL)}
	}

	c.logVerbose("%s %s -> %d (%dms)\n", method, reqURL, resp.StatusCode, elapsed.Milliseconds())

	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: Failed to read response body: %v", err)}
	}

	return resp, data, nil
}

// parseError reads a structured ErrorResponse from body bytes, falling back to a generic message.
func parseError(statusCode int, body []byte) *APIError {
	var errResp ErrorResponse
	if json.Unmarshal(body, &errResp) == nil {
		if errResp.Message != "" {
			return &APIError{StatusCode: statusCode, Message: errResp.Message}
		}
		if errResp.Error != "" {
			return &APIError{StatusCode: statusCode, Message: errResp.Error}
		}
	}
	return &APIError{StatusCode: statusCode, Message: fmt.Sprintf("Error: Server returned HTTP %d.", statusCode)}
}

// Health sends GET /health with a 5s timeout.
func (c *APIClient) Health() error {
	hc := &http.Client{Timeout: 5 * time.Second}
	reqURL := c.serverURL + "/health"
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return &APIError{Message: fmt.Sprintf("Error: Failed to build request: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	start := time.Now()
	resp, err := hc.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return &APIError{Message: fmt.Sprintf("Error: Server unreachable at %s. Check that the server is running.", c.serverURL)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	c.logVerbose("GET %s -> %d (%dms)\n", reqURL, resp.StatusCode, elapsed.Milliseconds())

	if resp.StatusCode != http.StatusOK {
		return parseError(resp.StatusCode, body)
	}
	return nil
}

// PostRun sends POST /run. Returns RunResponse on HTTP 200 or RunAcceptedResponse on HTTP 202.
func (c *APIClient) PostRun(req RunRequest) (*RunResponse, *RunAcceptedResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, nil, &APIError{Message: fmt.Sprintf("Error: Failed to encode request: %v", err)}
	}

	resp, body, err := c.do(http.MethodPost, "/run", bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var run RunResponse
		if err := json.Unmarshal(body, &run); err != nil {
			return nil, nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: Failed to parse response: %v", err)}
		}
		return &run, nil, nil
	case http.StatusAccepted:
		var accepted RunAcceptedResponse
		if err := json.Unmarshal(body, &accepted); err != nil {
			return nil, nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: Failed to parse response: %v", err)}
		}
		return nil, &accepted, nil
	default:
		return nil, nil, parseError(resp.StatusCode, body)
	}
}

// PostRunMultipart sends POST /run as multipart/form-data when files are present.
// Returns RunResponse on HTTP 200 or RunAcceptedResponse on HTTP 202.
func (c *APIClient) PostRunMultipart(req RunRequest, files []string) (*RunResponse, *RunAcceptedResponse, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	writeField := func(name, value string) error {
		if value == "" {
			return nil
		}
		return mw.WriteField(name, value)
	}
	for _, pair := range [][2]string{
		{"prompt", req.Prompt},
		{"agent", req.Agent},
		{"model", req.Model},
		{"system", req.System},
		{"output_format", req.OutputFormat},
	} {
		if err := writeField(pair[0], pair[1]); err != nil {
			return nil, nil, &APIError{Message: fmt.Sprintf("Error: Failed to build request: %v", err)}
		}
	}

	for _, filePath := range files {
		f, err := os.Open(filePath)
		if err != nil {
			return nil, nil, &APIError{Message: fmt.Sprintf("Error: Cannot open file %q: %v", filePath, err)}
		}
		part, err := mw.CreateFormFile("files", filepath.Base(filePath))
		if err != nil {
			f.Close()
			return nil, nil, &APIError{Message: fmt.Sprintf("Error: Failed to build request: %v", err)}
		}
		if _, err := io.Copy(part, f); err != nil {
			f.Close()
			return nil, nil, &APIError{Message: fmt.Sprintf("Error: Failed to read file %q: %v", filePath, err)}
		}
		f.Close()
	}

	if err := mw.Close(); err != nil {
		return nil, nil, &APIError{Message: fmt.Sprintf("Error: Failed to build request: %v", err)}
	}

	reqURL := c.serverURL + "/run"
	httpReq, err := http.NewRequest(http.MethodPost, reqURL, &body)
	if err != nil {
		return nil, nil, &APIError{Message: fmt.Sprintf("Error: Failed to build request: %v", err)}
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())

	uploadClient := &http.Client{Timeout: 5 * time.Minute}
	start := time.Now()
	resp, err := uploadClient.Do(httpReq)
	elapsed := time.Since(start)
	if err != nil {
		return nil, nil, &APIError{Message: fmt.Sprintf("Error: Server unreachable at %s. Check that the server is running.", c.serverURL)}
	}

	c.logVerbose("POST %s -> %d (%dms)\n", reqURL, resp.StatusCode, elapsed.Milliseconds())

	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: Failed to read response body: %v", err)}
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var run RunResponse
		if err := json.Unmarshal(data, &run); err != nil {
			return nil, nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: Failed to parse response: %v", err)}
		}
		return &run, nil, nil
	case http.StatusAccepted:
		var accepted RunAcceptedResponse
		if err := json.Unmarshal(data, &accepted); err != nil {
			return nil, nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: Failed to parse response: %v", err)}
		}
		return nil, &accepted, nil
	default:
		return nil, nil, parseError(resp.StatusCode, data)
	}
}

// GetRun sends GET /run/{id}.
func (c *APIClient) GetRun(id string) (*RunResponse, error) {
	resp, body, err := c.do(http.MethodGet, "/run/"+id, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp.StatusCode, body)
	}
	var run RunResponse
	if err := json.Unmarshal(body, &run); err != nil {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: Failed to parse response: %v", err)}
	}
	return &run, nil
}

// GetRuns sends GET /runs.
func (c *APIClient) GetRuns() ([]TaskSummary, error) {
	resp, body, err := c.do(http.MethodGet, "/runs", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp.StatusCode, body)
	}
	var tasks []TaskSummary
	if err := json.Unmarshal(body, &tasks); err != nil {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: Failed to parse response: %v", err)}
	}
	return tasks, nil
}

// CancelRun sends POST /run/{id}/cancel.
func (c *APIClient) CancelRun(id string) (*RunResponse, error) {
	resp, body, err := c.do(http.MethodPost, "/run/"+id+"/cancel", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp.StatusCode, body)
	}
	var run RunResponse
	if err := json.Unmarshal(body, &run); err != nil {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: Failed to parse response: %v", err)}
	}
	return &run, nil
}

// GetSessions sends GET /sessions.
func (c *APIClient) GetSessions() ([]SessionSummary, error) {
	resp, body, err := c.do(http.MethodGet, "/sessions", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp.StatusCode, body)
	}
	var sessions []SessionSummary
	if err := json.Unmarshal(body, &sessions); err != nil {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: Failed to parse response: %v", err)}
	}
	return sessions, nil
}

// TerminateSession sends POST /sessions/{id}/terminate.
func (c *APIClient) TerminateSession(id string) (*SessionSummary, error) {
	resp, body, err := c.do(http.MethodPost, "/sessions/"+id+"/terminate", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp.StatusCode, body)
	}
	var session SessionSummary
	if err := json.Unmarshal(body, &session); err != nil {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: Failed to parse response: %v", err)}
	}
	return &session, nil
}

// DialWebSocket opens a WebSocket connection to the given path with a 10s dial timeout.
// The token is sent as a Bearer token in the Upgrade request headers.
func (c *APIClient) DialWebSocket(wsPath string, queryParams url.Values) (*websocket.Conn, *http.Response, error) {
	// Convert http/https base URL to ws/wss.
	base := c.serverURL
	var wsURL string
	switch {
	case len(base) >= 8 && base[:8] == "https://":
		wsURL = "wss://" + base[8:]
	default:
		// Assumes http://
		wsURL = "ws://" + base[7:]
	}
	wsURL += wsPath
	if len(queryParams) > 0 {
		wsURL += "?" + queryParams.Encode()
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.token)

	start := time.Now()
	conn, resp, err := dialer.Dial(wsURL, header)
	elapsed := time.Since(start)
	if err != nil {
		if resp != nil {
			return nil, resp, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Error: WebSocket connection failed with HTTP %d.", resp.StatusCode)}
		}
		return nil, nil, &APIError{Message: fmt.Sprintf("Error: WebSocket connection to %s failed. Check that the server is running.", wsURL)}
	}

	c.logVerbose("WS CONNECT %s (%dms)\n", wsURL, elapsed.Milliseconds())

	return conn, resp, nil
}
