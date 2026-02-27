// Package config provides configuration management for the CLI Proxy API server.
// It handles loading and parsing YAML configuration files, and provides structured
// access to application settings including server port, authentication directory,
// debug settings, proxy configuration, and API keys.
package config

// SDKConfig represents the application's configuration, loaded from a YAML file.
type SDKConfig struct {
	// ProxyURL is the URL of an optional proxy server to use for outbound requests.
	ProxyURL string `yaml:"proxy-url" json:"proxy-url"`

	// AnthropicBaseURL overrides the upstream base URL used for Claude/Anthropic Messages API requests.
	// This is a persisted, UI-manageable alternative to the ANTHROPIC_BASE_URL environment variable.
	// When ProxyURL is configured, callers may prefer direct upstream base URL + proxy over a gateway base URL.
	AnthropicBaseURL string `yaml:"anthropic-base-url,omitempty" json:"anthropic-base-url,omitempty"`

	// AnthropicOAuthAuthURL overrides the OAuth authorization endpoint used to build the login URL.
	// Default: https://claude.ai/oauth/authorize
	AnthropicOAuthAuthURL string `yaml:"anthropic-oauth-auth-url,omitempty" json:"anthropic-oauth-auth-url,omitempty"`

	// AnthropicOAuthTokenURL overrides the OAuth token endpoint used for code exchange and refresh.
	// Default: https://console.anthropic.com/v1/oauth/token
	AnthropicOAuthTokenURL string `yaml:"anthropic-oauth-token-url,omitempty" json:"anthropic-oauth-token-url,omitempty"`

	// ForceModelPrefix requires explicit model prefixes (e.g., "teamA/gemini-3-pro-preview")
	// to target prefixed credentials. When false, unprefixed model requests may use prefixed
	// credentials as well.
	ForceModelPrefix bool `yaml:"force-model-prefix" json:"force-model-prefix"`

	// RequestLog enables or disables detailed request logging functionality.
	RequestLog bool `yaml:"request-log" json:"request-log"`

	// APIKeys is a list of keys for authenticating clients to this proxy server.
	APIKeys []string `yaml:"api-keys" json:"api-keys"`

	// Streaming configures server-side streaming behavior (keep-alives and safe bootstrap retries).
	Streaming StreamingConfig `yaml:"streaming" json:"streaming"`

	// NonStreamKeepAliveInterval controls how often blank lines are emitted for non-streaming responses.
	// <= 0 disables keep-alives. Value is in seconds.
	NonStreamKeepAliveInterval int `yaml:"nonstream-keepalive-interval,omitempty" json:"nonstream-keepalive-interval,omitempty"`
}

// StreamingConfig holds server streaming behavior configuration.
type StreamingConfig struct {
	// KeepAliveSeconds controls how often the server emits SSE heartbeats (": keep-alive\n\n").
	// <= 0 disables keep-alives. Default is 0.
	KeepAliveSeconds int `yaml:"keepalive-seconds,omitempty" json:"keepalive-seconds,omitempty"`

	// BootstrapRetries controls how many times the server may retry a streaming request before any bytes are sent,
	// to allow auth rotation / transient recovery.
	// <= 0 disables bootstrap retries. Default is 0.
	BootstrapRetries int `yaml:"bootstrap-retries,omitempty" json:"bootstrap-retries,omitempty"`
}
