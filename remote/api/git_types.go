package api

// CommitsResponse is returned by GET /projects/{name}/commits.
type CommitsResponse struct {
	Commits []CommitSummary `json:"commits"`
	Total   int             `json:"total"`
	HasMore bool            `json:"has_more"`
}

// CommitSummary is one entry in a commit log listing.
type CommitSummary struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"short_hash"`
	Message   string `json:"message"`
	Author    string `json:"author"`
	Date      string `json:"date"`
}

// CommitDetailResponse is returned by GET /projects/{name}/commits/{hash}.
type CommitDetailResponse struct {
	Hash         string `json:"hash"`
	ShortHash    string `json:"short_hash"`
	Message      string `json:"message"`
	Author       string `json:"author"`
	Date         string `json:"date"`
	FilesChanged int    `json:"files_changed"`
	Insertions   int    `json:"insertions"`
	Deletions    int    `json:"deletions"`
	Diff         string `json:"diff"`
}

// StatusResponse is returned by GET /projects/{name}/status.
type StatusResponse struct {
	Clean   bool          `json:"clean"`
	Files   []FileStatus  `json:"files"`
	Summary StatusSummary `json:"summary"`
}

// FileStatus describes one changed file in the working tree.
type FileStatus struct {
	Path       string  `json:"path"`
	Status     string  `json:"status"`
	Insertions int     `json:"insertions"`
	Deletions  int     `json:"deletions"`
	Diff       *string `json:"diff"`
}

// StatusSummary aggregates counts across all changed files.
type StatusSummary struct {
	FilesChanged int `json:"files_changed"`
	Insertions   int `json:"insertions"`
	Deletions    int `json:"deletions"`
}
