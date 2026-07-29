package api

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func tempDir(t *rapid.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "upload-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// Property 1: SaveUploadedFiles stores correct files (1-10 files, valid sizes, content matches)
func TestSaveUploadedFiles_StoresCorrectFiles(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numFiles := rapid.IntRange(1, MaxUploadFileCount).Draw(t, "numFiles")
		workspaceDir := tempDir(t)
		subdir := "task-abc"

		type fileInput struct {
			name    string
			content []byte
		}
		inputs := make([]fileInput, numFiles)
		for i := range inputs {
			// Prefix with index to guarantee unique filenames within the request.
			base := rapid.StringMatching(`[a-zA-Z0-9]{1,15}\.(txt|csv|json)`).Draw(t, "filename")
			name := fmt.Sprintf("%d_%s", i, base)
			size := rapid.IntRange(1, 1024).Draw(t, "size")
			content := rapid.SliceOfN(rapid.Byte(), size, size).Draw(t, "content")
			inputs[i] = fileInput{name: name, content: content}
		}

		headers := make([]*multipart.FileHeader, numFiles)
		for i, inp := range inputs {
			headers[i] = makeFileHeader(t, inp.name, inp.content)
		}

		uploaded, err := SaveUploadedFiles(headers, workspaceDir, subdir, MaxUploadFileSize, MaxUploadFileCount)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(uploaded) != numFiles {
			t.Fatalf("expected %d uploaded files, got %d", numFiles, len(uploaded))
		}

		for i, f := range uploaded {
			fullPath := filepath.Join(workspaceDir, f.WorkspacePath)
			got, err := os.ReadFile(fullPath)
			if err != nil {
				t.Fatalf("could not read saved file %q: %v", fullPath, err)
			}
			if !bytes.Equal(got, inputs[i].content) {
				t.Fatalf("file %q content mismatch", f.OriginalName)
			}
			if f.OriginalName != inputs[i].name {
				t.Fatalf("expected OriginalName %q, got %q", inputs[i].name, f.OriginalName)
			}
		}
	})
}

// Property 2: BuildUploadMetadata format correctness (N entries for N files, correct paths)
func TestBuildUploadMetadata_FormatCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numFiles := rapid.IntRange(0, 10).Draw(t, "numFiles")
		files := make([]UploadedFile, numFiles)
		for i := range files {
			files[i] = UploadedFile{
				OriginalName:  rapid.StringMatching(`[a-zA-Z0-9]{1,20}\.txt`).Draw(t, "name"),
				WorkspacePath: "uploads/sub/" + rapid.StringMatching(`[a-zA-Z0-9]{1,20}\.txt`).Draw(t, "path"),
			}
		}

		metadata := BuildUploadMetadata(files)

		if numFiles == 0 {
			if metadata != "" {
				t.Fatalf("expected empty metadata for 0 files, got %q", metadata)
			}
			return
		}

		if !strings.HasPrefix(metadata, "[Uploaded Files]\n") {
			t.Fatalf("metadata must start with [Uploaded Files] header")
		}

		for _, f := range files {
			entry := "- " + f.OriginalName + " → /workspace/" + f.WorkspacePath
			if !strings.Contains(metadata, entry) {
				t.Fatalf("metadata missing entry for file %q; metadata=%q", f.OriginalName, metadata)
			}
		}

		// Count entries: each line starting with "- " is one entry
		lines := strings.Split(metadata, "\n")
		entryCount := 0
		for _, l := range lines {
			if strings.HasPrefix(l, "- ") {
				entryCount++
			}
		}
		if entryCount != numFiles {
			t.Fatalf("expected %d entries, found %d in metadata", numFiles, entryCount)
		}
	})
}

