package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	"github.com/tidwall/gjson"
)

// ---------------------------------------------------------------------------
// Unit tests for rewriteResponseModelFields
// ---------------------------------------------------------------------------

func TestRewriteResponseModelFields_TopLevelModel(t *testing.T) {
	input := []byte(`{"id":"msg_123","model":"gpt-5.2","content":[],"usage":{}}`)
	result := rewriteResponseModelFields(input, "claude-opus-4-6")

	if got := gjson.GetBytes(result, "model").String(); got != "claude-opus-4-6" {
		t.Errorf("expected model=claude-opus-4-6, got %q", got)
	}
	// other fields untouched
	if got := gjson.GetBytes(result, "id").String(); got != "msg_123" {
		t.Errorf("expected id preserved, got %q", got)
	}
}

func TestRewriteResponseModelFields_MessageModel(t *testing.T) {
	input := []byte(`{"type":"message_start","message":{"model":"gpt-5.2-high","role":"assistant"}}`)
	result := rewriteResponseModelFields(input, "claude-opus-4-6")

	if got := gjson.GetBytes(result, "message.model").String(); got != "claude-opus-4-6" {
		t.Errorf("expected message.model=claude-opus-4-6, got %q", got)
	}
}

func TestRewriteResponseModelFields_BothPaths(t *testing.T) {
	// a response that has both top-level and nested model (unlikely but tests both paths)
	input := []byte(`{"model":"gpt-5.2","message":{"model":"gpt-5.2"}}`)
	result := rewriteResponseModelFields(input, "claude-opus-4-6")

	if got := gjson.GetBytes(result, "model").String(); got != "claude-opus-4-6" {
		t.Errorf("expected top-level model rewritten, got %q", got)
	}
	if got := gjson.GetBytes(result, "message.model").String(); got != "claude-opus-4-6" {
		t.Errorf("expected message.model rewritten, got %q", got)
	}
}

func TestRewriteResponseModelFields_NoModelField(t *testing.T) {
	input := []byte(`{"type":"content_block_delta","delta":{"text":"hello"}}`)
	result := rewriteResponseModelFields(input, "claude-opus-4-6")

	if !bytes.Equal(result, input) {
		t.Errorf("expected no modification when no model field, got %s", string(result))
	}
}

func TestRewriteResponseModelFields_EmptyModel(t *testing.T) {
	input := []byte(`{"model":"gpt-5.2"}`)
	result := rewriteResponseModelFields(input, "")

	if !bytes.Equal(result, input) {
		t.Errorf("expected no modification when target model is empty, got %s", string(result))
	}
}

func TestRewriteResponseModelFields_EmptyData(t *testing.T) {
	result := rewriteResponseModelFields(nil, "claude-opus-4-6")
	if result != nil {
		t.Errorf("expected nil for nil input, got %s", string(result))
	}

	result = rewriteResponseModelFields([]byte{}, "claude-opus-4-6")
	if len(result) != 0 {
		t.Errorf("expected empty for empty input, got %s", string(result))
	}
}

// ---------------------------------------------------------------------------
// Unit tests for rewriteStreamChunkModelFields
// ---------------------------------------------------------------------------

func TestRewriteStreamChunkModelFields_RawJSON(t *testing.T) {
	input := []byte(`{"type":"message_start","message":{"model":"gpt-5.2","role":"assistant"}}`)
	result := rewriteStreamChunkModelFields(input, "claude-opus-4-6")

	if got := gjson.GetBytes(result, "message.model").String(); got != "claude-opus-4-6" {
		t.Errorf("expected message.model=claude-opus-4-6, got %q", got)
	}
}

func TestRewriteStreamChunkModelFields_SSEFormat(t *testing.T) {
	input := []byte("data: {\"type\":\"message_start\",\"message\":{\"model\":\"gpt-5.2\",\"role\":\"assistant\"}}\n\n")
	result := rewriteStreamChunkModelFields(input, "claude-opus-4-6")

	// Should still be valid SSE format
	if !bytes.HasPrefix(result, []byte("data: ")) {
		t.Error("expected SSE data: prefix preserved")
	}

	// Extract JSON from SSE line
	jsonData := bytes.TrimPrefix(bytes.Split(result, []byte("\n"))[0], []byte("data: "))
	if got := gjson.GetBytes(jsonData, "message.model").String(); got != "claude-opus-4-6" {
		t.Errorf("expected message.model=claude-opus-4-6 in SSE, got %q", got)
	}
}

func TestRewriteStreamChunkModelFields_SSEMultipleEvents(t *testing.T) {
	input := []byte("data: {\"type\":\"message_start\",\"message\":{\"model\":\"gpt-5.2\"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hi\"}}\n\n")
	result := rewriteStreamChunkModelFields(input, "claude-opus-4-6")

	// first event should have model rewritten
	if !bytes.Contains(result, []byte(`"model":"claude-opus-4-6"`)) {
		t.Errorf("expected model rewritten in first event, got %s", string(result))
	}
	// second event should be untouched (no model field)
	if !bytes.Contains(result, []byte(`"text":"hi"`)) {
		t.Errorf("expected second event preserved, got %s", string(result))
	}
}

