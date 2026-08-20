package api

import (
	"log"
	"strings"
	"time"
)

// defaultModelConfig is used when OPENAI_MODELS is empty or unset.
const defaultModelConfig = "kiro:kiro:,claude:claude:,claude-sonnet:claude:sonnet"

// ModelEntry maps a public model name to internal agent + model combination.
type ModelEntry struct {
	ID      string // public name (e.g., "kiro", "claude-sonnet")
	Agent   string // internal agent ("kiro" or "claude")
	Model   string // model variant passed to agent (e.g., "sonnet", "" for default)
	Created int64  // Unix timestamp (fixed at server start)
}

// ModelRegistry holds the model mappings. Thread-safe (read-only after init).
type ModelRegistry struct {
	entries []ModelEntry
	lookup  map[string]ModelEntry // lowercase(id) → entry
}

// NewModelRegistry creates a registry from config.
// Config format (env var OPENAI_MODELS):
//
//	"kiro:kiro:,claude:claude:,claude-sonnet:claude:sonnet"
//
// Each entry: "public_name:agent:model_variant" (comma-separated). If config
// is empty, defaultModelConfig is used.
func NewModelRegistry(config string) *ModelRegistry {
	if strings.TrimSpace(config) == "" {
		config = defaultModelConfig
	}

	created := time.Now().Unix()
	entries := make([]ModelEntry, 0)
	lookup := make(map[string]ModelEntry)

	for _, raw := range strings.Split(config, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, ":", 3)
		if len(parts) < 2 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		agent := strings.TrimSpace(parts[1])
		if id == "" || agent == "" {
			continue
		}
		model := ""
		if len(parts) == 3 {
			model = strings.TrimSpace(parts[2])
		}

		entry := ModelEntry{ID: id, Agent: agent, Model: model, Created: created}
		entries = append(entries, entry)
		lookup[strings.ToLower(id)] = entry
	}

	if len(entries) == 0 {
		log.Printf("openai: no models configured in OPENAI_MODELS, GET /v1/models will return an empty list")
	}

	return &ModelRegistry{entries: entries, lookup: lookup}
}

// Resolve finds a model entry by name (case-insensitive).
// Returns (entry, true) if found, (zero, false) if not.
func (r *ModelRegistry) Resolve(name string) (ModelEntry, bool) {
	entry, ok := r.lookup[strings.ToLower(name)]
	return entry, ok
}

// List returns all registered models.
func (r *ModelRegistry) List() []ModelEntry {
	return r.entries
}
