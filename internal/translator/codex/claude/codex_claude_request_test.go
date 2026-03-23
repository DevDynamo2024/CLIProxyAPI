package claude

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertClaudeRequestToCodex_SystemMessageScenarios(t *testing.T) {
	tests := []struct {
		name             string
		inputJSON        string
		wantHasDeveloper bool
		wantTexts        []string
	}{
		{
			name: "No system field",
			inputJSON: `{
				"model": "claude-3-opus",
				"messages": [{"role": "user", "content": "hello"}]
			}`,
			wantHasDeveloper: false,
		},
		{
			name: "Empty string system field",
			inputJSON: `{
				"model": "claude-3-opus",
				"system": "",
				"messages": [{"role": "user", "content": "hello"}]
			}`,
			wantHasDeveloper: false,
		},
		{
			name: "String system field",
			inputJSON: `{
				"model": "claude-3-opus",
				"system": "Be helpful",
				"messages": [{"role": "user", "content": "hello"}]
			}`,
			wantHasDeveloper: true,
			wantTexts:        []string{"Be helpful"},
		},
		{
			name: "Array system field with filtered billing header",
			inputJSON: `{
				"model": "claude-3-opus",
				"system": [
					{"type": "text", "text": "x-anthropic-billing-header: tenant-123"},
					{"type": "text", "text": "Block 1"},
					{"type": "text", "text": "Block 2"}
				],
				"messages": [{"role": "user", "content": "hello"}]
			}`,
			wantHasDeveloper: true,
			wantTexts:        []string{"Block 1", "Block 2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertClaudeRequestToCodex("test-model", []byte(tt.inputJSON), false)
			resultJSON := gjson.ParseBytes(result)
			inputs := resultJSON.Get("input").Array()

			hasDeveloper := len(inputs) > 0 && inputs[0].Get("role").String() == "developer"
			if hasDeveloper != tt.wantHasDeveloper {
				t.Fatalf("got hasDeveloper = %v, want %v. Output: %s", hasDeveloper, tt.wantHasDeveloper, resultJSON.Get("input").Raw)
			}

			if !tt.wantHasDeveloper {
				return
			}

			content := inputs[0].Get("content").Array()
			if len(content) != len(tt.wantTexts) {
				t.Fatalf("got %d system content items, want %d. Content: %s", len(content), len(tt.wantTexts), inputs[0].Get("content").Raw)
			}

			for i, wantText := range tt.wantTexts {
				if gotType := content[i].Get("type").String(); gotType != "input_text" {
					t.Fatalf("content[%d] type = %q, want %q", i, gotType, "input_text")
				}
				if gotText := content[i].Get("text").String(); gotText != wantText {
					t.Fatalf("content[%d] text = %q, want %q", i, gotText, wantText)
				}
			}
		})
	}
}

func TestConvertClaudeRequestToCodex_AdaptiveThinkingPreservesExplicitEffort(t *testing.T) {
	input := []byte(`{
		"model": "claude-opus-4-6",
		"messages": [{"role":"user","content":"hi"}],
		"thinking": {"type":"adaptive"},
		"output_config": {"effort":"max"}
	}`)

	out := ConvertClaudeRequestToCodex("gpt-5.4", input, false)

	if got := gjson.GetBytes(out, "reasoning.effort").String(); got != "max" {
		t.Fatalf("reasoning.effort = %q, want %q; raw=%s", got, "max", string(out))
	}
}

func TestConvertClaudeRequestToCodex_StripsClaudeToolOnlyFields(t *testing.T) {
	input := []byte(`{
		"model": "claude-sonnet-4-6",
		"tools": [
			{
				"name": "Bash",
				"description": "Run shell commands",
				"cache_control": {"type":"ephemeral"},
				"defer_loading": true,
				"input_schema": {"type":"object","properties":{}}
			}
		],
		"messages": [{"role":"user","content":"hi"}]
	}`)

	out := ConvertClaudeRequestToCodex("gpt-5.4", input, false)

	if gjson.GetBytes(out, "tools.0.cache_control").Exists() {
		t.Fatalf("tools.0.cache_control should be removed; raw=%s", string(out))
	}
	if gjson.GetBytes(out, "tools.0.defer_loading").Exists() {
		t.Fatalf("tools.0.defer_loading should be removed; raw=%s", string(out))
	}
	if !gjson.GetBytes(out, "store").Exists() || gjson.GetBytes(out, "store").Bool() {
		t.Fatalf("store=false should be set; raw=%s", string(out))
	}
}

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

func TestConvertClaudeRequestToCodex_WebSearchToolUsesBuiltinSemantics(t *testing.T) {
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

	if got := gjson.GetBytes(out, "tools.0.type").String(); got != "web_search" {
		t.Fatalf("tools.0.type = %q, want %q; raw=%s", got, "web_search", string(out))
	}
	if gjson.GetBytes(out, "tools.0.name").Exists() {
		t.Fatalf("tools.0.name should not exist for builtin web_search; raw=%s", string(out))
	}
	if gjson.GetBytes(out, "tools.0.description").Exists() {
		t.Fatalf("tools.0.description should not exist for builtin web_search; raw=%s", string(out))
	}
	if gjson.GetBytes(out, "tools.0.parameters").Exists() {
		t.Fatalf("tools.0.parameters should not exist for builtin web_search; raw=%s", string(out))
	}
	if got := gjson.GetBytes(out, "tool_choice").String(); got != "auto" {
		t.Fatalf("tool_choice = %q, want %q; raw=%s", got, "auto", string(out))
	}
}

func TestConvertClaudeRequestToCodex_PreservesToolReferencePayloadAsJSONString(t *testing.T) {
	input := []byte(`{
		"model": "claude-sonnet-4-6",
		"messages": [
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_456",
						"content": [
							{"type": "text", "text": "Found 3 relevant results"},
							{"type": "tool_reference", "tool_name": "web_search", "title": "Reuters", "url": "https://www.reuters.com/example"}
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
	output := gjson.GetBytes(out, "input.0.output").String()
	if output == "" {
		t.Fatalf("expected stringified output payload, got empty; raw=%s", string(out))
	}
	if !gjson.Valid(output) {
		t.Fatalf("expected output to contain original JSON array string, got %q", output)
	}
	if !gjson.Get(output, `#(type=="tool_reference").url`).Exists() {
		t.Fatalf("expected tool_reference URL to be preserved in output, got %q", output)
	}
}
