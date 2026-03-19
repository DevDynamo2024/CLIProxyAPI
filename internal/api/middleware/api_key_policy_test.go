package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/billing"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/requesttrace"
	"github.com/tidwall/gjson"
)

func TestAPIKeyPolicyMiddleware_DowngradesOpus46(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		APIKeyPolicies: []config.APIKeyPolicy{
			{APIKey: "k", AllowClaudeOpus46: boolPtr(false)},
		},
	}
	cfg.SanitizeAPIKeyPolicies()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil, nil))
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		model := gjson.GetBytes(body, "model").String()
		c.JSON(200, gin.H{"model": model})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"claude-opus-4-6"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := gjson.GetBytes(w.Body.Bytes(), "model").String(); got != "claude-opus-4-5-20251101" {
		t.Fatalf("model=%q", got)
	}
}

func TestAPIKeyPolicyMiddleware_StripsClaudeOpus1MSignalsWhenGloballyDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{DisableClaudeOpus1M: true},
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil, nil))
	r.POST("/v1/messages", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(200, gin.H{
			"beta_header": c.Request.Header.Get("Anthropic-Beta"),
			"claude_1m":   c.Request.Header.Get("X-CPA-CLAUDE-1M"),
			"body_betas":  gjson.GetBytes(body, "betas").Value(),
			"body_model":  gjson.GetBytes(body, "model").String(),
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"claude-opus-4-6","betas":["context-1m-2025-08-07","other-beta"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CPA-CLAUDE-1M", "1")
	req.Header.Set("Anthropic-Beta", "foo, context-1m-2025-08-07, bar")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := gjson.GetBytes(w.Body.Bytes(), "claude_1m").String(); got != "" {
		t.Fatalf("claude_1m=%q", got)
	}
	if got := gjson.GetBytes(w.Body.Bytes(), "beta_header").String(); got != "foo,bar" {
		t.Fatalf("beta_header=%q", got)
	}
	if got := gjson.GetBytes(w.Body.Bytes(), "body_betas.0").String(); got != "other-beta" {
		t.Fatalf("body_betas.0=%q", got)
	}
	if got := gjson.GetBytes(w.Body.Bytes(), "body_betas.#").Int(); got != 1 {
		t.Fatalf("body_betas count=%d", got)
	}
}

func TestAPIKeyPolicyMiddleware_PreservesClaudeOpus1MSignalsForPerKeyOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{DisableClaudeOpus1M: true},
		APIKeyPolicies: []config.APIKeyPolicy{
			{APIKey: "k", EnableClaudeOpus1M: boolPtr(true)},
		},
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil, nil))
	r.POST("/v1/messages", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(200, gin.H{
			"beta_header": c.Request.Header.Get("Anthropic-Beta"),
			"claude_1m":   c.Request.Header.Get("X-CPA-CLAUDE-1M"),
			"body_betas":  gjson.GetBytes(body, "betas").Value(),
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"claude-opus-4-6","betas":["context-1m-2025-08-07","other-beta"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CPA-CLAUDE-1M", "1")
	req.Header.Set("Anthropic-Beta", "foo, context-1m-2025-08-07, bar")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := gjson.GetBytes(w.Body.Bytes(), "claude_1m").String(); got != "1" {
		t.Fatalf("claude_1m=%q", got)
	}
	if got := gjson.GetBytes(w.Body.Bytes(), "beta_header").String(); got != "foo, context-1m-2025-08-07, bar" {
		t.Fatalf("beta_header=%q", got)
	}
	if got := gjson.GetBytes(w.Body.Bytes(), "body_betas.#").Int(); got != 2 {
		t.Fatalf("body_betas count=%d", got)
	}
}

func TestAPIKeyPolicyMiddleware_ExcludedModelDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		APIKeyPolicies: []config.APIKeyPolicy{
			{APIKey: "k", ExcludedModels: []string{"claude-haiku-4-5-20251001"}},
		},
	}
	cfg.SanitizeAPIKeyPolicies()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil, nil))
	r.POST("/v1/messages", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"claude-haiku-4-5-20251001"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIKeyPolicyMiddleware_ExcludedChatGPTWildcardDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		APIKeyPolicies: []config.APIKeyPolicy{
			{APIKey: "k", ExcludedModels: []string{"gpt-*"}},
		},
	}
	cfg.SanitizeAPIKeyPolicies()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil, nil))
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-5.4(high)"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIKeyPolicyMiddleware_FastModeSetsPriorityServiceTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		APIKeyPolicies: []config.APIKeyPolicy{
			{APIKey: "k", FastMode: true},
		},
	}
	cfg.SanitizeAPIKeyPolicies()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil, nil))
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		trace := requesttrace.APIKeyPolicyTraceFromGin(c)
		traceApplied := false
		traceSource := ""
		if trace != nil {
			traceApplied = trace.FastModeApplied
			traceSource = trace.Source
		}
		c.JSON(200, gin.H{
			"model":         gjson.GetBytes(body, "model").String(),
			"service_tier":  gjson.GetBytes(body, "service_tier").String(),
			"trace_applied": traceApplied,
			"trace_source":  traceSource,
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-5.4(high)"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := gjson.GetBytes(w.Body.Bytes(), "service_tier").String(); got != "priority" {
		t.Fatalf("service_tier=%q body=%s", got, w.Body.String())
	}
	if !gjson.GetBytes(w.Body.Bytes(), "trace_applied").Bool() {
		t.Fatalf("expected trace_applied=true body=%s", w.Body.String())
	}
	if got := gjson.GetBytes(w.Body.Bytes(), "trace_source").String(); got != "api_key_policy_middleware" {
		t.Fatalf("trace_source=%q body=%s", got, w.Body.String())
	}
}

