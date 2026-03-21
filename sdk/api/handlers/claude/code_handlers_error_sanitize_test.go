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
	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	basehandlers "github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

const rawUsageLimitError = `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","plan_type":"team","resets_in_seconds":11773}}`
const rawServiceUnavailableError = "unexpected status 503 Service Unavailable: Service temporarily unavailable, url: https://www.open1.codes/responses, cf-ray: 9dd161f09d39cc65-LAX, request id: 7160033a-4597-4632-846c-d1872c107f06"
const rawModelCooldownError = `{"error":{"code":"model_cooldown","message":"All credentials for model gpt-5.4(high) are cooling down","model":"gpt-5.4(high)","reset_seconds":41,"reset_time":"41s"}}`
const rawOrganizationDisabledError = `API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"This organization has been disabled."},"request_id":"req_011CZAWGe3LLpSKgbUC5gMKy"}`

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

type modelCooldownExecutor struct{}

func (e *modelCooldownExecutor) Identifier() string { return "codex" }

func (e *modelCooldownExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawModelCooldownError,
		Retryable:  false,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

func (e *modelCooldownExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (<-chan coreexecutor.StreamChunk, error) {
	ch := make(chan coreexecutor.StreamChunk, 1)
	ch <- coreexecutor.StreamChunk{
		Err: &coreauth.Error{
			Code:       "upstream_error",
			Message:    rawModelCooldownError,
			Retryable:  false,
			HTTPStatus: http.StatusTooManyRequests,
		},
	}
	close(ch)
	return ch, nil
}

func (e *modelCooldownExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *modelCooldownExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawModelCooldownError,
		Retryable:  false,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

func (e *modelCooldownExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawModelCooldownError,
		Retryable:  false,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

type organizationDisabledExecutor struct{}

func (e *organizationDisabledExecutor) Identifier() string { return "claude" }

func (e *organizationDisabledExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawOrganizationDisabledError,
		Retryable:  false,
		HTTPStatus: http.StatusBadRequest,
	}
}

func (e *organizationDisabledExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (<-chan coreexecutor.StreamChunk, error) {
	ch := make(chan coreexecutor.StreamChunk, 1)
	ch <- coreexecutor.StreamChunk{
		Err: &coreauth.Error{
			Code:       "upstream_error",
			Message:    rawOrganizationDisabledError,
			Retryable:  false,
			HTTPStatus: http.StatusBadRequest,
		},
	}
	close(ch)
	return ch, nil
}

func (e *organizationDisabledExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *organizationDisabledExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawOrganizationDisabledError,
		Retryable:  false,
		HTTPStatus: http.StatusBadRequest,
	}
}

func (e *organizationDisabledExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{
		Code:       "upstream_error",
		Message:    rawOrganizationDisabledError,
		Retryable:  false,
		HTTPStatus: http.StatusBadRequest,
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
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusServiceUnavailable, body)
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

func TestClaudeMessages_SuppressesRawModelCooldownAfterFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&claudeFailoverTriggerExecutor{})
	manager.RegisterExecutor(&modelCooldownExecutor{})
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
	if strings.Contains(body, "model_cooldown") || strings.Contains(body, "gpt-5.4(high)") || strings.Contains(body, "reset_seconds") {
		t.Fatalf("expected sanitized cooldown body, got %s", body)
	}
	if !strings.Contains(body, "upstream model temporarily unavailable, please retry later") {
		t.Fatalf("expected generic retry message, got %s", body)
	}
}

func TestClaudeMessages_SuppressesOrganizationDisabledNonStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&organizationDisabledExecutor{})
	registerClaudeTestAuth(t, manager, "claude-opus-4-6")

	handler := NewClaudeCodeAPIHandler(basehandlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"claude-opus-4-6","stream":false}`)))

	handler.ClaudeMessages(c)

	body := w.Body.String()
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusServiceUnavailable, body)
	}
	if strings.Contains(body, "organization has been disabled") || strings.Contains(body, "request_id") {
		t.Fatalf("expected sanitized organization disabled body, got %s", body)
	}
	if !strings.Contains(body, "upstream model temporarily unavailable, please retry later") {
		t.Fatalf("expected generic retry message, got %s", body)
	}
}

func TestClaudeMessages_SuppressesOrganizationDisabledStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&organizationDisabledExecutor{})
	registerClaudeTestAuth(t, manager, "claude-opus-4-6")

	handler := NewClaudeCodeAPIHandler(basehandlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"claude-opus-4-6","stream":true}`)))

	handler.ClaudeMessages(c)

	body := w.Body.String()
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusServiceUnavailable, body)
	}
	if strings.Contains(body, "organization has been disabled") || strings.Contains(body, "request_id") {
		t.Fatalf("expected sanitized streaming organization disabled body, got %s", body)
	}
	if !strings.Contains(body, "upstream model temporarily unavailable, please retry later") {
		t.Fatalf("expected generic retry message, got %s", body)
	}
}

func TestSanitizeClientError_LogsRawUpstreamErrorInErrorField(t *testing.T) {
	logger, hook := logtest.NewNullLogger()
	prevHooks := log.StandardLogger().Hooks
	prevOut := log.StandardLogger().Out
	prevFormatter := log.StandardLogger().Formatter
	prevLevel := log.StandardLogger().Level
	log.SetOutput(logger.Out)
	log.StandardLogger().ReplaceHooks(logger.Hooks)
	log.SetFormatter(logger.Formatter)
	log.SetLevel(logger.Level)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.StandardLogger().ReplaceHooks(prevHooks)
		log.SetFormatter(prevFormatter)
		log.SetLevel(prevLevel)
	})

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("cpa_failover_provider", "codex")

	handler := &ClaudeCodeAPIHandler{}
	msg := &interfaces.ErrorMessage{
		StatusCode: http.StatusServiceUnavailable,
		Error: &coreauth.Error{
			Code:       "upstream_error",
			Message:    rawServiceUnavailableError,
			Retryable:  false,
			HTTPStatus: http.StatusServiceUnavailable,
		},
	}

	sanitized := handler.sanitizeClientError(c, msg)
	if sanitized == nil || sanitized.Error == nil {
		t.Fatal("expected sanitized error")
	}
	if sanitized.Error.Error() != "upstream model temporarily unavailable, please retry later" {
		t.Fatalf("unexpected sanitized error: %v", sanitized.Error)
	}

	entries := hook.AllEntries()
	if len(entries) == 0 {
		t.Fatal("expected sanitizeClientError to emit a log entry")
	}
	last := entries[len(entries)-1]
	errField, ok := last.Data["error"].(error)
	if !ok || errField == nil {
		t.Fatalf("expected error field in log entry, got %#v", last.Data["error"])
	}
	if !strings.Contains(errField.Error(), rawServiceUnavailableError) {
		t.Fatalf("expected raw upstream error in log field, got %v", errField)
	}
	if got := last.Data["provider"]; got != "codex" {
		t.Fatalf("expected provider=codex, got %#v", got)
	}
}
