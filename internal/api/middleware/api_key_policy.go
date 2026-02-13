package middleware

import (
	"bytes"
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

		if p := cfg.FindAPIKeyPolicy(apiKey); p != nil {
			copyPolicy := *p
			c.Set(apiKeyPolicyContextKey, &copyPolicy)
		}

		policyValue, _ := c.Get(apiKeyPolicyContextKey)
		policyEntry, _ := policyValue.(*config.APIKeyPolicy)

		// Only enforce request-body model rules for JSON body endpoints.
		// GET /v1/models is handled by response filtering.
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		// 0) Daily budget limits (USD) - based on persisted usage cost.
		if policyEntry != nil && policyEntry.DailyBudgetUSD > 0 {
			if costReader == nil {
				body := handlers.BuildErrorResponseBody(http.StatusInternalServerError, "billing store unavailable")
				c.Abort()
				c.Data(http.StatusInternalServerError, "application/json", body)
				return
			}
			dayKey := policy.DayKeyChina(time.Now())
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

		effectiveModel := model

		// 1) Transparent model downgrade rules.
		if policyEntry != nil && !policyEntry.AllowsClaudeOpus46() {
			if rewritten, changed := policy.DowngradeClaudeOpus46(effectiveModel); changed {
				effectiveModel = rewritten
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
				dayKey := policy.DayKeyChina(time.Now())
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
