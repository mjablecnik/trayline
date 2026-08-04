package env

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestParse_KeyValueAndComments(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, ".env", "# top comment\nFOO=bar\n\nBAZ=qux\n# another comment\nQUOTED=\"hello world\"\nSINGLE='it''s'\n")

	ef, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	wantVars := []Variable{
		{Key: "FOO", Value: "bar"},
		{Key: "BAZ", Value: "qux"},
		{Key: "QUOTED", Value: "hello world"},
		{Key: "SINGLE", Value: "it''s"},
	}
	if !reflect.DeepEqual(ef.Variables, wantVars) {
		t.Errorf("Variables = %+v, want %+v", ef.Variables, wantVars)
	}

	wantComments := []string{"# top comment", "# another comment"}
	if !reflect.DeepEqual(ef.Comments, wantComments) {
		t.Errorf("Comments = %+v, want %+v", ef.Comments, wantComments)
	}

	if ef.Filename != ".env" {
		t.Errorf("Filename = %q, want %q", ef.Filename, ".env")
	}
}

func TestParse_PreservesKeyOrder(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, ".env", "Z=1\nA=2\nM=3\n")

	ef, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var keys []string
	for _, v := range ef.Variables {
		keys = append(keys, v.Key)
	}
	want := []string{"Z", "A", "M"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("key order = %v, want %v", keys, want)
	}
}

func TestParse_MissingFile(t *testing.T) {
	if _, err := Parse(filepath.Join(t.TempDir(), ".env")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDiscover_FiltersAndSorts(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".env", ".env.example", ".env.prod", "notenv.txt", ".environment"} {
		writeTempFile(t, dir, name, "")
	}
	if err := os.Mkdir(filepath.Join(dir, ".env.dir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	want := []string{".env", ".env.example", ".env.prod"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Discover = %v, want %v", got, want)
	}
}

func TestDiscover_FindsNestedFiles(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, ".env", "")
	for _, sub := range []string{"backend", "frontend/config"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	writeTempFile(t, filepath.Join(dir, "backend"), ".env", "")
	writeTempFile(t, filepath.Join(dir, "backend"), ".env.local", "")
	writeTempFile(t, filepath.Join(dir, "frontend", "config"), ".env.prod", "")

	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	want := []string{".env", "backend/.env", "backend/.env.local", "frontend/config/.env.prod"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Discover = %v, want %v", got, want)
	}
}

func TestDiscover_SkipsDependencyDirectories(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, ".env", "")
	for _, skipped := range []string{".git", "node_modules", "vendor", ".venv", "venv"} {
		sub := filepath.Join(dir, skipped)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", skipped, err)
		}
		writeTempFile(t, sub, ".env", "")
	}

	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	want := []string{".env"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Discover = %v, want %v (dependency dirs should be skipped)", got, want)
	}
}

func TestDiscover_EmptyWhenNoEnvFiles(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "README.md", "")

	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("Discover = %v, want empty non-nil slice", got)
	}
}
