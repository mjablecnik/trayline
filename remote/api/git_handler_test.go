package api

import (
	"bytes"
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

// newTestProject creates a git repo with n sequential commits named "name"
// under a fresh projectsDir, returning the projectsDir.
func newTestProject(t *testing.T, name string, n int) string {
	t.Helper()
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, name)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
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

	for i := 1; i <= n; i++ {
		if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte{byte('0' + i)}, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		run("add", "file.txt")
		run("commit", "-m", "commit")
	}
	return projectsDir
}

func newTestGitHandler(projectsDir string) *GitHandler {
	return NewGitHandler(projectsDir, git.NewRunner(), core.NewLogger("test-token"))
}

func TestHandleGetCommits_Success(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 3)
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/commits", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleGetCommits(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp CommitsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 3 || len(resp.Commits) != 3 {
		t.Fatalf("expected 3 commits and total 3, got %+v", resp)
	}
	if resp.HasMore {
		t.Error("expected has_more false when all commits fit on one page")
	}
	if resp.Commits[0].Hash == "" || resp.Commits[0].ShortHash == "" {
		t.Errorf("expected non-empty hash fields, got %+v", resp.Commits[0])
	}
}

func TestHandleGetCommits_Pagination(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 5)
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/commits?limit=2&offset=0", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleGetCommits(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp CommitsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Commits) != 2 || resp.Total != 5 {
		t.Fatalf("expected 2 commits and total 5, got %+v", resp)
	}
	if !resp.HasMore {
		t.Error("expected has_more true when more commits remain")
	}
}

func TestHandleGetCommits_ProjectNotFound(t *testing.T) {
	projectsDir := t.TempDir()
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/nope/commits", nil)
	req.SetPathValue("name", "nope")
	rec := httptest.NewRecorder()

	h.HandleGetCommits(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetCommits_InvalidRef(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 1)
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/commits?ref=no-such-ref", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleGetCommits(rec, req)

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

func TestHandleGetCommits_RejectsPathTraversalName(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 1)
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/../commits", nil)
	req.SetPathValue("name", "../myproject")
	rec := httptest.NewRecorder()

	h.HandleGetCommits(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for path-traversal name, got %d: %s", rec.Code, rec.Body.String())
	}
}

// headHash returns the current HEAD commit hash of the repo at repoDir.
func headHash(t *testing.T, repoDir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return string(out[:len(out)-1])
}

func TestHandleGetCommitDetail_Success(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 2)
	h := newTestGitHandler(projectsDir)
	hash := headHash(t, filepath.Join(projectsDir, "myproject"))

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/commits/"+hash, nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("hash", hash)
	rec := httptest.NewRecorder()

	h.HandleGetCommitDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp CommitDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Hash != hash {
		t.Errorf("expected hash %q, got %q", hash, resp.Hash)
	}
	if resp.FilesChanged != 1 || resp.Insertions != 1 {
		t.Errorf("expected 1 file changed and 1 insertion, got %+v", resp)
	}
	if resp.Diff == "" {
		t.Error("expected non-empty diff")
	}
}

