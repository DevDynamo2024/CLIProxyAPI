package config

import "testing"

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
