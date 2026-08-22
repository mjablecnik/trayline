package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"

	"remote/core"
	"remote/docker"
	"remote/git"
	"remote/store"
)

// noopContainerClient implements docker.ContainerClient with zero-value
// responses. The router test never actually runs a task/session, so these
// methods only need to satisfy the interface for construction.
type noopContainerClient struct{}

func (noopContainerClient) ContainerCreate(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, string) (container.CreateResponse, error) {
	return container.CreateResponse{}, nil
}
func (noopContainerClient) ContainerStart(context.Context, string, dockertypes.ContainerStartOptions) error {
	return nil
}
func (noopContainerClient) ContainerLogs(context.Context, string, dockertypes.ContainerLogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(nil), nil
}
func (noopContainerClient) ContainerAttach(context.Context, string, dockertypes.ContainerAttachOptions) (dockertypes.HijackedResponse, error) {
	return dockertypes.HijackedResponse{}, nil
}
func (noopContainerClient) ContainerStop(context.Context, string, container.StopOptions) error {
	return nil
}
func (noopContainerClient) ContainerRemove(context.Context, string, dockertypes.ContainerRemoveOptions) error {
	return nil
}
func (noopContainerClient) ContainerInspect(context.Context, string) (dockertypes.ContainerJSON, error) {
	return dockertypes.ContainerJSON{}, nil
}
func (noopContainerClient) ContainerWait(context.Context, string, container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	return make(chan container.WaitResponse), make(chan error)
}
func (noopContainerClient) ContainerKill(context.Context, string, string) error { return nil }
func (noopContainerClient) CopyToContainer(context.Context, string, string, io.Reader, dockertypes.CopyToContainerOptions) error {
	return nil
}

// noopRunner is a ContainerRunner double; unused by the routes under test.
type noopRunner struct{}

func (noopRunner) RunOneShot(context.Context, string, string, string, string, time.Time, func(string)) (*docker.ContainerResult, error) {
	return &docker.ContainerResult{}, nil
}

func (noopRunner) RunOneShotStreaming(context.Context, string, string, string, string, time.Time) (*docker.OneShotStream, error) {
	return nil, fmt.Errorf("noopRunner: RunOneShotStreaming not implemented")
}

func (noopRunner) StopAndRemoveContainer(context.Context, string) error { return nil }

const testAuthToken = "test-router-token"

// newTestRouter builds a fully wired router (all handlers, real middleware
// chain) backed by a temp projects directory containing one git repo, so
// project routes resolve real data end-to-end.
func newTestRouter(t *testing.T, dashboardOrigin string, cookieSecure bool) (http.Handler, string) {
	t.Helper()
	projectsDir := newTestProjectWithTree(t, "myproject")

	logger := core.NewLogger(testAuthToken)
	taskH := NewTaskHandler(store.NewTaskStore(), noopRunner{}, logger, nil, t.TempDir(), MaxUploadFileSize, MaxUploadFileCount, 32000)
	assistantDataDir := t.TempDir()
	cfg := &core.Config{APIToken: testAuthToken, SessionTimeout: time.Minute, ProjectsDir: projectsDir, AssistantDataDir: assistantDataDir}
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)
	sessionStore := store.NewSessionStore()
	sessionH := NewSessionHandler(sessionStore, cm, logger, cfg, nil)
	gitH := NewGitHandler(projectsDir, git.NewRunner(), logger)
	envH := NewEnvHandler(projectsDir, logger)
	projectH := NewProjectHandler(projectsDir, git.NewRunner(), logger)
	projectAgentH := NewProjectAgentHandler(sessionStore, cm, logger, cfg, nil)
	rl := NewRateLimiter(1000)

	pipelineH := NewPipelineHandler(cfg, logger)
	specH := NewSpecHandler(cfg, logger)
	workflowStore := store.NewWorkflowStore()
	queues := NewWorkflowQueueManager(workflowStore, cm, cfg, logger, nil)
	workflowH := NewWorkflowHandler(workflowStore, cfg, logger, nil, queues)

	assistantFolderMgr := NewAssistantFolderManager(assistantDataDir, logger)
	if err := assistantFolderMgr.Init(); err != nil {
		t.Fatalf("assistant folder init: %v", err)
	}
	assistantH := NewAssistantHandler(sessionStore, cm, logger, cfg, nil, assistantFolderMgr)

	openaiRegistry := NewModelRegistry("")
	openaiH := NewOpenAIHandler(openaiRegistry, noopRunner{}, logger, time.Minute)

	csrf := NewCSRFStore()
	authH := NewAuthHandler(testAuthToken, cookieSecure, csrf)
	router := NewRouter(&HealthHandler{}, taskH, sessionH, gitH, envH, projectH, projectAgentH, pipelineH, specH, workflowH, assistantH, openaiH, authH, testAuthToken, csrf, rl, logger, dashboardOrigin)
	return router, projectsDir
}

