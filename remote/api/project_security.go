package api

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// errInvalidPath indicates a sub-path failed validation (contains ".."
// segments or escapes the project directory).
var errInvalidPath = errors.New("invalid path")

// projectNameRe restricts project names to a safe character set so they
// cannot be used to escape projectsDir via "..", separators, etc.
var projectNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// resolveProjectPath validates name against the safe-name pattern and
// confirms it names an existing git repository directly under projectsDir.
// It is shared by every handler that accepts a project name from a URL path.
func resolveProjectPath(projectsDir string, isRepo func(string) bool, name string) (string, error) {
	// The regex below permits "." and "-" but does not by itself exclude the
	// all-dots names "." and "..", which filepath.Join would resolve to
	// projectsDir itself or its parent — reject them explicitly.
	if name == "" || name == "." || name == ".." || !projectNameRe.MatchString(name) {
		return "", os.ErrNotExist
	}
	repoPath := filepath.Join(projectsDir, name)
	info, err := os.Stat(repoPath)
	if err != nil || !info.IsDir() || !isRepo(repoPath) {
		return "", os.ErrNotExist
	}
	return repoPath, nil
}

// validateSubPath rejects any ".." path segment and verifies the cleaned
// path, once joined to projectPath, does not escape it. It returns the
// cleaned, slash-trimmed relative path (suitable for use as a git tree-ish
// path) rather than an absolute filesystem path, since tree/blob content is
// read from the git object store rather than the working tree — a path can
// be valid at a historical ref without existing on disk at all.
//
// When the joined path does happen to exist in the current working tree,
// symlinks are additionally resolved and re-checked against projectPath, to
// catch escapes via a symlinked directory inside the repo.
func validateSubPath(projectPath, subPath string) (string, error) {
	trimmed := strings.Trim(subPath, "/")
	var segs []string
	if trimmed != "" {
		for _, seg := range strings.Split(trimmed, "/") {
			switch seg {
			case "", ".":
				continue
			case "..":
				return "", errInvalidPath
			default:
				segs = append(segs, seg)
			}
		}
	}
	cleaned := strings.Join(segs, "/")

	projectClean := filepath.Clean(projectPath)
	joined := filepath.Join(projectClean, cleaned)
	if joined != projectClean && !strings.HasPrefix(joined, projectClean+string(filepath.Separator)) {
		return "", errInvalidPath
	}

	if resolvedJoined, err := filepath.EvalSymlinks(joined); err == nil {
		if resolvedProject, err := filepath.EvalSymlinks(projectClean); err == nil {
			if resolvedJoined != resolvedProject && !strings.HasPrefix(resolvedJoined, resolvedProject+string(filepath.Separator)) {
				return "", errInvalidPath
			}
		}
	}

	return cleaned, nil
}
