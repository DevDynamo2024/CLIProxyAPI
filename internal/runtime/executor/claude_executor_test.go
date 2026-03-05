package executor

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

func TestApplyClaudeToolPrefix(t *testing.T) {
	input := []byte(`{"tools":[{"name":"alpha"},{"name":"proxy_bravo"}],"tool_choice":{"type":"tool","name":"charlie"},"messages":[{"role":"assistant","content":[{"type":"tool_use","name":"delta","id":"t1","input":{}}]}]}`)
	out := applyClaudeToolPrefix(input, "proxy_")

	if got := gjson.GetBytes(out, "tools.0.name").String(); got != "proxy_alpha" {
		t.Fatalf("tools.0.name = %q, want %q", got, "proxy_alpha")
	}
	if got := gjson.GetBytes(out, "tools.1.name").String(); got != "proxy_bravo" {
		t.Fatalf("tools.1.name = %q, want %q", got, "proxy_bravo")
	}
	if got := gjson.GetBytes(out, "tool_choice.name").String(); got != "proxy_charlie" {
		t.Fatalf("tool_choice.name = %q, want %q", got, "proxy_charlie")
	}
	if got := gjson.GetBytes(out, "messages.0.content.0.name").String(); got != "proxy_delta" {
		t.Fatalf("messages.0.content.0.name = %q, want %q", got, "proxy_delta")
	}
}

func TestApplyClaudeToolPrefix_SkipsBuiltinTools(t *testing.T) {
	input := []byte(`{"tools":[{"type":"web_search_20250305","name":"web_search"},{"name":"my_custom_tool","input_schema":{"type":"object"}}]}`)
	out := applyClaudeToolPrefix(input, "proxy_")

	if got := gjson.GetBytes(out, "tools.0.name").String(); got != "web_search" {
		t.Fatalf("built-in tool name should not be prefixed: tools.0.name = %q, want %q", got, "web_search")
	}
	if got := gjson.GetBytes(out, "tools.1.name").String(); got != "proxy_my_custom_tool" {
		t.Fatalf("custom tool should be prefixed: tools.1.name = %q, want %q", got, "proxy_my_custom_tool")
	}
}

func TestStripClaudeToolPrefixFromResponse(t *testing.T) {
	input := []byte(`{"content":[{"type":"tool_use","name":"proxy_alpha","id":"t1","input":{}},{"type":"tool_use","name":"bravo","id":"t2","input":{}}]}`)
	out := stripClaudeToolPrefixFromResponse(input, "proxy_")

	if got := gjson.GetBytes(out, "content.0.name").String(); got != "alpha" {
		t.Fatalf("content.0.name = %q, want %q", got, "alpha")
	}
	if got := gjson.GetBytes(out, "content.1.name").String(); got != "bravo" {
		t.Fatalf("content.1.name = %q, want %q", got, "bravo")
	}
}

func TestStripClaudeToolPrefixFromStreamLine(t *testing.T) {
	line := []byte(`data: {"type":"content_block_start","content_block":{"type":"tool_use","name":"proxy_alpha","id":"t1"},"index":0}`)
	out := stripClaudeToolPrefixFromStreamLine(line, "proxy_")

	payload := bytes.TrimSpace(out)
	if bytes.HasPrefix(payload, []byte("data:")) {
		payload = bytes.TrimSpace(payload[len("data:"):])
	}
	if got := gjson.GetBytes(payload, "content_block.name").String(); got != "alpha" {
		t.Fatalf("content_block.name = %q, want %q", got, "alpha")
	}
}

func TestResolveClaudeBaseURL_EnvOverridesAndTrimsSlash(t *testing.T) {
	t.Setenv(anthropicBaseURLEnv, "https://gateway.example.com/v1/acct/gw/anthropic/")

	baseURL := resolveClaudeBaseURL(&cliproxyauth.Auth{
		Attributes: map[string]string{
			"base_url": "https://should-not-win.example.com",
		},
	})
	if baseURL != "https://gateway.example.com/v1/acct/gw/anthropic" {
		t.Fatalf("baseURL = %q, want %q", baseURL, "https://gateway.example.com/v1/acct/gw/anthropic")
	}
}

func TestApplyClaudeHeaders_EnvForcesXAPIKeyWhenUsingAPIKey(t *testing.T) {
	t.Setenv(anthropicBaseURLEnv, "https://gateway.example.com/v1/acct/gw/anthropic")
	req, err := http.NewRequest(http.MethodPost, "https://gateway.example.com/v1/acct/gw/anthropic/v1/messages", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{
			"api_key": "sk-ant-test",
		},
	}
	applyClaudeHeaders(req, auth, "sk-ant-test", false, nil)

	if got := req.Header.Get("x-api-key"); got != "sk-ant-test" {
		t.Fatalf("x-api-key = %q, want %q", got, "sk-ant-test")
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty", got)
	}
}

func TestDecodeResponseBytesBestEffort_GzipWithHeader(t *testing.T) {
	want := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"boom"}}`)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(want); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	got := decodeResponseBytesBestEffort(buf.Bytes(), "gzip")
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded = %q, want %q", string(got), string(want))
	}
}

func TestDecodeResponseBytesBestEffort_GzipWithoutHeader(t *testing.T) {
	want := []byte(`{"error":{"message":"compressed"}}`)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := io.Copy(zw, bytes.NewReader(want)); err != nil {
		t.Fatalf("gzip copy: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	got := decodeResponseBytesBestEffort(buf.Bytes(), "")
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded = %q, want %q", string(got), string(want))
	}
}

func TestNormalizeClaudeToolsForUpstream_UnwrapsToolsWrapperAndHoistsToolChoice(t *testing.T) {
	in := []byte(`{
  "tools": {
    "defer_loading": true,
    "tool_choice": {"type":"tool","name":"Bash"},
    "tools": [
      {"name":"Bash","description":"Run shell","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}}
    ]
  }
}`)

	out := normalizeClaudeToolsForUpstream(in)
	if got := gjson.GetBytes(out, "tools.0.name").String(); got != "Bash" {
		t.Fatalf("tools.0.name = %q, want %q", got, "Bash")
	}
	if gjson.GetBytes(out, "tools.defer_loading").Exists() {
		t.Fatalf("tools.defer_loading should be removed")
	}
	if got := gjson.GetBytes(out, "tool_choice.name").String(); got != "Bash" {
		t.Fatalf("tool_choice.name = %q, want %q", got, "Bash")
	}
}
