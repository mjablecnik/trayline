package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"remote/core"
)

// SpecHandler serves the spec discovery REST endpoint.
type SpecHandler struct {
	config *core.Config
	logger *core.Logger
}

// NewSpecHandler creates a SpecHandler.
func NewSpecHandler(config *core.Config, logger *core.Logger) *SpecHandler {
	return &SpecHandler{config: config, logger: logger}
}

// projectExists reports whether name is a safe project name that resolves to
// an existing directory under config.ProjectsDir. Spec discovery does not
// require the project to be a git repository, only for the directory to
// exist — same rule as PipelineHandler.projectExists.
func (h *SpecHandler) projectExists(name string) bool {
	if name == "" || name == "." || name == ".." || !projectNameRe.MatchString(name) {
		return false
	}
	info, err := os.Stat(filepath.Join(h.config.ProjectsDir, name))
	return err == nil && info.IsDir()
}

// HandleListSpecs handles GET /projects/{name}/specs.
func (h *SpecHandler) HandleListSpecs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !h.projectExists(name) {
		writeProjectNotFound(w, name)
		return
	}

	specsDir := filepath.Join(h.config.ProjectsDir, name, ".kiro", "specs")
	entries, err := os.ReadDir(specsDir)
	if err != nil {
		// Missing .kiro/specs/ directory is not an error — just no specs yet.
		writeJSON(w, http.StatusOK, []SpecSummary{})
		return
	}

	type specWithTime struct {
		name    string
		modTime time.Time
	}
	specs := make([]specWithTime, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		tasksPath := filepath.Join(specsDir, entry.Name(), "tasks.md")
		if !hasUncheckedTasks(tasksPath) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		specs = append(specs, specWithTime{name: entry.Name(), modTime: info.ModTime()})
	}

	sort.Slice(specs, func(i, j int) bool {
		return specs[i].modTime.After(specs[j].modTime)
	})

	resp := make([]SpecSummary, len(specs))
	for i, s := range specs {
		resp[i] = SpecSummary{
			Name:      s.name,
			CreatedAt: s.modTime.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// hasUncheckedTasks reports whether the tasks.md file at path exists and
// contains at least one unchecked task marker "- [ ]".
func hasUncheckedTasks(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "- [ ]")
}
