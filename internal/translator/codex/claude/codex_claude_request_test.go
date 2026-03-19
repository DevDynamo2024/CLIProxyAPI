package claude

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertClaudeRequestToCodex_PreservesStructuredToolResultContent(t *testing.T) {
	input := []byte(`{
		"model": "claude-sonnet-4-6",
		"messages": [
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_123",
						"content": [
							{"type": "text", "text": "Search result summary"},
							{"type": "image", "source": {"type":"base64","media_type":"image/png","data":"aGVsbG8="}}
						]
					}
				]
			}
		]
	}`)

	out := ConvertClaudeRequestToCodex("gpt-5.4", input, false)

	if got := gjson.GetBytes(out, "input.0.type").String(); got != "function_call_output" {
		t.Fatalf("input.0.type = %q, want %q; raw=%s", got, "function_call_output", string(out))
	}
	if got := gjson.GetBytes(out, "input.0.call_id").String(); got != "toolu_123" {
		t.Fatalf("input.0.call_id = %q, want %q; raw=%s", got, "toolu_123", string(out))
	}
	if got := gjson.GetBytes(out, "input.0.output.0.type").String(); got != "input_text" {
		t.Fatalf("input.0.output.0.type = %q, want %q; raw=%s", got, "input_text", string(out))
	}
	if got := gjson.GetBytes(out, "input.0.output.0.text").String(); got != "Search result summary" {
		t.Fatalf("input.0.output.0.text = %q, want %q; raw=%s", got, "Search result summary", string(out))
	}
	if got := gjson.GetBytes(out, "input.0.output.1.type").String(); got != "input_image" {
		t.Fatalf("input.0.output.1.type = %q, want %q; raw=%s", got, "input_image", string(out))
	}
	if got := gjson.GetBytes(out, "input.0.output.1.image_url").String(); got != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("input.0.output.1.image_url = %q, want %q; raw=%s", got, "data:image/png;base64,aGVsbG8=", string(out))
	}
}

func TestConvertClaudeRequestToCodex_WebSearchToolUsesFunctionSemantics(t *testing.T) {
	input := []byte(`{
		"model": "claude-sonnet-4-6",
		"tools": [
			{
				"type": "web_search_20250305",
				"name": "web_search",
				"description": "Search the web for recent news",
				"input_schema": {
					"type": "object",
					"properties": {
						"query": {"type": "string"}
					},
					"required": ["query"]
				}
			}
		],
		"messages": [{"role":"user","content":"Find today's headlines"}]
	}`)

	out := ConvertClaudeRequestToCodex("gpt-5.4", input, false)

	if got := gjson.GetBytes(out, "tools.0.type").String(); got != "function" {
		t.Fatalf("tools.0.type = %q, want %q; raw=%s", got, "function", string(out))
	}
	if got := gjson.GetBytes(out, "tools.0.name").String(); got != "web_search" {
		t.Fatalf("tools.0.name = %q, want %q; raw=%s", got, "web_search", string(out))
	}
	if got := gjson.GetBytes(out, "tools.0.description").String(); got != "Search the web for recent news" {
		t.Fatalf("tools.0.description = %q, want %q; raw=%s", got, "Search the web for recent news", string(out))
	}
	if got := gjson.GetBytes(out, "tools.0.parameters.type").String(); got != "object" {
		t.Fatalf("tools.0.parameters.type = %q, want %q; raw=%s", got, "object", string(out))
	}
	if got := gjson.GetBytes(out, "tools.0.parameters.properties.query.type").String(); got != "string" {
		t.Fatalf("tools.0.parameters.properties.query.type = %q, want %q; raw=%s", got, "string", string(out))
	}
	if got := gjson.GetBytes(out, "tools.0.required.0").String(); got != "" {
		t.Fatalf("tools.0.required should not exist at top level; raw=%s", string(out))
	}
	if got := gjson.GetBytes(out, "tools.0.parameters.required.0").String(); got != "query" {
		t.Fatalf("tools.0.parameters.required.0 = %q, want %q; raw=%s", got, "query", string(out))
	}
	if got := gjson.GetBytes(out, "tools.0.input_schema").Exists(); got {
		t.Fatalf("tools.0.input_schema should be removed; raw=%s", string(out))
	}
}