func TestHandleGetCommitDetail_NotFoundHash(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 1)
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/commits/abcdef1", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("hash", "abcdef1")
	rec := httptest.NewRecorder()

	h.HandleGetCommitDetail(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetCommitDetail_InvalidHashFormat(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 1)
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/commits/not-hex!", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("hash", "not-hex!")
	rec := httptest.NewRecorder()

	h.HandleGetCommitDetail(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for malformed hash, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetCommitDetail_ProjectNotFound(t *testing.T) {
	projectsDir := t.TempDir()
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/nope/commits/abcdef1", nil)
	req.SetPathValue("name", "nope")
	req.SetPathValue("hash", "abcdef1")
	rec := httptest.NewRecorder()

	h.HandleGetCommitDetail(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetStatus_Clean(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 1)
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/status", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleGetStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Clean {
		t.Errorf("expected clean tree, got %+v", resp)
	}
}

func TestHandleGetStatus_WithChanges(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 1)
	repoDir := filepath.Join(projectsDir, "myproject")
	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("9"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/status", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleGetStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Clean {
		t.Fatal("expected dirty tree")
	}
	if resp.Summary.FilesChanged != 2 {
		t.Fatalf("expected 2 changed files, got %+v", resp.Summary)
	}
	var modified, untracked *FileStatus
	for i := range resp.Files {
		f := &resp.Files[i]
		switch f.Path {
		case "file.txt":
			modified = f
		case "new.txt":
			untracked = f
		}
	}
	if modified == nil || modified.Status != "modified" || modified.Diff == nil {
		t.Errorf("expected modified file.txt with a diff, got %+v", modified)
	}
	if untracked == nil || untracked.Status != "untracked" || untracked.Diff != nil {
		t.Errorf("expected untracked new.txt with nil diff, got %+v", untracked)
	}
}

func TestHandleGetStatus_ProjectNotFound(t *testing.T) {
	projectsDir := t.TempDir()
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/nope/status", nil)
	req.SetPathValue("name", "nope")
	rec := httptest.NewRecorder()

	h.HandleGetStatus(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func discardFileRequest(t *testing.T, name, path string) *http.Request {
	t.Helper()
	body, err := json.Marshal(DiscardFileRequest{Path: path})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/projects/"+name+"/changes/discard", bytes.NewReader(body))
	req.SetPathValue("name", name)
	return req
}

func TestHandleDiscardFile_Success(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 1)
	repoDir := filepath.Join(projectsDir, "myproject")
	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	h := newTestGitHandler(projectsDir)

	rec := httptest.NewRecorder()
	h.HandleDiscardFile(rec, discardFileRequest(t, "myproject", "file.txt"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	status, err := git.NewRunner().Status(repoDir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Clean {
		t.Errorf("expected clean working tree after discard, got %+v", status.Files)
	}
}

func TestHandleDiscardFile_IndexLocked(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 1)
	repoDir := filepath.Join(projectsDir, "myproject")
	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	lockPath := filepath.Join(repoDir, ".git", "index.lock")
	if err := os.WriteFile(lockPath, nil, 0o644); err != nil {
		t.Fatalf("create stale lock: %v", err)
	}
	t.Cleanup(func() { os.Remove(lockPath) })
	h := newTestGitHandler(projectsDir)

	rec := httptest.NewRecorder()
	h.HandleDiscardFile(rec, discardFileRequest(t, "myproject", "file.txt"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp core.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "GIT_INDEX_LOCKED" {
		t.Errorf("expected error code GIT_INDEX_LOCKED, got %q", resp.Error)
	}
}

func TestHandleDiscardFile_InvalidPath(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 1)
	h := newTestGitHandler(projectsDir)

	rec := httptest.NewRecorder()
	h.HandleDiscardFile(rec, discardFileRequest(t, "myproject", "../outside.txt"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path escaping the project, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDiscardFile_ProjectNotFound(t *testing.T) {
	projectsDir := t.TempDir()
	h := newTestGitHandler(projectsDir)

	rec := httptest.NewRecorder()
	h.HandleDiscardFile(rec, discardFileRequest(t, "nope", "file.txt"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDiscardAll_Success(t *testing.T) {
	projectsDir := newTestProject(t, "myproject", 1)
	repoDir := filepath.Join(projectsDir, "myproject")
	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodPost, "/projects/myproject/changes/discard-all", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()
	h.HandleDiscardAll(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	status, err := git.NewRunner().Status(repoDir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Clean {
		t.Errorf("expected clean working tree after discard-all, got %+v", status.Files)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("expected untracked new.txt removed, stat error: %v", err)
	}
}

func TestHandleDiscardAll_ProjectNotFound(t *testing.T) {
	projectsDir := t.TempDir()
	h := newTestGitHandler(projectsDir)

	req := httptest.NewRequest(http.MethodPost, "/projects/nope/changes/discard-all", nil)
	req.SetPathValue("name", "nope")
	rec := httptest.NewRecorder()
	h.HandleDiscardAll(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
