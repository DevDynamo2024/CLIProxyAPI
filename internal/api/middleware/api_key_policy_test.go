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
