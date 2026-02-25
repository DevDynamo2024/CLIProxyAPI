package executor

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestStripThinkingBlocksFromClaudePayload_StripsSystemAndMessages(t *testing.T) {
	input := []byte(`{
		"model":"claude-opus-4-6",
		"system":[
			{"type":"thinking","thinking":"secret","signature":"sig"},
			{"type":"text","text":"sys"}
		],
		"messages":[
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"t1","signature":"sig1"},
				{"type":"tool_use","id":"toolu_1","name":"bash","input":{"cmd":"echo hi"}},
				{"type":"text","text":"ok"}
			]},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"t2","signature":"sig2"}
			]},
			{"role":"user","content":[
				{"type":"text","text":"hi"}
			]}
		]
	}`)

	out := stripThinkingBlocksFromClaudePayload(input)
	root := gjson.ParseBytes(out)

	system := root.Get("system")
	if !system.IsArray() || len(system.Array()) != 1 {
		t.Fatalf("system=%s, want single text item", system.Raw)
	}
	if system.Array()[0].Get("type").String() != "text" {
		t.Fatalf("system[0].type=%q, want %q", system.Array()[0].Get("type").String(), "text")
	}

	msgs := root.Get("messages")
	if !msgs.IsArray() {
		t.Fatalf("messages is not array: %s", msgs.Raw)
	}
	// Middle assistant message had only thinking -> should be removed.
	if len(msgs.Array()) != 2 {
		t.Fatalf("messages len=%d, want 2; raw=%s", len(msgs.Array()), msgs.Raw)
	}

	assistant := msgs.Array()[0]
	content := assistant.Get("content")
	if !content.IsArray() {
		t.Fatalf("assistant.content is not array: %s", content.Raw)
	}
	for _, part := range content.Array() {
		typ := part.Get("type").String()
		if typ == "thinking" || typ == "redacted_thinking" {
			t.Fatalf("unexpected thinking part in assistant content: %s", part.Raw)
		}
	}
}

func TestIsInvalidThinkingSignatureError(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"messages.1.content.0: Invalid ` + "`signature`" + ` in ` + "`thinking`" + ` block"}}`)
	if !isInvalidThinkingSignatureError(400, body) {
		t.Fatal("expected invalid thinking signature error to be detected")
	}
	if isInvalidThinkingSignatureError(500, body) {
		t.Fatal("expected non-400 to not be detected")
	}
}

