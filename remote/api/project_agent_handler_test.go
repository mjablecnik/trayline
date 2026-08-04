package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"remote/core"
	"remote/docker"
	"remote/store"
)

// Feature: project-ai-agent, Property 3: Forbidden project name characters are rejected
func TestValidateProjectName_RejectsForbiddenCharacters(t *testing.T) {
	forbiddenChars := []rune{
		'/', '\\', ' ', '!', '@', '#', '$', '%', '^', '&', '*', '(', ')',
		'+', '=', '{', '}', '[', ']', '|', ':', ';', '"', '\'', '<', '>', ',', '?', '~', '`',
	}
	h := &ProjectAgentHandler{}

	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.StringMatching(`[a-zA-Z0-9._-]{0,10}`).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[a-zA-Z0-9._-]{0,10}`).Draw(t, "suffix")
		useDotDot := rapid.Bool().Draw(t, "useDotDot")

		var name string
		if useDotDot {
			name = prefix + ".." + suffix
		} else {
			ch := rapid.SampledFrom(forbiddenChars).Draw(t, "forbiddenChar")
			name = prefix + string(ch) + suffix
		}

		err := h.validateProjectName(name)
		if err == nil {
			t.Fatalf("expected validation error for name %q", name)
		}
		if err.Error != "VALIDATION_ERROR" {
			t.Fatalf("expected error code VALIDATION_ERROR for name %q, got %q", name, err.Error)
		}
	})
}

func TestValidateProjectName_AcceptsValidNames(t *testing.T) {
	h := &ProjectAgentHandler{}

	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,39}`).
			Filter(func(s string) bool { return !strings.Contains(s, "..") }).
			Draw(t, "name")

		if err := h.validateProjectName(name); err != nil {
			t.Fatalf("expected no error for valid name %q, got %v", name, err)
		}
	})
}

// Feature: project-ai-agent, Property 2: Invalid agent strings are rejected
func TestValidateAgent_RejectsInvalidStrings(t *testing.T) {
	h := &ProjectAgentHandler{}

	rapid.Check(t, func(t *rapid.T) {
		agent := rapid.String().
			Filter(func(s string) bool { return s != "kiro" && s != "claude" }).
			Draw(t, "agent")

		err := h.validateAgent(agent)
		if err == nil {
			t.Fatalf("expected validation error for agent %q", agent)
		}
		if err.Error != "VALIDATION_ERROR" {
			t.Fatalf("expected error code VALIDATION_ERROR for agent %q, got %q", agent, err.Error)
		}
	})
}

func TestValidateAgent_AcceptsKiroAndClaude(t *testing.T) {
	h := &ProjectAgentHandler{}

	if err := h.validateAgent("kiro"); err != nil {
		t.Errorf("expected no error for \"kiro\", got %v", err)
	}
	if err := h.validateAgent("claude"); err != nil {
		t.Errorf("expected no error for \"claude\", got %v", err)
	}
}

// Feature: project-ai-agent, Property 5: Unrecognized WebSocket message types produce an error
func TestIsKnownMessageType_RejectsUnrecognizedTypes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		msgType := rapid.String().
			Filter(func(s string) bool { return s != "message" && s != "interrupt" && s != "terminate" }).
			Draw(t, "msgType")

		if isKnownMessageType(msgType) {
			t.Fatalf("expected %q to be an unrecognized message type", msgType)
		}
	})
}

func TestIsKnownMessageType_AcceptsRecognizedTypes(t *testing.T) {
	for _, mt := range []string{"message", "interrupt", "terminate"} {
		if !isKnownMessageType(mt) {
			t.Errorf("expected %q to be a recognized message type", mt)
		}
	}
}

// Feature: project-ai-agent, Requirement 12.3: upload metadata prepended to next prompt
func TestBuildProjectUploadMetadata_EmptyForNoFiles(t *testing.T) {
	if got := buildProjectUploadMetadata(nil); got != "" {
		t.Errorf("expected empty string for no uploaded files, got %q", got)
	}
}

func TestBuildProjectUploadMetadata_FormatsEachFile(t *testing.T) {
	files := []store.UploadedFile{
		{OriginalName: "notes.txt", SafeName: "notes.txt"},
		{OriginalName: "data.csv", SafeName: "data.csv"},
	}

	got := buildProjectUploadMetadata(files)

	if !strings.HasPrefix(got, "[Uploaded Files]\n") {
		t.Fatalf("expected metadata to start with header, got %q", got)
	}
	if !strings.Contains(got, "- notes.txt → /tmp/uploads/notes.txt\n") {
		t.Errorf("expected metadata to reference notes.txt, got %q", got)
	}
	if !strings.Contains(got, "- data.csv → /tmp/uploads/data.csv\n") {
		t.Errorf("expected metadata to reference data.csv, got %q", got)
	}
}

// Regression: terminating a session must remove it from the store and
// release its chat slot synchronously with the request, not only once the
// container has finished stopping in the background - otherwise the
// session list still shows it for however long that takes.
func TestHandleTerminateProjectSession_RemovesSessionAndReleasesSlotImmediately(t *testing.T) {
	logger := core.NewLogger("test-token")
	cfg := &core.Config{MaxChatSessions: 1}
	cm := docker.NewContainerManager(noopContainerClient{}, cfg, logger)
	sessionStore := store.NewSessionStore()
	h := NewProjectAgentHandler(sessionStore, cm, logger, cfg, nil)

	if !cm.TryAcquireSlot() {
		t.Fatal("expected to acquire the only available slot")
	}

	sess := &store.Session{
		ID:         "sess-1",
		Project:    "myproject",
		SlotHeld:   true,
		CancelFunc: func() {},
	}
	sessionStore.Add(sess)

	req := httptest.NewRequest(http.MethodPost, "/projects/myproject/sessions/sess-1/terminate", nil)
	req.SetPathValue("name", "myproject")
	req.SetPathValue("id", "sess-1")
	rec := httptest.NewRecorder()

	h.HandleTerminateProjectSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := sessionStore.Get("sess-1"); got != nil {
		t.Errorf("expected session removed from store immediately after terminate, still present: %+v", got)
	}
	if !cm.TryAcquireSlot() {
		t.Error("expected the chat slot to be released immediately, but the pool is still exhausted")
	}
}
