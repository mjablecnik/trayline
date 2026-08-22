package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"remote/core"
)

func newTestEnvHandler(projectsDir string) *EnvHandler {
	return NewEnvHandler(projectsDir, core.NewLogger("test-token"))
}

func newTestEnvProject(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, name)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for relPath, content := range files {
		full := filepath.Join(repoDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", relPath, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
	}
	return projectsDir
}

func TestHandleGetEnv_Success(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{
		".env":         "# comment\nFOO=bar\nBAZ=qux\n",
		".env.example": "FOO=\n",
	})
	h := newTestEnvHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/env", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleGetEnv(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp EnvListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Files) != 2 {
		t.Fatalf("expected 2 files, got %+v", resp.Files)
	}
	// Discover returns filenames sorted, so ".env" precedes ".env.example".
	if resp.Files[0].Path != ".env" || len(resp.Files[0].Variables) != 2 {
		t.Errorf("Files[0] = %+v", resp.Files[0])
	}
	if resp.Files[1].Path != ".env.example" || len(resp.Files[1].Variables) != 1 {
		t.Errorf("Files[1] = %+v", resp.Files[1])
	}
}

func TestHandleGetEnv_EmptyWhenNoEnvFiles(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{"README.md": ""})
	h := newTestEnvHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/env", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()

	h.HandleGetEnv(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp EnvListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Files == nil || len(resp.Files) != 0 {
		t.Errorf("expected empty non-nil files, got %+v", resp.Files)
	}
	if !bytesContainEmptyArray(rec.Body.Bytes()) {
		t.Errorf("expected JSON files array to be [] not null, got %s", rec.Body.String())
	}
}

func bytesContainEmptyArray(b []byte) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return false
	}
	return string(raw["files"]) == "[]"
}

