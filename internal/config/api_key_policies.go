package config

import (
	"strings"
)

// APIKeyPolicy defines restrictions and quotas applied to an authenticated client API key.
// The APIKey value must match the authenticated principal as provided by the access manager.
type APIKeyPolicy struct {
	APIKey string `yaml:"api-key" json:"api-key"`

	// ExcludedModels lists model IDs or wildcard patterns that this API key is NOT allowed to access.
	// Matching is case-insensitive. Supports '*' wildcard.
	ExcludedModels []string `yaml:"excluded-models,omitempty" json:"excluded-models,omitempty"`

	// AllowClaudeOpus46 controls whether requests may use claude-opus-4-6.
	// When false, the server will transparently downgrade claude-opus-4-6* to claude-opus-4-5-20251101*.
	// Defaults to true when unset.
	AllowClaudeOpus46 *bool `yaml:"allow-claude-opus-4-6,omitempty" json:"allow-claude-opus-4-6,omitempty"`

	// DailyLimits defines per-model daily request limits for this API key.
	// Key is a model ID (case-insensitive). Values <= 0 are treated as disabled and dropped.
	DailyLimits map[string]int `yaml:"daily-limits,omitempty" json:"daily-limits,omitempty"`

	// DailyBudgetUSD defines the maximum daily spend (USD) allowed for this API key.
	// Values <= 0 are treated as disabled.
	DailyBudgetUSD float64 `yaml:"daily-budget-usd,omitempty" json:"daily-budget-usd,omitempty"`
}

func (p *APIKeyPolicy) AllowsClaudeOpus46() bool {
	if p == nil || p.AllowClaudeOpus46 == nil {
		return true
	}
	return *p.AllowClaudeOpus46
}

// FindAPIKeyPolicy returns the APIKeyPolicy matching the provided key.
// It returns nil when no policy is configured or the key is blank.
func (cfg *Config) FindAPIKeyPolicy(apiKey string) *APIKeyPolicy {
	if cfg == nil {
		return nil
	}
	key := strings.TrimSpace(apiKey)
	if key == "" || len(cfg.APIKeyPolicies) == 0 {
		return nil
	}
	for i := range cfg.APIKeyPolicies {
		if strings.TrimSpace(cfg.APIKeyPolicies[i].APIKey) == key {
			return &cfg.APIKeyPolicies[i]
		}
	}
	return nil
}

// SanitizeAPIKeyPolicies trims keys, normalizes excluded-model patterns, and drops invalid limits.
func (cfg *Config) SanitizeAPIKeyPolicies() {
	if cfg == nil || len(cfg.APIKeyPolicies) == 0 {
		return
	}

	type indexEntry struct {
		idx int
	}
	seen := make(map[string]indexEntry, len(cfg.APIKeyPolicies))
	out := make([]APIKeyPolicy, 0, len(cfg.APIKeyPolicies))

	for i := range cfg.APIKeyPolicies {
		entry := cfg.APIKeyPolicies[i]
		entry.APIKey = strings.TrimSpace(entry.APIKey)
		if entry.APIKey == "" {
			continue
		}

		entry.ExcludedModels = NormalizeExcludedModels(entry.ExcludedModels)

		if len(entry.DailyLimits) > 0 {
			normalized := make(map[string]int, len(entry.DailyLimits))
			for modelID, limit := range entry.DailyLimits {
				m := strings.ToLower(strings.TrimSpace(modelID))
				if m == "" {
					continue
				}
				if limit <= 0 {
					continue
				}
				normalized[m] = limit
			}
			if len(normalized) > 0 {
				entry.DailyLimits = normalized
			} else {
				entry.DailyLimits = nil
			}
		}

		if entry.DailyBudgetUSD <= 0 {
			entry.DailyBudgetUSD = 0
		}

		key := entry.APIKey
		if prior, ok := seen[key]; ok {
			out[prior.idx] = entry
			continue
		}
		seen[key] = indexEntry{idx: len(out)}
		out = append(out, entry)
	}

	cfg.APIKeyPolicies = out
}
