package config

import (
	"hash/fnv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/policy"
)

// APIKeyPolicy defines restrictions and quotas applied to an authenticated client API key.
// The APIKey value must match the authenticated principal as provided by the access manager.
type APIKeyPolicy struct {
	APIKey string `yaml:"api-key" json:"api-key"`

	// UpstreamBaseURL overrides the upstream API base URL for this client API key.
	// When set, /v1/* requests will be transparently proxied to this base URL instead of
	// routing to the configured providers in this server. This is useful for chaining
	// CLIProxyAPI instances (per-client upstream proxy routing).
	//
	// Examples:
	//   - "http://127.0.0.1:8001" (incoming /v1/models -> http://127.0.0.1:8001/v1/models)
	//   - "http://127.0.0.1:8001/v1" (incoming /v1/models -> http://127.0.0.1:8001/v1/models)
	UpstreamBaseURL string `yaml:"upstream-base-url,omitempty" json:"upstream-base-url,omitempty"`

	// ExcludedModels lists model IDs or wildcard patterns that this API key is NOT allowed to access.
	// Matching is case-insensitive. Supports '*' wildcard.
	ExcludedModels []string `yaml:"excluded-models,omitempty" json:"excluded-models,omitempty"`

	// ModelRouting optionally rewrites an incoming request model to a configured target model.
	// This supports deterministic time-window split routing for coherence (e.g. 50% of 1h windows
	// routed to Codex while still presenting the original requested model to the client).
	ModelRouting APIKeyModelRoutingPolicy `yaml:"model-routing,omitempty" json:"model-routing,omitempty"`

	// Failover controls automatic provider failover behaviour for this API key.
	// It allows transparent retry against a configured target model when a provider becomes unavailable
	// (e.g., Claude weekly cap, rolling-window caps, or account issues).
	Failover APIKeyFailoverPolicy `yaml:"failover,omitempty" json:"failover,omitempty"`

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

// APIKeyModelRoutingPolicy groups model routing configuration for a client API key.
type APIKeyModelRoutingPolicy struct {
	Rules []ModelRoutingRule `yaml:"rules,omitempty" json:"rules,omitempty"`
}

// ModelRoutingRule deterministically routes a subset of time windows to a target model.
// Matching is case-insensitive and supports '*' wildcard on from-model.
type ModelRoutingRule struct {
	// Enabled toggles routing for this rule. Defaults to true when unset.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	FromModel string `yaml:"from-model,omitempty" json:"from-model,omitempty"`

	// TargetModel is the model ID to execute when the rule routes to target.
	TargetModel string `yaml:"target-model,omitempty" json:"target-model,omitempty"`

	// TargetPercent is the percentage (0-100) of time windows that should route to TargetModel.
	TargetPercent int `yaml:"target-percent,omitempty" json:"target-percent,omitempty"`

	// StickyWindowSeconds is the routing time window size in seconds. Defaults to 3600 when <= 0.
	StickyWindowSeconds int `yaml:"sticky-window-seconds,omitempty" json:"sticky-window-seconds,omitempty"`
}

type ModelRoutingDecision struct {
	FromModel           string
	TargetModel         string
	TargetPercent       int
	StickyWindowSeconds int
	Bucket              int64
}

// ProviderFailoverPolicy defines per-provider automatic failover settings.
type ProviderFailoverPolicy struct {
	// Enabled toggles failover behaviour for the provider.
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// TargetModel is the model ID to retry when failover triggers (e.g. "gpt-5.2(high)").
	TargetModel string `yaml:"target-model,omitempty" json:"target-model,omitempty"`

	// Rules optionally override the target model based on the requested model.
	// Matching is case-insensitive and supports '*' wildcard.
	Rules []ModelFailoverRule `yaml:"rules,omitempty" json:"rules,omitempty"`
}

// ModelFailoverRule defines a model-specific failover target.
type ModelFailoverRule struct {
	FromModel   string `yaml:"from-model,omitempty" json:"from-model,omitempty"`
	TargetModel string `yaml:"target-model,omitempty" json:"target-model,omitempty"`
}

// APIKeyFailoverPolicy groups failover configuration for a client API key.
// Provider keys match internal provider identifiers (e.g. "claude").
type APIKeyFailoverPolicy struct {
	// Claude controls failover behaviour when the request is routed to the Claude provider.
	Claude ProviderFailoverPolicy `yaml:"claude,omitempty" json:"claude,omitempty"`
}

// RoutedModelFor resolves the effective model that should be executed for a request.
// It returns the target model and a decision object when routing to target is selected.
// When no rule matches or the decision keeps the original model, it returns ("", nil).
func (p *APIKeyPolicy) RoutedModelFor(apiKey, requestedModel string, now time.Time) (string, *ModelRoutingDecision) {
	if p == nil {
		return "", nil
	}
	requestKey := policy.NormaliseModelKey(requestedModel)
	if requestKey == "" || len(p.ModelRouting.Rules) == 0 {
		return "", nil
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		key = strings.TrimSpace(p.APIKey)
	}
	for _, rule := range p.ModelRouting.Rules {
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		from := strings.ToLower(strings.TrimSpace(rule.FromModel))
		if from == "" {
			continue
		}
		if !policy.MatchWildcard(from, requestKey) {
			continue
		}
		target := strings.TrimSpace(rule.TargetModel)
		if target == "" {
			return "", nil
		}

		percent := rule.TargetPercent
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		if percent == 0 {
			return "", nil
		}
		if percent == 100 {
			return target, &ModelRoutingDecision{
				FromModel:           from,
				TargetModel:         target,
				TargetPercent:       percent,
				StickyWindowSeconds: normalizeStickyWindowSeconds(rule.StickyWindowSeconds),
				Bucket:              routingBucket(now, normalizeStickyWindowSeconds(rule.StickyWindowSeconds)),
			}
		}

		window := normalizeStickyWindowSeconds(rule.StickyWindowSeconds)
		bucket := routingBucket(now, window)
		phase := routingPhase(key, requestKey)
		n := bucket + phase
		value := (n * int64(percent)) % 100
		if value < int64(percent) {
			return target, &ModelRoutingDecision{
				FromModel:           from,
				TargetModel:         target,
				TargetPercent:       percent,
				StickyWindowSeconds: window,
				Bucket:              bucket,
			}
		}
		return "", nil
	}
	return "", nil
}

func normalizeStickyWindowSeconds(seconds int) int {
	if seconds <= 0 {
		return 3600
	}
	return seconds
}

func routingBucket(now time.Time, windowSeconds int) int64 {
	if windowSeconds <= 0 {
		windowSeconds = 3600
	}
	sec := now.Unix()
	if sec < 0 {
		sec = 0
	}
	return sec / int64(windowSeconds)
}

func routingPhase(apiKey, requestedModelKey string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(apiKey))))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(requestedModelKey))))
	return int64(h.Sum32() % 100)
}

