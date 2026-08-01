package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// Client is an HTTP client for the Taskline server API.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a Client targeting baseURL with a 10-second timeout.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// CreateTask sends POST /tasks.
func (c *Client) CreateTask(command, name, cwd string, position *int) (*CreateTaskResponse, error) {
	body := struct {
		Command  string `json:"command"`
		Name     string `json:"name,omitempty"`
		Cwd      string `json:"cwd,omitempty"`
		Position *int   `json:"position,omitempty"`
	}{Command: command, Name: name, Cwd: cwd, Position: position}

	var resp CreateTaskResponse
	if err := c.do(http.MethodPost, "/tasks", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListTasks sends GET /tasks.
func (c *Client) ListTasks() ([]TaskListItem, error) {
	var resp []TaskListItem
	if err := c.do(http.MethodGet, "/tasks", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// DeleteTask sends DELETE /tasks/{identifier}.
func (c *Client) DeleteTask(identifier string) (*Task, error) {
	var resp Task
	if err := c.do(http.MethodDelete, "/tasks/"+url.PathEscape(identifier), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateTask sends PATCH /tasks/{identifier}.
func (c *Client) UpdateTask(identifier, command, name string) (*Task, error) {
	body := struct {
		Command string `json:"command,omitempty"`
		Name    string `json:"name,omitempty"`
	}{Command: command, Name: name}

	var resp Task
	if err := c.do(http.MethodPatch, "/tasks/"+url.PathEscape(identifier), body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Retry sends POST /tasks/retry.
func (c *Client) Retry() (*Task, error) {
	var resp Task
	if err := c.do(http.MethodPost, "/tasks/retry", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Skip sends POST /tasks/skip.
func (c *Client) Skip() (*IDNameResult, error) {
	var resp IDNameResult
	if err := c.do(http.MethodPost, "/tasks/skip", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Stop sends POST /tasks/stop.
func (c *Client) Stop() (*Task, error) {
	var resp Task
	if err := c.do(http.MethodPost, "/tasks/stop", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Resume sends POST /queue/resume.
func (c *Client) Resume() (*QueueActionResult, error) {
	var resp QueueActionResult
	if err := c.do(http.MethodPost, "/queue/resume", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Status sends GET /queue/status.
func (c *Client) Status() (*QueueStatusResult, error) {
	var resp QueueStatusResult
	if err := c.do(http.MethodGet, "/queue/status", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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
