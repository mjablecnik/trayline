package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"remote/core"
)

// newTestSpecHandler builds a SpecHandler backed by a fresh temp projects
// directory.
func newTestSpecHandler(t *testing.T) (h *SpecHandler, projectsDir string) {
	t.Helper()
	projectsDir = t.TempDir()
	cfg := &core.Config{ProjectsDir: projectsDir}
	return NewSpecHandler(cfg, core.NewLogger("test-token")), projectsDir
}

// writeSpec creates a spec directory with a tasks.md file at the given
// content, and sets its modification time explicitly so tests can control
// sort order.
func writeSpec(t *testing.T, projectsDir, project, specName, tasksContent string, modTime time.Time) {
	t.Helper()
	dir := filepath.Join(projectsDir, project, ".kiro", "specs", specName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir spec dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte(tasksContent), 0o644); err != nil {
		t.Fatalf("write tasks.md: %v", err)
	}
	if err := os.Chtimes(dir, modTime, modTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

func TestHandleListSpecs_FiltersAndSortsByCreatedAtDesc(t *testing.T) {
	h, projectsDir := newTestSpecHandler(t)
	mustMkdirProject(t, projectsDir, "myproject")

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	writeSpec(t, projectsDir, "myproject", "001-older", "- [x] done\n- [ ] todo\n", older)
	writeSpec(t, projectsDir, "myproject", "002-newer", "- [ ] todo\n", newer)
	// All tasks checked — must be excluded from the result.
	writeSpec(t, projectsDir, "myproject", "003-complete", "- [x] done\n", newer)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/specs", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleListSpecs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []SpecSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 specs, got %+v", resp)
	}
	if resp[0].Name != "002-newer" || resp[1].Name != "001-older" {
		t.Errorf("expected [002-newer, 001-older] order, got [%s, %s]", resp[0].Name, resp[1].Name)
	}
}

func TestHandleListSpecs_NoSpecsDirYieldsEmptyArray(t *testing.T) {
	h, projectsDir := newTestSpecHandler(t)
	mustMkdirProject(t, projectsDir, "myproject")

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/specs", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleListSpecs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "[]\n" {
		t.Errorf("expected empty JSON array, got %q", got)
	}
}

func TestHandleListSpecs_AllSpecsCompleteYieldsEmptyArray(t *testing.T) {
	h, projectsDir := newTestSpecHandler(t)
	mustMkdirProject(t, projectsDir, "myproject")
	writeSpec(t, projectsDir, "myproject", "001-done", "- [x] done\n", time.Now())

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/specs", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleListSpecs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []SpecSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty result, got %+v", resp)
	}
}

func TestHandleListSpecs_ProjectNotFound(t *testing.T) {
	h, _ := newTestSpecHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/projects/nope/specs", nil)
	req.SetPathValue("name", "nope")
	rec := httptest.NewRecorder()

	h.HandleListSpecs(rec, req)

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
