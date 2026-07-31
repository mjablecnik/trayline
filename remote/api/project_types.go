package api

// ProjectSummary is one entry in the GET /projects listing.
type ProjectSummary struct {
	Name                  string  `json:"name"`
	Path                  string  `json:"path"`
	Branch                string  `json:"branch"`
	LastCommit            *Commit `json:"last_commit"`
	HasUncommittedChanges bool    `json:"has_uncommitted_changes"`
}

// ProjectDetail is returned by GET /projects/{name}.
type ProjectDetail struct {
	Name       string   `json:"name"`
	Branch     string   `json:"branch"`
	Branches   []string `json:"branches"`
	RemoteURL  string   `json:"remote_url"`
	LastCommit *Commit  `json:"last_commit"`
}

// TreeResponse is returned by GET /projects/{name}/tree/{ref}/{path...}.
type TreeResponse struct {
	Type    string      `json:"type"`
	Path    string      `json:"path"`
	Entries []TreeEntry `json:"entries"`
}

// TreeEntry is one entry in a directory listing.
type TreeEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "file" or "directory"
	Size int64  `json:"size,omitempty"`
}

// BlobResponse is returned by GET /projects/{name}/blob/{ref}/{path...}.
type BlobResponse struct {
	Type      string  `json:"type"` // "file"
	Path      string  `json:"path"`
	Size      int64   `json:"size"`
	Content   *string `json:"content"` // null if binary/truncated
	Language  string  `json:"language"`
	Binary    bool    `json:"binary,omitempty"`
	Truncated bool    `json:"truncated,omitempty"`
}

// Commit describes a single git commit for project API responses.
type Commit struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"` // ISO 8601
}
