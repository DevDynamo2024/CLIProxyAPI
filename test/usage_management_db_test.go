package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/billing"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/policy"
)

func newUsageDBTestHandler(t *testing.T) (*management.Handler, *billing.SQLiteStore) {
	t.Helper()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 8080\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store, err := billing.NewSQLiteStore(filepath.Join(tmpDir, "billing.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	h := management.NewHandler(&config.Config{}, configPath, nil)
	h.SetBillingStore(store)
	return h, store
}

func setupUsageDBRouter(h *management.Handler) *gin.Engine {
	r := gin.New()
	mgmt := r.Group("/v0/management")
	{
		mgmt.GET("/usage", h.GetUsageStatistics)
		mgmt.GET("/usage/export", h.ExportUsageStatistics)
		mgmt.POST("/usage/import", h.ImportUsageStatistics)
	}
	return r
}

func TestUsageManagement_GetUsageStatistics_UsesDatabaseForToday(t *testing.T) {
	h, store := newUsageDBTestHandler(t)
	defer store.Close()
	r := setupUsageDBRouter(h)

	if err := store.AddUsageEvent(t.Context(), billing.UsageEventRow{
		RequestedAt:  time.Date(2026, 3, 13, 9, 0, 0, 0, policy.ChinaLocation()).Unix(),
		APIKey:       "k1",
		Source:       "openai",
		AuthIndex:    "0",
		Model:        "gpt-5.4",
		InputTokens:  10,
		OutputTokens: 5,
		TotalTokens:  15,
	}); err != nil {
		t.Fatalf("AddUsageEvent: %v", err)
	}
	if err := store.AddUsage(t.Context(), "k1", "gpt-5.4", "2026-03-13", billing.DailyUsageRow{
		Requests:     1,
		InputTokens:  10,
		OutputTokens: 5,
		TotalTokens:  15,
	}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	usageMap, _ := resp["usage"].(map[string]any)
	if got := int64(usageMap["total_requests"].(float64)); got != 1 {
		t.Fatalf("total_requests=%d", got)
	}
}

func TestUsageManagement_ExportImport_UsesDatabase(t *testing.T) {
	h, store := newUsageDBTestHandler(t)
	defer store.Close()
	r := setupUsageDBRouter(h)

	if err := store.AddUsageEvent(t.Context(), billing.UsageEventRow{
		RequestedAt:  time.Date(2026, 3, 13, 11, 0, 0, 0, policy.ChinaLocation()).Unix(),
		APIKey:       "k1",
		Source:       "openai",
		AuthIndex:    "0",
		Model:        "gpt-5.4",
		InputTokens:  20,
		OutputTokens: 10,
		TotalTokens:  30,
	}); err != nil {
		t.Fatalf("AddUsageEvent: %v", err)
	}
	if err := store.AddUsage(t.Context(), "k1", "gpt-5.4", "2026-03-13", billing.DailyUsageRow{
		Requests:     1,
		InputTokens:  20,
		OutputTokens: 10,
		TotalTokens:  30,
	}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", w.Code, w.Body.String())
	}
	exported := w.Body.Bytes()

	freshHandler, freshStore := newUsageDBTestHandler(t)
	defer freshStore.Close()
	freshRouter := setupUsageDBRouter(freshHandler)

	req = httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", bytes.NewReader(exported))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	freshRouter.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", bytes.NewReader(exported))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	freshRouter.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("import second status=%d body=%s", w.Code, w.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal second import: %v", err)
	}
	if got := int64(result["skipped"].(float64)); got != 1 {
		t.Fatalf("skipped=%d", got)
	}
}