func TestAPIKeyPolicyMiddleware_ExcludedRequestedCategoryDoesNotBlockRoutingTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		APIKeyPolicies: []config.APIKeyPolicy{
			{
				APIKey:         "k",
				ExcludedModels: []string{"gpt-*"},
				ModelRouting: config.APIKeyModelRoutingPolicy{
					Rules: []config.ModelRoutingRule{
						{
							FromModel:           "claude-*",
							TargetModel:         "gpt-5.4(high)",
							TargetPercent:       100,
							StickyWindowSeconds: 3600,
						},
					},
				},
			},
		},
	}
	cfg.SanitizeAPIKeyPolicies()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil, nil))
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		model := gjson.GetBytes(body, "model").String()
		c.JSON(200, gin.H{"model": model})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"claude-opus-4-6"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := gjson.GetBytes(w.Body.Bytes(), "model").String(); got != "claude-opus-4-6" {
		t.Fatalf("model=%q", got)
	}
}

func TestAPIKeyPolicyMiddleware_DailyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "limits.sqlite")
	limiter, err := policy.NewSQLiteDailyLimiter(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteDailyLimiter: %v", err)
	}
	defer limiter.Close()

	cfg := &config.Config{
		APIKeyPolicies: []config.APIKeyPolicy{
			{
				APIKey:      "k",
				DailyLimits: map[string]int{"claude-opus-4-6": 1},
			},
		},
	}
	cfg.SanitizeAPIKeyPolicies()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, limiter, nil))
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	makeReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"claude-opus-4-6"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := makeReq(); w.Code != http.StatusOK {
		t.Fatalf("first request status=%d body=%s", w.Code, w.Body.String())
	}
	if w := makeReq(); w.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status=%d body=%s", w.Code, w.Body.String())
	}
}

type stubCostReader struct {
	dailyCost   int64
	weeklyCost  int64
	timeCost    int64
	priceSource string
	err         error
}

func (s stubCostReader) GetDailyCostMicroUSD(ctx context.Context, apiKey, dayKey string) (int64, error) {
	return s.dailyCost, s.err
}

func (s stubCostReader) GetCostMicroUSDByDayRange(ctx context.Context, apiKey, startDay, endDayExclusive string) (int64, error) {
	return s.weeklyCost, s.err
}

func (s stubCostReader) GetCostMicroUSDByTimeRange(ctx context.Context, apiKey string, startInclusive, endExclusive time.Time) (int64, error) {
	return s.timeCost, s.err
}

func (s stubCostReader) ResolvePriceMicro(ctx context.Context, model string) (billing.PriceMicroUSDPer1M, string, int64, error) {
	source := s.priceSource
	if source == "" {
		source = "saved"
	}
	return billing.PriceMicroUSDPer1M{Prompt: 1}, source, 0, s.err
}

func TestAPIKeyPolicyMiddleware_DailyBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		APIKeyPolicies: []config.APIKeyPolicy{
			{APIKey: "k", DailyBudgetUSD: 10},
		},
	}
	cfg.SanitizeAPIKeyPolicies()

	reader := stubCostReader{dailyCost: 10_000_000, priceSource: "saved"}
	var _ billing.DailyCostReader = reader

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil, reader))
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"claude-opus-4-5-20251101"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIKeyPolicyMiddleware_WeeklyBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		APIKeyPolicies: []config.APIKeyPolicy{
			{APIKey: "k", WeeklyBudgetUSD: 400},
		},
	}
	cfg.SanitizeAPIKeyPolicies()

	reader := stubCostReader{weeklyCost: 400_000_000, priceSource: "saved"}
	var _ billing.DailyCostReader = reader

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil, reader))
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"claude-opus-4-5-20251101"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIKeyPolicyMiddleware_WeeklyBudgetAnchoredWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		APIKeyPolicies: []config.APIKeyPolicy{
			{
				APIKey:               "k",
				WeeklyBudgetUSD:      400,
				WeeklyBudgetAnchorAt: "2026-03-15T10:15:00+08:00",
			},
		},
	}
	cfg.SanitizeAPIKeyPolicies()

	reader := stubCostReader{timeCost: 400_000_000, priceSource: "saved"}
	var _ billing.DailyCostReader = reader

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil, reader))
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"claude-opus-4-5-20251101"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	if got := cfg.FindAPIKeyPolicy("k").WeeklyBudgetAnchorAt; got != "2026-03-15T10:00:00+08:00" {
		t.Fatalf("anchor=%q", got)
	}
}

func TestAPIKeyPolicyMiddleware_BudgetedModelRequiresPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		APIKeyPolicies: []config.APIKeyPolicy{
			{APIKey: "k", DailyBudgetUSD: 10},
		},
	}
	cfg.SanitizeAPIKeyPolicies()

	reader := stubCostReader{priceSource: "missing"}
	var _ billing.DailyCostReader = reader

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("apiKey", "k")
		c.Next()
	})
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil, reader))
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"claude-opus-4-5-20251101"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func boolPtr(v bool) *bool { return &v }
