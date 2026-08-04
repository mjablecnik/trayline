package api

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"remote/core"
)

func newTestAssistantFolderManager(dataDir string) *AssistantFolderManager {
	return NewAssistantFolderManager(dataDir, core.NewLogger("test-token"))
}

func TestAssistantFolderManager_Init_CreatesStructure(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "assistant")
	m := newTestAssistantFolderManager(dataDir)

	if err := m.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	for _, sub := range []string{"memory", "prompts"} {
		info, err := os.Stat(filepath.Join(dataDir, sub))
		if err != nil {
			t.Fatalf("expected %s/ to exist: %v", sub, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", sub)
		}
	}

	if _, err := os.Stat(filepath.Join(dataDir, ".git")); err != nil {
		t.Fatalf("expected .git/ to exist: %v", err)
	}

	claudeMD := filepath.Join(dataDir, "CLAUDE.md")
	content, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("expected CLAUDE.md to exist: %v", err)
	}
	if len(content) == 0 {
		t.Fatalf("expected CLAUDE.md to be non-empty")
	}
}

func TestAssistantFolderManager_Init_PathExistsNotDirectory(t *testing.T) {
	parent := t.TempDir()
	dataDir := filepath.Join(parent, "assistant")
	if err := os.WriteFile(dataDir, []byte("not a dir"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m := newTestAssistantFolderManager(dataDir)
	if err := m.Init(); err == nil {
		t.Fatalf("expected error when path exists but is not a directory")
	}
}

func TestAssistantFolderManager_Init_PreservesExistingClaudeMD(t *testing.T) {
	dataDir := t.TempDir()
	custom := "# Custom personality\n"
	if err := os.WriteFile(filepath.Join(dataDir, "CLAUDE.md"), []byte(custom), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m := newTestAssistantFolderManager(dataDir)
	if err := m.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dataDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if string(content) != custom {
		t.Fatalf("expected existing CLAUDE.md to be preserved, got %q", content)
	}
}

func TestAssistantFolderManager_Init_SkipsReinitGitRepo(t *testing.T) {
	dataDir := t.TempDir()
	gitDir := filepath.Join(dataDir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	marker := filepath.Join(gitDir, "marker")
	if err := os.WriteFile(marker, []byte("keep me"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m := newTestAssistantFolderManager(dataDir)
	if err := m.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected existing .git/ contents to be preserved: %v", err)
	}
}

func TestAssistantFolderManager_ValidatePath(t *testing.T) {
	m := newTestAssistantFolderManager(t.TempDir())

	valid := []string{"", "/", "memory", "memory/notes.md", "a/b/c.txt", "CLAUDE.md"}
	for _, p := range valid {
		if _, err := m.validatePath(p); err != nil {
			t.Errorf("validatePath(%q) unexpected error: %v", p, err)
		}
	}

	invalid := []string{"../etc/passwd", "memory/../..", "/etc/passwd", "mem ory", "mem$ory", "a\\b"}
	for _, p := range invalid {
		if _, err := m.validatePath(p); err == nil {
			t.Errorf("validatePath(%q) expected error, got nil", p)
		}
	}
}

func TestAssistantFolderManager_ListDirectory(t *testing.T) {
	dataDir := t.TempDir()
	m := newTestAssistantFolderManager(dataDir)
	if err := m.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(dataDir, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "a.txt"), []byte("aa"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "zzz-dir"), 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	entries, err := m.ListDirectory(".")
	if err != nil {
		t.Fatalf("ListDirectory() error = %v", err)
	}

	var names []string
	for _, e := range entries {
		if e.Name == ".git" {
			t.Fatalf("expected .git/ to be excluded from listing")
		}
		names = append(names, e.Name)
	}

	// directories first (zzz-dir), then files alphabetically (a.txt, b.txt, CLAUDE.md, memory/prompts already dirs)
	expectDirsFirst := []string{"memory", "prompts", "zzz-dir", "CLAUDE.md", "a.txt", "b.txt"}
	if len(names) != len(expectDirsFirst) {
		t.Fatalf("expected %d entries, got %d: %v", len(expectDirsFirst), len(names), names)
	}
	if names[0] != "memory" || names[1] != "prompts" || names[2] != "zzz-dir" {
		t.Fatalf("expected directories sorted first, got %v", names)
	}
}

func TestAssistantFolderManager_ReadFile(t *testing.T) {
	dataDir := t.TempDir()
	m := newTestAssistantFolderManager(dataDir)

	small := "hello world"
	if err := os.WriteFile(filepath.Join(dataDir, "small.txt"), []byte(small), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resp, err := m.ReadFile("small.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if resp.Truncated {
		t.Fatalf("expected small file to not be truncated")
	}
	if resp.Content == nil || *resp.Content != small {
		t.Fatalf("expected content %q, got %v", small, resp.Content)
	}
	if resp.Filename != "small.txt" || resp.Size != int64(len(small)) {
		t.Fatalf("unexpected filename/size: %+v", resp)
	}

	big := make([]byte, maxAssistantFileSize+1)
	if err := os.WriteFile(filepath.Join(dataDir, "big.txt"), big, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resp, err = m.ReadFile("big.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !resp.Truncated {
		t.Fatalf("expected large file to be truncated")
	}
	if resp.Content != nil {
		t.Fatalf("expected nil content for truncated file, got %v", *resp.Content)
	}
}

func TestValidatePromptFilename(t *testing.T) {
	valid := []string{"a.md", "daily-review.txt", "code_review.md", "CLAUDE.md", "a"}
	for _, f := range valid {
		if err := ValidatePromptFilename(f); err != nil {
			t.Errorf("ValidatePromptFilename(%q) unexpected error: %v", f, err)
		}
	}

	invalid := []string{
		"", "../etc/passwd", "a/b.md", "a\\b.md", "has space.md", "has$dollar.md",
		strings.Repeat("a", 101),
	}
	for _, f := range invalid {
		if err := ValidatePromptFilename(f); err == nil {
			t.Errorf("ValidatePromptFilename(%q) expected error, got nil", f)
		}
	}
}

func TestAssistantFolderManager_PutPrompt_GetPrompt_RoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	m := newTestAssistantFolderManager(dataDir)
	if err := m.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	content := "Review the diff and flag issues."
	if err := m.PutPrompt("code-review.md", content); err != nil {
		t.Fatalf("PutPrompt() error = %v", err)
	}

	prompt, err := m.GetPrompt("code-review.md")
	if err != nil {
		t.Fatalf("GetPrompt() error = %v", err)
	}
	if prompt.Content != content {
		t.Fatalf("expected content %q, got %q", content, prompt.Content)
	}
	if prompt.DisplayName != "code review" {
		t.Fatalf("expected display name %q, got %q", "code review", prompt.DisplayName)
	}
	if prompt.Filename != "code-review.md" {
		t.Fatalf("expected filename %q, got %q", "code-review.md", prompt.Filename)
	}
}

func TestAssistantFolderManager_DeletePrompt(t *testing.T) {
	dataDir := t.TempDir()
	m := newTestAssistantFolderManager(dataDir)
	if err := m.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if err := m.PutPrompt("temp.txt", "scratch"); err != nil {
		t.Fatalf("PutPrompt() error = %v", err)
	}
	if err := m.DeletePrompt("temp.txt"); err != nil {
		t.Fatalf("DeletePrompt() error = %v", err)
	}
	if _, err := m.GetPrompt("temp.txt"); err == nil {
		t.Fatalf("expected error reading deleted prompt")
	}
}

// runGit runs a git command in dataDir with a fixed test identity, failing
// the test on error.
func runGit(t *testing.T, dataDir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dataDir
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestAssistantFolderManager_GetCommits(t *testing.T) {
	dataDir := t.TempDir()
	m := newTestAssistantFolderManager(dataDir)
	if err := m.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	runGit(t, dataDir, "config", "user.email", "test@example.com")
	runGit(t, dataDir, "config", "user.name", "Test")
	runGit(t, dataDir, "add", "-A")
	runGit(t, dataDir, "commit", "-m", "initial commit")

	if err := os.WriteFile(filepath.Join(dataDir, "memory", "notes.md"), []byte("note"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	runGit(t, dataDir, "add", "-A")
	runGit(t, dataDir, "commit", "-m", "add notes")

	commits, err := m.GetCommits(20, 0)
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d: %+v", len(commits), commits)
	}
	if commits[0].Message != "add notes" || commits[1].Message != "initial commit" {
		t.Fatalf("expected most-recent-first order, got %+v", commits)
	}
	if commits[0].Hash == "" || commits[0].ShortHash == "" || commits[0].Date == "" {
		t.Fatalf("expected hash/short_hash/date to be populated, got %+v", commits[0])
	}

	limited, err := m.GetCommits(1, 0)
	if err != nil {
		t.Fatalf("GetCommits(limit=1) error = %v", err)
	}
	if len(limited) != 1 || limited[0].Message != "add notes" {
		t.Fatalf("expected 1 most-recent commit, got %+v", limited)
	}
}

func TestAssistantFolderManager_GetCommits_EmptyRepo(t *testing.T) {
	dataDir := t.TempDir()
	m := newTestAssistantFolderManager(dataDir)
	if err := m.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	commits, err := m.GetCommits(20, 0)
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("expected 0 commits for a fresh repo, got %+v", commits)
	}
}

func TestAssistantFolderManager_GetStatus_Clean(t *testing.T) {
	dataDir := t.TempDir()
	m := newTestAssistantFolderManager(dataDir)
	if err := m.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	runGit(t, dataDir, "config", "user.email", "test@example.com")
	runGit(t, dataDir, "config", "user.name", "Test")
	runGit(t, dataDir, "add", "-A")
	runGit(t, dataDir, "commit", "-m", "initial commit")

	status, err := m.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if !status.Clean {
		t.Fatalf("expected clean status, got %+v", status)
	}
	if len(status.Files) != 0 {
		t.Fatalf("expected no changed files, got %+v", status.Files)
	}
}

func TestAssistantFolderManager_GetStatus_Dirty(t *testing.T) {
	dataDir := t.TempDir()
	m := newTestAssistantFolderManager(dataDir)
	if err := m.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	runGit(t, dataDir, "config", "user.email", "test@example.com")
	runGit(t, dataDir, "config", "user.name", "Test")
	runGit(t, dataDir, "add", "-A")
	runGit(t, dataDir, "commit", "-m", "initial commit")

	// Modify a tracked file and add an untracked one.
	if err := os.WriteFile(filepath.Join(dataDir, "CLAUDE.md"), []byte("changed content\nline two\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "memory", "new.md"), []byte("new"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	status, err := m.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if status.Clean {
		t.Fatalf("expected dirty status, got %+v", status)
	}
	if status.Summary.FilesChanged != 2 {
		t.Fatalf("expected 2 changed files, got %+v", status.Summary)
	}
	if status.Summary.Insertions == 0 {
		t.Fatalf("expected non-zero insertions for modified tracked file, got %+v", status.Summary)
	}

	statuses := map[string]string{}
	for _, f := range status.Files {
		statuses[f.Path] = f.Status
	}
	if statuses["CLAUDE.md"] != "modified" {
		t.Fatalf("expected CLAUDE.md to be modified, got %+v", statuses)
	}
	// memory/ itself was never committed (git does not track empty
	// directories), so it is reported as a whole untracked directory
	// rather than as the individual file within it.
	if statuses["memory/"] != "untracked" {
		t.Fatalf("expected memory/ to be untracked, got %+v", statuses)
	}
}

func TestAssistantFolderManager_GetStatus_UninitializedRepo(t *testing.T) {
	dataDir := t.TempDir()
	m := newTestAssistantFolderManager(dataDir)
	// Do not call Init() — no .git/ exists at all.

	status, err := m.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if !status.Clean {
		t.Fatalf("expected clean status for uninitialized repo, got %+v", status)
	}
}

func TestAssistantFolderManager_ListPrompts(t *testing.T) {
	dataDir := t.TempDir()
	m := newTestAssistantFolderManager(dataDir)
	if err := m.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if err := m.PutPrompt("zeta.txt", "z"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := m.PutPrompt("alpha-one.md", "a"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "prompts", "ignored.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	prompts, err := m.ListPrompts()
	if err != nil {
		t.Fatalf("ListPrompts() error = %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d: %+v", len(prompts), prompts)
	}
	if prompts[0].Filename != "alpha-one.md" || prompts[1].Filename != "zeta.txt" {
		t.Fatalf("expected alphabetical order, got %v, %v", prompts[0].Filename, prompts[1].Filename)
	}
	if prompts[0].DisplayName != "alpha one" {
		t.Fatalf("expected display name %q, got %q", "alpha one", prompts[0].DisplayName)
	}
}

// --- Property 5: Prompt filename validation ---

func TestPropertyValidatePromptFilenameAcceptsValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		filename := rapid.StringMatching(`[a-zA-Z0-9._\-]{1,100}`).
			Filter(func(s string) bool { return !strings.Contains(s, "..") && s != "." }).
			Draw(t, "filename")

		if err := ValidatePromptFilename(filename); err != nil {
			t.Fatalf("ValidatePromptFilename(%q) unexpected error: %v", filename, err)
		}
	})
}

func TestPropertyValidatePromptFilenameRejectsInvalid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom([]string{"empty", "too_long", "dotdot", "bad_char", "path_sep"}).Draw(t, "kind")

		var filename string
		switch kind {
		case "empty":
			filename = ""
		case "too_long":
			filename = rapid.StringMatching(`[a-zA-Z0-9._\-]{101,150}`).Draw(t, "filename")
		case "dotdot":
			pre := rapid.StringMatching(`[a-zA-Z0-9._\-]{0,20}`).Draw(t, "pre")
			post := rapid.StringMatching(`[a-zA-Z0-9._\-]{0,20}`).Draw(t, "post")
			filename = pre + ".." + post
		case "bad_char":
			base := rapid.StringMatching(`[a-zA-Z0-9._\-]{0,20}`).Draw(t, "base")
			badChar := rapid.SampledFrom([]rune{' ', '$', '@', '#', '!', '*', '?', '~', '%'}).Draw(t, "badChar")
			filename = base + string(badChar)
		case "path_sep":
			base := rapid.StringMatching(`[a-zA-Z0-9._\-]{0,20}`).Draw(t, "base")
			sep := rapid.SampledFrom([]string{"/", "\\"}).Draw(t, "sep")
			filename = base + sep + "b"
		}

		if err := ValidatePromptFilename(filename); err == nil {
			t.Fatalf("ValidatePromptFilename(%q) [%s] expected error, got nil", filename, kind)
		}
	})
}

// --- Property 6: Prompt content round-trip ---

func TestPropertyPutPromptGetPromptRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dataDir, err := os.MkdirTemp("", "assistant-prop-*")
		if err != nil {
			t.Fatalf("MkdirTemp: %v", err)
		}
		defer os.RemoveAll(dataDir)

		m := newTestAssistantFolderManager(dataDir)
		if err := m.Init(); err != nil {
			t.Fatalf("Init() error = %v", err)
		}

		filename := rapid.StringMatching(`[a-zA-Z0-9._\-]{1,100}`).
			Filter(func(s string) bool { return ValidatePromptFilename(s) == nil }).
			Draw(t, "filename")
		content := rapid.StringN(0, 10000, -1).Draw(t, "content")

		if err := m.PutPrompt(filename, content); err != nil {
			t.Fatalf("PutPrompt(%q) error = %v", filename, err)
		}

		prompt, err := m.GetPrompt(filename)
		if err != nil {
			t.Fatalf("GetPrompt(%q) error = %v", filename, err)
		}
		if prompt.Content != content {
			t.Fatalf("expected round-tripped content %q, got %q", content, prompt.Content)
		}
		if prompt.Filename != filename {
			t.Fatalf("expected filename %q, got %q", filename, prompt.Filename)
		}
	})
}

// --- Property 7: Prompt listing is complete and sorted ---

func TestPropertyListPromptsCompletenessAndSort(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dataDir, err := os.MkdirTemp("", "assistant-prop-*")
		if err != nil {
			t.Fatalf("MkdirTemp: %v", err)
		}
		defer os.RemoveAll(dataDir)

		m := newTestAssistantFolderManager(dataDir)
		if err := m.Init(); err != nil {
			t.Fatalf("Init() error = %v", err)
		}

		n := rapid.IntRange(0, 8).Draw(t, "n")
		expected := map[string]string{}
		for i := 0; i < n; i++ {
			base := rapid.StringMatching(`[a-zA-Z0-9_\-]{1,20}`).Draw(t, fmt.Sprintf("base%d", i))
			ext := rapid.SampledFrom([]string{".md", ".txt"}).Draw(t, fmt.Sprintf("ext%d", i))
			filename := base + ext
			content := rapid.StringN(0, 500, -1).Draw(t, fmt.Sprintf("content%d", i))
			expected[filename] = content
			if err := m.PutPrompt(filename, content); err != nil {
				t.Fatalf("PutPrompt(%q) error = %v", filename, err)
			}
		}

		// A non-.md/.txt file in prompts/ must be excluded from the listing.
		if err := os.WriteFile(filepath.Join(dataDir, "prompts", "ignored.json"), []byte("{}"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		prompts, err := m.ListPrompts()
		if err != nil {
			t.Fatalf("ListPrompts() error = %v", err)
		}

		if len(prompts) != len(expected) {
			t.Fatalf("expected %d prompts, got %d: %+v", len(expected), len(prompts), prompts)
		}

		var lastFilename string
		for i, p := range prompts {
			wantContent, ok := expected[p.Filename]
			if !ok {
				t.Fatalf("unexpected prompt filename %q in listing", p.Filename)
			}
			if p.Content != wantContent {
				t.Fatalf("prompt %q: expected content %q, got %q", p.Filename, wantContent, p.Content)
			}
			if p.DisplayName != promptDisplayName(p.Filename) {
				t.Fatalf("prompt %q: expected display name %q, got %q", p.Filename, promptDisplayName(p.Filename), p.DisplayName)
			}
			if i > 0 && p.Filename < lastFilename {
				t.Fatalf("expected alphabetical order, %q came after %q", p.Filename, lastFilename)
			}
			lastFilename = p.Filename
		}
	})
}

// --- Property 8: File path validation rejects traversal and invalid characters ---

func TestPropertyValidatePathAcceptsValid(t *testing.T) {
	m := newTestAssistantFolderManager(t.TempDir())

	rapid.Check(t, func(t *rapid.T) {
		path := rapid.StringMatching(`[a-zA-Z0-9._/\-]{1,40}`).
			Filter(func(s string) bool { return !strings.Contains(s, "..") && !strings.HasPrefix(s, "/") }).
			Draw(t, "path")

		cleaned, err := m.validatePath(path)
		if err != nil {
			t.Fatalf("validatePath(%q) unexpected error: %v", path, err)
		}
		if filepath.IsAbs(cleaned) || strings.Contains(cleaned, "..") {
			t.Fatalf("validatePath(%q) returned unsafe cleaned path %q", path, cleaned)
		}
	})
}

func TestPropertyValidatePathRejectsInvalid(t *testing.T) {
	m := newTestAssistantFolderManager(t.TempDir())

	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom([]string{"dotdot", "absolute", "bad_char"}).Draw(t, "kind")

		var path string
		switch kind {
		case "dotdot":
			pre := rapid.StringMatching(`[a-zA-Z0-9._/\-]{0,20}`).Draw(t, "pre")
			post := rapid.StringMatching(`[a-zA-Z0-9._/\-]{0,20}`).Draw(t, "post")
			path = pre + ".." + post
		case "absolute":
			rest := rapid.StringMatching(`[a-zA-Z0-9._/\-]{1,20}`).
				Filter(func(s string) bool { return !strings.Contains(s, "..") }).
				Draw(t, "rest")
			path = "/" + rest
		case "bad_char":
			base := rapid.StringMatching(`[a-zA-Z0-9._/\-]{0,20}`).Draw(t, "base")
			badChar := rapid.SampledFrom([]rune{' ', '$', '@', '#', '!', '*', '?', '~', '%', '\\'}).Draw(t, "badChar")
			path = base + string(badChar)
		}

		if _, err := m.validatePath(path); err == nil {
			t.Fatalf("validatePath(%q) [%s] expected error, got nil", path, kind)
		}
	})
}

