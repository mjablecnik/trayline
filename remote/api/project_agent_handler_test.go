package api

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
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
