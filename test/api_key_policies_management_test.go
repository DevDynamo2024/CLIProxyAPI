package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func newAPIKeyPoliciesHandler(t *testing.T) (*management.Handler, string) {
	t.Helper()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 8080\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := &config.Config{
		APIKeyPolicies: []config.APIKeyPolicy{
			{APIKey: "k1", WeeklyBudgetUSD: 0},
		},
	}
	cfg.SanitizeAPIKeyPolicies()
	return management.NewHandler(cfg, configPath, nil), configPath
}

func setupAPIKeyPoliciesRouter(h *management.Handler) *gin.Engine {
	r := gin.New()
	mgmt := r.Group("/v0/management")
	{
		mgmt.GET("/api-key-policies", h.GetAPIKeyPolicies)
		mgmt.PATCH("/api-key-policies", h.PatchAPIKeyPolicies)
	}
	return r
}

func TestPatchAPIKeyPolicies_WeeklyBudgetUSD(t *testing.T) {
	h, configPath := newAPIKeyPoliciesHandler(t)
	r := setupAPIKeyPoliciesRouter(h)

	body := `{"api-key":"k1","value":{"weekly-budget-usd":400}}`
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/api-key-policies", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	loaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	policy := loaded.FindAPIKeyPolicy("k1")
	if policy == nil {
		t.Fatalf("missing policy after patch")
	}
	if policy.WeeklyBudgetUSD != 400 {
		t.Fatalf("weekly-budget-usd=%v", policy.WeeklyBudgetUSD)
	}

	req = httptest.NewRequest(http.MethodGet, "/v0/management/api-key-policies", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Items []config.APIKeyPolicy `json:"api-key-policies"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].WeeklyBudgetUSD != 400 {
		t.Fatalf("response=%+v", resp.Items)
	}
}

func TestPatchAPIKeyPolicies_WeeklyBudgetAnchorAt(t *testing.T) {
	h, configPath := newAPIKeyPoliciesHandler(t)
	r := setupAPIKeyPoliciesRouter(h)

	body := `{"api-key":"k1","value":{"weekly-budget-usd":400,"weekly-budget-anchor-at":"2026-03-15T10:37:00+08:00"}}`
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/api-key-policies", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	loaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	policy := loaded.FindAPIKeyPolicy("k1")
	if policy == nil {
		t.Fatalf("missing policy after patch")
	}
	if policy.WeeklyBudgetAnchorAt != "2026-03-15T10:00:00+08:00" {
		t.Fatalf("weekly-budget-anchor-at=%q", policy.WeeklyBudgetAnchorAt)
	}
}

func TestPatchAPIKeyPolicies_ExcludedCategoryPatterns(t *testing.T) {
	h, configPath := newAPIKeyPoliciesHandler(t)
	r := setupAPIKeyPoliciesRouter(h)

	body := `{"api-key":"k1","value":{"excluded-models":["Claude-*","gpt-*","gpt-*"]}}`
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/api-key-policies", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	loaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	policy := loaded.FindAPIKeyPolicy("k1")
	if policy == nil {
		t.Fatalf("missing policy after patch")
	}
	if len(policy.ExcludedModels) != 2 || policy.ExcludedModels[0] != "claude-*" || policy.ExcludedModels[1] != "gpt-*" {
		t.Fatalf("excluded-models=%v", policy.ExcludedModels)
	}
}

func TestPatchAPIKeyPolicies_EnableClaudeModels(t *testing.T) {
	h, configPath := newAPIKeyPoliciesHandler(t)
	r := setupAPIKeyPoliciesRouter(h)

	body := `{"api-key":"k1","value":{"enable-claude-models":true}}`
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/api-key-policies", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	loaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	policy := loaded.FindAPIKeyPolicy("k1")
	if policy == nil || policy.EnableClaudeModels == nil || !*policy.EnableClaudeModels {
		t.Fatalf("enable-claude-models not persisted: %+v", policy)
	}
}
