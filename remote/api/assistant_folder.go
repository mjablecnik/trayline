package api

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"remote/core"
)

// AssistantFolderManager manages the personal assistant's data folder
// lifecycle: directory structure, git repository, and default CLAUDE.md.
type AssistantFolderManager struct {
	dataDir string
	logger  *core.Logger
}

// NewAssistantFolderManager constructs an AssistantFolderManager for the
// given assistant data directory.
func NewAssistantFolderManager(dataDir string, logger *core.Logger) *AssistantFolderManager {
	return &AssistantFolderManager{dataDir: dataDir, logger: logger}
}

// Init ensures the assistant folder exists with its required structure
// (memory/ and prompts/ subdirectories), is a git repository, and has a
// default CLAUDE.md file. Called once during server startup. Returns an
// error only on fatal conditions (e.g. the path exists but is not a
// directory).
func (m *AssistantFolderManager) Init() error {
	info, err := os.Stat(m.dataDir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("assistant data path %q exists but is not a directory", m.dataDir)
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(m.dataDir, 0755); err != nil {
			return fmt.Errorf("failed to create assistant folder: %w", err)
		}
	} else {
		return fmt.Errorf("failed to stat assistant folder: %w", err)
	}

	for _, sub := range []string{"memory", "prompts"} {
		subPath := filepath.Join(m.dataDir, sub)
		if err := os.MkdirAll(subPath, 0755); err != nil {
			return fmt.Errorf("failed to create %s/ subdirectory: %w", sub, err)
		}
	}

	m.initGitRepo()
	m.ensureClaudeMD()

	return nil
}

// initGitRepo initializes the assistant folder as a git repository if it
// is not already one. A pre-existing .git/ directory is left untouched.
func (m *AssistantFolderManager) initGitRepo() {
	gitDir := filepath.Join(m.dataDir, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		return
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = m.dataDir
	if out, err := cmd.CombinedOutput(); err != nil {
		m.logger.Warn(context.Background(),
			fmt.Sprintf("git init in assistant folder failed: %v: %s", err, out))
	}
}

const defaultClaudeMD = `# Personal Assistant

You are a personal assistant with access to all projects and a persistent workspace.

## Workspace Layout

- /workspace — your assistant data directory (this is your CWD)
  - CLAUDE.md — this file (your personality and rules)
  - memory/ — persistent knowledge, session highlights, and notes
  - prompts/ — saved starter prompts for quick session starts
  - summary.md — latest conversation summary (created via Summarize action)
- /projects/ — all user projects (read-write access)
  - Each subdirectory is a separate project repository

## Rules

1. After creating or modifying any file in /workspace/, always run:
   git add -A && git commit -m "<descriptive message>"
   in the /workspace/ directory.

2. Commit messages must be concise and descriptive (e.g., "Add session notes from programming discussion", "Update task list with new items").

3. When asked to summarize a conversation, create a concise summary covering key topics, decisions, and action items, and save it to /workspace/summary.md (overwriting any previous summary).

4. You can browse and modify files in /projects/ when the user asks about specific projects.

5. Store persistent notes and session context in /workspace/memory/ for future reference.
`

// ensureClaudeMD creates a default CLAUDE.md file at the root of the
// assistant folder if one does not already exist.
func (m *AssistantFolderManager) ensureClaudeMD() {
	path := filepath.Join(m.dataDir, "CLAUDE.md")
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.WriteFile(path, []byte(defaultClaudeMD), 0644); err != nil {
		m.logger.Warn(context.Background(),
			"failed to create default CLAUDE.md: "+err.Error())
	}
}

// fileEntry describes a single file or directory within the assistant
// folder, as returned by ListDirectory.
type fileEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "file" or "directory"
	Size int64  `json:"size"`
}

// fileContentResponse describes the content of a single file within the
// assistant folder, as returned by ReadFile.
type fileContentResponse struct {
	Path      string  `json:"path"`
	Filename  string  `json:"filename"`
	Size      int64   `json:"size"`
	Content   *string `json:"content"` // nil if truncated
	Truncated bool    `json:"truncated"`
}

const maxAssistantFileSize = 1 * 1024 * 1024 // 1 MB

var validAssistantPath = regexp.MustCompile(`^[a-zA-Z0-9._/\-]+$`)

