// Package env parses and writes .env-style files for the dashboard API's
// environment variable endpoints.
package env

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Variable is a single key=value pair from an .env file.
type Variable struct {
	Key   string
	Value string
}

// EnvFile is a parsed .env file: its variables (in original order) plus any
// comment lines, preserved so they can be re-written on the next PUT.
type EnvFile struct {
	Filename  string
	Variables []Variable
	Comments  []string
}

// filenameRe matches valid .env filenames: ".env" optionally followed by a
// dot-prefixed suffix (".env", ".env.example", ".env.prod", ...).
var filenameRe = regexp.MustCompile(`^\.env(\..+)?$`)

// Parse reads and parses the .env file at path. Empty lines are skipped,
// lines starting with "#" are preserved as comments, and all other lines are
// split on the first "=" into a key/value pair. Values may be surrounded by
// matching single or double quotes, which are stripped.
func Parse(path string) (*EnvFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ef := &EnvFile{Filename: filepath.Base(path)}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			ef.Comments = append(ef.Comments, line)
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := unquote(strings.TrimSpace(line[idx+1:]))
		ef.Variables = append(ef.Variables, Variable{Key: key, Value: value})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ef, nil
}

// unquote strips a single matching pair of surrounding single or double quotes.
func unquote(v string) string {
	if len(v) >= 2 {
		first, last := v[0], v[len(v)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// Discover lists projectPath and returns the sorted names of files matching
// the .env* pattern. Returns an empty (non-nil) slice if none exist.
func Discover(projectPath string) ([]string, error) {
	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return nil, err
	}

	names := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filenameRe.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
