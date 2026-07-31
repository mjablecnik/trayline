package api

import (
	"context"
	"io"
	"net/http"
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

// noopRunner is a ContainerRunner double; unused by the routes under test.
type noopRunner struct{}

func (noopRunner) RunOneShot(context.Context, string, string, string, string, time.Time) (*docker.ContainerResult, error) {
	return &docker.ContainerResult{}, nil
}

const testAuthToken = "test-router-token"

// newTestRouter builds a fully wired router (all handlers, real middleware
// chain) backed by a temp projects directory containing one git repo, so
// project routes resolve real data end-to-end.
func newTestRouter(t *testing.T, dashboardOrigin string) (http.Handler, string) {
	t.Helper()
	projectsDir := newTestProjectWithTree(t, "myproject")

	logger := core.NewLogger(testAuthToken)
	taskH := NewTaskHandler(store.NewTaskStore(), noopRunner{}, logger, nil, t.TempDir(), MaxUploadFileSize, MaxUploadFileCount, 32000)
	cfg := &core.Config{APIToken: testAuthToken, SessionTimeout: time.Minute}
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)
	sessionH := NewSessionHandler(store.NewSessionStore(), cm, logger, cfg, nil)
	gitH := NewGitHandler(projectsDir, git.NewRunner(), logger)
	envH := NewEnvHandler(projectsDir, logger)
	projectH := NewProjectHandler(projectsDir, git.NewRunner(), logger)
	rl := NewRateLimiter(1000)

	router := NewRouter(&HealthHandler{}, taskH, sessionH, gitH, envH, projectH, testAuthToken, rl, logger, dashboardOrigin)
	return router, projectsDir
}

func TestRouter_ProjectRoutes_RequireAuth(t *testing.T) {
	router, _ := newTestRouter(t, "")
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
	router, _ := newTestRouter(t, "")
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
	router, _ := newTestRouter(t, "https://dashboard.example.com")
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
