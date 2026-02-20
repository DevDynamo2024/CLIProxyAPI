package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
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
