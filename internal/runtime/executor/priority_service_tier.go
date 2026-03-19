package executor

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/requesttrace"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func apiKeyPolicyFromContext(ctx context.Context) *config.APIKeyPolicy {
	if ctx == nil {
		return nil
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return nil
	}
	value, exists := ginCtx.Get("apiKeyPolicy")
	if !exists || value == nil {
		return nil
	}
	p, ok := value.(*config.APIKeyPolicy)
	if !ok || p == nil {
		return nil
	}
	return p
}

func applyPriorityServiceTier(body []byte, ctx context.Context) []byte {
	if len(body) == 0 {
		return body
	}
	policyEntry := apiKeyPolicyFromContext(ctx)
	if policyEntry == nil || !policyEntry.FastModeEnabled() {
		return body
	}

	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	requesttrace.UpsertAPIKeyPolicyTraceOnContext(ctx, func(trace *requesttrace.APIKeyPolicyTrace) {
		trace.APIKey = policyEntry.APIKey
		trace.FastModeEnabled = true
		if model != "" {
			trace.Model = model
		}
		if strings.TrimSpace(trace.Source) == "" {
			trace.Source = "api_key_policy"
		}
	})

	updated, err := sjson.SetBytes(body, "service_tier", "priority")
	if err != nil {
		return body
	}
	requesttrace.UpsertAPIKeyPolicyTraceOnContext(ctx, func(trace *requesttrace.APIKeyPolicyTrace) {
		trace.APIKey = policyEntry.APIKey
		trace.FastModeEnabled = true
		trace.FastModeApplied = true
		trace.ServiceTier = "priority"
		if model != "" {
			trace.Model = model
		}
		trace.Source = "executor"
	})
	return updated
}

func modelSupportsPriorityServiceTier(model string) bool {
	key := policy.NormaliseModelKey(model)
	switch {
	case strings.HasPrefix(key, "gpt-"):
		return true
	case strings.HasPrefix(key, "chatgpt-"):
		return true
	case strings.HasPrefix(key, "o1"):
		return true
	case strings.HasPrefix(key, "o3"):
		return true
	case strings.HasPrefix(key, "o4"):
		return true
	default:
		return false
	}
}
