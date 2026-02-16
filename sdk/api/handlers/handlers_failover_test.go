package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

type failStatusExecutor struct {
	id     string
	status int
	msg    string
}

func (e *failStatusExecutor) Identifier() string { return e.id }

func (e *failStatusExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    e.msg,
		Retryable:  false,
		HTTPStatus: e.status,
	}
}

func (e *failStatusExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (<-chan coreexecutor.StreamChunk, error) {
	ch := make(chan coreexecutor.StreamChunk, 1)
	ch <- coreexecutor.StreamChunk{
		Err: &coreauth.Error{
			Code:       "upstream_error",
			Message:    e.msg,
			Retryable:  false,
			HTTPStatus: e.status,
		},
	}
	close(ch)
	return ch, nil
}

func (e *failStatusExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *failStatusExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{
		Code:       "upstream_error",
		Message:    e.msg,
		Retryable:  false,
		HTTPStatus: e.status,
	}
}

func (e *failStatusExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{
		Code:       "upstream_error",
		Message:    e.msg,
		Retryable:  false,
		HTTPStatus: e.status,
	}
}

type okExecutor struct {
	id      string
	payload []byte
}

func (e *okExecutor) Identifier() string { return e.id }

func (e *okExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{Payload: bytes.Clone(e.payload)}, nil
}

func (e *okExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (<-chan coreexecutor.StreamChunk, error) {
	ch := make(chan coreexecutor.StreamChunk, 1)
	ch <- coreexecutor.StreamChunk{Payload: bytes.Clone(e.payload)}
	close(ch)
	return ch, nil
}

func (e *okExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *okExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{Payload: bytes.Clone(e.payload)}, nil
}

func (e *okExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{Code: "not_implemented", Message: "HttpRequest not implemented", HTTPStatus: http.StatusNotImplemented}
}

func TestExecuteWithAuthManager_ClaudeFailoverEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&failStatusExecutor{id: "claude", status: http.StatusTooManyRequests, msg: "weekly cap"})
	manager.RegisterExecutor(&okExecutor{id: "codex", payload: []byte("ok")})

	claudeAuth := &coreauth.Auth{ID: "claude-auth", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), claudeAuth); err != nil {
		t.Fatalf("manager.Register(claude): %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "codex-auth", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("manager.Register(codex): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(claudeAuth.ID, claudeAuth.Provider, []*registry.ModelInfo{{ID: "claude-model"}})
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
	payload := []byte(`{"model":"claude-model","stream":false}`)
	resp, errMsg := handler.ExecuteWithAuthManager(ctx, "claude", "claude-model", payload, "")
	if errMsg != nil {
		t.Fatalf("expected nil error, got: %+v", errMsg)
	}
	if string(resp) != "ok" {
		t.Fatalf("expected ok, got %q", string(resp))
	}
}

func TestExecuteWithAuthManager_ClaudeFailoverDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&failStatusExecutor{id: "claude", status: http.StatusTooManyRequests, msg: "weekly cap"})
	manager.RegisterExecutor(&okExecutor{id: "codex", payload: []byte("ok")})

	claudeAuth := &coreauth.Auth{ID: "claude-auth", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), claudeAuth); err != nil {
		t.Fatalf("manager.Register(claude): %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "codex-auth", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("manager.Register(codex): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(claudeAuth.ID, claudeAuth.Provider, []*registry.ModelInfo{{ID: "claude-model"}})
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
				Enabled: false,
			},
		},
	})

	ctx := context.WithValue(context.Background(), "gin", c)
	payload := []byte(`{"model":"claude-model","stream":false}`)
	_, errMsg := handler.ExecuteWithAuthManager(ctx, "claude", "claude-model", payload, "")
	if errMsg == nil || errMsg.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 error, got: %+v", errMsg)
	}
}

func TestExecuteStreamWithAuthManager_ClaudeFailoverBeforeFirstByte(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&failStatusExecutor{id: "claude", status: http.StatusTooManyRequests, msg: "rolling cap"})
	manager.RegisterExecutor(&okExecutor{id: "codex", payload: []byte("ok")})

	claudeAuth := &coreauth.Auth{ID: "claude-auth", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), claudeAuth); err != nil {
		t.Fatalf("manager.Register(claude): %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "codex-auth", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("manager.Register(codex): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(claudeAuth.ID, claudeAuth.Provider, []*registry.ModelInfo{{ID: "claude-model"}})
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
	payload := []byte(`{"model":"claude-model","stream":true}`)
	dataChan, errChan := handler.ExecuteStreamWithAuthManager(ctx, "claude", "claude-model", payload, "")
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

	if string(got) != "ok" {
		t.Fatalf("expected ok, got %q", string(got))
	}
}
