package api

import "strings"

// ComposeMessages transforms an OpenAI messages array into (system, prompt)
// suitable for agent execution via RunOneShot.
//
// Rules:
//   - Messages with role "system" are concatenated (newline-separated) → system param
//   - If exactly one "user" message remains (no assistant messages) → its content
//     is used directly as the prompt, without role-label formatting
//   - Otherwise the remaining user/assistant messages are formatted with role
//     labels, in order: "User:\n{content}\n\nAssistant:\n{content}\n\n..."
//   - Adjacent same-role messages are preserved with individual labels
func ComposeMessages(messages []OpenAIMessage) (system string, prompt string) {
	var systemParts []string
	var rest []OpenAIMessage

	for _, m := range messages {
		if m.Role == "system" {
			systemParts = append(systemParts, m.Content)
			continue
		}
		rest = append(rest, m)
	}
	system = strings.Join(systemParts, "\n")

	if len(rest) == 1 && rest[0].Role == "user" {
		return system, rest[0].Content
	}

	var b strings.Builder
	for _, m := range rest {
		label := "User"
		if m.Role == "assistant" {
			label = "Assistant"
		}
		b.WriteString(label)
		b.WriteString(":\n")
		b.WriteString(m.Content)
		b.WriteString("\n\n")
	}
	prompt = strings.TrimSuffix(b.String(), "\n\n")

	return system, prompt
}
