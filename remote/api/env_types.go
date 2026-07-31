package api

// EnvListResponse is returned by GET /projects/{name}/env.
type EnvListResponse struct {
	Files []EnvFileResponse `json:"files"`
}

// EnvFileResponse describes one .env file and its parsed variables.
type EnvFileResponse struct {
	Filename  string           `json:"filename"`
	Variables []EnvVarResponse `json:"variables"`
}

// EnvVarResponse is a single key/value pair in an EnvFileResponse.
type EnvVarResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// PutEnvRequest is the request body for PUT /projects/{name}/env.
type PutEnvRequest struct {
	Filename  string          `json:"filename"`
	Variables []EnvVarRequest `json:"variables"`
}

// EnvVarRequest is a single key/value pair in a PutEnvRequest.
type EnvVarRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
