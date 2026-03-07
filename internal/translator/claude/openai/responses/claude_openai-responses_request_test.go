package responses

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIResponsesRequestToClaude_UsesAdaptiveThinkingForClaude46(t *testing.T) {
	in := []byte(`{
  "model":"claude-opus-4-6",
  "input":[{"role":"user","content":[{"type":"input_text","text":"check"}]}],
  "reasoning":{"effort":"xhigh"}
}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-opus-4-6", in, false)
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "adaptive" {
		t.Fatalf("thinking.type = %q, want %q", got, "adaptive")
	}
	if got := gjson.GetBytes(out, "output_config.effort").String(); got != "max" {
		t.Fatalf("output_config.effort = %q, want %q", got, "max")
	}
}

func TestConvertOpenAIResponsesRequestToClaude_MapsInputFileToDocument(t *testing.T) {
	in := []byte(`{
  "model":"claude-opus-4-6",
  "input":[{"role":"user","content":[{"type":"input_file","file_data":"data:text/plain;base64,SGVsbG8="}]}]
}`)

	out := ConvertOpenAIResponsesRequestToClaude("claude-opus-4-6", in, false)
	if got := gjson.GetBytes(out, "messages.0.content.0.type").String(); got != "document" {
		t.Fatalf("messages.0.content.0.type = %q, want %q", got, "document")
	}
	if got := gjson.GetBytes(out, "messages.0.content.0.source.media_type").String(); got != "text/plain" {
		t.Fatalf("messages.0.content.0.source.media_type = %q, want %q", got, "text/plain")
	}
	if got := gjson.GetBytes(out, "messages.0.content.0.source.data").String(); got != "SGVsbG8=" {
		t.Fatalf("messages.0.content.0.source.data = %q, want %q", got, "SGVsbG8=")
	}
}
