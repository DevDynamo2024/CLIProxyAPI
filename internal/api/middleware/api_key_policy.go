package middleware

import (
	"bytes"
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/billing"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	apiKeyPolicyContextKey = "apiKeyPolicy"
)

type priceResolver interface {
	ResolvePriceMicro(ctx context.Context, model string) (billing.PriceMicroUSDPer1M, string, int64, error)
}

// APIKeyPolicyMiddleware enforces per-client API key restrictions and quotas.
// It assumes AuthMiddleware already stored the authenticated key as gin context value "apiKey".
func APIKeyPolicyMiddleware(getConfig func() *config.Config, limiter *policy.SQLiteDailyLimiter, costReader billing.DailyCostReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil || c.Request == nil {
			return
		}
		cfg := (*config.Config)(nil)
		if getConfig != nil {
			cfg = getConfig()
		}
		if cfg == nil {
			c.Next()
			return
		}

		apiKey := strings.TrimSpace(c.GetString("apiKey"))
		if apiKey == "" {
			c.Next()
			return
		}

		if p := cfg.EffectiveAPIKeyPolicy(apiKey); p != nil {
			c.Set(apiKeyPolicyContextKey, p)
		}

		policyValue, _ := c.Get(apiKeyPolicyContextKey)
		policyEntry, _ := policyValue.(*config.APIKeyPolicy)

		// Only enforce request-body model rules for JSON body endpoints.
		// GET /v1/models is handled by response filtering.
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Next()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		model := strings.TrimSpace(gjson.GetBytes(bodyBytes, "model").String())
		if model == "" {
			c.Next()
			return
		}

		requestNow := time.Now()
		// Access controls are evaluated against the client-requested model namespace.
		// Downstream routing/failover targets remain unaffected by excluded-models.
		effectiveModel := model

		// 1) Transparent model downgrade rules.
		if policyEntry != nil && !policyEntry.AllowsClaudeOpus46() {
			if rewritten, changed := policy.DowngradeClaudeOpus46(effectiveModel); changed {
				effectiveModel = rewritten
			}
		}
		budgetModel := effectiveModel
		if policyEntry != nil {
			if routed, decision := policyEntry.RoutedModelFor(apiKey, effectiveModel, requestNow); decision != nil && strings.TrimSpace(routed) != "" {
				budgetModel = routed
			}
		}

		// 2) Model allow/deny checks.
		if policyEntry != nil && len(policyEntry.ExcludedModels) > 0 {
			modelKey := policy.NormaliseModelKey(effectiveModel)
			denied := false
			for _, pattern := range policyEntry.ExcludedModels {
				if policy.MatchWildcard(pattern, modelKey) {
					denied = true
					break
				}
			}
			if denied {
				body := handlers.BuildErrorResponseBody(http.StatusForbidden, "model access denied by api key policy")
				c.Abort()
				c.Data(http.StatusForbidden, "application/json", body)
				return
			}
		}

		// 2.1) Budget checks rely on a known model price; otherwise spend would be silently undercounted.
		if policyEntry != nil && (policyEntry.DailyBudgetUSD > 0 || policyEntry.WeeklyBudgetUSD > 0) {
			if costReader == nil {
				body := handlers.BuildErrorResponseBody(http.StatusInternalServerError, "billing store unavailable")
				c.Abort()
				c.Data(http.StatusInternalServerError, "application/json", body)
				return
			}
			resolver, ok := costReader.(priceResolver)
			if !ok {
				body := handlers.BuildErrorResponseBody(http.StatusInternalServerError, "billing price resolver unavailable")
				c.Abort()
				c.Data(http.StatusInternalServerError, "application/json", body)
				return
			}
			if _, source, _, errPrice := resolver.ResolvePriceMicro(c.Request.Context(), budgetModel); errPrice != nil {
				body := handlers.BuildErrorResponseBody(http.StatusInternalServerError, errPrice.Error())
				c.Abort()
				c.Data(http.StatusInternalServerError, "application/json", body)
				return
			} else if source == "missing" {
				body := handlers.BuildErrorResponseBody(http.StatusServiceUnavailable, "budgeted model price unavailable")
				c.Abort()
				c.Data(http.StatusServiceUnavailable, "application/json", body)
				return
			}
		}

		// 2.2) Daily budget limits (USD) - based on persisted usage cost.
		if policyEntry != nil && policyEntry.DailyBudgetUSD > 0 {
			dayKey := policy.DayKeyChina(requestNow)
			spentMicro, errSpent := costReader.GetDailyCostMicroUSD(c.Request.Context(), apiKey, dayKey)
			if errSpent != nil {
				body := handlers.BuildErrorResponseBody(http.StatusInternalServerError, errSpent.Error())
				c.Abort()
				c.Data(http.StatusInternalServerError, "application/json", body)
				return
			}
			budgetMicro := int64(math.Round(policyEntry.DailyBudgetUSD * 1_000_000))
			if budgetMicro > 0 && spentMicro >= budgetMicro {
				body := handlers.BuildErrorResponseBody(http.StatusTooManyRequests, "daily budget exceeded")
				c.Abort()
				c.Data(http.StatusTooManyRequests, "application/json", body)
				return
			}
		}

		// 2.3) Weekly budget limits (USD) - based on persisted usage cost.
		if policyEntry != nil && policyEntry.WeeklyBudgetUSD > 0 {
			start, end := policyEntry.WeeklyBudgetBounds(requestNow)
			var spentMicro int64
			var errSpent error
			if strings.TrimSpace(policyEntry.WeeklyBudgetAnchorAt) != "" {
				spentMicro, errSpent = costReader.GetCostMicroUSDByTimeRange(c.Request.Context(), apiKey, start, end)
			} else {
				spentMicro, errSpent = costReader.GetCostMicroUSDByDayRange(
					c.Request.Context(),
					apiKey,
					policy.DayKeyChina(start),
					policy.DayKeyChina(end),
				)
			}
			if errSpent != nil {
				body := handlers.BuildErrorResponseBody(http.StatusInternalServerError, errSpent.Error())
				c.Abort()
				c.Data(http.StatusInternalServerError, "application/json", body)
				return
			}
			budgetMicro := int64(math.Round(policyEntry.WeeklyBudgetUSD * 1_000_000))
			if budgetMicro > 0 && spentMicro >= budgetMicro {
				body := handlers.BuildErrorResponseBody(http.StatusTooManyRequests, "weekly budget exceeded")
				c.Abort()
				c.Data(http.StatusTooManyRequests, "application/json", body)
				return
			}
		}

		// 3) Daily usage limits.
		if policyEntry != nil && len(policyEntry.DailyLimits) > 0 {
			modelKey := policy.NormaliseModelKey(effectiveModel)
			limit, limitKey := resolveDailyLimit(policyEntry, modelKey)
			if limit > 0 {
				if limiter == nil {
					body := handlers.BuildErrorResponseBody(http.StatusInternalServerError, "daily limiter unavailable")
					c.Abort()
					c.Data(http.StatusInternalServerError, "application/json", body)
					return
				}
				dayKey := policy.DayKeyChina(requestNow)
				_, allowed, errConsume := limiter.Consume(c.Request.Context(), apiKey, limitKey, dayKey, limit)
				if errConsume != nil {
					body := handlers.BuildErrorResponseBody(http.StatusInternalServerError, errConsume.Error())
					c.Abort()
					c.Data(http.StatusInternalServerError, "application/json", body)
					return
				}
				if !allowed {
					body := handlers.BuildErrorResponseBody(http.StatusTooManyRequests, "daily model limit exceeded")
					c.Abort()
					c.Data(http.StatusTooManyRequests, "application/json", body)
					return
				}
			}
		}

		// If we rewrote the model, patch the request body for downstream handlers.
		if effectiveModel != model {
			modified, errSet := sjson.SetBytes(bodyBytes, "model", effectiveModel)
			if errSet == nil {
				c.Request.Body = io.NopCloser(bytes.NewBuffer(modified))
				c.Request.ContentLength = int64(len(modified))
			}
		}

		c.Next()
	}
}

func resolveDailyLimit(p *config.APIKeyPolicy, modelKey string) (limit int, limitKey string) {
	if p == nil || len(p.DailyLimits) == 0 {
		return 0, ""
	}
	key := strings.ToLower(strings.TrimSpace(modelKey))
	if key == "" {
		return 0, ""
	}
	if v, ok := p.DailyLimits[key]; ok && v > 0 {
		return v, key
	}
	if strings.HasSuffix(key, "-thinking") {
		base := strings.TrimSuffix(key, "-thinking")
		if v, ok := p.DailyLimits[base]; ok && v > 0 {
			return v, base
		}
	}
	return 0, ""
}
