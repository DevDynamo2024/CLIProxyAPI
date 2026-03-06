package chat_completions

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestToClaude_UnwrapsToolsWrapperAndMapsToolChoice(t *testing.T) {
	in := []byte(`{
  "model":"claude-opus-4-6",
  "messages":[{"role":"user","content":"check"}],
  "tools":{"defer_loading":true,"tools":[{"type":"function","function":{"name":"Bash","description":"Run shell","parameters":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}}]},
  "tool_choice":{"type":"function","function":{"name":"Bash"}},
  "stream":true
}`)

	out := ConvertOpenAIRequestToClaude("claude-opus-4-6", in, true)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid json: %s", string(out))
	}

	if got := gjson.GetBytes(out, "tools.0.name").String(); got != "Bash" {
		t.Fatalf("tools.0.name = %q, want %q", got, "Bash")
	}
	if got := gjson.GetBytes(out, "tools.0.input_schema.properties.command.type").String(); got != "string" {
		t.Fatalf("tools.0.input_schema.properties.command.type = %q, want %q", got, "string")
	}
	if got := gjson.GetBytes(out, "tool_choice.type").String(); got != "tool" {
		t.Fatalf("tool_choice.type = %q, want %q", got, "tool")
	}
	if got := gjson.GetBytes(out, "tool_choice.name").String(); got != "Bash" {
		t.Fatalf("tool_choice.name = %q, want %q", got, "Bash")
	}
}

func TestConvertOpenAIRequestToClaude_ToolChoiceNestedUnderToolsWrapper(t *testing.T) {
	in := []byte(`{
  "model":"claude-opus-4-6",
  "messages":[{"role":"user","content":"check"}],
  "tools":{
    "tool_choice":{"type":"function","function":{"name":"Bash"}},
    "tools":[{"type":"function","function":{"name":"Bash","description":"Run shell","parameters":{"type":"object","properties":{"command":{"type":"string"}}}}}]
  }
}`)

	out := ConvertOpenAIRequestToClaude("claude-opus-4-6", in, false)
	if got := gjson.GetBytes(out, "tool_choice.name").String(); got != "Bash" {
		t.Fatalf("tool_choice.name = %q, want %q", got, "Bash")
	}
}

func TestConvertOpenAIRequestToClaude_DropsSpecificToolChoiceWhenToolMissing(t *testing.T) {
	in := []byte(`{
  "model":"claude-opus-4-6",
  "messages":[{"role":"user","content":"check"}],
  "tools":[{"type":"function","function":{"name":"OtherTool","description":"x","parameters":{"type":"object"}}}],
  "tool_choice":{"type":"function","function":{"name":"Bash"}}
}`)

	out := ConvertOpenAIRequestToClaude("claude-opus-4-6", in, false)
	if got := gjson.GetBytes(out, "tools.0.name").String(); got != "OtherTool" {
		t.Fatalf("tools.0.name = %q, want %q", got, "OtherTool")
	}
	if gjson.GetBytes(out, "tool_choice").Exists() {
		t.Fatalf("tool_choice should be dropped when referenced tool missing; got %s", gjson.GetBytes(out, "tool_choice").Raw)
	}
}

func TestConvertOpenAIRequestToClaude_MapsLegacyFunctionsAndFunctionCall(t *testing.T) {
	in := []byte(`{
  "model":"claude-opus-4-6",
  "messages":[{"role":"user","content":"check"}],
  "functions":[{"name":"Read","description":"Read file","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}],
  "function_call":{"name":"Read"}
}`)

	out := ConvertOpenAIRequestToClaude("claude-opus-4-6", in, false)
	if got := gjson.GetBytes(out, "tools.0.name").String(); got != "Read" {
		t.Fatalf("tools.0.name = %q, want %q", got, "Read")
	}
	if got := gjson.GetBytes(out, "tool_choice.name").String(); got != "Read" {
		t.Fatalf("tool_choice.name = %q, want %q", got, "Read")
	}
}
