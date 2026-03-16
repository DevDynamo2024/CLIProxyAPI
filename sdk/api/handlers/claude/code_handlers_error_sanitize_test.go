package claude

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	basehandlers "github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

const rawUsageLimitError = `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","plan_type":"team","resets_in_seconds":11773}}`
const rawServiceUnavailableError = "unexpected status 503 Service Unavailable: Service temporarily unavailable, url: https://www.open1.codes/responses, cf-ray: 9dd161f09d39cc65-LAX, request id: 7160033a-4597-4632-846c-d1872c107f06"

type claudeFailoverTriggerExecutor struct{}

func (e *claudeFailoverTriggerExecutor) Identifier() string { return "claude" }

func (e *claudeFailoverTriggerExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    "weekly cap",
		Retryable:  false,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

func (e *claudeFailoverTriggerExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (<-chan coreexecutor.StreamChunk, error) {
	ch := make(chan coreexecutor.StreamChunk, 1)
	ch <- coreexecutor.StreamChunk{
		Err: &coreauth.Error{
			Code:       "upstream_error",
			Message:    "weekly cap",
			Retryable:  false,
			HTTPStatus: http.StatusTooManyRequests,
		},
	}
	close(ch)
	return ch, nil
}

func (e *claudeFailoverTriggerExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *claudeFailoverTriggerExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    "weekly cap",
		Retryable:  false,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

func (e *claudeFailoverTriggerExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{
		Code:       "upstream_error",
		Message:    "weekly cap",
		Retryable:  false,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

type usageLimitExecuteExecutor struct{}

func (e *usageLimitExecuteExecutor) Identifier() string { return "codex" }

func (e *usageLimitExecuteExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawUsageLimitError,
		Retryable:  false,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

func (e *usageLimitExecuteExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (<-chan coreexecutor.StreamChunk, error) {
	ch := make(chan coreexecutor.StreamChunk, 1)
	ch <- coreexecutor.StreamChunk{
		Err: &coreauth.Error{
			Code:       "upstream_error",
			Message:    rawUsageLimitError,
			Retryable:  false,
			HTTPStatus: http.StatusTooManyRequests,
		},
	}
	close(ch)
	return ch, nil
}

func (e *usageLimitExecuteExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *usageLimitExecuteExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawUsageLimitError,
		Retryable:  false,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

func (e *usageLimitExecuteExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawUsageLimitError,
		Retryable:  false,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

type usageLimitStreamAfterPayloadExecutor struct{}

func (e *usageLimitStreamAfterPayloadExecutor) Identifier() string { return "codex" }

func (e *usageLimitStreamAfterPayloadExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, nil
}

func (e *usageLimitStreamAfterPayloadExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (<-chan coreexecutor.StreamChunk, error) {
	ch := make(chan coreexecutor.StreamChunk, 2)
	ch <- coreexecutor.StreamChunk{Payload: []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n")}
	ch <- coreexecutor.StreamChunk{
		Err: &coreauth.Error{
			Code:       "upstream_error",
			Message:    rawUsageLimitError,
			Retryable:  false,
			HTTPStatus: http.StatusTooManyRequests,
		},
	}
	close(ch)
	return ch, nil
}

func (e *usageLimitStreamAfterPayloadExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *usageLimitStreamAfterPayloadExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, nil
}

func (e *usageLimitStreamAfterPayloadExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawUsageLimitError,
		Retryable:  false,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

type serviceUnavailableExecutor struct{}

func (e *serviceUnavailableExecutor) Identifier() string { return "codex" }

func (e *serviceUnavailableExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawServiceUnavailableError,
		Retryable:  false,
		HTTPStatus: http.StatusServiceUnavailable,
	}
}

func (e *serviceUnavailableExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (<-chan coreexecutor.StreamChunk, error) {
	ch := make(chan coreexecutor.StreamChunk, 1)
	ch <- coreexecutor.StreamChunk{
		Err: &coreauth.Error{
			Code:       "upstream_error",
			Message:    rawServiceUnavailableError,
			Retryable:  false,
			HTTPStatus: http.StatusServiceUnavailable,
		},
	}
	close(ch)
	return ch, nil
}

func (e *serviceUnavailableExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *serviceUnavailableExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawServiceUnavailableError,
		Retryable:  false,
		HTTPStatus: http.StatusServiceUnavailable,
	}
}

func (e *serviceUnavailableExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawServiceUnavailableError,
		Retryable:  false,
		HTTPStatus: http.StatusServiceUnavailable,
	}
}

func registerCodexTestAuth(t *testing.T, manager *coreauth.Manager, model string) {
	t.Helper()

	auth := &coreauth.Auth{ID: "codex-auth", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("manager.Register(codex): %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})
}

func registerClaudeTestAuth(t *testing.T, manager *coreauth.Manager, model string) {
	t.Helper()

	auth := &coreauth.Auth{ID: "claude-auth", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("manager.Register(claude): %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})
}

func attachFailoverPolicy(c *gin.Context) {
	c.Set("apiKey", "client-key")
	c.Set("apiKeyPolicy", &internalconfig.APIKeyPolicy{
		APIKey: "client-key",
		Failover: internalconfig.APIKeyFailoverPolicy{
			Claude: internalconfig.ProviderFailoverPolicy{
				Enabled:     true,
				TargetModel: "gpt-5.4(high)",
			},
		},
	})
}

func TestClaudeMessages_SuppressesRawUsageLimitErrorNonStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&claudeFailoverTriggerExecutor{})
	manager.RegisterExecutor(&usageLimitExecuteExecutor{})
	registerClaudeTestAuth(t, manager, "claude-opus-4-6")
	registerCodexTestAuth(t, manager, "gpt-5.4")

	handler := NewClaudeCodeAPIHandler(basehandlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"claude-opus-4-6","stream":false}`)))
	attachFailoverPolicy(c)

	handler.ClaudeMessages(c)

	body := w.Body.String()
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusTooManyRequests, body)
	}
	if strings.Contains(body, "usage_limit_reached") || strings.Contains(body, "resets_in_seconds") {
		t.Fatalf("expected sanitized error body, got %s", body)
	}
	if !strings.Contains(body, "upstream model temporarily unavailable, please retry later") {
		t.Fatalf("expected generic retry message, got %s", body)
	}
}

func TestClaudeMessages_SuppressesRawUsageLimitErrorStreamingTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&claudeFailoverTriggerExecutor{})
	manager.RegisterExecutor(&usageLimitStreamAfterPayloadExecutor{})
	registerClaudeTestAuth(t, manager, "claude-opus-4-6")
	registerCodexTestAuth(t, manager, "gpt-5.4")

	handler := NewClaudeCodeAPIHandler(basehandlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"claude-opus-4-6","stream":true}`)))
	attachFailoverPolicy(c)

	handler.ClaudeMessages(c)

	body := w.Body.String()
	if !strings.Contains(body, "message_start") {
		t.Fatalf("expected initial stream payload, got %s", body)
	}
	if strings.Contains(body, "usage_limit_reached") || strings.Contains(body, "resets_in_seconds") {
		t.Fatalf("expected sanitized stream error, got %s", body)
	}
	if !strings.Contains(body, "upstream model temporarily unavailable, please retry later") {
		t.Fatalf("expected generic retry message, got %s", body)
	}
}

func TestClaudeMessages_SuppressesRawServiceUnavailableAfterFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&claudeFailoverTriggerExecutor{})
	manager.RegisterExecutor(&serviceUnavailableExecutor{})
	registerClaudeTestAuth(t, manager, "claude-opus-4-6")
	registerCodexTestAuth(t, manager, "gpt-5.4")

	handler := NewClaudeCodeAPIHandler(basehandlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"claude-opus-4-6","stream":false}`)))
	attachFailoverPolicy(c)

	handler.ClaudeMessages(c)

	body := w.Body.String()
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusServiceUnavailable, body)
	}
	if strings.Contains(body, "open1.codes") || strings.Contains(body, "cf-ray") || strings.Contains(body, "request id") {
		t.Fatalf("expected sanitized 503 body, got %s", body)
	}
	if !strings.Contains(body, "upstream model temporarily unavailable, please retry later") {
		t.Fatalf("expected generic retry message, got %s", body)
	}
}
