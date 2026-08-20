package api

import "testing"

// TestComposeMessages_Table covers the full Requirement 11 matrix in one place:
// system extraction and joining, the single-user shortcut, role labelling for
// multi-turn conversations, and adjacent same-role messages.
func TestComposeMessages_Table(t *testing.T) {
	tests := []struct {
		name       string
		messages   []OpenAIMessage
		wantSystem string
		wantPrompt string
	}{
		{
			// Req 11.5: a lone user message is passed through verbatim, with no
			// role labels wrapped around it.
			name:       "single user message passes through unlabelled",
			messages:   []OpenAIMessage{{Role: "user", Content: "Hello"}},
			wantSystem: "",
			wantPrompt: "Hello",
		},
		{
			// Req 11.4: no system message → empty system prompt.
			name: "no system message yields empty system",
			messages: []OpenAIMessage{
				{Role: "user", Content: "A"},
				{Role: "assistant", Content: "B"},
			},
			wantSystem: "",
			wantPrompt: "User:\nA\n\nAssistant:\nB",
		},
		{
			// Req 11.1: system content goes to the system parameter, separate
			// from the conversation prompt.
			name: "system extracted from single user turn",
			messages: []OpenAIMessage{
				{Role: "system", Content: "You are a Go expert"},
				{Role: "user", Content: "What is a goroutine?"},
			},
			wantSystem: "You are a Go expert",
			wantPrompt: "What is a goroutine?",
		},
		{
			// Req 11.1: multiple system messages are joined with a single newline,
			// in array order.
			name: "multiple system messages joined with newline",
			messages: []OpenAIMessage{
				{Role: "system", Content: "First rule"},
				{Role: "user", Content: "Hi"},
				{Role: "system", Content: "Second rule"},
			},
			wantSystem: "First rule\nSecond rule",
			wantPrompt: "Hi",
		},
		{
			// Req 11.2 + 11.3: full multi-turn conversation, labels preserved in
			// array order, system kept out of the prompt.
			name: "multi turn with system",
			messages: []OpenAIMessage{
				{Role: "system", Content: "You are a Go expert"},
				{Role: "user", Content: "What is a goroutine?"},
				{Role: "assistant", Content: "A lightweight thread."},
				{Role: "user", Content: "How do I use channels?"},
			},
			wantSystem: "You are a Go expert",
			wantPrompt: "User:\nWhat is a goroutine?\n\nAssistant:\nA lightweight thread.\n\nUser:\nHow do I use channels?",
		},
		{
			// Req 11.6: consecutive same-role messages each keep their own label
			// rather than being merged.
			name: "adjacent same role messages each labelled",
			messages: []OpenAIMessage{
				{Role: "user", Content: "First"},
				{Role: "user", Content: "Second"},
			},
			wantSystem: "",
			wantPrompt: "User:\nFirst\n\nUser:\nSecond",
		},
		{
			name: "adjacent assistant messages each labelled",
			messages: []OpenAIMessage{
				{Role: "user", Content: "Q"},
				{Role: "assistant", Content: "A1"},
				{Role: "assistant", Content: "A2"},
			},
			wantSystem: "",
			wantPrompt: "User:\nQ\n\nAssistant:\nA1\n\nAssistant:\nA2",
		},
		{
			// A conversation that ends on an assistant turn must not gain a
			// trailing blank line.
			name: "no trailing separator after final message",
			messages: []OpenAIMessage{
				{Role: "user", Content: "Q"},
				{Role: "assistant", Content: "A"},
			},
			wantSystem: "",
			wantPrompt: "User:\nQ\n\nAssistant:\nA",
		},
		{
			// Multi-line content must survive composition untouched.
			name: "multiline content preserved verbatim",
			messages: []OpenAIMessage{
				{Role: "user", Content: "line one\nline two"},
				{Role: "assistant", Content: "ok"},
			},
			wantSystem: "",
			wantPrompt: "User:\nline one\nline two\n\nAssistant:\nok",
		},
		{
			// Non-ASCII content must not be mangled or re-encoded.
			name:       "unicode content preserved",
			messages:   []OpenAIMessage{{Role: "user", Content: "Příliš žluťoučký kůň 🐴"}},
			wantSystem: "",
			wantPrompt: "Příliš žluťoučký kůň 🐴",
		},
		{
			// A system-only array leaves nothing for the prompt.
			name:       "system only yields empty prompt",
			messages:   []OpenAIMessage{{Role: "system", Content: "Be brief"}},
			wantSystem: "Be brief",
			wantPrompt: "",
		},
		{
			// Req 11.5 applies to the user message only: a single assistant
			// message still gets its label.
			name:       "single assistant message keeps its label",
			messages:   []OpenAIMessage{{Role: "assistant", Content: "Solo"}},
			wantSystem: "",
			wantPrompt: "Assistant:\nSolo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			system, prompt := ComposeMessages(tt.messages)
			if system != tt.wantSystem {
				t.Errorf("system:\n got %q\nwant %q", system, tt.wantSystem)
			}
			if prompt != tt.wantPrompt {
				t.Errorf("prompt:\n got %q\nwant %q", prompt, tt.wantPrompt)
			}
		})
	}
}

// TestComposeMessages_DoesNotMutateInput guards against the composer reordering
// or rewriting the caller's slice, which would corrupt the request the handler
// still needs for the response's model echo and logging.
func TestComposeMessages_DoesNotMutateInput(t *testing.T) {
	messages := []OpenAIMessage{
		{Role: "system", Content: "S"},
		{Role: "user", Content: "U"},
		{Role: "assistant", Content: "A"},
	}
	before := make([]OpenAIMessage, len(messages))
	copy(before, messages)

	ComposeMessages(messages)

	for i := range messages {
		if messages[i] != before[i] {
			t.Errorf("input mutated at index %d: got %+v, want %+v", i, messages[i], before[i])
		}
	}
}

// TestComposeMessages_Empty ensures the composer tolerates an empty slice
// without panicking. The handler rejects empty messages before reaching here
// (Req 9.2), but the function must not be a landmine for future callers.
func TestComposeMessages_Empty(t *testing.T) {
	system, prompt := ComposeMessages(nil)
	if system != "" || prompt != "" {
		t.Errorf("got (%q, %q), want two empty strings", system, prompt)
	}
}
