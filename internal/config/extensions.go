package config

// WeChatConfig configures the WeChat iLink direct connection bot service.
type WeChatConfig struct {
	Enabled            bool     `yaml:"enabled" json:"enabled"`
	BaseURL            string   `yaml:"base-url" json:"base-url"`
	Token              string   `yaml:"token" json:"token"`
	AccountID          string   `yaml:"account-id" json:"account-id"`
	AllowAll           bool     `yaml:"allow-all" json:"allow-all"`
	AllowUserIDs       []string `yaml:"allow-user-ids" json:"allow-user-ids"`
	RequirePrefix      bool     `yaml:"require-prefix" json:"require-prefix"`
	Prefix             string   `yaml:"prefix" json:"prefix"`
	DefaultModel       string   `yaml:"default-model" json:"default-model"`
	PollTimeoutSeconds int      `yaml:"poll-timeout-seconds" json:"poll-timeout-seconds"`
	MaxReplyChars      int      `yaml:"max-reply-chars" json:"max-reply-chars"`
	RouteTag           string   `yaml:"route-tag" json:"route-tag"`
}

// AgentPluginsConfig configures the Codex/Agent plugin marketplace and proxy.
type AgentPluginsConfig struct {
	Enabled  bool                `yaml:"enabled" json:"enabled"`
	Sources  []AgentPluginSource `yaml:"sources" json:"sources"`
	CacheDir string              `yaml:"cache-dir" json:"cache-dir"`
}

// AgentPluginSource represents a registry or repository for Codex/Agent plugins.
type AgentPluginSource struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	URL         string `yaml:"url" json:"url"`
	Repository  string `yaml:"repository" json:"repository"`
	Description string `yaml:"description" json:"description"`
}
