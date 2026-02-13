package management

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func (h *Handler) GetAPIKeyPolicies(c *gin.Context) {
	policies := []config.APIKeyPolicy(nil)
	if h != nil && h.cfg != nil {
		policies = append([]config.APIKeyPolicy(nil), h.cfg.APIKeyPolicies...)
	}
	c.JSON(http.StatusOK, gin.H{"api-key-policies": policies})
}

func (h *Handler) PutAPIKeyPolicies(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "config unavailable"})
		return
	}

	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	var arr []config.APIKeyPolicy
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.APIKeyPolicy `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}

	h.cfg.APIKeyPolicies = append([]config.APIKeyPolicy(nil), arr...)
	h.cfg.SanitizeAPIKeyPolicies()
	h.persist(c)
}

func (h *Handler) PatchAPIKeyPolicies(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "config unavailable"})
		return
	}

	type policyPatch struct {
		ExcludedModels    *[]string       `json:"excluded-models"`
		AllowClaudeOpus46 *bool           `json:"allow-claude-opus-4-6"`
		DailyLimits       *map[string]int `json:"daily-limits"`
		DailyBudgetUSD    *float64        `json:"daily-budget-usd"`
		APIKey            *string         `json:"api-key"`
	}
	var body struct {
		APIKey string       `json:"api-key"`
		Value  *policyPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	apiKey := strings.TrimSpace(body.APIKey)
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api-key is required"})
		return
	}

	targetIndex := -1
	for i := range h.cfg.APIKeyPolicies {
		if strings.TrimSpace(h.cfg.APIKeyPolicies[i].APIKey) == apiKey {
			targetIndex = i
			break
		}
	}

	entry := config.APIKeyPolicy{APIKey: apiKey}
	if targetIndex >= 0 {
		entry = h.cfg.APIKeyPolicies[targetIndex]
	}

	if body.Value.APIKey != nil {
		trimmed := strings.TrimSpace(*body.Value.APIKey)
		if trimmed == "" {
			if targetIndex >= 0 {
				h.cfg.APIKeyPolicies = append(h.cfg.APIKeyPolicies[:targetIndex], h.cfg.APIKeyPolicies[targetIndex+1:]...)
				h.cfg.SanitizeAPIKeyPolicies()
				h.persist(c)
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "api-key cannot be empty"})
			return
		}
		entry.APIKey = trimmed
	}

	if body.Value.ExcludedModels != nil {
		entry.ExcludedModels = config.NormalizeExcludedModels(*body.Value.ExcludedModels)
	}
	if body.Value.AllowClaudeOpus46 != nil {
		v := *body.Value.AllowClaudeOpus46
		entry.AllowClaudeOpus46 = &v
	}
	if body.Value.DailyLimits != nil {
		entry.DailyLimits = *body.Value.DailyLimits
	}
	if body.Value.DailyBudgetUSD != nil {
		entry.DailyBudgetUSD = *body.Value.DailyBudgetUSD
	}

	if targetIndex >= 0 {
		h.cfg.APIKeyPolicies[targetIndex] = entry
	} else {
		h.cfg.APIKeyPolicies = append(h.cfg.APIKeyPolicies, entry)
	}
	h.cfg.SanitizeAPIKeyPolicies()
	h.persist(c)
}

func (h *Handler) DeleteAPIKeyPolicies(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "config unavailable"})
		return
	}

	apiKey := strings.TrimSpace(c.Query("api-key"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(c.Query("apiKey"))
	}
	if apiKey != "" {
		out := make([]config.APIKeyPolicy, 0, len(h.cfg.APIKeyPolicies))
		for _, v := range h.cfg.APIKeyPolicies {
			if strings.TrimSpace(v.APIKey) == apiKey {
				continue
			}
			out = append(out, v)
		}
		if len(out) == len(h.cfg.APIKeyPolicies) {
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
			return
		}
		h.cfg.APIKeyPolicies = out
		h.cfg.SanitizeAPIKeyPolicies()
		h.persist(c)
		return
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": "missing api-key"})
}
