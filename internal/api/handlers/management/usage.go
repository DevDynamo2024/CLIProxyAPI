package management

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

type usageExportPayload struct {
	Version    int                      `json:"version"`
	ExportedAt time.Time                `json:"exported_at"`
	Usage      usage.StatisticsSnapshot `json:"usage"`
}

type usageImportPayload struct {
	Version int                      `json:"version"`
	Usage   usage.StatisticsSnapshot `json:"usage"`
}

// GetUsageStatistics returns a merged usage snapshot.
// Historical day-level data is loaded from SQLite, while the current day's
// request-level details remain sourced from in-memory statistics.
func (h *Handler) GetUsageStatistics(c *gin.Context) {
	var snapshot usage.StatisticsSnapshot
	if h != nil && h.usageStats != nil {
		snapshot = h.usageStats.Snapshot()
	}
	if h != nil && h.billingStore != nil {
		historical, err := h.billingStore.BuildHistoricalUsageSnapshot(c.Request.Context(), policy.DayKeyChina(time.Now()))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		snapshot = mergeStatisticsSnapshots(historical, snapshot)
	}
	c.JSON(http.StatusOK, gin.H{
		"usage":           snapshot,
		"failed_requests": snapshot.FailureCount,
	})
}

// ExportUsageStatistics returns a complete usage snapshot for backup/migration.
func (h *Handler) ExportUsageStatistics(c *gin.Context) {
	var snapshot usage.StatisticsSnapshot
	if h != nil && h.usageStats != nil {
		snapshot = h.usageStats.Snapshot()
	}
	c.JSON(http.StatusOK, usageExportPayload{
		Version:    1,
		ExportedAt: time.Now().UTC(),
		Usage:      snapshot,
	})
}

// ImportUsageStatistics merges a previously exported usage snapshot into memory.
func (h *Handler) ImportUsageStatistics(c *gin.Context) {
	if h == nil || h.usageStats == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "usage statistics unavailable"})
		return
	}

	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var payload usageImportPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if payload.Version != 0 && payload.Version != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported version"})
		return
	}

	result := h.usageStats.MergeSnapshot(payload.Usage)
	snapshot := h.usageStats.Snapshot()
	c.JSON(http.StatusOK, gin.H{
		"added":           result.Added,
		"skipped":         result.Skipped,
		"total_requests":  snapshot.TotalRequests,
		"failed_requests": snapshot.FailureCount,
	})
}

func mergeStatisticsSnapshots(base, overlay usage.StatisticsSnapshot) usage.StatisticsSnapshot {
	result := usage.StatisticsSnapshot{
		TotalRequests:  base.TotalRequests + overlay.TotalRequests,
		SuccessCount:   base.SuccessCount + overlay.SuccessCount,
		FailureCount:   base.FailureCount + overlay.FailureCount,
		TotalTokens:    base.TotalTokens + overlay.TotalTokens,
		APIs:           make(map[string]usage.APISnapshot, len(base.APIs)+len(overlay.APIs)),
		RequestsByDay:  make(map[string]int64, len(base.RequestsByDay)+len(overlay.RequestsByDay)),
		RequestsByHour: make(map[string]int64, len(base.RequestsByHour)+len(overlay.RequestsByHour)),
		TokensByDay:    make(map[string]int64, len(base.TokensByDay)+len(overlay.TokensByDay)),
		TokensByHour:   make(map[string]int64, len(base.TokensByHour)+len(overlay.TokensByHour)),
	}

	for key, value := range base.RequestsByDay {
		result.RequestsByDay[key] = value
	}
	for key, value := range overlay.RequestsByDay {
		result.RequestsByDay[key] += value
	}
	for key, value := range base.RequestsByHour {
		result.RequestsByHour[key] = value
	}
	for key, value := range overlay.RequestsByHour {
		result.RequestsByHour[key] += value
	}
	for key, value := range base.TokensByDay {
		result.TokensByDay[key] = value
	}
	for key, value := range overlay.TokensByDay {
		result.TokensByDay[key] += value
	}
	for key, value := range base.TokensByHour {
		result.TokensByHour[key] = value
	}
	for key, value := range overlay.TokensByHour {
		result.TokensByHour[key] += value
	}

	copyAPI := func(value usage.APISnapshot) usage.APISnapshot {
		next := usage.APISnapshot{
			TotalRequests: value.TotalRequests,
			SuccessCount:  value.SuccessCount,
			FailureCount:  value.FailureCount,
			TotalTokens:   value.TotalTokens,
			Models:        make(map[string]usage.ModelSnapshot, len(value.Models)),
		}
		for model, modelValue := range value.Models {
			details := make([]usage.RequestDetail, len(modelValue.Details))
			copy(details, modelValue.Details)
			next.Models[model] = usage.ModelSnapshot{
				TotalRequests: modelValue.TotalRequests,
				SuccessCount:  modelValue.SuccessCount,
				FailureCount:  modelValue.FailureCount,
				TotalTokens:   modelValue.TotalTokens,
				Details:       details,
			}
		}
		return next
	}

	for apiKey, apiValue := range base.APIs {
		result.APIs[apiKey] = copyAPI(apiValue)
	}
	for apiKey, apiValue := range overlay.APIs {
		current, exists := result.APIs[apiKey]
		if !exists {
			result.APIs[apiKey] = copyAPI(apiValue)
			continue
		}

		current.TotalRequests += apiValue.TotalRequests
		current.SuccessCount += apiValue.SuccessCount
		current.FailureCount += apiValue.FailureCount
		current.TotalTokens += apiValue.TotalTokens
		if current.Models == nil {
			current.Models = map[string]usage.ModelSnapshot{}
		}
		for model, modelValue := range apiValue.Models {
			currentModel := current.Models[model]
			currentModel.TotalRequests += modelValue.TotalRequests
			currentModel.SuccessCount += modelValue.SuccessCount
			currentModel.FailureCount += modelValue.FailureCount
			currentModel.TotalTokens += modelValue.TotalTokens
			currentModel.Details = append(currentModel.Details, modelValue.Details...)
			current.Models[model] = currentModel
		}
		result.APIs[apiKey] = current
	}

	return result
}
