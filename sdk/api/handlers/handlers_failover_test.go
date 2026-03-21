package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor"
	_ "github.com/router-for-me/CLIProxyAPI/v6/internal/translator"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	"github.com/tidwall/gjson"
)

func boolPtr(v bool) *bool { return &v }

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

type recordModelExecutor struct {
	id        string
	payload   []byte
	lastModel *string
}

func (e *recordModelExecutor) Identifier() string { return e.id }

func (e *recordModelExecutor) Execute(_ context.Context, _ *coreauth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	if e.lastModel != nil {
		*e.lastModel = req.Model
	}
	return coreexecutor.Response{Payload: bytes.Clone(e.payload)}, nil
}

func (e *recordModelExecutor) ExecuteStream(_ context.Context, _ *coreauth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (<-chan coreexecutor.StreamChunk, error) {
	if e.lastModel != nil {
		*e.lastModel = req.Model
	}
	ch := make(chan coreexecutor.StreamChunk, 1)
	ch <- coreexecutor.StreamChunk{Payload: bytes.Clone(e.payload)}
	close(ch)
	return ch, nil
}

func (e *recordModelExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *recordModelExecutor) CountTokens(_ context.Context, _ *coreauth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	if e.lastModel != nil {
		*e.lastModel = req.Model
	}
	return coreexecutor.Response{Payload: bytes.Clone(e.payload)}, nil
}

func (e *recordModelExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
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
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.4"}})
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
				TargetModel: "gpt-5.4(high)",
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

func TestExecuteWithAuthManager_ClaudeFailoverModelRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&failStatusExecutor{id: "claude", status: http.StatusTooManyRequests, msg: "weekly cap"})

	var gotModel string
	manager.RegisterExecutor(&recordModelExecutor{id: "codex", payload: []byte("ok"), lastModel: &gotModel})

	claudeAuth := &coreauth.Auth{ID: "claude-auth", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), claudeAuth); err != nil {
		t.Fatalf("manager.Register(claude): %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "codex-auth", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("manager.Register(codex): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(claudeAuth.ID, claudeAuth.Provider, []*registry.ModelInfo{{ID: "claude-sonnet-4-6"}})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.4"}})
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
				TargetModel: "gpt-5.4(high)",
				Rules: []internalconfig.ModelFailoverRule{
					{FromModel: "claude-sonnet-4-6*", TargetModel: "gpt-5.4(high)"},
				},
			},
		},
	})

	ctx := context.WithValue(context.Background(), "gin", c)
	payload := []byte(`{"model":"claude-sonnet-4-6","stream":false}`)
	resp, errMsg := handler.ExecuteWithAuthManager(ctx, "claude", "claude-sonnet-4-6", payload, "")
	if errMsg != nil {
		t.Fatalf("expected nil error, got: %+v", errMsg)
	}
	if string(resp) != "ok" {
		t.Fatalf("expected ok, got %q", string(resp))
	}
	if gotModel != "gpt-5.4(high)" {
		t.Fatalf("expected failover model %q, got %q", "gpt-5.4(high)", gotModel)
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
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.4"}})
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

func TestExecuteWithAuthManager_ClaudeFailoverUnknownProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&okExecutor{id: "codex", payload: []byte("ok")})

	codexAuth := &coreauth.Auth{ID: "codex-auth", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("manager.Register(codex): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.4"}})
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
				TargetModel: "gpt-5.4(high)",
			},
		},
	})

	ctx := context.WithValue(context.Background(), "gin", c)
	payload := []byte(`{"model":"claude-opus-4-6","stream":false}`)
	resp, errMsg := handler.ExecuteWithAuthManager(ctx, "claude", "claude-opus-4-6", payload, "")
	if errMsg != nil {
		t.Fatalf("expected nil error, got: %+v", errMsg)
	}
	if string(resp) != "ok" {
		t.Fatalf("expected ok, got %q", string(resp))
	}
}

func TestExecuteWithAuthManager_ClaudeFailoverToCustomCodexStripsInclude(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var mu sync.Mutex
	var seenBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		mu.Lock()
		seenBody = append([]byte(nil), body...)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
	}))
	defer srv.Close()

	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&failStatusExecutor{id: "claude", status: http.StatusTooManyRequests, msg: "weekly cap"})
	manager.RegisterExecutor(runtimeexecutor.NewCodexExecutor(&internalconfig.Config{}))

	claudeAuth := &coreauth.Auth{ID: "claude-auth-strip-include", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), claudeAuth); err != nil {
		t.Fatalf("manager.Register(claude): %v", err)
	}
	codexAuth := &coreauth.Auth{
		ID:       "codex-auth-strip-include",
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"api_key":  "test",
			"base_url": srv.URL,
		},
	}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("manager.Register(codex): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(claudeAuth.ID, claudeAuth.Provider, []*registry.ModelInfo{{ID: "claude-sonnet-4-6"}})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.4"}})
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
				TargetModel: "gpt-5.4(high)",
			},
		},
	})

	ctx := context.WithValue(context.Background(), "gin", c)
	payload := []byte(`{
		"model":"claude-sonnet-4-6",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"hi"}]}
		],
		"stream":false
	}`)
	if _, errMsg := handler.ExecuteWithAuthManager(ctx, "claude", "claude-sonnet-4-6", payload, ""); errMsg != nil {
		t.Fatalf("expected nil error, got: %+v", errMsg)
	}

	mu.Lock()
	got := append([]byte(nil), seenBody...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatal("expected upstream request body to be captured")
	}
	if bytes.Contains(got, []byte(`"include"`)) {
		t.Fatalf("expected include to be stripped before custom codex upstream request, got %s", string(got))
	}
}

