package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const requestTimeout = 10 * time.Second

// Task is the JSON representation of a task as returned by the delete,
// update, retry, and stop endpoints.
type Task struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	ExitCode  *int      `json:"exit_code,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateTaskResponse is the JSON body returned by POST /tasks.
type CreateTaskResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

// TaskListItem is one entry of the GET /tasks response array.
type TaskListItem struct {
	Position  int       `json:"position"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// IDNameResult is the JSON body returned by POST /tasks/skip.
type IDNameResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// QueueActionResult is the JSON body returned by POST /queue/resume.
type QueueActionResult struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// TaskBrief identifies the currently running task in a queue status response.
type TaskBrief struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// FailedInfo identifies the currently failed task in a queue status response.
type FailedInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
}

// QueueStatusResult is the JSON body returned by GET /queue/status.
type QueueStatusResult struct {
	State        string      `json:"state"`
	PendingCount int         `json:"pendingCount"`
	CurrentTask  *TaskBrief  `json:"currentTask,omitempty"`
	FailedTask   *FailedInfo `json:"failedTask,omitempty"`
}

// apiErrorBody is the JSON error schema returned by the server for every
// error case (VALIDATION_ERROR, NOT_FOUND, CONFLICT).
type apiErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// APIError is returned when the server responds with an HTTP 4xx/5xx status.
type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return e.Message
}

// ProjectListItem is one entry of the GET /projects response array.
type ProjectListItem struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	PendingCount int    `json:"pendingCount"`
}

// Client is an HTTP client for the Taskline server API, scoped to a single
// project (FR-5.3): every task/queue/log method is sent to
// /projects/{project}/...
type Client struct {
	baseURL string
	project string
	token   string
	http    *http.Client
	// streamHTTP has no timeout, since GetLogs streaming reads run for as
	// long as the caller keeps the connection open.
	streamHTTP *http.Client
}

// NewClient returns a Client targeting baseURL, scoped to project, with a
// 10-second timeout on non-streaming requests. token is sent as a Bearer
// Authorization header on every request; pass "" if the server has no
// APP_TOKEN configured (e.g. default loopback-only deployments).
func NewClient(baseURL, project, token string) *Client {
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		project:    project,
		token:      token,
		http:       &http.Client{Timeout: requestTimeout},
		streamHTTP: &http.Client{},
	}
}

// projectPath returns "/projects/{project}" + suffix, with the project name
// escaped for use in a URL path.
func (c *Client) projectPath(suffix string) string {
	return "/projects/" + url.PathEscape(c.project) + suffix
}

