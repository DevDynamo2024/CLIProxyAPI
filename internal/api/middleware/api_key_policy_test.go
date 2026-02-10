package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
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
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil))
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
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, nil))
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
	r.Use(APIKeyPolicyMiddleware(func() *config.Config { return cfg }, limiter))
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

func boolPtr(v bool) *bool { return &v }