func (p *APIKeyPolicy) AllowsClaudeOpus46() bool {
	if p == nil || p.AllowClaudeOpus46 == nil {
		return true
	}
	return *p.AllowClaudeOpus46
}

// ClaudeFailoverTargetModel resolves the configured Claude failover target model.
// Returns ("", false) when failover is disabled.
// When enabled but target-model is empty, it returns a safe default.
func (p *APIKeyPolicy) ClaudeFailoverTargetModel() (string, bool) {
	if p == nil {
		return "", false
	}
	if !p.Failover.Claude.Enabled {
		return "", false
	}
	target := strings.TrimSpace(p.Failover.Claude.TargetModel)
	if target == "" {
		target = "gpt-5.2(high)"
	}
	return target, true
}

// ClaudeFailoverTargetModelFor resolves the configured Claude failover target model for a specific request.
// Rules are evaluated first; when no rules match, it falls back to ClaudeFailoverTargetModel().
func (p *APIKeyPolicy) ClaudeFailoverTargetModelFor(requestedModel string) (string, bool) {
	if p == nil {
		return "", false
	}
	if !p.Failover.Claude.Enabled {
		return "", false
	}

	requestKey := policy.NormaliseModelKey(requestedModel)
	if requestKey != "" && len(p.Failover.Claude.Rules) > 0 {
		for _, rule := range p.Failover.Claude.Rules {
			from := strings.ToLower(strings.TrimSpace(rule.FromModel))
			if from == "" {
				continue
			}
			if !policy.MatchWildcard(from, requestKey) {
				continue
			}
			target := strings.TrimSpace(rule.TargetModel)
			if target == "" {
				continue
			}
			return target, true
		}
	}

	return p.ClaudeFailoverTargetModel()
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

		entry.UpstreamBaseURL = strings.TrimSpace(entry.UpstreamBaseURL)

		entry.ExcludedModels = NormalizeExcludedModels(entry.ExcludedModels)

		// Routing sanitization.
		if len(entry.ModelRouting.Rules) > 0 {
			rules := make([]ModelRoutingRule, 0, len(entry.ModelRouting.Rules))
			for _, rule := range entry.ModelRouting.Rules {
				rule.FromModel = strings.TrimSpace(rule.FromModel)
				rule.TargetModel = strings.TrimSpace(rule.TargetModel)
				if rule.TargetPercent < 0 {
					rule.TargetPercent = 0
				}
				if rule.TargetPercent > 100 {
					rule.TargetPercent = 100
				}
				if rule.StickyWindowSeconds < 0 {
					rule.StickyWindowSeconds = 0
				}
				if rule.FromModel == "" || rule.TargetModel == "" {
					continue
				}
				rules = append(rules, rule)
			}
			entry.ModelRouting.Rules = rules
		}

		// Failover sanitization.
		entry.Failover.Claude.TargetModel = strings.TrimSpace(entry.Failover.Claude.TargetModel)
		if len(entry.Failover.Claude.Rules) > 0 {
			rules := make([]ModelFailoverRule, 0, len(entry.Failover.Claude.Rules))
			for _, rule := range entry.Failover.Claude.Rules {
				rule.FromModel = strings.TrimSpace(rule.FromModel)
				rule.TargetModel = strings.TrimSpace(rule.TargetModel)
				if rule.FromModel == "" || rule.TargetModel == "" {
					continue
				}
				rules = append(rules, rule)
			}
			entry.Failover.Claude.Rules = rules
		}

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
