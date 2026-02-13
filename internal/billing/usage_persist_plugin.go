package billing

import (
	"context"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/policy"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
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

	promptTokens := detail.InputTokens - detail.CachedTokens
	if promptTokens < 0 {
		promptTokens = 0
	}
	completionTokens := detail.OutputTokens + detail.ReasoningTokens

	price, _, _, err := p.store.ResolvePriceMicro(ctx, modelKey)
	if err != nil {
		return
	}
	cost := int64(0)
	cost += costMicroUSD(promptTokens, price.Prompt)
	cost += costMicroUSD(detail.CachedTokens, price.Cached)
	cost += costMicroUSD(completionTokens, price.Completion)

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
}

func boolToInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
