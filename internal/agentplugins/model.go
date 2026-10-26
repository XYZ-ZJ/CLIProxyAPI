package agentplugins

// Plugin represents an agent skill or tool plugin in the marketplace.
type Plugin struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Version     string   `json:"version"`
	Category    string   `json:"category"`
	SourceID    string   `json:"source_id"`
	Repository  string   `json:"repository,omitempty"`
	DownloadURL string   `json:"download_url,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Installed   bool     `json:"installed"`
}

// Source represents a registered repository or registry of agent plugins.
type Source struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// MarketResponse represents the structured listing response for client consumers.
type MarketResponse struct {
	Sources []Source `json:"sources"`
	Plugins []Plugin `json:"plugins"`
	Total   int      `json:"total"`
}