func TestRouter_ProjectRoutes_RequireAuth(t *testing.T) {
	router, _ := newTestRouter(t, "", true)
	srv := httptest.NewServer(router)
	defer srv.Close()

	paths := []string{
		"/projects",
		"/projects/myproject",
		"/projects/myproject/tree/main/",
		"/projects/myproject/blob/main/README.md",
	}
	for _, p := range paths {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without token: expected 401, got %d", p, resp.StatusCode)
		}
	}
}

func TestRouter_ProjectRoutes_Authenticated(t *testing.T) {
	router, _ := newTestRouter(t, "", true)
	srv := httptest.NewServer(router)
	defer srv.Close()

	cases := []struct {
		path string
		want int
	}{
		{"/projects", http.StatusOK},
		{"/projects/myproject", http.StatusOK},
		{"/projects/myproject/tree/main/", http.StatusOK},
		{"/projects/myproject/blob/main/README.md", http.StatusOK},
		{"/projects/myproject/blob/main/nope.txt", http.StatusNotFound},
		{"/projects/doesnotexist", http.StatusNotFound},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+c.path, nil)
		req.Header.Set("Authorization", "Bearer "+testAuthToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", c.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("GET %s: expected %d, got %d", c.path, c.want, resp.StatusCode)
		}
	}
}

func TestRouter_ProjectRoutes_CORSHeaders(t *testing.T) {
	router, _ := newTestRouter(t, "https://dashboard.example.com", true)
	srv := httptest.NewServer(router)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/projects", nil)
	req.Header.Set("Authorization", "Bearer "+testAuthToken)
	req.Header.Set("Origin", "https://dashboard.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /projects: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://dashboard.example.com" {
		t.Errorf("expected Access-Control-Allow-Origin header, got %q", got)
	}

	// Preflight OPTIONS on a project route: 204, no auth required.
	preflight, _ := http.NewRequest(http.MethodOptions, srv.URL+"/projects/myproject/blob/main/README.md", nil)
	preflight.Header.Set("Origin", "https://dashboard.example.com")
	preflight.Header.Set("Access-Control-Request-Method", "GET")
	presp, err := http.DefaultClient.Do(preflight)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	defer presp.Body.Close()
	if presp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204 for preflight, got %d", presp.StatusCode)
	}
	if got := presp.Header.Get("Access-Control-Allow-Origin"); got != "https://dashboard.example.com" {
		t.Errorf("expected CORS header on preflight, got %q", got)
	}
}

// End-to-end regression test for the full SameSite=None + CSRF migration,
// through the REAL middleware chain (recovery → CORS → rate limit → auth →
// requestID → mux) — not just AuthMiddleware in isolation. Simulates the
// dashboard's actual flow: POST /auth/login to get a session cookie + CSRF
// token, then a mutating request using both.
func TestRouter_CookieSessionWithCSRF_EndToEnd(t *testing.T) {
	// cookieSecure=false: httptest.NewServer is plain HTTP, and a Secure
	// cookie would never be attached back to it by a real cookie jar — this
	// test exercises the CSRF/cookie *mechanism*, not the Secure flag itself
	// (already covered by TestHandleLogin_CookieSecureFalseInDevelopment).
	router, _ := newTestRouter(t, "https://dashboard.example.com", false)
	srv := httptest.NewServer(router)
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	loginBody, _ := json.Marshal(map[string]string{"token": testAuthToken})
	loginReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/auth/login", bytes.NewReader(loginBody))
	loginResp, err := client.Do(loginReq)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected login 200, got %d", loginResp.StatusCode)
	}
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&login); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if login.CSRFToken == "" {
		t.Fatal("expected a non-empty csrfToken from login")
	}

	// The cookie jar now holds the session cookie automatically (this
	// simulates the browser) — a mutating request WITHOUT the CSRF header
	// must be rejected even though the session cookie is valid and present.
	pinNoCSRF, _ := http.NewRequest(http.MethodPut, srv.URL+"/projects/myproject/pin", nil)
	respNoCSRF, err := client.Do(pinNoCSRF)
	if err != nil {
		t.Fatalf("PUT (no CSRF): %v", err)
	}
	defer respNoCSRF.Body.Close()
	if respNoCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF token, got %d", respNoCSRF.StatusCode)
	}

	// With the CSRF token attached, the same mutating request succeeds.
	pinWithCSRF, _ := http.NewRequest(http.MethodPut, srv.URL+"/projects/myproject/pin", nil)
	pinWithCSRF.Header.Set("X-CSRF-Token", login.CSRFToken)
	respWithCSRF, err := client.Do(pinWithCSRF)
	if err != nil {
		t.Fatalf("PUT (with CSRF): %v", err)
	}
	defer respWithCSRF.Body.Close()
	if respWithCSRF.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 with a valid CSRF token, got %d: %s", respWithCSRF.StatusCode, mustReadBody(t, respWithCSRF))
	}

	// GET (safe method) via the same cookie session works with no CSRF token
	// at all — CSRF only guards mutating methods.
	getReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/projects/myproject", nil)
	getResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for a safe-method cookie request with no CSRF token, got %d", getResp.StatusCode)
	}
}

func mustReadBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}
