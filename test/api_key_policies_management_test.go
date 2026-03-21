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
		mgmt.GET("/claude-to-gpt-target-family", h.GetClaudeToGPTTargetFamily)
		mgmt.PATCH("/claude-to-gpt-target-family", h.PutClaudeToGPTTargetFamily)
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

func TestPatchAPIKeyPolicies_ClaudeGPTTargetFamily(t *testing.T) {
	h, configPath := newAPIKeyPoliciesHandler(t)
	r := setupAPIKeyPoliciesRouter(h)

	body := `{"api-key":"k1","value":{"claude-gpt-target-family":"gpt-5.2"}}`
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
	if policy == nil || policy.ClaudeGPTTargetFamily != "gpt-5.2" {
		t.Fatalf("claude-gpt-target-family not persisted: %+v", policy)
	}
}

func TestPatchGlobalClaudeToGPTTargetFamily(t *testing.T) {
	h, configPath := newAPIKeyPoliciesHandler(t)
	r := setupAPIKeyPoliciesRouter(h)

	body := `{"value":"gpt-5.2"}`
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/claude-to-gpt-target-family", bytes.NewBufferString(body))
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
	if loaded.ClaudeToGPTTargetFamily != "gpt-5.2" {
		t.Fatalf("claude-to-gpt-target-family=%q", loaded.ClaudeToGPTTargetFamily)
	}

	req = httptest.NewRequest(http.MethodGet, "/v0/management/claude-to-gpt-target-family", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["claude-to-gpt-target-family"] != "gpt-5.2" {
		t.Fatalf("response=%v", resp)
	}
}

func TestPatchAPIKeyPolicies_DisableClaudeFailoverRemovesEnabledFlagFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	initial := `port: 8080
api-key-policies:
  - api-key: k1
    failover:
      claude:
        enabled: true
        target-model: gpt-5.4(medium)
        rules:
          - from-model: "claude-opus-4-6*"
            target-model: "gpt-5.4(high)"
`
	if err := os.WriteFile(configPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	h := management.NewHandler(cfg, configPath, nil)
	r := setupAPIKeyPoliciesRouter(h)

	body := `{"api-key":"k1","value":{"failover":{"claude":{"enabled":false,"target-model":"gpt-5.4(medium)","rules":[{"from-model":"claude-opus-4-6*","target-model":"gpt-5.4(high)"}]}}}}`
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/api-key-policies", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if bytes.Contains(raw, []byte("enabled: true")) {
		t.Fatalf("config still contains enabled: true\n%s", string(raw))
	}
	if bytes.Contains(raw, []byte("target-model: gpt-5.4(medium)")) {
		t.Fatalf("config still contains disabled failover target-model\n%s", string(raw))
	}
	if bytes.Contains(raw, []byte("from-model:")) {
		t.Fatalf("config still contains disabled failover rules\n%s", string(raw))
	}

	loaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig after patch: %v", err)
	}
	policy := loaded.FindAPIKeyPolicy("k1")
	if policy == nil {
		t.Fatalf("missing policy after patch")
	}
	if policy.Failover.Claude.Enabled {
		t.Fatalf("expected failover disabled after reload: %+v", policy.Failover.Claude)
	}
}

func TestPatchAPIKeyPolicies_FailoverProviderBlockReplacesPreviousEnabledState(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	initial := `port: 8080
api-key-policies:
  - api-key: k1
    failover:
      claude:
        enabled: true
        target-model: gpt-5.4(medium)
        rules:
          - from-model: "claude-opus-4-6*"
            target-model: "gpt-5.4(high)"
`
	if err := os.WriteFile(configPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	h := management.NewHandler(cfg, configPath, nil)
	r := setupAPIKeyPoliciesRouter(h)

	// Simulate a legacy frontend that sends a full provider block but omits enabled=false.
	body := `{"api-key":"k1","value":{"failover":{"claude":{"target-model":"gpt-5.4(medium)"}}}}`
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/api-key-policies", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	loaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig after patch: %v", err)
	}
	policy := loaded.FindAPIKeyPolicy("k1")
	if policy == nil {
		t.Fatalf("missing policy after patch")
	}
	if policy.Failover.Claude.Enabled {
		t.Fatalf("expected failover disabled after replacement patch: %+v", policy.Failover.Claude)
	}
	if policy.Failover.Claude.TargetModel != "" {
		t.Fatalf("expected disabled failover target to be cleared, got %q", policy.Failover.Claude.TargetModel)
	}
	if len(policy.Failover.Claude.Rules) != 0 {
		t.Fatalf("expected old failover rules to be cleared, got %+v", policy.Failover.Claude.Rules)
	}
}
