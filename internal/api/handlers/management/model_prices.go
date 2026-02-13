package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/billing"
)

func (h *Handler) GetModelPrices(c *gin.Context) {
	if h == nil || h.billingStore == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "billing store unavailable"})
		return
	}
	prices, err := h.billingStore.ListModelPrices(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"prices": prices})
}

func (h *Handler) PutModelPrice(c *gin.Context) {
	if h == nil || h.billingStore == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "billing store unavailable"})
		return
	}

	var body struct {
		Model              string   `json:"model"`
		PromptUSDPer1M     *float64 `json:"prompt_usd_per_1m"`
		CompletionUSDPer1M *float64 `json:"completion_usd_per_1m"`
		CachedUSDPer1M     *float64 `json:"cached_usd_per_1m"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	model := strings.TrimSpace(body.Model)
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}
	if body.PromptUSDPer1M == nil || body.CompletionUSDPer1M == nil || body.CachedUSDPer1M == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt_usd_per_1m, completion_usd_per_1m, cached_usd_per_1m are required"})
		return
	}

	price := billing.PriceMicroUSDPer1M{
		Prompt:     billing.USDPer1MToMicroUSDPer1M(*body.PromptUSDPer1M),
		Completion: billing.USDPer1MToMicroUSDPer1M(*body.CompletionUSDPer1M),
		Cached:     billing.USDPer1MToMicroUSDPer1M(*body.CachedUSDPer1M),
	}
	if price.Prompt < 0 || price.Completion < 0 || price.Cached < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prices must be >= 0"})
		return
	}

	if err := h.billingStore.UpsertModelPrice(c.Request.Context(), model, price); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) DeleteModelPrice(c *gin.Context) {
	if h == nil || h.billingStore == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "billing store unavailable"})
		return
	}
	model := strings.TrimSpace(c.Query("model"))
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}
	deleted, err := h.billingStore.DeleteModelPrice(c.Request.Context(), model)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
