package config

import (
	"testing"
	"time"
)

func boolPtr(v bool) *bool { return &v }

func TestAPIKeyPolicy_RoutedModelFor_TargetPercent100(t *testing.T) {
	p := &APIKeyPolicy{
		APIKey: "k1",
		ModelRouting: APIKeyModelRoutingPolicy{
			Rules: []ModelRoutingRule{
				{
					Enabled:       boolPtr(true),
					FromModel:     "claude-opus-4-6*",
					TargetModel:   "gpt-5.2(high)",
					TargetPercent: 100,
				},
			},
		},
	}

	target, decision := p.RoutedModelFor("k1", "claude-opus-4-6", time.Unix(0, 0))
	if target != "gpt-5.2(high)" {
		t.Fatalf("expected target model, got %q", target)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.TargetPercent != 100 {
		t.Fatalf("expected target percent 100, got %d", decision.TargetPercent)
	}
	if decision.StickyWindowSeconds != 3600 {
		t.Fatalf("expected default sticky window 3600, got %d", decision.StickyWindowSeconds)
	}
}

func TestAPIKeyPolicy_RoutedModelFor_TargetPercent0_DisablesRouting(t *testing.T) {
	p := &APIKeyPolicy{
		APIKey: "k1",
		ModelRouting: APIKeyModelRoutingPolicy{
			Rules: []ModelRoutingRule{
				{
					Enabled:       boolPtr(true),
					FromModel:     "claude-opus-4-6*",
					TargetModel:   "gpt-5.2(high)",
					TargetPercent: 0,
				},
			},
		},
	}

	target, decision := p.RoutedModelFor("k1", "claude-opus-4-6", time.Unix(0, 0))
	if target != "" || decision != nil {
		t.Fatalf("expected no routing, got target=%q decision=%+v", target, decision)
	}
}

func TestAPIKeyPolicy_RoutedModelFor_TargetPercent50_StableWithinWindowAndAlternatesPerBucket(t *testing.T) {
	p := &APIKeyPolicy{
		APIKey: "k1",
		ModelRouting: APIKeyModelRoutingPolicy{
			Rules: []ModelRoutingRule{
				{
					FromModel:           "claude-opus-4-6*",
					TargetModel:         "gpt-5.2(high)",
					TargetPercent:       50,
					StickyWindowSeconds: 3600,
				},
			},
		},
	}

	t0 := time.Unix(0, 0)
	t0b := time.Unix(600, 0)  // same bucket
	t1 := time.Unix(3600, 0)  // next bucket
	t1b := time.Unix(4200, 0) // same bucket as t1
	route0, _ := p.RoutedModelFor("k1", "claude-opus-4-6", t0)
	route0b, _ := p.RoutedModelFor("k1", "claude-opus-4-6", t0b)
	if (route0 != "") != (route0b != "") {
		t.Fatalf("expected stable decision within bucket, got %q vs %q", route0, route0b)
	}
	route1, _ := p.RoutedModelFor("k1", "claude-opus-4-6", t1)
	if (route0 != "") == (route1 != "") {
		t.Fatalf("expected alternating decision across buckets, got %q then %q", route0, route1)
	}
	route1b, _ := p.RoutedModelFor("k1", "claude-opus-4-6", t1b)
	if (route1 != "") != (route1b != "") {
		t.Fatalf("expected stable decision within bucket, got %q vs %q", route1, route1b)
	}
}

func TestAPIKeyPolicy_RoutedModelFor_TargetPercent30_HitsExactRatioPerPeriod(t *testing.T) {
	p := &APIKeyPolicy{
		APIKey: "k1",
		ModelRouting: APIKeyModelRoutingPolicy{
			Rules: []ModelRoutingRule{
				{
					FromModel:           "claude-opus-4-6*",
					TargetModel:         "gpt-5.2(high)",
					TargetPercent:       30,
					StickyWindowSeconds: 3600,
				},
			},
		},
	}

	routed := 0
	for i := 0; i < 10; i++ {
		now := time.Unix(int64(i*3600), 0)
		target, _ := p.RoutedModelFor("k1", "claude-opus-4-6", now)
		if target != "" {
			routed++
		}
	}
	if routed != 3 {
		t.Fatalf("expected 3 routed buckets out of 10, got %d", routed)
	}
}

func TestAPIKeyPolicy_RoutedModelFor_RulePriority(t *testing.T) {
	p := &APIKeyPolicy{
		APIKey: "k1",
		ModelRouting: APIKeyModelRoutingPolicy{
			Rules: []ModelRoutingRule{
				{
					FromModel:     "claude-opus-4-6*",
					TargetModel:   "gpt-5.2(high)",
					TargetPercent: 0, // first match disables routing
				},
				{
					FromModel:     "claude-*",
					TargetModel:   "gpt-5.3-codex(high)",
					TargetPercent: 100,
				},
			},
		},
	}

	target, decision := p.RoutedModelFor("k1", "claude-opus-4-6", time.Unix(0, 0))
	if target != "" || decision != nil {
		t.Fatalf("expected first matching rule to win (no routing), got target=%q decision=%+v", target, decision)
	}
}