func TestExecuteWithAuthManager_ClaudeFailoverToCodexPreservesBuiltinWebSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var mu sync.Mutex
	var seenBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		mu.Lock()
		seenBody = append([]byte(nil), body...)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
	}))
	defer srv.Close()

	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&failStatusExecutor{id: "claude", status: http.StatusTooManyRequests, msg: "weekly cap"})
	manager.RegisterExecutor(runtimeexecutor.NewCodexExecutor(&internalconfig.Config{}))

	claudeAuth := &coreauth.Auth{ID: "claude-auth-web-search", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), claudeAuth); err != nil {
		t.Fatalf("manager.Register(claude): %v", err)
	}
	codexAuth := &coreauth.Auth{
		ID:       "codex-auth-web-search",
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"api_key":  "test",
			"base_url": srv.URL,
		},
	}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("manager.Register(codex): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(claudeAuth.ID, claudeAuth.Provider, []*registry.ModelInfo{{ID: "claude-sonnet-4-6"}})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.4"}})
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
				TargetModel: "gpt-5.4(high)",
			},
		},
	})

	ctx := context.WithValue(context.Background(), "gin", c)
	payload := []byte(`{
		"model":"claude-sonnet-4-6",
		"tools":[
			{
				"type":"web_search_20250305",
				"name":"web_search",
				"description":"Search the web for recent news",
				"input_schema":{
					"type":"object",
					"properties":{"query":{"type":"string"}},
					"required":["query"]
				}
			}
		],
		"messages":[
			{"role":"user","content":[{"type":"text","text":"Find today's headlines"}]}
		],
		"stream":false
	}`)
	if _, errMsg := handler.ExecuteWithAuthManager(ctx, "claude", "claude-sonnet-4-6", payload, ""); errMsg != nil {
		t.Fatalf("expected nil error, got: %+v", errMsg)
	}

	mu.Lock()
	got := append([]byte(nil), seenBody...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatal("expected upstream request body to be captured")
	}
	if gotType := gjson.GetBytes(got, "tools.0.type").String(); gotType != "web_search" {
		t.Fatalf("expected builtin web_search tool after failover, got tools.0.type=%q in %s", gotType, string(got))
	}
	if gjson.GetBytes(got, "tools.0.name").Exists() {
		t.Fatalf("expected builtin web_search tool without name after failover, got %s", string(got))
	}
	if gotType := gjson.GetBytes(got, "tools.0.function.type").String(); gotType == "function" {
		t.Fatalf("expected web_search not to degrade into function tool, got %s", string(got))
	}
	if gjson.GetBytes(got, "tool_choice").Exists() {
		t.Fatalf("expected tool_choice to be stripped before Codex upstream request, got %s", string(got))
	}
}

func TestExecuteWithAuthManager_ClaudeFailoverAuthUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&failStatusExecutor{id: "claude", status: http.StatusInternalServerError, msg: "auth_unavailable: no auth available"})
	manager.RegisterExecutor(&okExecutor{id: "codex", payload: []byte("ok")})

	claudeAuth := &coreauth.Auth{ID: "claude-auth", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), claudeAuth); err != nil {
		t.Fatalf("manager.Register(claude): %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "codex-auth", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("manager.Register(codex): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(claudeAuth.ID, claudeAuth.Provider, []*registry.ModelInfo{{ID: "claude-opus-4-6"}})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.4"}})
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
				TargetModel: "gpt-5.4(high)",
			},
		},
	})

	ctx := context.WithValue(context.Background(), "gin", c)
	payload := []byte(`{"model":"claude-opus-4-6","stream":false}`)
	resp, errMsg := handler.ExecuteWithAuthManager(ctx, "claude", "claude-opus-4-6", payload, "")
	if errMsg != nil {
		t.Fatalf("expected nil error, got: %+v", errMsg)
	}
	if string(resp) != "ok" {
		t.Fatalf("expected ok, got %q", string(resp))
	}
}

func TestExecuteWithAuthManager_ClaudeOptInFallsBackWhenNoClaudeAuthAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&okExecutor{id: "codex", payload: []byte("ok")})

	codexAuth := &coreauth.Auth{ID: "codex-auth", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("manager.Register(codex): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.4"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)

	cfg := &internalconfig.Config{
		SDKConfig: internalconfig.SDKConfig{ClaudeToGPTRoutingEnabled: true},
		APIKeyPolicies: []internalconfig.APIKeyPolicy{
			{APIKey: "client-key", EnableClaudeModels: boolPtr(true)},
		},
	}
	policy := cfg.EffectiveAPIKeyPolicy("client-key")
	if policy == nil {
		t.Fatal("expected synthesized api key policy")
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{}`)))
	c.Set("apiKey", "client-key")
	c.Set("apiKeyPolicy", policy)

	ctx := context.WithValue(context.Background(), "gin", c)
	payload := []byte(`{"model":"claude-opus-4-6","stream":false}`)
	resp, errMsg := handler.ExecuteWithAuthManager(ctx, "claude", "claude-opus-4-6", payload, "")
	if errMsg != nil {
		t.Fatalf("expected nil error, got: %+v", errMsg)
	}
	if string(resp) != "ok" {
		t.Fatalf("expected ok, got %q", string(resp))
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
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.4"}})
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
				TargetModel: "gpt-5.4(high)",
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

func TestExecuteStreamWithAuthManager_ClaudeFailoverUnknownProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&okExecutor{id: "codex", payload: []byte("ok")})

	codexAuth := &coreauth.Auth{ID: "codex-auth", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("manager.Register(codex): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.4"}})
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
				TargetModel: "gpt-5.4(high)",
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

	if string(got) != "ok" {
		t.Fatalf("expected ok, got %q", string(got))
	}
}
