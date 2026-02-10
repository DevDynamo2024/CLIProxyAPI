package auth

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type invalidRequestStatusErr struct {
	msg string
}

func (e invalidRequestStatusErr) Error() string   { return e.msg }
func (e invalidRequestStatusErr) StatusCode() int { return http.StatusBadRequest }

type failoverTestExecutor struct {
	provider  string
	failByID  map[string]error
	response  cliproxyexecutor.Response
	execCalls []string
}

func (e *failoverTestExecutor) Identifier() string { return e.provider }

func (e *failoverTestExecutor) Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = req
	_ = opts
	if auth != nil {
		e.execCalls = append(e.execCalls, auth.ID)
		if err := e.failByID[auth.ID]; err != nil {
			return cliproxyexecutor.Response{}, err
		}
	}
	return e.response, nil
}

func (e *failoverTestExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (<-chan cliproxyexecutor.StreamChunk, error) {
	return nil, fmt.Errorf("ExecuteStream not implemented")
}

func (e *failoverTestExecutor) Refresh(ctx context.Context, auth *Auth) (*Auth, error) {
	_ = ctx
	return auth, nil
}

func (e *failoverTestExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.Execute(ctx, auth, req, opts)
}

func (e *failoverTestExecutor) HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("HttpRequest not implemented")
}

func TestManagerExecute_InvalidRequestError_ClaudeContinuesTraversal(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &FillFirstSelector{}, nil)
	executor := &failoverTestExecutor{
		provider: "claude",
		failByID: map[string]error{
			"a": invalidRequestStatusErr{msg: `{"error":{"type":"invalid_request_error","message":"account disabled"}}`},
			"b": invalidRequestStatusErr{msg: `{"error":{"type":"invalid_request_error","message":"account disabled"}}`},
		},
		response: cliproxyexecutor.Response{Payload: []byte("ok")},
	}
	manager.RegisterExecutor(executor)

	_, _ = manager.Register(context.Background(), &Auth{ID: "a", Provider: "claude"})
	_, _ = manager.Register(context.Background(), &Auth{ID: "b", Provider: "claude"})
	_, _ = manager.Register(context.Background(), &Auth{ID: "c", Provider: "claude"})

	resp, err := manager.Execute(context.Background(), []string{"claude"}, cliproxyexecutor.Request{}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("Execute() payload = %q, want %q", string(resp.Payload), "ok")
	}
	if len(executor.execCalls) != 3 {
		t.Fatalf("Execute() executor calls = %v, want 3 calls", executor.execCalls)
	}
}

