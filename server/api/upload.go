package api

import (
	"encoding/binary"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxUploadFileSize  = 50 * 1024 * 1024 // 50 MB per file
	MaxUploadFileCount = 10               // max files per request
	uploadSubdir       = "uploads"
)

// UploadValidationError is returned when a file upload request fails validation
// (count or size limits). Distinct from filesystem errors for HTTP status mapping.
type UploadValidationError struct {
	Msg string
}

func (e *UploadValidationError) Error() string { return e.Msg }

// UploadedFile describes a single file that was stored in the workspace.
type UploadedFile struct {
	OriginalName  string // original filename from the upload
	WorkspacePath string // path relative to workspace root (e.g., "uploads/{id}/file.txt")
}

// SaveUploadedFiles validates and saves multipart files to the workspace.
// Returns the list of saved files or an error if validation fails.
// On validation failure, no files are written.
// maxSize is the per-file size limit in bytes; maxCount is the maximum number of files allowed.
func SaveUploadedFiles(files []*multipart.FileHeader, workspaceDir, subdir string, maxSize int64, maxCount int) ([]UploadedFile, error) {
	if len(files) > maxCount {
		return nil, &UploadValidationError{Msg: fmt.Sprintf("request contains %d files, maximum allowed is %d", len(files), maxCount)}
	}

	for _, fh := range files {
		if fh.Size > maxSize {
			return nil, &UploadValidationError{Msg: fmt.Sprintf("file %q exceeds maximum size of %d bytes (got %d bytes)", fh.Filename, maxSize, fh.Size)}
		}
	}

	destDir := filepath.Join(workspaceDir, uploadSubdir, subdir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	var uploaded []UploadedFile
	for _, fh := range files {
		safeName := sanitizeFilename(fh.Filename)
		destPath := filepath.Join(destDir, safeName)

		if err := saveMultipartFile(fh, destPath); err != nil {
			cleanupDir(destDir)
			return nil, fmt.Errorf("failed to save file %q: %w", fh.Filename, err)
		}

		uploaded = append(uploaded, UploadedFile{
			OriginalName:  fh.Filename,
			WorkspacePath: filepath.Join(uploadSubdir, subdir, safeName),
		})
	}

	return uploaded, nil
}

// SaveSingleFile validates and saves a single file from a WebSocket binary frame.
// maxSize is the per-file size limit in bytes.
func SaveSingleFile(filename string, data []byte, workspaceDir, subdir string, maxSize int64) (*UploadedFile, error) {
	if int64(len(data)) > maxSize {
		return nil, fmt.Errorf("file %q exceeds maximum size of %d bytes (got %d bytes)", filename, maxSize, len(data))
	}

	destDir := filepath.Join(workspaceDir, uploadSubdir, subdir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	safeName := sanitizeFilename(filename)
	destPath := filepath.Join(destDir, safeName)

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return &UploadedFile{
		OriginalName:  filename,
		WorkspacePath: filepath.Join(uploadSubdir, subdir, safeName),
	}, nil
}

// BuildUploadMetadata constructs the metadata block to prepend to the prompt.
func BuildUploadMetadata(files []UploadedFile) string {
	if len(files) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[Uploaded Files]\n")
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("- %s → /workspace/%s\n", f.OriginalName, f.WorkspacePath))
	}
	sb.WriteString("\n")
	return sb.String()
}

// CleanupUploadDir removes the upload directory for a task or session.
func CleanupUploadDir(workspaceDir, subdir string) error {
	dir := filepath.Join(workspaceDir, uploadSubdir, subdir)
	return os.RemoveAll(dir)
}

// EncodeBinaryFrame encodes a filename and file content into the WebSocket binary frame format:
// [4 bytes: filename length (big-endian uint32)][N bytes: filename][remaining: file content]
func EncodeBinaryFrame(filename string, data []byte) []byte {
	nameBytes := []byte(filename)
	frame := make([]byte, 4+len(nameBytes)+len(data))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(nameBytes)))
	copy(frame[4:], nameBytes)
	copy(frame[4+len(nameBytes):], data)
	return frame
}

// DecodeBinaryFrame decodes a WebSocket binary frame into filename and file content.
func DecodeBinaryFrame(frame []byte) (filename string, data []byte, err error) {
	if len(frame) < 4 {
		return "", nil, fmt.Errorf("invalid file upload frame format")
	}
	nameLen := binary.BigEndian.Uint32(frame[:4])
	if int(nameLen) > len(frame)-4 {
		return "", nil, fmt.Errorf("invalid file upload frame format")
	}
	filename = string(frame[4 : 4+nameLen])
	data = frame[4+nameLen:]
	return filename, data, nil
}

// sanitizeFilename removes path traversal and unsafe characters from a filename.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	// filepath.Base("/") returns "/" on Linux — strip leading slash
	name = strings.TrimLeft(name, "/\\")
	name = strings.ReplaceAll(name, "..", "_")
	if name == "" || name == "." {
		name = "unnamed"
	}
	return name
}

func saveMultipartFile(fh *multipart.FileHeader, destPath string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func cleanupDir(dir string) {
	_ = os.RemoveAll(dir)
}
