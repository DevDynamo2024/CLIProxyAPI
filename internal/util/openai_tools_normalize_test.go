package util

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIToolsPayload_DropsDanglingToolChoice(t *testing.T) {
	in := []byte(`{
		"model":"gpt-5.4",
		"messages":[{"role":"user","content":"hi"}],
		"tool_choice":"auto",
		"parallel_tool_calls":true
	}`)

	out := NormalizeOpenAIToolsPayload(in)

	if gjson.GetBytes(out, "tool_choice").Exists() {
		t.Fatalf("expected tool_choice to be removed, got %s", string(out))
	}
	if gjson.GetBytes(out, "parallel_tool_calls").Exists() {
		t.Fatalf("expected parallel_tool_calls to be removed, got %s", string(out))
	}
}

func TestNormalizeOpenAIToolsPayload_DropsUnknownSpecificToolChoice(t *testing.T) {
	in := []byte(`{
		"model":"gpt-5.4",
		"messages":[{"role":"user","content":"hi"}],
		"tools":[{"type":"function","function":{"name":"Read","parameters":{"type":"object"}}}],
		"tool_choice":{"type":"function","function":{"name":"Write"}}
	}`)

	out := NormalizeOpenAIToolsPayload(in)

	if gjson.GetBytes(out, "tool_choice").Exists() {
		t.Fatalf("expected unknown specific tool_choice to be removed, got %s", string(out))
	}
}