// validatePath rejects path traversal, absolute paths, and characters
// outside the allowlist, returning a cleaned relative path on success.
func (m *AssistantFolderManager) validatePath(path string) (string, error) {
	if path == "" || path == "/" {
		return ".", nil
	}
	if filepath.IsAbs(path) || strings.Contains(path, "..") ||
		!validAssistantPath.MatchString(path) {
		return "", fmt.Errorf("invalid path")
	}
	return filepath.Clean(path), nil
}

// ListDirectory returns the entries of a directory within the assistant
// folder, excluding .git/, sorted with directories first then
// alphabetically within each group.
func (m *AssistantFolderManager) ListDirectory(relPath string) ([]fileEntry, error) {
	absPath := filepath.Join(m.dataDir, relPath)
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, err
	}

	result := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		entry := fileEntry{Name: e.Name(), Type: "file", Size: info.Size()}
		if e.IsDir() {
			entry.Type = "directory"
		}
		result = append(result, entry)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Type != result[j].Type {
			return result[i].Type == "directory"
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// ReadFile returns the content of a file within the assistant folder.
// Files larger than 1 MB return a nil Content with Truncated set to true.
func (m *AssistantFolderManager) ReadFile(relPath string) (*fileContentResponse, error) {
	absPath := filepath.Join(m.dataDir, relPath)
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}

	resp := &fileContentResponse{
		Path:     relPath,
		Filename: filepath.Base(relPath),
		Size:     info.Size(),
	}

	if info.Size() > maxAssistantFileSize {
		resp.Truncated = true
		return resp, nil
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	content := string(data)
	resp.Content = &content
	return resp, nil
}

// starterPrompt describes a single starter prompt stored in the assistant
// folder's prompts/ subdirectory.
type starterPrompt struct {
	Filename    string `json:"filename"`
	DisplayName string `json:"display_name"`
	Content     string `json:"content"`
}

const (
	maxPromptFilenameLen = 100
	maxPromptContentLen  = 10000
)

var validPromptFilename = regexp.MustCompile(`^[a-zA-Z0-9._\-]+$`)

// ValidatePromptFilename enforces the starter prompt filename allowlist:
// only alphanumeric characters, hyphens, underscores, and dots, with a
// maximum length of 100 characters. Path separators and dot-dot sequences
// are rejected.
func ValidatePromptFilename(filename string) error {
	if filename == "" || len(filename) > maxPromptFilenameLen {
		return fmt.Errorf("filename must be 1-100 characters")
	}
	if filename == "." || strings.Contains(filename, "..") || !validPromptFilename.MatchString(filename) {
		return fmt.Errorf("filename contains invalid characters")
	}
	return nil
}

// promptDisplayName derives a human-readable display name from a prompt
// filename: strips the extension and replaces hyphens/underscores with
// spaces.
func promptDisplayName(filename string) string {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return name
}

// ListPrompts returns all starter prompts (.md and .txt files) from the
// prompts/ subdirectory, sorted alphabetically by filename.
func (m *AssistantFolderManager) ListPrompts() ([]starterPrompt, error) {
	promptsDir := filepath.Join(m.dataDir, "prompts")
	entries, err := os.ReadDir(promptsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []starterPrompt{}, nil
		}
		return nil, err
	}

	prompts := make([]starterPrompt, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".md" && ext != ".txt" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(promptsDir, e.Name()))
		if err != nil {
			continue
		}
		prompts = append(prompts, starterPrompt{
			Filename:    e.Name(),
			DisplayName: promptDisplayName(e.Name()),
			Content:     string(content),
		})
	}

	sort.Slice(prompts, func(i, j int) bool {
		return prompts[i].Filename < prompts[j].Filename
	})
	return prompts, nil
}

// GetPrompt returns a single starter prompt's content by filename.
func (m *AssistantFolderManager) GetPrompt(filename string) (*starterPrompt, error) {
	content, err := os.ReadFile(filepath.Join(m.dataDir, "prompts", filename))
	if err != nil {
		return nil, err
	}
	return &starterPrompt{
		Filename:    filename,
		DisplayName: promptDisplayName(filename),
		Content:     string(content),
	}, nil
}

// PutPrompt creates or overwrites a starter prompt file with the given
// content.
func (m *AssistantFolderManager) PutPrompt(filename, content string) error {
	return os.WriteFile(filepath.Join(m.dataDir, "prompts", filename), []byte(content), 0644)
}

