package git

import (
	"errors"
	"path"
	"sort"
	"strconv"
	"strings"
)

// ErrNotFound indicates the requested ref or path does not exist in the repo.
var ErrNotFound = errors.New("git: path not found")

// TreeEntry describes a single entry in a directory listing.
type TreeEntry struct {
	Name string
	Type string // "file" or "directory"
	Size int64
}

// Tree lists the contents of path at ref. An empty path lists the repo root.
func (r *Runner) Tree(repoPath, ref, treePath string) ([]TreeEntry, error) {
	spec := ref
	cleaned := strings.Trim(treePath, "/")
	if cleaned != "" {
		spec = ref + ":" + cleaned
	} else {
		spec = ref + ":"
	}

	out, err := r.Run(repoPath, "ls-tree", "--long", spec)
	if err != nil {
		var gitErr *Error
		if errors.As(err, &gitErr) && !gitErr.Timeout {
			return nil, ErrNotFound
		}
		return nil, err
	}

	entries, err := parseLsTree(out)
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		if (entries[i].Type == "directory") != (entries[j].Type == "directory") {
			return entries[i].Type == "directory"
		}
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

// parseLsTree parses the output of `git ls-tree --long <ref>:<path>`.
// Each line has the form: "<mode> <type> <hash> <size>\t<name>".
func parseLsTree(out string) ([]TreeEntry, error) {
	var entries []TreeEntry
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		tab := strings.Index(line, "\t")
		if tab == -1 {
			continue
		}
		meta := strings.Fields(line[:tab])
		name := line[tab+1:]
		if len(meta) < 4 {
			continue
		}

		entryType := "file"
		if meta[1] == "tree" {
			entryType = "directory"
		}

		var size int64
		if entryType == "file" {
			size, _ = strconv.ParseInt(meta[3], 10, 64)
		}

		entries = append(entries, TreeEntry{
			Name: path.Base(name),
			Type: entryType,
			Size: size,
		})
	}
	return entries, nil
}

// Blob returns the raw content of the file at path at ref. It returns
// ErrNotFound if path does not exist, or if it refers to a directory
// rather than a file.
func (r *Runner) Blob(repoPath, ref, filePath string) ([]byte, error) {
	cleaned := strings.Trim(filePath, "/")
	spec := ref + ":" + cleaned

	objType, err := r.Run(repoPath, "cat-file", "-t", spec)
	if err != nil {
		var gitErr *Error
		if errors.As(err, &gitErr) && !gitErr.Timeout {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if strings.TrimSpace(objType) != "blob" {
		return nil, ErrNotFound
	}

	out, err := r.Run(repoPath, "show", spec)
	if err != nil {
		var gitErr *Error
		if errors.As(err, &gitErr) && !gitErr.Timeout {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return []byte(out), nil
}
