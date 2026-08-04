package api

// EnvListResponse is returned by GET /projects/{name}/env.
type EnvListResponse struct {
	Files []EnvFileResponse `json:"files"`
}

// EnvFileResponse describes one .env file and its parsed variables. Path is
// relative to the project root and "/"-joined, so files with the same
// filename in different directories (e.g. "backend/.env" and
// "frontend/.env") are distinguishable.
type EnvFileResponse struct {
	Path      string           `json:"path"`
	Variables []EnvVarResponse `json:"variables"`
}

// EnvVarResponse is a single key/value pair in an EnvFileResponse.
type EnvVarResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// PutEnvRequest is the request body for PUT /projects/{name}/env.
type PutEnvRequest struct {
	Path      string          `json:"path"`
	Variables []EnvVarRequest `json:"variables"`
}

// EnvVarRequest is a single key/value pair in a PutEnvRequest.
type EnvVarRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
