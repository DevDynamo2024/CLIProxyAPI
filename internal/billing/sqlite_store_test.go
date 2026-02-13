package billing

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/policy"
)

func TestSQLiteStore_ModelPrices_DefaultAndOverride(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "billing.sqlite")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	model := "claude-opus-4-5-20251101"

	price, source, _, err := store.ResolvePriceMicro(ctx, model)
	if err != nil {
		t.Fatalf("ResolvePriceMicro: %v", err)
	}
	if source != "default" {
		t.Fatalf("source=%q", source)
	}
	if price.Prompt == 0 || price.Completion == 0 {
		t.Fatalf("unexpected default price: %+v", price)
	}

	override := PriceMicroUSDPer1M{Prompt: 1, Completion: 2, Cached: 3}
	if err := store.UpsertModelPrice(ctx, model, override); err != nil {
		t.Fatalf("UpsertModelPrice: %v", err)
	}
	price2, source2, _, err := store.ResolvePriceMicro(ctx, model)
	if err != nil {
		t.Fatalf("ResolvePriceMicro(override): %v", err)
	}
	if source2 != "saved" {
		t.Fatalf("source=%q", source2)
	}
	if price2 != override {
		t.Fatalf("price=%+v want=%+v", price2, override)
	}
}

func TestSQLiteStore_AddUsageAndDailyCost(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "billing.sqlite")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	apiKey := "k"
	model := "claude-opus-4-5-20251101"
	modelKey := policy.NormaliseModelKey(model)
	day := "2026-02-13"

	if err := store.UpsertModelPrice(ctx, model, PriceMicroUSDPer1M{Prompt: 1_000_000, Completion: 0, Cached: 0}); err != nil {
		t.Fatalf("UpsertModelPrice: %v", err)
	}

	// 2 tokens @ $1 / 1M => 2 micro-USD
	if err := store.AddUsage(ctx, apiKey, modelKey, day, DailyUsageRow{
		Requests:     1,
		InputTokens:  2,
		TotalTokens:  2,
		CostMicroUSD: 2,
	}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
	cost, err := store.GetDailyCostMicroUSD(ctx, apiKey, day)
	if err != nil {
		t.Fatalf("GetDailyCostMicroUSD: %v", err)
	}
	if cost != 2 {
		t.Fatalf("cost=%d", cost)
	}

	report, err := store.GetDailyUsageReport(ctx, apiKey, day)
	if err != nil {
		t.Fatalf("GetDailyUsageReport: %v", err)
	}
	if report.TotalCostMicro != 2 || report.TotalRequests != 1 || report.TotalTokens != 2 {
		t.Fatalf("report=%+v", report)
	}
	if len(report.Models) != 1 {
		t.Fatalf("models=%d", len(report.Models))
	}
}