// Property 4: Count validation rejects and leaves no files on disk
func TestSaveUploadedFiles_CountValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		excess := rapid.IntRange(1, 5).Draw(t, "excess")
		numFiles := MaxUploadFileCount + excess
		workspaceDir := tempDir(t)
		subdir := "task-count"

		headers := make([]*multipart.FileHeader, numFiles)
		for i := range headers {
			headers[i] = makeFileHeader(t, "file.txt", []byte("x"))
		}

		_, err := SaveUploadedFiles(headers, workspaceDir, subdir, MaxUploadFileSize, MaxUploadFileCount)
		if err == nil {
			t.Fatalf("expected error for %d files (limit %d), got nil", numFiles, MaxUploadFileCount)
		}

		// No files should be written
		destDir := filepath.Join(workspaceDir, uploadSubdir, subdir)
		if _, statErr := os.Stat(destDir); !os.IsNotExist(statErr) {
			entries, _ := os.ReadDir(destDir)
			if len(entries) > 0 {
				t.Fatalf("expected no files written on count validation failure, found %d", len(entries))
			}
		}
	})
}

// Property 5: Size validation rejects and leaves no files on disk
func TestSaveUploadedFiles_SizeValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		workspaceDir := tempDir(t)
		subdir := "task-size"

		// One oversized file (report Size > max; content can be small — multipart.FileHeader.Size is what we check)
		bigHeader := &multipart.FileHeader{
			Filename: "big.bin",
			Size:     MaxUploadFileSize + 1,
		}

		numSmall := rapid.IntRange(0, 3).Draw(t, "numSmall")
		headers := make([]*multipart.FileHeader, 0, numSmall+1)
		for i := 0; i < numSmall; i++ {
			headers = append(headers, makeFileHeader(t, "small.txt", []byte("ok")))
		}
		headers = append(headers, bigHeader)

		_, err := SaveUploadedFiles(headers, workspaceDir, subdir, MaxUploadFileSize, MaxUploadFileCount)
		if err == nil {
			t.Fatalf("expected error for oversized file, got nil")
		}

		// No files should be on disk
		destDir := filepath.Join(workspaceDir, uploadSubdir, subdir)
		if _, statErr := os.Stat(destDir); !os.IsNotExist(statErr) {
			entries, _ := os.ReadDir(destDir)
			if len(entries) > 0 {
				t.Fatalf("expected no files written on size validation failure, found %d", len(entries))
			}
		}
	})
}

// Property 6: sanitizeFilename prevents path traversal for any input
func TestSanitizeFilename_PreventsPathTraversal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		input := rapid.StringMatching(`[a-zA-Z0-9./\\_ -]{0,30}`).Draw(t, "filename")
		result := sanitizeFilename(input)

		if result == "" {
			t.Fatalf("sanitizeFilename returned empty string for input %q", input)
		}
		if strings.Contains(result, "/") {
			t.Fatalf("sanitizeFilename result %q contains forward slash for input %q", result, input)
		}

		// Joining with a base dir must not escape it
		base := "/tmp/uploads"
		joined := filepath.Join(base, result)
		if !strings.HasPrefix(joined, base) {
			t.Fatalf("sanitized path %q escapes base dir %q (input=%q)", joined, base, input)
		}
	})
}

// Property 8: Binary frame encode/decode round-trip
func TestBinaryFrame_RoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		filename := rapid.StringMatching(`[a-zA-Z0-9._-]{1,50}`).Draw(t, "filename")
		size := rapid.IntRange(1, 1024).Draw(t, "size")
		content := rapid.SliceOfN(rapid.Byte(), size, size).Draw(t, "content")

		frame := EncodeBinaryFrame(filename, content)
		gotName, gotData, err := DecodeBinaryFrame(frame)
		if err != nil {
			t.Fatalf("DecodeBinaryFrame error: %v", err)
		}
		if gotName != filename {
			t.Fatalf("filename mismatch: want %q got %q", filename, gotName)
		}
		if !bytes.Equal(gotData, content) {
			t.Fatalf("content mismatch after round-trip")
		}
	})
}

// makeFileHeader builds a *multipart.FileHeader backed by real in-memory content.
func makeFileHeader(t *rapid.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("files", filename)
	if err != nil {
		t.Fatalf("createFormFile: %v", err)
	}
	fw.Write(content)
	mw.Close()

	mr := multipart.NewReader(&buf, mw.Boundary())
	form, err := mr.ReadForm(10 * 1024 * 1024)
	if err != nil {
		t.Fatalf("ReadForm: %v", err)
	}
	headers := form.File["files"]
	if len(headers) == 0 {
		t.Fatalf("no file headers found")
	}
	return headers[0]
}
