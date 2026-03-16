package billing

import (
	"context"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/policy"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

type UsagePersistPlugin struct {
	store *SQLiteStore
}

func NewUsagePersistPlugin(store *SQLiteStore) *UsagePersistPlugin {
	return &UsagePersistPlugin{store: store}
}

func (p *UsagePersistPlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if p == nil || p.store == nil {
		return
	}
	apiKey := strings.TrimSpace(record.APIKey)
	if apiKey == "" {
		return
	}
	modelKey := policy.NormaliseModelKey(record.Model)
	if modelKey == "" {
		modelKey = "unknown"
	}

	ts := record.RequestedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	dayKey := policy.DayKeyChina(ts)

	detail := record.Detail
	if detail.TotalTokens == 0 {
		detail.TotalTokens = detail.InputTokens + detail.OutputTokens + detail.ReasoningTokens + detail.CachedTokens
	}
	if detail.TotalTokens < 0 {
		detail.TotalTokens = 0
	}

	price, priceSource, _, err := p.store.ResolvePriceMicro(ctx, modelKey)
	if err != nil {
		return
	}
	if priceSource == "missing" {
		log.WithFields(log.Fields{
			"component": "billing",
			"api_key":   apiKey,
			"model":     modelKey,
		}).Warn("billing price missing for usage record; request will be tracked with zero cost")
	}
	cost := calculateUsageCostMicro(detail.InputTokens, detail.OutputTokens, detail.ReasoningTokens, detail.CachedTokens, price)

	delta := DailyUsageRow{
		Requests:        1,
		FailedRequests:  boolToInt64(record.Failed),
		InputTokens:     max64(0, detail.InputTokens),
		OutputTokens:    max64(0, detail.OutputTokens),
		ReasoningTokens: max64(0, detail.ReasoningTokens),
		CachedTokens:    max64(0, detail.CachedTokens),
		TotalTokens:     max64(0, detail.TotalTokens),
		CostMicroUSD:    max64(0, cost),
	}
	_ = p.store.AddUsage(ctx, apiKey, modelKey, dayKey, delta)
	_ = p.store.AddUsageEvent(ctx, UsageEventRow{
		RequestedAt:     ts.Unix(),
		APIKey:          apiKey,
		Source:          strings.TrimSpace(record.Source),
		AuthIndex:       strings.TrimSpace(record.AuthIndex),
		Model:           modelKey,
		Failed:          record.Failed,
		InputTokens:     max64(0, detail.InputTokens),
		OutputTokens:    max64(0, detail.OutputTokens),
		ReasoningTokens: max64(0, detail.ReasoningTokens),
		CachedTokens:    max64(0, detail.CachedTokens),
		TotalTokens:     max64(0, detail.TotalTokens),
		CostMicroUSD:    max64(0, cost),
	})
}

func boolToInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func boolToSQLiteInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
