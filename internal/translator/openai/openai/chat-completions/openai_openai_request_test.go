package chat_completions

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestToOpenAI_StripsThinkingBlocks_ArrayContent(t *testing.T) {
	input := []byte(`{
		"model":"claude-opus-4-6",
		"messages":[
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"t","signature":"sig"},
				{"type":"text","text":"hi"}
			]},
			{"role":"user","content":"ok"}
		]
	}`)

	out := ConvertOpenAIRequestToOpenAI("gpt-5.2", input, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("model").String(); got != "gpt-5.2" {
		t.Fatalf("model=%q, want %q", got, "gpt-5.2")
	}

	msg0 := root.Get("messages.0")
	content0 := msg0.Get("content")
	if !content0.IsArray() {
		t.Fatalf("messages.0.content is not array: %s", content0.Raw)
	}
	arr := content0.Array()
	if len(arr) != 1 {
		t.Fatalf("messages.0.content len=%d, want 1; raw=%s", len(arr), content0.Raw)
	}
	if typ := arr[0].Get("type").String(); typ != "text" {
		t.Fatalf("messages.0.content[0].type=%q, want %q", typ, "text")
	}
	if txt := arr[0].Get("text").String(); txt != "hi" {
		t.Fatalf("messages.0.content[0].text=%q, want %q", txt, "hi")
	}
}

func TestConvertOpenAIRequestToOpenAI_StripsThinkingBlocks_ThinkingOnlyBecomesEmptyString(t *testing.T) {
	input := []byte(`{
		"model":"claude-opus-4-6",
		"messages":[
			{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"sig"}]}
		]
	}`)

	out := ConvertOpenAIRequestToOpenAI("gpt-5.2", input, false)
	root := gjson.ParseBytes(out)

	c := root.Get("messages.0.content")
	if c.Type != gjson.String {
		t.Fatalf("messages.0.content type=%v, want string; raw=%s", c.Type, c.Raw)
	}
	if c.String() != "" {
		t.Fatalf("messages.0.content=%q, want empty string", c.String())
	}
}

func TestConvertOpenAIRequestToOpenAI_StripsThinkingBlocks_ObjectContent(t *testing.T) {
	input := []byte(`{
		"model":"claude-opus-4-6",
		"messages":[
			{"role":"assistant","content":{"type":"thinking","thinking":"t","signature":"sig"}}
		]
	}`)

	out := ConvertOpenAIRequestToOpenAI("gpt-5.2", input, false)
	root := gjson.ParseBytes(out)

	c := root.Get("messages.0.content")
	if c.Type != gjson.String {
		t.Fatalf("messages.0.content type=%v, want string; raw=%s", c.Type, c.Raw)
	}
	if c.String() != "" {
		t.Fatalf("messages.0.content=%q, want empty string", c.String())
	}
}

func TestConvertOpenAIRequestToOpenAI_NormalizesToolsWrapperObject(t *testing.T) {
	input := []byte(`{
		"model":"claude-opus-4-6",
		"messages":[{"role":"user","content":"hi"}],
		"tools":{
			"defer_loading":true,
			"tool_choice":"auto",
			"tools":[
				{"type":"function","function":{"name":"t","description":"d","parameters":{"type":"object","properties":{}}}}
			]
		}
	}`)

	out := ConvertOpenAIRequestToOpenAI("gpt-5.2", input, false)
	root := gjson.ParseBytes(out)

	if root.Get("tools").Exists() && !root.Get("tools").IsArray() {
		t.Fatalf("tools is not array after normalization: %s", root.Get("tools").Raw)
	}
	if got := root.Get("tools.0.type").String(); got != "function" {
		t.Fatalf("tools.0.type=%q, want %q", got, "function")
	}
	if root.Get("tools.defer_loading").Exists() {
		t.Fatalf("tools.defer_loading still exists: %s", root.Get("tools").Raw)
	}
	if got := root.Get("tool_choice").String(); got != "auto" {
		t.Fatalf("tool_choice=%q, want %q; raw=%s", got, "auto", root.Raw)
	}
}

func TestConvertOpenAIRequestToOpenAI_RemovesToolEntryDeferLoading(t *testing.T) {
	input := []byte(`{
		"model":"claude-opus-4-6",
		"messages":[{"role":"user","content":"hi"}],
		"tools":[
			{"type":"function","defer_loading":true,"function":{"name":"t","parameters":{"type":"object","properties":{}}}}
		]
	}`)

	out := ConvertOpenAIRequestToOpenAI("gpt-5.2", input, false)
	root := gjson.ParseBytes(out)

	if root.Get("tools.0.defer_loading").Exists() {
		t.Fatalf("tools.0.defer_loading still exists: %s", root.Get("tools").Raw)
	}
}
