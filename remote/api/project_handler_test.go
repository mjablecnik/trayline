package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"remote/core"
	"remote/git"
)

func newTestProjectHandler(projectsDir string) *ProjectHandler {
	return NewProjectHandler(projectsDir, git.NewRunner(), core.NewLogger("test-token"))
}

// newTestProjectWithTree creates a git repo with a small file/directory tree
// and one commit, returning the projectsDir.
func newTestProjectWithTree(t *testing.T, name string) string {
	t.Helper()
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, name)
	if err := os.MkdirAll(filepath.Join(repoDir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "src", "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "initial commit")

	return projectsDir
}

func TestHandleListProjects_Success(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 2)
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	rec := httptest.NewRecorder()

	h.HandleListProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var projects []ProjectSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &projects); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].Name != "myproject" || projects[0].Branch != "main" {
		t.Errorf("unexpected project summary: %+v", projects[0])
	}
	if projects[0].LastCommit == nil || projects[0].LastCommit.Hash == "" {
		t.Errorf("expected non-nil last commit, got %+v", projects[0].LastCommit)
	}
}

func TestHandleListProjects_EmptyReturnsEmptyArray(t *testing.T) {
	projectsDir := t.TempDir()
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	rec := httptest.NewRecorder()

	h.HandleListProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("expected empty JSON array, got %q", body)
	}
}

func TestHandleListProjects_SkipsNonGitDirs(t *testing.T) {
	projectsDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectsDir, "not-a-repo"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	rec := httptest.NewRecorder()

	h.HandleListProjects(rec, req)

	var projects []ProjectSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &projects); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected non-git dir to be skipped, got %+v", projects)
	}
}

func TestHandleGetProject_Success(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 1)
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleGetProject(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var detail ProjectDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if detail.Name != "myproject" || detail.Branch != "main" {
		t.Errorf("unexpected project detail: %+v", detail)
	}
	if len(detail.Branches) != 1 || detail.Branches[0] != "main" {
		t.Errorf("expected branches [main], got %+v", detail.Branches)
	}
	if detail.LastCommit == nil {
		t.Error("expected non-nil last commit")
	}
}

