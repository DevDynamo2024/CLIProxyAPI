package openai

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIToolsPayload_UnwrapsToolsWrapperObject(t *testing.T) {
	input := []byte(`{
		"model":"gpt-5.2",
		"tools":{
			"defer_loading":true,
			"tool_choice":"auto",
			"tools":[
				{"type":"function","function":{"name":"t","parameters":{"type":"object","properties":{}}}}
			]
		}
	}`)

	out := normalizeOpenAIToolsPayload(input)
	root := gjson.ParseBytes(out)

	if root.Get("tools").Exists() && !root.Get("tools").IsArray() {
		t.Fatalf("tools is not array after normalization: %s", root.Get("tools").Raw)
	}
	if root.Get("tools.defer_loading").Exists() {
		t.Fatalf("tools.defer_loading still exists: %s", root.Get("tools").Raw)
	}
	if got := root.Get("tool_choice").String(); got != "auto" {
		t.Fatalf("tool_choice=%q, want %q; raw=%s", got, "auto", root.Raw)
	}
}

