package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"remote/core"

	"pgregory.net/rapid"
)

// Feature: 010-dashboard-workflow-runner, Property 1: Pipeline name transformation
func TestPipelineNameFromFilename(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base := rapid.StringMatching(`[a-zA-Z0-9-]+`).Draw(t, "base")
		filename := base + ".yaml"

		name, displayName := pipelineNameFromFilename(filename)

		if name != base {
			t.Fatalf("pipelineNameFromFilename(%q) name = %q, want %q", filename, name, base)
		}
		wantDisplay := strings.ReplaceAll(base, "-", " ")
		if displayName != wantDisplay {
			t.Fatalf("pipelineNameFromFilename(%q) displayName = %q, want %q", filename, displayName, wantDisplay)
		}
	})
}

// newTestPipelineHandler builds a PipelineHandler backed by fresh temp
// directories for projects and pipelines.
func newTestPipelineHandler(t *testing.T) (h *PipelineHandler, projectsDir, pipelinesDir string) {
	t.Helper()
	projectsDir = t.TempDir()
	pipelinesDir = t.TempDir()
	cfg := &core.Config{ProjectsDir: projectsDir, PipelinesDir: pipelinesDir}
	return NewPipelineHandler(cfg, core.NewLogger("test-token")), projectsDir, pipelinesDir
}

func mustMkdirProject(t *testing.T, projectsDir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(projectsDir, name), 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
}

func writePipelineFile(t *testing.T, pipelinesDir, pipelineType, name, content string) {
	t.Helper()
	dir := filepath.Join(pipelinesDir, pipelineType)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir pipeline dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write pipeline file: %v", err)
	}
}

func TestHandleListPipelines_Success(t *testing.T) {
	h, projectsDir, pipelinesDir := newTestPipelineHandler(t)
	mustMkdirProject(t, projectsDir, "myproject")
	writePipelineFile(t, pipelinesDir, "tasks", "check-build", "variables:\n  path: \"this\"\n")
	writePipelineFile(t, pipelinesDir, "processes", "4-create-code", "variables:\n  number: \"1\"\n")
	writePipelineFile(t, pipelinesDir, "workflows", "fix-bugs", "variables:\n  brief: \"BRIEF.md\"\n")

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/pipelines", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleListPipelines(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp PipelinesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].Name != "check-build" || resp.Tasks[0].Type != "tasks" || resp.Tasks[0].DisplayName != "check build" {
		t.Errorf("Tasks = %+v", resp.Tasks)
	}
	if len(resp.Processes) != 1 || resp.Processes[0].Name != "4-create-code" || resp.Processes[0].DisplayName != "4 create code" {
		t.Errorf("Processes = %+v", resp.Processes)
	}
	if len(resp.Workflows) != 1 || resp.Workflows[0].Name != "fix-bugs" {
		t.Errorf("Workflows = %+v", resp.Workflows)
	}
}

func TestHandleListPipelines_MissingSubdirYieldsEmptySlice(t *testing.T) {
	h, projectsDir, pipelinesDir := newTestPipelineHandler(t)
	mustMkdirProject(t, projectsDir, "myproject")
	writePipelineFile(t, pipelinesDir, "tasks", "check-build", "variables:\n  path: \"this\"\n")
	// No "processes" or "workflows" subdirectories created at all.

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/pipelines", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleListPipelines(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp PipelinesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Processes == nil || len(resp.Processes) != 0 {
		t.Errorf("expected empty (non-nil) Processes, got %+v", resp.Processes)
	}
	if resp.Workflows == nil || len(resp.Workflows) != 0 {
		t.Errorf("expected empty (non-nil) Workflows, got %+v", resp.Workflows)
	}
}

func TestHandleListPipelines_ProjectNotFound(t *testing.T) {
	h, _, _ := newTestPipelineHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/projects/nope/pipelines", nil)
	req.SetPathValue("name", "nope")
	rec := httptest.NewRecorder()

	h.HandleListPipelines(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp core.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND, got %q", resp.Error)
	}
}

