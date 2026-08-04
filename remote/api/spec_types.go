package api

// SpecSummary describes one spec directory under a project's .kiro/specs/
// that has at least one unchecked task.
type SpecSummary struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}
