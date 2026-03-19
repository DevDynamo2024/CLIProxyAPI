package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/requesttrace"
)

func TestResponseWriterExtractAPIRequestIncludesAPIKeyPolicyTrace(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginCtx.Set("API_REQUEST", []byte("=== API REQUEST 1 ===\nBody:\n{\"service_tier\":\"priority\"}\n\n"))
	requesttrace.UpsertAPIKeyPolicyTraceOnGin(ginCtx, func(trace *requesttrace.APIKeyPolicyTrace) {
		trace.APIKey = "client-fast-key"
		trace.FastModeEnabled = true
		trace.FastModeApplied = true
		trace.ServiceTier = "priority"
		trace.Model = "gpt-5.4"
		trace.Source = "executor"
	})

	wrapper := NewResponseWriterWrapper(ginCtx.Writer, nil, &RequestInfo{})
	got := string(wrapper.extractAPIRequest(ginCtx))

	for _, want := range []string{
		"=== API REQUEST 1 ===",
		"=== API KEY POLICY ===",
		"Fast Mode Enabled: true",
		"Fast Mode Applied: true",
		"Service Tier: priority",
		"Model: gpt-5.4",
		"Source: executor",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in API request log: %s", want, got)
		}
	}
}

func TestResponseWriterExtractAPIRequestBuildsPolicySectionWithoutUpstreamRequest(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	requesttrace.UpsertAPIKeyPolicyTraceOnGin(ginCtx, func(trace *requesttrace.APIKeyPolicyTrace) {
		trace.APIKey = "client-fast-key"
		trace.FastModeEnabled = true
		trace.FastModeApplied = false
		trace.Model = "claude-sonnet-4"
		trace.Source = "api_key_policy"
	})

	wrapper := NewResponseWriterWrapper(ginCtx.Writer, nil, &RequestInfo{})
	got := string(wrapper.extractAPIRequest(ginCtx))

	for _, want := range []string{
		"=== API KEY POLICY ===",
		"Fast Mode Enabled: true",
		"Fast Mode Applied: false",
		"Model: claude-sonnet-4",
		"Source: api_key_policy",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in API request log: %s", want, got)
		}
	}
	if strings.Contains(got, "=== API REQUEST") {
		t.Fatalf("did not expect upstream request block, got %s", got)
	}
}