// setAuthHeader attaches the Bearer token to req, if one is configured.
func (c *Client) setAuthHeader(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// CreateTask sends POST /projects/{project}/tasks.
func (c *Client) CreateTask(command, name, cwd string, position *int) (*CreateTaskResponse, error) {
	body := struct {
		Command  string `json:"command"`
		Name     string `json:"name,omitempty"`
		Cwd      string `json:"cwd,omitempty"`
		Position *int   `json:"position,omitempty"`
	}{Command: command, Name: name, Cwd: cwd, Position: position}

	var resp CreateTaskResponse
	if err := c.do(http.MethodPost, c.projectPath("/tasks"), body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListTasks sends GET /projects/{project}/tasks.
func (c *Client) ListTasks() ([]TaskListItem, error) {
	var resp []TaskListItem
	if err := c.do(http.MethodGet, c.projectPath("/tasks"), nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// DeleteTask sends DELETE /projects/{project}/tasks/{identifier}.
func (c *Client) DeleteTask(identifier string) (*Task, error) {
	var resp Task
	if err := c.do(http.MethodDelete, c.projectPath("/tasks/"+url.PathEscape(identifier)), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateTask sends PATCH /projects/{project}/tasks/{identifier}.
func (c *Client) UpdateTask(identifier, command, name string) (*Task, error) {
	body := struct {
		Command string `json:"command,omitempty"`
		Name    string `json:"name,omitempty"`
	}{Command: command, Name: name}

	var resp Task
	if err := c.do(http.MethodPatch, c.projectPath("/tasks/"+url.PathEscape(identifier)), body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Retry sends POST /projects/{project}/tasks/retry.
func (c *Client) Retry() (*Task, error) {
	var resp Task
	if err := c.do(http.MethodPost, c.projectPath("/tasks/retry"), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Skip sends POST /projects/{project}/tasks/skip.
func (c *Client) Skip() (*IDNameResult, error) {
	var resp IDNameResult
	if err := c.do(http.MethodPost, c.projectPath("/tasks/skip"), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Stop sends POST /projects/{project}/tasks/stop.
func (c *Client) Stop() (*Task, error) {
	var resp Task
	if err := c.do(http.MethodPost, c.projectPath("/tasks/stop"), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Resume sends POST /projects/{project}/queue/resume.
func (c *Client) Resume() (*QueueActionResult, error) {
	var resp QueueActionResult
	if err := c.do(http.MethodPost, c.projectPath("/queue/resume"), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Status sends GET /projects/{project}/queue/status.
func (c *Client) Status() (*QueueStatusResult, error) {
	var resp QueueStatusResult
	if err := c.do(http.MethodGet, c.projectPath("/queue/status"), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListProjects sends GET /projects (not project-scoped — lists every
// project known to the server).
func (c *Client) ListProjects() ([]ProjectListItem, error) {
	var resp []ProjectListItem
	if err := c.do(http.MethodGet, "/projects", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// GetLogs sends GET /projects/{project}/logs?tail=N and returns the raw log
// text. tail <= 0 omits the query parameter, returning the full log.
func (c *Client) GetLogs(tail int) (string, error) {
	path := c.projectPath("/logs")
	if tail > 0 {
		path += "?tail=" + strconv.Itoa(tail)
	}
	resp, err := c.rawRequest(http.MethodGet, c.http, path)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response from %s: %w", c.baseURL, err)
	}
	return string(data), nil
}

// StreamLogs sends GET /projects/{project}/logs/stream and returns the raw,
// still-open response body for the caller to read as a stream of
// "data: <line>\n\n" Server-Sent Events. The caller must Close it.
func (c *Client) StreamLogs() (io.ReadCloser, error) {
	resp, err := c.rawRequest(http.MethodGet, c.streamHTTP, c.projectPath("/logs/stream"))
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// rawRequest sends a bodyless request to path via httpClient and returns the
// raw response on success (status < 400) for a caller that needs to read a
// non-JSON body itself. The caller must close the returned response's Body.
// On a non-2xx response, the body is fully read, the connection closed, and
// an *APIError is returned instead.
func (c *Client) rawRequest(method string, httpClient *http.Client, path string) (*http.Response, error) {
	req, err := http.NewRequest(method, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	c.setAuthHeader(req)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", c.baseURL, err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		var apiErr apiErrorBody
		if err := json.Unmarshal(data, &apiErr); err != nil || apiErr.Message == "" {
			return nil, &APIError{Code: "UNKNOWN", Message: strings.TrimSpace(string(data))}
		}
		return nil, &APIError{Code: apiErr.Error, Message: apiErr.Message}
	}
	return resp, nil
}

// do sends an HTTP request to path, JSON-encoding body (if non-nil) as the
// request payload and JSON-decoding the response into out (if non-nil and
// the response is a success). Non-2xx responses are parsed into an
// *APIError using the server's error schema (Requirement 13's "structured
// error parsing from response body").
func (c *Client) do(method, path string, body interface{}, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuthHeader(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response from %s: %w", c.baseURL, err)
	}

	if resp.StatusCode >= 400 {
		var apiErr apiErrorBody
		if err := json.Unmarshal(data, &apiErr); err != nil || apiErr.Message == "" {
			return &APIError{Code: "UNKNOWN", Message: strings.TrimSpace(string(data))}
		}
		return &APIError{Code: apiErr.Error, Message: apiErr.Message}
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response from %s: %w", c.baseURL, err)
		}
	}
	return nil
}
