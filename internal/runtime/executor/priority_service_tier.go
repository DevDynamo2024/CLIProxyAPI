package executor

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/policy"
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

func fastModeEnabledForContext(ctx context.Context) bool {
	p := apiKeyPolicyFromContext(ctx)
	return p != nil && p.FastModeEnabled()
}

func applyPriorityServiceTier(body []byte, ctx context.Context) []byte {
	if len(body) == 0 || !fastModeEnabledForContext(ctx) {
		return body
	}
	updated, err := sjson.SetBytes(body, "service_tier", "priority")
	if err != nil {
		return body
	}
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
