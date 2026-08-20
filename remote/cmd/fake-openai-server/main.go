// Command fake-openai-server runs the production Trayline router with the agent
// execution layer replaced by a scripted fake.
//
// Everything above the container boundary is the real thing: the same
// NewRouter, the same middleware chain (recovery → CORS → rate limit → auth →
// requestID), the same OpenAI handler, registry, composer and SSE writer. Only
// the agent itself is simulated. That makes it possible to run a full
// OpenAI-SDK conformance suite against the real server code with no Docker
// daemon, no agent credentials, no API credits and fully deterministic output.
//
// Usage:
//
//	go run ./cmd/fake-openai-server -addr 127.0.0.1:8099
//
// Environment:
//
//	API_TOKEN            bearer token clients must present (default "test-token")
//	OPENAI_MODELS        model registry config (default: the built-in defaults)
//	MAX_CONCURRENT_TASKS task slot pool size (default 2)
//	TASK_TIMEOUT         per-request agent timeout (default 10s)
//	RATE_LIMIT           requests per minute per IP (default 10000)
//	CHUNK_DELAY          pause between streamed chunks (default 0)
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"remote/api"
	"remote/core"
	"remote/docker"
	"remote/git"
	"remote/internal/faketest"
	"remote/store"
)

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8099", "address to listen on")
	flag.Parse()

	token := envString("API_TOKEN", "test-token")

	dataDir, err := os.MkdirTemp("", "fake-openai-server-")
	if err != nil {
		log.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dataDir)

	cfg := &core.Config{
		APIToken:           token,
		MaxConcurrentTasks: envInt("MAX_CONCURRENT_TASKS", 2),
		MaxChatSessions:    2,
		SessionTimeout:     time.Minute,
		TaskTimeout:        envDuration("TASK_TIMEOUT", 10*time.Second),
		RateLimit:          envInt("RATE_LIMIT", 10000),
		ProjectsDir:        dataDir,
		AssistantDataDir:   dataDir,
	}

	logger := core.NewLogger(token)
	runner := faketest.NewRunner(cfg.MaxConcurrentTasks, envDuration("CHUNK_DELAY", 0))

	// A ContainerManager is still needed by the handlers this server does not
	// exercise (sessions, workflows, project agents); it is backed by a no-op
	// docker client. The OpenAI handler receives the scripted runner instead.
	cm := docker.NewContainerManager(faketest.NoopContainerClient{}, cfg, logger)
	sessionStore := store.NewSessionStore()
	workflowStore := store.NewWorkflowStore()

	taskH := api.NewTaskHandler(store.NewTaskStore(), runner, logger, nil, dataDir,
		api.MaxUploadFileSize, api.MaxUploadFileCount, 32000)
	sessionH := api.NewSessionHandler(sessionStore, cm, logger, cfg, nil)
	gitH := api.NewGitHandler(cfg.ProjectsDir, git.NewRunner(), logger)
	envH := api.NewEnvHandler(cfg.ProjectsDir, logger)
	projectH := api.NewProjectHandler(cfg.ProjectsDir, git.NewRunner(), logger)
	projectAgentH := api.NewProjectAgentHandler(sessionStore, cm, logger, cfg, nil)
	pipelineH := api.NewPipelineHandler(cfg, logger)
	specH := api.NewSpecHandler(cfg, logger)
	workflowQueues := api.NewWorkflowQueueManager(workflowStore, cm, cfg, logger, nil)
	workflowH := api.NewWorkflowHandler(workflowStore, cfg, logger, nil, workflowQueues)

	assistantFolderMgr := api.NewAssistantFolderManager(cfg.AssistantDataDir, logger)
	if err := assistantFolderMgr.Init(); err != nil {
		log.Fatalf("assistant folder init: %v", err)
	}
	assistantH := api.NewAssistantHandler(sessionStore, cm, logger, cfg, nil, assistantFolderMgr)

	openaiH := api.NewOpenAIHandler(
		api.NewModelRegistry(os.Getenv("OPENAI_MODELS")), runner, logger, cfg.TaskTimeout)

	router := api.NewRouter(&api.HealthHandler{}, taskH, sessionH, gitH, envH, projectH,
		projectAgentH, pipelineH, specH, workflowH, assistantH, openaiH,
		cfg.APIToken, api.NewRateLimiter(cfg.RateLimit), logger, "")

	fmt.Printf("fake-openai-server listening on http://%s (token %q, %d task slots)\n",
		*addr, token, cfg.MaxConcurrentTasks)

	srv := &http.Server{
		Addr:    *addr,
		Handler: router,
		// Streaming responses must not be cut off by a write deadline.
		WriteTimeout: 0,
		ReadTimeout:  30 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
