package api

import (
	"testing"
	"time"
)

// TestModelRegistry_ResolveCaseInsensitive covers Req 4.2: any casing of a
// registered name resolves to the same entry.
func TestModelRegistry_ResolveCaseInsensitive(t *testing.T) {
	r := NewModelRegistry("kiro:kiro:,claude-sonnet:claude:sonnet")

	for _, name := range []string{"kiro", "Kiro", "KIRO", "kIrO"} {
		entry, ok := r.Resolve(name)
		if !ok {
			t.Fatalf("Resolve(%q): not found", name)
		}
		if entry.ID != "kiro" || entry.Agent != "kiro" || entry.Model != "" {
			t.Errorf("Resolve(%q) = %+v, want ID=kiro Agent=kiro Model=\"\"", name, entry)
		}
	}

	entry, ok := r.Resolve("CLAUDE-SONNET")
	if !ok {
		t.Fatal("Resolve(\"CLAUDE-SONNET\"): not found")
	}
	if entry.Agent != "claude" || entry.Model != "sonnet" {
		t.Errorf("got Agent=%q Model=%q, want claude/sonnet", entry.Agent, entry.Model)
	}
}

// TestModelRegistry_ResolveIdempotent covers design Property 1: repeated
// resolution of the same name always yields the same entry.
func TestModelRegistry_ResolveIdempotent(t *testing.T) {
	r := NewModelRegistry("")

	first, ok := r.Resolve("claude-sonnet")
	if !ok {
		t.Fatal("Resolve: not found")
	}
	for i := 0; i < 100; i++ {
		got, ok := r.Resolve("Claude-Sonnet")
		if !ok || got != first {
			t.Fatalf("iteration %d: got (%+v, %v), want (%+v, true)", i, got, ok, first)
		}
	}
}

// TestModelRegistry_MalformedConfig covers Req 4.4/4.5: operator typos in
// OPENAI_MODELS must be skipped rather than panicking or producing junk entries.
func TestModelRegistry_MalformedConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantIDs []string
	}{
		{"entry without colon skipped", "kiro:kiro:,brokenentry", []string{"kiro"}},
		{"empty name skipped", ":kiro:,ok:claude:", []string{"ok"}},
		{"empty agent skipped", "bad::,ok:claude:", []string{"ok"}},
		{"trailing comma tolerated", "kiro:kiro:,", []string{"kiro"}},
		{"repeated commas tolerated", "kiro:kiro:,,,claude:claude:", []string{"kiro", "claude"}},
		{"surrounding whitespace trimmed", " kiro : kiro : , claude:claude: ", []string{"kiro", "claude"}},
		{"model variant may contain colons", "x:claude:a:b", nil}, // documented below
		{"only junk yields empty registry", "garbage,,,more-garbage", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewModelRegistry(tt.config)
			if tt.wantIDs == nil {
				// SplitN(..., 3) means a third colon lands inside the model
				// variant rather than creating a fourth field. Pin that
				// behaviour so a future parser change is a deliberate choice.
				entry, ok := r.Resolve("x")
				if !ok {
					t.Fatal("Resolve(\"x\"): not found")
				}
				if entry.Model != "a:b" {
					t.Errorf("Model = %q, want %q", entry.Model, "a:b")
				}
				return
			}
			got := r.List()
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("List() = %+v, want %d entries", got, len(tt.wantIDs))
			}
			for i, id := range tt.wantIDs {
				if got[i].ID != id {
					t.Errorf("entry %d: ID = %q, want %q", i, got[i].ID, id)
				}
			}
		})
	}
}

// TestModelRegistry_ListPreservesOrder checks that GET /v1/models returns models
// in the order the operator configured them, which is the only ordering
// guarantee clients can reasonably rely on.
func TestModelRegistry_ListPreservesOrder(t *testing.T) {
	r := NewModelRegistry("zebra:kiro:,alpha:claude:,middle:claude:sonnet")

	want := []string{"zebra", "alpha", "middle"}
	got := r.List()
	if len(got) != len(want) {
		t.Fatalf("List() returned %d entries, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("entry %d: ID = %q, want %q", i, got[i].ID, id)
		}
	}
}

// TestModelRegistry_CreatedTimestamp covers Req 3.2: every model object carries
// a plausible Unix timestamp, fixed at registry construction.
func TestModelRegistry_CreatedTimestamp(t *testing.T) {
	before := time.Now().Unix()
	r := NewModelRegistry("")
	after := time.Now().Unix()

	entries := r.List()
	if len(entries) == 0 {
		t.Fatal("default registry is empty")
	}
	for _, e := range entries {
		if e.Created < before || e.Created > after {
			t.Errorf("model %q: Created = %d, want within [%d, %d]", e.ID, e.Created, before, after)
		}
	}
	// All entries share the single construction-time timestamp.
	for _, e := range entries[1:] {
		if e.Created != entries[0].Created {
			t.Errorf("model %q: Created = %d, want %d (same for all entries)", e.ID, e.Created, entries[0].Created)
		}
	}
}

// TestModelRegistry_DefaultsMatchSpec pins the documented default mapping so a
// silent change to defaultModelConfig cannot break clients that rely on it.
func TestModelRegistry_DefaultsMatchSpec(t *testing.T) {
	for _, config := range []string{"", "   ", "\t\n"} {
		r := NewModelRegistry(config)
		want := map[string]ModelEntry{
			"kiro":          {ID: "kiro", Agent: "kiro", Model: ""},
			"claude":        {ID: "claude", Agent: "claude", Model: ""},
			"claude-sonnet": {ID: "claude-sonnet", Agent: "claude", Model: "sonnet"},
		}
		if len(r.List()) != len(want) {
			t.Fatalf("config %q: List() = %+v, want %d entries", config, r.List(), len(want))
		}
		for id, w := range want {
			got, ok := r.Resolve(id)
			if !ok {
				t.Fatalf("config %q: Resolve(%q): not found", config, id)
			}
			if got.ID != w.ID || got.Agent != w.Agent || got.Model != w.Model {
				t.Errorf("config %q: Resolve(%q) = %+v, want %+v", config, id, got, w)
			}
		}
	}
}
