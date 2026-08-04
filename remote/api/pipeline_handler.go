package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"remote/core"

	"gopkg.in/yaml.v3"
)

// pipelineSubdirs are the pipeline-type subdirectories scanned under
// config.PipelinesDir, in the order they appear in PipelinesResponse.
var pipelineSubdirs = []string{"tasks", "processes", "workflows"}

// PipelineHandler serves the pipeline discovery REST endpoints.
type PipelineHandler struct {
	config *core.Config
	logger *core.Logger
}

// NewPipelineHandler creates a PipelineHandler.
func NewPipelineHandler(config *core.Config, logger *core.Logger) *PipelineHandler {
	return &PipelineHandler{config: config, logger: logger}
}

// projectExists reports whether name is a safe project name that resolves to
// an existing directory under config.ProjectsDir. Unlike GitHandler's
// resolveProjectPath, pipeline discovery does not require the project to be a
// git repository — it only needs the directory to exist.
func (h *PipelineHandler) projectExists(name string) bool {
	if name == "" || name == "." || name == ".." || !projectNameRe.MatchString(name) {
		return false
	}
	info, err := os.Stat(filepath.Join(h.config.ProjectsDir, name))
	return err == nil && info.IsDir()
}

// HandleListPipelines handles GET /projects/{name}/pipelines.
func (h *PipelineHandler) HandleListPipelines(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !h.projectExists(name) {
		writeProjectNotFound(w, name)
		return
	}

	resp := PipelinesResponse{
		Tasks:     []PipelineSummary{},
		Processes: []PipelineSummary{},
		Workflows: []PipelineSummary{},
	}
	for _, pipelineType := range pipelineSubdirs {
		summaries := h.listPipelinesOfType(pipelineType)
		switch pipelineType {
		case "tasks":
			resp.Tasks = summaries
		case "processes":
			resp.Processes = summaries
		case "workflows":
			resp.Workflows = summaries
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// listPipelinesOfType scans config.PipelinesDir/pipelineType for *.yaml files
// and returns their summaries. A missing subdirectory yields an empty slice,
// not an error — not every project's pipelines directory has all three
// subdirectories populated.
func (h *PipelineHandler) listPipelinesOfType(pipelineType string) []PipelineSummary {
	dir := filepath.Join(h.config.PipelinesDir, pipelineType)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []PipelineSummary{}
	}

	summaries := make([]PipelineSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		name, displayName := pipelineNameFromFilename(entry.Name())
		summaries = append(summaries, PipelineSummary{
			Name:        name,
			Type:        pipelineType,
			DisplayName: displayName,
		})
	}
	return summaries
}

// pipelineNameFromFilename derives a pipeline's name and display_name from
// its YAML filename, per design.md's Property 1: name is the filename
// without its ".yaml" extension, and display_name is name with every "-"
// replaced by a space.
func pipelineNameFromFilename(filename string) (name, displayName string) {
	name = strings.TrimSuffix(filename, ".yaml")
	displayName = strings.ReplaceAll(name, "-", " ")
	return name, displayName
}

// HandleGetPipelineDetail handles
// GET /projects/{name}/pipelines/{type}/{pipeline}.
func (h *PipelineHandler) HandleGetPipelineDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !h.projectExists(name) {
		writeProjectNotFound(w, name)
		return
	}

	pipelineType := r.PathValue("type")
	if !isValidPipelineType(pipelineType) {
		writeJSON(w, http.StatusBadRequest, core.ErrorResponse{
			Error:   "VALIDATION_ERROR",
			Message: fmt.Sprintf("invalid pipeline type %q: must be one of tasks, processes, workflows", pipelineType),
		})
		return
	}

	pipeline := r.PathValue("pipeline")
	if pipeline == "" || pipeline == "." || pipeline == ".." || !projectNameRe.MatchString(pipeline) {
		writeJSON(w, http.StatusNotFound, core.ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("pipeline %q not found", pipeline),
		})
		return
	}

	filePath := filepath.Join(h.config.PipelinesDir, pipelineType, pipeline+".yaml")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			h.logger.Error(r.Context(), "pipeline read error: "+err.Error())
		}
		writeJSON(w, http.StatusNotFound, core.ErrorResponse{
			Error:   "NOT_FOUND",
			Message: fmt.Sprintf("pipeline %q not found", pipeline),
		})
		return
	}

	variables, err := parsePipelineVariables(data)
	if err != nil {
		h.logger.Error(r.Context(), "pipeline parse error: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to parse pipeline file",
		})
		return
	}

	writeJSON(w, http.StatusOK, PipelineDetailResponse{
		Name:      pipeline,
		Type:      pipelineType,
		Variables: variables,
	})
}

// parsePipelineVariables parses the top-level "variables" section of a
// pipeline YAML file into a string map. Values are decoded as YAML scalars of
// any type (pipeline authors sometimes write unquoted booleans, e.g.
// "skip-seo-audit: true" in workflows/maintenance.yaml, alongside the more
// common quoted-string defaults) and then stringified, since workflow
// variables are always plain strings by the time they reach --var flags. A
// pipeline file with no "variables" key yields an empty, non-nil map.
func parsePipelineVariables(data []byte) (map[string]string, error) {
	var doc struct {
		Variables map[string]interface{} `yaml:"variables"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	variables := make(map[string]string, len(doc.Variables))
	for key, value := range doc.Variables {
		variables[key] = fmt.Sprint(value)
	}
	return variables, nil
}
