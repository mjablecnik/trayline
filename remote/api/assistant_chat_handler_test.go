package api

import (
	"os"
	"path/filepath"
	"testing"

	"pgregory.net/rapid"

	"remote/core"
)

func TestValidateAssistantAgent_AcceptsKiroAndClaude(t *testing.T) {
	h := &AssistantHandler{}
	for _, agent := range []string{"kiro", "claude"} {
		if err := h.validateAssistantAgent(agent); err != nil {
			t.Errorf("expected agent %q to be valid, got %v", agent, err)
		}
	}
}

func TestValidateAssistantAgent_RejectsInvalidStrings(t *testing.T) {
	h := &AssistantHandler{}
	for _, agent := range []string{"", "gpt", "Kiro", "claude "} {
		err := h.validateAssistantAgent(agent)
		if err == nil {
			t.Fatalf("expected validation error for agent %q", agent)
		}
		if err.Error != "VALIDATION_ERROR" {
			t.Errorf("expected error code VALIDATION_ERROR for agent %q, got %q", agent, err.Error)
		}
	}
}

func TestAssistantDataDirReady(t *testing.T) {
	dir := t.TempDir()

	h := &AssistantHandler{config: &core.Config{AssistantDataDir: dir}}
	if !h.assistantDataDirReady() {
		t.Error("expected an existing directory to be ready")
	}

	h = &AssistantHandler{config: &core.Config{}}
	if h.assistantDataDirReady() {
		t.Error("expected an unset AssistantDataDir to not be ready")
	}

	h = &AssistantHandler{config: &core.Config{AssistantDataDir: filepath.Join(dir, "missing")}}
	if h.assistantDataDirReady() {
		t.Error("expected a nonexistent path to not be ready")
	}

	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	h = &AssistantHandler{config: &core.Config{AssistantDataDir: file}}
	if h.assistantDataDirReady() {
		t.Error("expected a file (not a directory) to not be ready")
	}
}

// --- Property 4: Invalid agent strings are rejected ---

func TestPropertyValidateAssistantAgentRejectsInvalid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		agent := rapid.String().
			Filter(func(s string) bool { return s != "kiro" && s != "claude" }).
			Draw(t, "agent")

		h := &AssistantHandler{}
		err := h.validateAssistantAgent(agent)
		if err == nil {
			t.Fatalf("validateAssistantAgent(%q) expected error, got nil", agent)
		}
		if err.Error != "VALIDATION_ERROR" {
			t.Errorf("validateAssistantAgent(%q) error code = %q, want VALIDATION_ERROR", agent, err.Error)
		}
	})
}

func TestPropertyValidateAssistantAgentAcceptsValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		agent := rapid.SampledFrom([]string{"kiro", "claude"}).Draw(t, "agent")

		h := &AssistantHandler{}
		if err := h.validateAssistantAgent(agent); err != nil {
			t.Fatalf("validateAssistantAgent(%q) unexpected error: %v", agent, err)
		}
	})
}

// --- Property 14: File upload size validation ---

func TestPropertyValidateUploadFileSize_AcceptsWithinLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		size := rapid.IntRange(0, MaxUploadFileSize).Draw(t, "size")

		if err := validateUploadFileSize("f.txt", size); err != nil {
			t.Fatalf("validateUploadFileSize(size=%d) unexpected error: %v", size, err)
		}
	})
}

func TestPropertyValidateUploadFileSize_RejectsOverLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		over := rapid.IntRange(1, MaxUploadFileSize).Draw(t, "over")
		size := MaxUploadFileSize + over

		err := validateUploadFileSize("f.txt", size)
		if err == nil {
			t.Fatalf("validateUploadFileSize(size=%d) expected error, got nil", size)
		}
	})
}