// DeletePrompt removes a starter prompt file.
func (m *AssistantFolderManager) DeletePrompt(filename string) error {
	return os.Remove(filepath.Join(m.dataDir, "prompts", filename))
}

// gitCommitEntry describes a single commit in the assistant folder's git
// history, as returned by GetCommits.
type gitCommitEntry struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"short_hash"`
	Message   string `json:"message"`
	Date      string `json:"date"`
}

const defaultCommitLimit = 20

// GetCommits returns a page of commits from the assistant folder's git
// history, most recent first. Returns an empty (not nil) slice, rather than
// an error, when the repository has no commits yet or is uninitialized.
func (m *AssistantFolderManager) GetCommits(limit, offset int) ([]gitCommitEntry, error) {
	if limit <= 0 {
		limit = defaultCommitLimit
	}

	cmd := exec.Command("git", "log",
		"--format=%H|%h|%s|%aI",
		fmt.Sprintf("--skip=%d", offset),
		fmt.Sprintf("-n%d", limit),
	)
	cmd.Dir = m.dataDir
	out, err := cmd.Output()
	if err != nil {
		return []gitCommitEntry{}, nil
	}

	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return []gitCommitEntry{}, nil
	}

	lines := strings.Split(trimmed, "\n")
	commits := make([]gitCommitEntry, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}
		commits = append(commits, gitCommitEntry{
			Hash: parts[0], ShortHash: parts[1], Message: parts[2], Date: parts[3],
		})
	}
	return commits, nil
}

// gitStatusFile describes a single changed file in the assistant folder's
// working tree, as returned by GetStatus.
type gitStatusFile struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "modified", "untracked", "deleted", "added"
}

// gitStatusSummary aggregates counts across all changed files in a
// GetStatus response.
type gitStatusSummary struct {
	FilesChanged int `json:"files_changed"`
	Insertions   int `json:"insertions"`
	Deletions    int `json:"deletions"`
}

// gitStatusResponse describes the working tree status of the assistant
// folder, as returned by GetStatus.
type gitStatusResponse struct {
	Clean   bool             `json:"clean"`
	Files   []gitStatusFile  `json:"files"`
	Summary gitStatusSummary `json:"summary"`
}

// GetStatus returns the working tree status of the assistant folder,
// including a summary of insertions and deletions across tracked changes.
// Returns a clean status, rather than an error, when the repository has no
// commits yet or is uninitialized.
func (m *AssistantFolderManager) GetStatus() (*gitStatusResponse, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = m.dataDir
	out, err := cmd.Output()
	if err != nil {
		return &gitStatusResponse{Clean: true, Files: []gitStatusFile{}}, nil
	}

	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return &gitStatusResponse{Clean: true, Files: []gitStatusFile{}}, nil
	}

	resp := &gitStatusResponse{Files: []gitStatusFile{}}
	for _, line := range strings.Split(trimmed, "\n") {
		if len(line) < 4 {
			continue
		}
		statusCode := strings.TrimSpace(line[:2])
		filePath := strings.TrimSpace(line[3:])
		status := "modified"
		switch {
		case statusCode == "??":
			status = "untracked"
		case strings.Contains(statusCode, "D"):
			status = "deleted"
		case strings.Contains(statusCode, "A"):
			status = "added"
		}
		resp.Files = append(resp.Files, gitStatusFile{Path: filePath, Status: status})
		resp.Summary.FilesChanged++
	}

	resp.Summary.Insertions, resp.Summary.Deletions = m.diffNumstatTotals()

	return resp, nil
}

// diffNumstatTotals sums insertions/deletions for tracked working-tree
// changes against HEAD via `git diff HEAD --numstat`. Untracked files have
// no diff and are not represented here, matching git's own behavior.
// Returns zeros if the repository has no commits yet or the diff cannot be
// read.
func (m *AssistantFolderManager) diffNumstatTotals() (insertions, deletions int) {
	cmd := exec.Command("git", "diff", "HEAD", "--numstat")
	cmd.Dir = m.dataDir
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}

	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return 0, 0
	}

	for _, line := range strings.Split(trimmed, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		if ins, err := strconv.Atoi(parts[0]); err == nil {
			insertions += ins
		}
		if del, err := strconv.Atoi(parts[1]); err == nil {
			deletions += del
		}
	}
	return insertions, deletions
}