func TestRewriteStreamChunkModelFields_NoModelField(t *testing.T) {
	input := []byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hello\"}}\n\n")
	result := rewriteStreamChunkModelFields(input, "claude-opus-4-6")

	if !bytes.Equal(result, input) {
		t.Errorf("expected no modification when no model field in SSE, got %s", string(result))
	}
}

func TestRewriteStreamChunkModelFields_EmptyInputs(t *testing.T) {
	if result := rewriteStreamChunkModelFields(nil, "claude-opus-4-6"); result != nil {
		t.Errorf("expected nil for nil input")
	}
	if result := rewriteStreamChunkModelFields([]byte("data: {\"model\":\"x\"}"), ""); !bytes.Equal(result, []byte("data: {\"model\":\"x\"}")) {
		t.Errorf("expected no modification for empty target model")
	}
}

func TestRewriteStreamChunkModelFields_NonJSONData(t *testing.T) {
	input := []byte("data: [DONE]\n\n")
	result := rewriteStreamChunkModelFields(input, "claude-opus-4-6")

	if !bytes.Equal(result, input) {
		t.Errorf("expected no modification for non-JSON SSE data, got %s", string(result))
	}
}

// ---------------------------------------------------------------------------
// Integration: non-streaming failover rewrites model in response
// ---------------------------------------------------------------------------

func TestExecuteWithAuthManager_FailoverRewritesModelInResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)

	// Claude executor fails with 429
	manager.RegisterExecutor(&failStatusExecutor{id: "claude", status: http.StatusTooManyRequests, msg: "weekly cap"})
	// Codex executor succeeds with a response containing gpt-5.2 model field
	failoverResp := []byte(`{"id":"msg_abc","model":"gpt-5.2","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}]}`)
	manager.RegisterExecutor(&okExecutor{id: "codex", payload: failoverResp})

	claudeAuth := &coreauth.Auth{ID: "claude-auth-mr", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), claudeAuth); err != nil {
		t.Fatalf("register claude: %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "codex-auth-mr", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("register codex: %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(claudeAuth.ID, claudeAuth.Provider, []*registry.ModelInfo{{ID: "claude-opus-4-6"}})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.2"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(claudeAuth.ID)
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{}`)))
	c.Set("apiKey", "client-key")
	c.Set("apiKeyPolicy", &internalconfig.APIKeyPolicy{
		APIKey: "client-key",
		Failover: internalconfig.APIKeyFailoverPolicy{
			Claude: internalconfig.ProviderFailoverPolicy{
				Enabled:     true,
				TargetModel: "gpt-5.2(high)",
			},
		},
	})

	ctx := context.WithValue(context.Background(), "gin", c)
	payload := []byte(`{"model":"claude-opus-4-6","stream":false}`)
	resp, errMsg := handler.ExecuteWithAuthManager(ctx, "claude", "claude-opus-4-6", payload, "")
	if errMsg != nil {
		t.Fatalf("expected nil error, got: %+v", errMsg)
	}

	// The response must have model rewritten to the original requested model
	gotModel := gjson.GetBytes(resp, "model").String()
	if gotModel != "claude-opus-4-6" {
		t.Errorf("expected response model=claude-opus-4-6, got %q (failover model leaked)", gotModel)
	}

	// Verify the rest of the response is intact
	if got := gjson.GetBytes(resp, "id").String(); got != "msg_abc" {
		t.Errorf("expected id=msg_abc, got %q", got)
	}
	if !json.Valid(resp) {
		t.Errorf("response is not valid JSON: %s", string(resp))
	}
}

// ---------------------------------------------------------------------------
// Integration: streaming failover rewrites model in chunked response
// ---------------------------------------------------------------------------

func TestExecuteStreamWithAuthManager_FailoverRewritesModelInChunks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)

	// Claude executor fails with 429
	manager.RegisterExecutor(&failStatusExecutor{id: "claude", status: http.StatusTooManyRequests, msg: "rolling cap"})
	// Codex executor succeeds with a streaming chunk containing gpt-5.2 model
	streamChunk := []byte(`{"type":"message_start","message":{"model":"gpt-5.2","role":"assistant","content":[]}}`)
	manager.RegisterExecutor(&okExecutor{id: "codex", payload: streamChunk})

	claudeAuth := &coreauth.Auth{ID: "claude-auth-smr", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), claudeAuth); err != nil {
		t.Fatalf("register claude: %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "codex-auth-smr", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("register codex: %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(claudeAuth.ID, claudeAuth.Provider, []*registry.ModelInfo{{ID: "claude-opus-4-6"}})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.2"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(claudeAuth.ID)
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{}`)))
	c.Set("apiKey", "client-key")
	c.Set("apiKeyPolicy", &internalconfig.APIKeyPolicy{
		APIKey: "client-key",
		Failover: internalconfig.APIKeyFailoverPolicy{
			Claude: internalconfig.ProviderFailoverPolicy{
				Enabled:     true,
				TargetModel: "gpt-5.2(high)",
			},
		},
	})

	ctx := context.WithValue(context.Background(), "gin", c)
	payload := []byte(`{"model":"claude-opus-4-6","stream":true}`)
	dataChan, errChan := handler.ExecuteStreamWithAuthManager(ctx, "claude", "claude-opus-4-6", payload, "")
	if dataChan == nil || errChan == nil {
		t.Fatalf("expected non-nil channels")
	}

	var got []byte
	for chunk := range dataChan {
		got = append(got, chunk...)
	}
	for msg := range errChan {
		if msg != nil {
			t.Fatalf("unexpected error: %+v", msg)
		}
	}

	// The streamed chunk must have model rewritten
	gotModel := gjson.GetBytes(got, "message.model").String()
	if gotModel != "claude-opus-4-6" {
		t.Errorf("expected streamed message.model=claude-opus-4-6, got %q (failover model leaked)", gotModel)
	}
}

