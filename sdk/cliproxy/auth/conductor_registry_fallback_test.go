package auth

import (
	"context"
	"net/http"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type staticExecutor struct {
	id      string
	payload []byte
}

func (e *staticExecutor) Identifier() string { return e.id }

func (e *staticExecutor) Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	_ = opts
	return cliproxyexecutor.Response{Payload: e.payload}, nil
}

func (e *staticExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (<-chan cliproxyexecutor.StreamChunk, error) {
	_ = ctx
	_ = auth
	_ = req
	_ = opts
	ch := make(chan cliproxyexecutor.StreamChunk)
	close(ch)
	return ch, nil
}

func (e *staticExecutor) Refresh(ctx context.Context, auth *Auth) (*Auth, error) {
	_ = ctx
	return auth, nil
}

func (e *staticExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	_ = opts
	return cliproxyexecutor.Response{Payload: []byte(`{"count":1}`)}, nil
}

func (e *staticExecutor) HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	return nil, nil
}

func TestExecute_AllowsUnknownModelWithoutRegistryWarming(t *testing.T) {
	t.Parallel()

	mgr := NewManager(nil, &FillFirstSelector{}, nil)
	mgr.RegisterExecutor(&staticExecutor{id: "claude", payload: []byte(`{"ok":true}`)})
	_, err := mgr.Register(context.Background(), &Auth{ID: "c1", Provider: "claude", Status: StatusActive})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Use a unique model name that is extremely unlikely to be pre-registered by other tests.
	model := "claude-unittest-registry-miss-9f8c7a"
	resp, err := mgr.Execute(context.Background(), []string{"claude"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(resp.Payload) != `{"ok":true}` {
		t.Fatalf("Execute() payload = %s, want %s", string(resp.Payload), `{"ok":true}`)
	}
}
