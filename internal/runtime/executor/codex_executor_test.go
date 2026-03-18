package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v6/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCodexExecuteStopsAfterResponseCompleted(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}

		// Simulate an upstream that keeps the connection open for a while even after completion.
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	exec := NewCodexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test", "base_url": srv.URL}}
	req := cliproxyexecutor.Request{Model: "gpt-5.2", Payload: []byte(`{"input":"hi"}`)}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("codex")}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := exec.Execute(ctx, auth, req, opts)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if elapsed >= time.Second {
		t.Fatalf("Execute took %v; expected to return before upstream closes", elapsed)
	}
	if got := gjson.GetBytes(resp.Payload, "type").String(); got != "response.completed" {
		t.Fatalf("type = %q, want %q (payload=%s)", got, "response.completed", string(resp.Payload))
	}
}

func TestCodexExecuteFallsBackToCompactWhenStreamEndsEarly(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	calls := map[string]int{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()

		switch r.URL.Path {
		case "/responses":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"type\":\"response.created\"}\n\n"))
			return
		case "/responses/compact":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"resp_compact","model":"gpt-5.2","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2},"stop_reason":"stop"}`))
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	defer srv.Close()

	exec := NewCodexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test", "base_url": srv.URL}}
	req := cliproxyexecutor.Request{Model: "gpt-5.2", Payload: []byte(`{"input":"hi"}`)}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("codex")}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := exec.Execute(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got := gjson.GetBytes(resp.Payload, "type").String(); got != "response.completed" {
		t.Fatalf("type = %q, want %q (payload=%s)", got, "response.completed", string(resp.Payload))
	}
	if got := gjson.GetBytes(resp.Payload, "response.id").String(); got != "resp_compact" {
		t.Fatalf("response.id = %q, want %q (payload=%s)", got, "resp_compact", string(resp.Payload))
	}

	mu.Lock()
	defer mu.Unlock()
	if calls["/responses"] != 1 {
		t.Fatalf("/responses calls = %d, want 1", calls["/responses"])
	}
	if calls["/responses/compact"] != 1 {
		t.Fatalf("/responses/compact calls = %d, want 1", calls["/responses/compact"])
	}
}

func TestCodexExecutorStripsToolsDeferLoadingBeforeUpstream(t *testing.T) {
	var mu sync.Mutex
	var seenBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		mu.Lock()
		seenBody = b
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
	}))
	defer srv.Close()

	exec := NewCodexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test", "base_url": srv.URL}}
	req := cliproxyexecutor.Request{Model: "gpt-5.2", Payload: []byte(`{
		"input":"hi",
		"tools":{
			"defer_loading":true,
			"tool_choice":"auto",
			"tools":[{"type":"function","function":{"name":"t","parameters":{"type":"object","properties":{}}}}]
		}
	}`)}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("codex")}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := exec.Execute(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	mu.Lock()
	got := append([]byte(nil), seenBody...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatalf("upstream body is empty")
	}
	if gjson.GetBytes(got, "tools.defer_loading").Exists() {
		t.Fatalf("tools.defer_loading still exists: %s", string(got))
	}
	if gjson.GetBytes(got, "tools").Exists() && !gjson.GetBytes(got, "tools").IsArray() {
		t.Fatalf("tools is not array after normalization: %s", gjson.GetBytes(got, "tools").Raw)
	}
	if gjson.GetBytes(got, "tool_choice").Exists() {
		t.Fatalf("expected tool_choice to be removed for Codex upstream, got %s", string(got))
	}
}

func TestCodexExecutorStripsToolChoiceForClaudeFailoverRequests(t *testing.T) {
	var mu sync.Mutex
	var seenBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		mu.Lock()
		seenBody = b
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
	}))
	defer srv.Close()

	exec := NewCodexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test", "base_url": srv.URL}}
	req := cliproxyexecutor.Request{Model: "gpt-5.4", Payload: []byte(`{
		"model":"claude-sonnet-4-6",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"hi"}]}
		],
		"tools":[
			{"name":"Bash","description":"Run shell commands","input_schema":{"type":"object","properties":{}}}
		],
		"tool_choice":{"type":"auto"}
	}`)}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := exec.Execute(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	mu.Lock()
	got := append([]byte(nil), seenBody...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatalf("upstream body is empty")
	}
	if !gjson.GetBytes(got, "tools").IsArray() {
		t.Fatalf("expected translated tools array, got %s", string(got))
	}
	if gjson.GetBytes(got, "tool_choice").Exists() {
		t.Fatalf("expected tool_choice to be removed for Claude failover, got %s", string(got))
	}
	if gjson.GetBytes(got, "store").Exists() {
		t.Fatalf("expected store to be removed for Claude failover, got %s", string(got))
	}
}

func TestCodexExecutorStripsStoreBeforeUpstream(t *testing.T) {
	var mu sync.Mutex
	var seenBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		mu.Lock()
		seenBody = b
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
	}))
	defer srv.Close()

	exec := NewCodexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test", "base_url": srv.URL}}
	req := cliproxyexecutor.Request{Model: "gpt-5.4", Payload: []byte(`{"input":"hi","store":false}`)}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("codex")}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := exec.Execute(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	mu.Lock()
	got := append([]byte(nil), seenBody...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatalf("upstream body is empty")
	}
	if gjson.GetBytes(got, "store").Exists() {
		t.Fatalf("expected store to be removed before upstream request, got %s", string(got))
	}
}

func TestCodexExecutorDropsToolsForImageInputs(t *testing.T) {
	var mu sync.Mutex
	var seenBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		mu.Lock()
		seenBody = b
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
	}))
	defer srv.Close()

	exec := NewCodexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test", "base_url": srv.URL}}
	req := cliproxyexecutor.Request{Model: "gpt-5.4", Payload: []byte(`{
		"model":"claude-sonnet-4-6",
		"messages":[
			{
				"role":"user",
				"content":[
					{"type":"text","text":"describe this image"},
					{"type":"image","source":{"type":"base64","media_type":"image/png","data":"YWJj"}}
				]
			}
		],
		"tools":[
			{"name":"Bash","description":"Run shell commands","input_schema":{"type":"object","properties":{}}}
		]
	}`)}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := exec.Execute(ctx, auth, req, opts)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	mu.Lock()
	got := append([]byte(nil), seenBody...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatalf("upstream body is empty")
	}
	if !gjson.GetBytes(got, "input.0.content.1.image_url").Exists() {
		t.Fatalf("expected translated image input, got %s", string(got))
	}
	if gjson.GetBytes(got, "tools").Exists() {
		t.Fatalf("expected tools to be removed for image request, got %s", string(got))
	}
	if gjson.GetBytes(got, "tool_choice").Exists() {
		t.Fatalf("expected tool_choice to be removed for image request, got %s", string(got))
	}
}
