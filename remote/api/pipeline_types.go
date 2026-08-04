package api

// PipelinesResponse is returned by GET /projects/{name}/pipelines.
type PipelinesResponse struct {
	Tasks     []PipelineSummary `json:"tasks"`
	Processes []PipelineSummary `json:"processes"`
	Workflows []PipelineSummary `json:"workflows"`
}

// PipelineSummary describes one pipeline YAML file found under a pipelines
// subdirectory.
type PipelineSummary struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
}

// PipelineDetailResponse is returned by
// GET /projects/{name}/pipelines/{type}/{pipeline}.
type PipelineDetailResponse struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Variables map[string]string `json:"variables"`
}