func TestHandleGetPipelineDetail_Success(t *testing.T) {
	h, projectsDir, pipelinesDir := newTestPipelineHandler(t)
	mustMkdirProject(t, projectsDir, "myproject")
	writePipelineFile(t, pipelinesDir, "workflows", "maintenance",
		"variables:\n  path: \"this\"\n  skip-seo-audit: true\n")

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/pipelines/workflows/maintenance", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("type", "workflows")
	req.SetPathValue("pipeline", "maintenance")
	rec := httptest.NewRecorder()

	h.HandleGetPipelineDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp PipelineDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Name != "maintenance" || resp.Type != "workflows" {
		t.Errorf("Name/Type = %q/%q", resp.Name, resp.Type)
	}
	// An unquoted YAML boolean default must be coerced to its string form,
	// same as an explicitly-quoted "true" would be.
	if resp.Variables["skip-seo-audit"] != "true" {
		t.Errorf("expected skip-seo-audit=true, got variables=%+v", resp.Variables)
	}
	if resp.Variables["path"] != "this" {
		t.Errorf("expected path=this, got variables=%+v", resp.Variables)
	}
}

func TestHandleGetPipelineDetail_NoVariablesKeyReturnsEmptyObject(t *testing.T) {
	h, projectsDir, pipelinesDir := newTestPipelineHandler(t)
	mustMkdirProject(t, projectsDir, "myproject")
	writePipelineFile(t, pipelinesDir, "tasks", "sync-push", "steps:\n  - name: push\n")

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/pipelines/tasks/sync-push", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("type", "tasks")
	req.SetPathValue("pipeline", "sync-push")
	rec := httptest.NewRecorder()

	h.HandleGetPipelineDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, `"variables":{}`) {
		t.Errorf("expected empty variables object in body, got %s", got)
	}
}

func TestHandleGetPipelineDetail_InvalidType(t *testing.T) {
	h, projectsDir, _ := newTestPipelineHandler(t)
	mustMkdirProject(t, projectsDir, "myproject")

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/pipelines/bogus/thing", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("type", "bogus")
	req.SetPathValue("pipeline", "thing")
	rec := httptest.NewRecorder()

	h.HandleGetPipelineDetail(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp core.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %q", resp.Error)
	}
}

func TestHandleGetPipelineDetail_PipelineNotFound(t *testing.T) {
	h, projectsDir, _ := newTestPipelineHandler(t)
	mustMkdirProject(t, projectsDir, "myproject")

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/pipelines/tasks/nope", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("type", "tasks")
	req.SetPathValue("pipeline", "nope")
	rec := httptest.NewRecorder()

	h.HandleGetPipelineDetail(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetPipelineDetail_ProjectNotFound(t *testing.T) {
	h, _, pipelinesDir := newTestPipelineHandler(t)
	writePipelineFile(t, pipelinesDir, "tasks", "check-build", "variables:\n  path: \"this\"\n")

	req := httptest.NewRequest(http.MethodGet, "/projects/nope/pipelines/tasks/check-build", nil)
	req.SetPathValue("name", "nope")
	req.SetPathValue("type", "tasks")
	req.SetPathValue("pipeline", "check-build")
	rec := httptest.NewRecorder()

	h.HandleGetPipelineDetail(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetPipelineDetail_PathTraversalRejected(t *testing.T) {
	h, projectsDir, pipelinesDir := newTestPipelineHandler(t)
	mustMkdirProject(t, projectsDir, "myproject")
	// A secret file one directory above the "tasks" pipeline subdir.
	if err := os.WriteFile(filepath.Join(pipelinesDir, "secret.yaml"), []byte("variables:\n  x: \"leak\"\n"), 0o644); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/pipelines/tasks/..%2Fsecret", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("type", "tasks")
	req.SetPathValue("pipeline", "../secret")
	rec := httptest.NewRecorder()

	h.HandleGetPipelineDetail(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
