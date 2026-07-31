package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectPath_Success(t *testing.T) {
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, "myproject")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	isRepo := func(p string) bool {
		_, err := os.Stat(filepath.Join(p, ".git"))
		return err == nil
	}

	got, err := resolveProjectPath(projectsDir, isRepo, "myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != repoDir {
		t.Errorf("expected %q, got %q", repoDir, got)
	}
}

func TestResolveProjectPath_RejectsUnknownName(t *testing.T) {
	projectsDir := t.TempDir()
	isRepo := func(string) bool { return true }

	if _, err := resolveProjectPath(projectsDir, isRepo, "nope"); err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestResolveProjectPath_RejectsPathTraversalName(t *testing.T) {
	projectsDir := t.TempDir()
	isRepo := func(string) bool { return true }

	for _, name := range []string{"../etc", "foo/../bar", "..", ""} {
		if _, err := resolveProjectPath(projectsDir, isRepo, name); err == nil {
			t.Errorf("expected error for name %q", name)
		}
	}
}

func TestResolveProjectPath_RejectsNonRepoDir(t *testing.T) {
	projectsDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectsDir, "notarepo"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	isRepo := func(string) bool { return false }

	if _, err := resolveProjectPath(projectsDir, isRepo, "notarepo"); err == nil {
		t.Fatal("expected error for non-repo directory")
	}
}

func TestValidateSubPath_Success(t *testing.T) {
	projectPath := t.TempDir()

	cases := map[string]string{
		"":              "",
		"/":             "",
		"src":           "src",
		"/src/":         "src",
		"src/main.go":   "src/main.go",
		"./src/main.go": "src/main.go",
	}
	for input, want := range cases {
		got, err := validateSubPath(projectPath, input)
		if err != nil {
			t.Errorf("validateSubPath(%q): unexpected error: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("validateSubPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateSubPath_RejectsTraversal(t *testing.T) {
	projectPath := t.TempDir()

	for _, input := range []string{"..", "../etc/passwd", "src/../../etc/passwd", "a/b/../../.."} {
		if _, err := validateSubPath(projectPath, input); err != errInvalidPath {
			t.Errorf("validateSubPath(%q): expected errInvalidPath, got %v", input, err)
		}
	}
}

func TestValidateSubPath_RejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(projectPath, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := validateSubPath(projectPath, "link/secret.txt"); err != errInvalidPath {
		t.Errorf("expected errInvalidPath for symlink escape, got %v", err)
	}
}
