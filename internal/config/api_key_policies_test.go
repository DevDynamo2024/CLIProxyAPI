package config

import (
	"testing"
	"time"
)

func TestConfig_SanitizeAPIKeyPolicies_WeeklyBudgetDisabledWhenNonPositive(t *testing.T) {
	cfg := &Config{
		APIKeyPolicies: []APIKeyPolicy{
			{APIKey: "k1", WeeklyBudgetUSD: -1},
			{APIKey: "k2", WeeklyBudgetUSD: 400},
		},
	}

	cfg.SanitizeAPIKeyPolicies()

	if got := cfg.FindAPIKeyPolicy("k1"); got == nil || got.WeeklyBudgetUSD != 0 {
		t.Fatalf("k1 weekly budget = %v", got)
	}
	if got := cfg.FindAPIKeyPolicy("k2"); got == nil || got.WeeklyBudgetUSD != 400 {
		t.Fatalf("k2 weekly budget = %v", got)
	}
}

func TestConfig_SanitizeAPIKeyPolicies_NormalizesWeeklyBudgetAnchor(t *testing.T) {
	cfg := &Config{
		APIKeyPolicies: []APIKeyPolicy{
			{APIKey: "k1", WeeklyBudgetUSD: 400, WeeklyBudgetAnchorAt: "2026-03-15T10:37:12+08:00"},
			{APIKey: "k2", WeeklyBudgetUSD: 400, WeeklyBudgetAnchorAt: "invalid"},
		},
	}

	cfg.SanitizeAPIKeyPolicies()

	if got := cfg.FindAPIKeyPolicy("k1"); got == nil || got.WeeklyBudgetAnchorAt != "2026-03-15T10:00:00+08:00" {
		t.Fatalf("k1 anchor = %v", got)
	}
	if got := cfg.FindAPIKeyPolicy("k2"); got == nil || got.WeeklyBudgetAnchorAt != "" {
		t.Fatalf("k2 anchor = %v", got)
	}
}

func TestConfig_ShouldRouteClaudeToGPT_DefaultsToAllKeysWhenGlobalEnabled(t *testing.T) {
	cfg := &Config{SDKConfig: SDKConfig{ClaudeToGPTRoutingEnabled: true}}

	if !cfg.ShouldRouteClaudeToGPT("k1") {
		t.Fatal("expected global Claude -> GPT routing to apply to keys without explicit policy")
	}
}

func TestConfig_ShouldRouteClaudeToGPT_RespectsPerKeyClaudeEnable(t *testing.T) {
	cfg := &Config{
		SDKConfig: SDKConfig{ClaudeToGPTRoutingEnabled: true},
		APIKeyPolicies: []APIKeyPolicy{
			{APIKey: "k1", EnableClaudeModels: boolPtr(true)},
			{APIKey: "k2"},
		},
	}

	if cfg.ShouldRouteClaudeToGPT("k1") {
		t.Fatal("expected k1 to bypass global Claude -> GPT routing")
	}
	if !cfg.ShouldRouteClaudeToGPT("k2") {
		t.Fatal("expected k2 to inherit global Claude -> GPT routing")
	}
}

func TestConfig_AllowsClaudeOpus1M_DefaultsEnabledWhenGlobalDisabled(t *testing.T) {
	cfg := &Config{}

	if !cfg.AllowsClaudeOpus1M("k1") {
		t.Fatal("expected Opus 1M to remain enabled when the global switch is off")
	}
}

func TestConfig_AllowsClaudeOpus1M_RespectsPerKeyOverride(t *testing.T) {
	cfg := &Config{
		SDKConfig: SDKConfig{DisableClaudeOpus1M: true},
		APIKeyPolicies: []APIKeyPolicy{
			{APIKey: "k1", EnableClaudeOpus1M: boolPtr(true)},
			{APIKey: "k2"},
		},
	}

	if !cfg.AllowsClaudeOpus1M("k1") {
		t.Fatal("expected k1 to override the global Opus 1M disable switch")
	}
	if cfg.AllowsClaudeOpus1M("k2") {
		t.Fatal("expected k2 to inherit the global Opus 1M disable switch")
	}
	if cfg.AllowsClaudeOpus1M("k3") {
		t.Fatal("expected unknown keys to inherit the global Opus 1M disable switch")
	}
}

func TestConfig_EffectiveAPIKeyPolicy_AddsGlobalClaudeRoutingRules(t *testing.T) {
	cfg := &Config{SDKConfig: SDKConfig{ClaudeToGPTRoutingEnabled: true}}

	policy := cfg.EffectiveAPIKeyPolicy("k1")
	if policy == nil {
		t.Fatal("expected synthesized policy")
	}
	if len(policy.ModelRouting.Rules) < 2 {
		t.Fatalf("expected synthesized routing rules, got %+v", policy.ModelRouting.Rules)
	}

	target, decision := policy.RoutedModelFor("k1", "claude-opus-4-6", time.Unix(0, 0))
	if decision == nil || target != "gpt-5.4(high)" {
		t.Fatalf("expected opus routing to high, got target=%q decision=%+v", target, decision)
	}

	target, decision = policy.RoutedModelFor("k1", "claude-sonnet-4-6", time.Unix(0, 0))
	if decision == nil || target != "gpt-5.4(medium)" {
		t.Fatalf("expected sonnet routing to medium, got target=%q decision=%+v", target, decision)
	}
}

func TestAPIKeyPolicy_ClaudeFailoverTargetModel_DefaultsToMedium(t *testing.T) {
	policy := &APIKeyPolicy{
		Failover: APIKeyFailoverPolicy{
			Claude: ProviderFailoverPolicy{Enabled: true},
		},
	}

	target, ok := policy.ClaudeFailoverTargetModel()
	if !ok {
		t.Fatal("expected failover target to be enabled")
	}
	if target != "gpt-5.4(medium)" {
		t.Fatalf("expected default failover target gpt-5.4(medium), got %q", target)
	}
}
