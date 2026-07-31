package env

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// needsQuoteRe matches values that require quoting when written back out:
// whitespace, quotes, backslashes, and shell-special characters.
var needsQuoteRe = regexp.MustCompile(`[\s"'\\$` + "`" + `#]`)

// Write atomically writes variables to path, preceded by comments (one per
// line, written verbatim). It writes to a temp file in the same directory
// and renames it into place so readers never observe a partial write. If a
// file already exists at path, its permissions are preserved; otherwise the
// new file is created with mode 0o644.
func Write(path string, variables []Variable, comments []string) error {
	perm := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}

	var sb strings.Builder
	for _, c := range comments {
		sb.WriteString(c)
		sb.WriteString("\n")
	}
	for _, v := range variables {
		sb.WriteString(v.Key)
		sb.WriteString("=")
		sb.WriteString(quoteIfNeeded(v.Value))
		sb.WriteString("\n")
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(sb.String()), perm); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file into place: %w", err)
	}
	return nil
}

// quoteIfNeeded wraps v in double quotes (escaping backslashes and double
// quotes) if it contains whitespace or shell-special characters.
func quoteIfNeeded(v string) string {
	if v == "" || !needsQuoteRe.MatchString(v) {
		return v
	}
	escaped := strings.ReplaceAll(v, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