func TestHandleGetProject_NotFound(t *testing.T) {
	projectsDir := t.TempDir()
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/nope", nil)
	req.SetPathValue("name", "nope")
	rec := httptest.NewRecorder()

	h.HandleGetProject(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetTree_Root(t *testing.T) {
	projectsDir := newTestProjectWithTree(t, "myproject")
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/tree/main/", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("ref", "main")
	req.SetPathValue("path", "")
	rec := httptest.NewRecorder()

	h.HandleGetTree(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp TreeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Type != "directory" {
		t.Errorf("expected type directory, got %q", resp.Type)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %+v", resp.Entries)
	}
	// Directories sorted first, then files, both alphabetically.
	if resp.Entries[0].Name != "src" || resp.Entries[0].Type != "directory" {
		t.Errorf("expected src directory first, got %+v", resp.Entries[0])
	}
	if resp.Entries[1].Name != "README.md" || resp.Entries[1].Type != "file" {
		t.Errorf("expected README.md file second, got %+v", resp.Entries[1])
	}
}

func TestHandleGetTree_Subdirectory(t *testing.T) {
	projectsDir := newTestProjectWithTree(t, "myproject")
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/tree/main/src", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("ref", "main")
	req.SetPathValue("path", "src")
	rec := httptest.NewRecorder()

	h.HandleGetTree(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp TreeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Entries) != 1 || resp.Entries[0].Name != "main.go" {
		t.Fatalf("expected [main.go], got %+v", resp.Entries)
	}
}

func TestHandleGetTree_NotFound(t *testing.T) {
	projectsDir := newTestProjectWithTree(t, "myproject")
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/tree/main/nope", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("ref", "main")
	req.SetPathValue("path", "nope")
	rec := httptest.NewRecorder()

	h.HandleGetTree(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetTree_InvalidPath(t *testing.T) {
	projectsDir := newTestProjectWithTree(t, "myproject")
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/tree/main/../../etc", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("ref", "main")
	req.SetPathValue("path", "../../etc")
	rec := httptest.NewRecorder()

	h.HandleGetTree(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetTree_ProjectNotFound(t *testing.T) {
	projectsDir := t.TempDir()
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/nope/tree/main/", nil)
	req.SetPathValue("name", "nope")
	req.SetPathValue("ref", "main")
	req.SetPathValue("path", "")
	rec := httptest.NewRecorder()

	h.HandleGetTree(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetBlob_Success(t *testing.T) {
	projectsDir := newTestProjectWithTree(t, "myproject")
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/blob/main/README.md", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("ref", "main")
	req.SetPathValue("path", "README.md")
	rec := httptest.NewRecorder()

	h.HandleGetBlob(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp BlobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Type != "file" || resp.Content == nil || *resp.Content != "hello" {
		t.Errorf("unexpected blob response: %+v", resp)
	}
	if resp.Binary || resp.Truncated {
		t.Errorf("expected non-binary, non-truncated response, got %+v", resp)
	}
	if resp.Language != "markdown" {
		t.Errorf("expected markdown language, got %q", resp.Language)
	}
	if resp.Size != 5 {
		t.Errorf("expected size 5, got %d", resp.Size)
	}
}

func TestHandleGetBlob_Binary(t *testing.T) {
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, "myproject")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoDir, "data.bin"), []byte("bin\x00ary"), 0o644); err != nil {
		t.Fatalf("write data.bin: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "add binary file")

	h := newTestProjectHandler(projectsDir)
	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/blob/main/data.bin", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("ref", "main")
	req.SetPathValue("path", "data.bin")
	rec := httptest.NewRecorder()

	h.HandleGetBlob(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp BlobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Binary {
		t.Errorf("expected binary:true, got %+v", resp)
	}
	if resp.Content != nil {
		t.Errorf("expected nil content for binary file, got %+v", *resp.Content)
	}
}

func TestHandleGetBlob_NotFound(t *testing.T) {
	projectsDir := newTestProjectWithTree(t, "myproject")
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/blob/main/nope.txt", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("ref", "main")
	req.SetPathValue("path", "nope.txt")
	rec := httptest.NewRecorder()

	h.HandleGetBlob(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetBlob_InvalidPath(t *testing.T) {
	projectsDir := newTestProjectWithTree(t, "myproject")
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/blob/main/../../etc/passwd", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("ref", "main")
	req.SetPathValue("path", "../../etc/passwd")
	rec := httptest.NewRecorder()

	h.HandleGetBlob(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetBlob_ProjectNotFound(t *testing.T) {
	projectsDir := t.TempDir()
	h := newTestProjectHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/nope/blob/main/README.md", nil)
	req.SetPathValue("name", "nope")
	req.SetPathValue("ref", "main")
	req.SetPathValue("path", "README.md")
	rec := httptest.NewRecorder()

	h.HandleGetBlob(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetBlob_Truncated(t *testing.T) {
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, "myproject")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	big := make([]byte, maxInlineBlobSize+1)
	for i := range big {
		big[i] = 'a'
	}
	if err := os.WriteFile(filepath.Join(repoDir, "big.txt"), big, 0o644); err != nil {
		t.Fatalf("write big.txt: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "add big file")

	h := newTestProjectHandler(projectsDir)
	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/blob/main/big.txt", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("ref", "main")
	req.SetPathValue("path", "big.txt")
	rec := httptest.NewRecorder()

	h.HandleGetBlob(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp BlobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Truncated {
		t.Errorf("expected truncated:true, got %+v", resp)
	}
	if resp.Content != nil {
		t.Errorf("expected nil content for truncated file, got %+v", *resp.Content)
	}
}

func TestIsBinary(t *testing.T) {
	if isBinary([]byte("hello world")) {
		t.Error("expected plain text to not be detected as binary")
	}
	if !isBinary([]byte("hello\x00world")) {
		t.Error("expected data with a null byte to be detected as binary")
	}
}

func TestLanguageForPath(t *testing.T) {
	cases := map[string]string{
		"main.go":     "go",
		"src/app.tsx": "typescript",
		"README.md":   "markdown",
		"unknown.xyz": "",
	}
	for path, want := range cases {
		if got := languageForPath(path); got != want {
			t.Errorf("languageForPath(%q) = %q, want %q", path, got, want)
		}
	}
}