// --- Property 9: File content response respects size threshold ---

func TestPropertyReadFileRespectsSizeThreshold(t *testing.T) {
	dataDir := t.TempDir()
	m := newTestAssistantFolderManager(dataDir)

	rapid.Check(t, func(t *rapid.T) {
		overThreshold := rapid.Bool().Draw(t, "overThreshold")

		var size int
		if overThreshold {
			size = maxAssistantFileSize + rapid.IntRange(1, 2000).Draw(t, "excess")
		} else {
			size = maxAssistantFileSize - rapid.IntRange(0, 2000).Draw(t, "deficit")
		}

		data := bytes.Repeat([]byte("a"), size)
		if err := os.WriteFile(filepath.Join(dataDir, "sized.txt"), data, 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		resp, err := m.ReadFile("sized.txt")
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		if resp.Size != int64(size) {
			t.Fatalf("expected reported size %d, got %d", size, resp.Size)
		}

		if overThreshold {
			if !resp.Truncated {
				t.Fatalf("size %d > threshold %d: expected Truncated=true", size, maxAssistantFileSize)
			}
			if resp.Content != nil {
				t.Fatalf("size %d > threshold %d: expected nil Content, got non-nil", size, maxAssistantFileSize)
			}
		} else {
			if resp.Truncated {
				t.Fatalf("size %d <= threshold %d: expected Truncated=false", size, maxAssistantFileSize)
			}
			if resp.Content == nil {
				t.Fatalf("size %d <= threshold %d: expected non-nil Content", size, maxAssistantFileSize)
			}
		}
	})
}

// --- Property 10: Directory listing is sorted correctly ---

func TestPropertyListDirectorySortedCorrectly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dataDir, err := os.MkdirTemp("", "assistant-prop-*")
		if err != nil {
			t.Fatalf("MkdirTemp: %v", err)
		}
		defer os.RemoveAll(dataDir)

		m := newTestAssistantFolderManager(dataDir)

		if err := os.MkdirAll(filepath.Join(dataDir, ".git"), 0755); err != nil {
			t.Fatalf("setup .git: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, ".git", "config"), []byte("x"), 0644); err != nil {
			t.Fatalf("setup .git/config: %v", err)
		}

		names := rapid.SliceOfNDistinct(
			rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_-]{0,15}`),
			1, 10, func(s string) string { return s },
		).Draw(t, "names")

		var wantDirs, wantFiles []string
		for i, name := range names {
			isDir := rapid.Bool().Draw(t, fmt.Sprintf("isDir%d", i))
			if isDir {
				if err := os.MkdirAll(filepath.Join(dataDir, name), 0755); err != nil {
					t.Fatalf("setup dir %q: %v", name, err)
				}
				wantDirs = append(wantDirs, name)
			} else {
				if err := os.WriteFile(filepath.Join(dataDir, name), []byte("content"), 0644); err != nil {
					t.Fatalf("setup file %q: %v", name, err)
				}
				wantFiles = append(wantFiles, name)
			}
		}
		sort.Strings(wantDirs)
		sort.Strings(wantFiles)

		entries, err := m.ListDirectory(".")
		if err != nil {
			t.Fatalf("ListDirectory() error = %v", err)
		}

		for _, e := range entries {
			if e.Name == ".git" {
				t.Fatalf("expected .git/ to be excluded from listing, got entries %v", entries)
			}
		}

		wantCount := len(wantDirs) + len(wantFiles)
		if len(entries) != wantCount {
			t.Fatalf("expected %d entries, got %d: %v", wantCount, len(entries), entries)
		}

		var gotDirs, gotFiles []string
		sawFile := false
		for _, e := range entries {
			switch e.Type {
			case "directory":
				if sawFile {
					t.Fatalf("expected all directories before files, got entries %v", entries)
				}
				gotDirs = append(gotDirs, e.Name)
			case "file":
				sawFile = true
				gotFiles = append(gotFiles, e.Name)
			default:
				t.Fatalf("unexpected entry type %q for %q", e.Type, e.Name)
			}
		}

		if !sort.StringsAreSorted(gotDirs) {
			t.Fatalf("expected directories sorted alphabetically, got %v", gotDirs)
		}
		if !sort.StringsAreSorted(gotFiles) {
			t.Fatalf("expected files sorted alphabetically, got %v", gotFiles)
		}
		if len(gotDirs) != len(wantDirs) {
			t.Fatalf("expected directories %v, got %v", wantDirs, gotDirs)
		}
		for i := range wantDirs {
			if gotDirs[i] != wantDirs[i] {
				t.Fatalf("expected directories %v, got %v", wantDirs, gotDirs)
			}
		}
		if len(gotFiles) != len(wantFiles) {
			t.Fatalf("expected files %v, got %v", wantFiles, gotFiles)
		}
		for i := range wantFiles {
			if gotFiles[i] != wantFiles[i] {
				t.Fatalf("expected files %v, got %v", wantFiles, gotFiles)
			}
		}
	})
}