func TestHandleGetEnv_ProjectNotFound(t *testing.T) {
	projectsDir := t.TempDir()
	h := newTestEnvHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/nope/env", nil)
	req.SetPathValue("name", "nope")
	rec := httptest.NewRecorder()

	h.HandleGetEnv(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetEnv_RejectsPathTraversalName(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{".env": "FOO=bar\n"})
	h := newTestEnvHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/../env", nil)
	req.SetPathValue("name", "../myproject")
	rec := httptest.NewRecorder()

	h.HandleGetEnv(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for path-traversal name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetEnv_RejectsBareDotDotName(t *testing.T) {
	// Regression test: "..", unlike "../myproject", contains no "/" so it
	// alone satisfies projectNameRe (^[A-Za-z0-9._-]+$) and used to reach
	// filepath.Join(projectsDir, "..") unchecked, disclosing .env files
	// from the parent of projectsDir.
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{".env": "FOO=bar\n"})
	secretPath := filepath.Join(filepath.Dir(projectsDir), ".env.secret")
	if err := os.WriteFile(secretPath, []byte("SECRET=leaked\n"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	defer os.Remove(secretPath)

	h := newTestEnvHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/../env", nil)
	req.SetPathValue("name", "..")
	rec := httptest.NewRecorder()

	h.HandleGetEnv(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for bare \"..\" name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetEnv_RejectsBareDotName(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{".env": "FOO=bar\n"})
	h := newTestEnvHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/./env", nil)
	req.SetPathValue("name", ".")
	rec := httptest.NewRecorder()

	h.HandleGetEnv(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for bare \".\" name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePutEnv_RejectsBareDotDotName(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{".env": "FOO=bar\n"})
	h := newTestEnvHandler(projectsDir)

	req := putEnvRequest(t, "..", `{"path":".env.pwned","variables":[{"key":"X","value":"y"}]}`)
	rec := httptest.NewRecorder()

	h.HandlePutEnv(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for bare \"..\" name, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(projectsDir), ".env.pwned")); !os.IsNotExist(err) {
		t.Fatalf("expected no file written outside projectsDir, stat err = %v", err)
	}
}

func putEnvRequest(t *testing.T, name, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/projects/"+name+"/env", bytes.NewBufferString(body))
	req.SetPathValue("name", name)
	return req
}

func TestHandlePutEnv_Success(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{
		".env": "# keep me\nFOO=old\n",
	})
	h := newTestEnvHandler(projectsDir)

	body := `{"path":".env","variables":[{"key":"FOO","value":"new"},{"key":"BAR","value":"baz qux"}]}`
	rec := httptest.NewRecorder()
	h.HandlePutEnv(rec, putEnvRequest(t, "myproject", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp EnvFileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Variables) != 2 || resp.Variables[0].Value != "new" {
		t.Errorf("Variables = %+v", resp.Variables)
	}

	written, err := os.ReadFile(filepath.Join(projectsDir, "myproject", ".env"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	content := string(written)
	if !bytes.Contains(written, []byte("# keep me\n")) {
		t.Errorf("expected preserved comment in written file, got %q", content)
	}
	if !bytes.Contains(written, []byte("FOO=new\n")) {
		t.Errorf("expected FOO=new in written file, got %q", content)
	}
}

func TestHandlePutEnv_CreatesNewFile(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{})
	h := newTestEnvHandler(projectsDir)

	body := `{"path":".env.prod","variables":[{"key":"FOO","value":"bar"}]}`
	rec := httptest.NewRecorder()
	h.HandlePutEnv(rec, putEnvRequest(t, "myproject", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(projectsDir, "myproject", ".env.prod")); err != nil {
		t.Errorf("expected new file to be created: %v", err)
	}
}

func TestHandlePutEnv_ProjectNotFound(t *testing.T) {
	projectsDir := t.TempDir()
	h := newTestEnvHandler(projectsDir)

	rec := httptest.NewRecorder()
	h.HandlePutEnv(rec, putEnvRequest(t, "nope", `{"path":".env","variables":[]}`))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePutEnv_InvalidFilename(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{})
	h := newTestEnvHandler(projectsDir)

	rec := httptest.NewRecorder()
	h.HandlePutEnv(rec, putEnvRequest(t, "myproject", `{"path":"config.txt","variables":[]}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePutEnv_RejectsNonEnvBasenameEvenNested(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{})
	h := newTestEnvHandler(projectsDir)

	rec := httptest.NewRecorder()
	h.HandlePutEnv(rec, putEnvRequest(t, "myproject", `{"path":"backend/secrets.txt","variables":[]}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePutEnv_RejectsPathTraversal(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{})
	h := newTestEnvHandler(projectsDir)

	rec := httptest.NewRecorder()
	h.HandlePutEnv(rec, putEnvRequest(t, "myproject", `{"path":"../.env","variables":[]}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Regression: the whole point of this feature is finding/editing .env files
// that aren't at the project root.
func TestHandleGetEnv_FindsNestedFiles(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{
		".env":             "ROOT=1\n",
		"backend/.env":     "BACKEND=1\n",
		"backend/.env.dev": "BACKEND_DEV=1\n",
	})
	h := newTestEnvHandler(projectsDir)

	req := httptest.NewRequest(http.MethodGet, "/projects/myproject/env", nil)
	req.SetPathValue("name", "myproject")
	rec := httptest.NewRecorder()
	h.HandleGetEnv(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp EnvListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Files) != 3 {
		t.Fatalf("expected 3 files, got %+v", resp.Files)
	}
	paths := make(map[string]bool, len(resp.Files))
	for _, f := range resp.Files {
		paths[f.Path] = true
	}
	for _, want := range []string{".env", "backend/.env", "backend/.env.dev"} {
		if !paths[want] {
			t.Errorf("expected %q among discovered files, got %+v", want, resp.Files)
		}
	}
}

func TestHandlePutEnv_WritesNestedFile(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{
		"backend/.env": "OLD=1\n",
	})
	h := newTestEnvHandler(projectsDir)

	body := `{"path":"backend/.env","variables":[{"key":"NEW","value":"2"}]}`
	rec := httptest.NewRecorder()
	h.HandlePutEnv(rec, putEnvRequest(t, "myproject", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp EnvFileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Path != "backend/.env" {
		t.Errorf("expected response path %q, got %q", "backend/.env", resp.Path)
	}

	written, err := os.ReadFile(filepath.Join(projectsDir, "myproject", "backend", ".env"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if !bytes.Contains(written, []byte("NEW=2\n")) {
		t.Errorf("expected NEW=2 in written file, got %q", written)
	}

	// Confirm the write landed only in backend/, not at the project root too.
	if _, err := os.Stat(filepath.Join(projectsDir, "myproject", ".env")); !os.IsNotExist(err) {
		t.Errorf("expected no root .env to have been created, stat err = %v", err)
	}
}

func TestHandlePutEnv_EmptyKey(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{})
	h := newTestEnvHandler(projectsDir)

	rec := httptest.NewRecorder()
	h.HandlePutEnv(rec, putEnvRequest(t, "myproject", `{"path":".env","variables":[{"key":"","value":"x"}]}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePutEnv_InvalidKeyFormat(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{})
	h := newTestEnvHandler(projectsDir)

	rec := httptest.NewRecorder()
	h.HandlePutEnv(rec, putEnvRequest(t, "myproject", `{"path":".env","variables":[{"key":"1BAD","value":"x"}]}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePutEnv_DuplicateKey(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{})
	h := newTestEnvHandler(projectsDir)

	body := `{"path":".env","variables":[{"key":"FOO","value":"a"},{"key":"FOO","value":"b"}]}`
	rec := httptest.NewRecorder()
	h.HandlePutEnv(rec, putEnvRequest(t, "myproject", body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePutEnv_EmptyValueAllowed(t *testing.T) {
	projectsDir := newTestEnvProject(t, "myproject", map[string]string{})
	h := newTestEnvHandler(projectsDir)

	rec := httptest.NewRecorder()
	h.HandlePutEnv(rec, putEnvRequest(t, "myproject", `{"path":".env","variables":[{"key":"FOO","value":""}]}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
