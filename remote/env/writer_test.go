package env

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWrite_CreatesFileWithCommentsAndVariables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	vars := []Variable{
		{Key: "FOO", Value: "bar"},
		{Key: "SPACED", Value: "has space"},
		{Key: "EMPTY", Value: ""},
	}
	comments := []string{"# preserved comment"}

	if err := Write(path, vars, comments); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Round-trip through Parse to verify content is readable and correct.
	ef, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse after Write: %v", err)
	}
	if len(ef.Comments) != 1 || ef.Comments[0] != "# preserved comment" {
		t.Errorf("Comments = %v, want [# preserved comment]", ef.Comments)
	}
	if len(ef.Variables) != 3 {
		t.Fatalf("Variables = %+v, want 3 entries", ef.Variables)
	}
	for i, want := range vars {
		if ef.Variables[i] != want {
			t.Errorf("Variables[%d] = %+v, want %+v", i, ef.Variables[i], want)
		}
	}

	// Temp file must not be left behind.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected temp file to be removed, stat err = %v", err)
	}
}

func TestWrite_PreservesExistingFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("OLD=1\n"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if err := Write(path, []Variable{{Key: "NEW", Value: "1"}}, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}
}

func TestWrite_QuotesValuesWithSpecialChars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	if err := Write(path, []Variable{{Key: "V", Value: `has space and "quote"`}}, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "V=\"has space and \\\"quote\\\"\"\n"
	if string(data) != want {
		t.Errorf("content = %q, want %q", data, want)
	}
}