// ---------------------------------------------------------------------------
// Integration: unknown-provider failover rewrites model in response
// ---------------------------------------------------------------------------

func TestExecuteWithAuthManager_UnknownProviderFailoverRewritesModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)

	// No claude executor registered -> unknown provider triggers early failover
	failoverResp := []byte(`{"id":"msg_xyz","model":"gpt-5.2-high","type":"message","content":[]}`)
	manager.RegisterExecutor(&okExecutor{id: "codex", payload: failoverResp})

	codexAuth := &coreauth.Auth{ID: "codex-auth-up", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("register codex: %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.2"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{}`)))
	c.Set("apiKey", "client-key")
	c.Set("apiKeyPolicy", &internalconfig.APIKeyPolicy{
		APIKey: "client-key",
		Failover: internalconfig.APIKeyFailoverPolicy{
			Claude: internalconfig.ProviderFailoverPolicy{
				Enabled:     true,
				TargetModel: "gpt-5.2(high)",
			},
		},
	})

	ctx := context.WithValue(context.Background(), "gin", c)
	payload := []byte(`{"model":"claude-opus-4-6","stream":false}`)
	resp, errMsg := handler.ExecuteWithAuthManager(ctx, "claude", "claude-opus-4-6", payload, "")
	if errMsg != nil {
		t.Fatalf("expected nil error, got: %+v", errMsg)
	}

	gotModel := gjson.GetBytes(resp, "model").String()
	if gotModel != "claude-opus-4-6" {
		t.Errorf("expected response model=claude-opus-4-6 after unknown-provider failover, got %q", gotModel)
	}
}

// ---------------------------------------------------------------------------
// Integration: no failover -> model field untouched
// ---------------------------------------------------------------------------

func TestExecuteWithAuthManager_NoFailoverModelUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)

	// Claude executor succeeds normally
	normalResp := []byte(`{"id":"msg_ok","model":"claude-opus-4-6","type":"message","content":[]}`)
	manager.RegisterExecutor(&okExecutor{id: "claude", payload: normalResp})

	claudeAuth := &coreauth.Auth{ID: "claude-auth-nf", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), claudeAuth); err != nil {
		t.Fatalf("register claude: %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(claudeAuth.ID, claudeAuth.Provider, []*registry.ModelInfo{{ID: "claude-opus-4-6"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(claudeAuth.ID)
	})

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{}`)))
	c.Set("apiKey", "client-key")
	c.Set("apiKeyPolicy", &internalconfig.APIKeyPolicy{
		APIKey: "client-key",
		Failover: internalconfig.APIKeyFailoverPolicy{
			Claude: internalconfig.ProviderFailoverPolicy{
				Enabled:     true,
				TargetModel: "gpt-5.2(high)",
			},
		},
	})

	ctx := context.WithValue(context.Background(), "gin", c)
	payload := []byte(`{"model":"claude-opus-4-6","stream":false}`)
	resp, errMsg := handler.ExecuteWithAuthManager(ctx, "claude", "claude-opus-4-6", payload, "")
	if errMsg != nil {
		t.Fatalf("expected nil error, got: %+v", errMsg)
	}

	// Model should remain as-is since no failover happened
	gotModel := gjson.GetBytes(resp, "model").String()
	if gotModel != "claude-opus-4-6" {
		t.Errorf("expected model=claude-opus-4-6 (no failover), got %q", gotModel)
	}
}
