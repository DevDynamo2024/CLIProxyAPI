package claude

import (
	"context"
	"strings"
	"testing"
)

func TestConvertCodexResponseToClaude_EmitsArgumentsFromDoneWhenDeltaMissing(t *testing.T) {
	var param any
	originalReq := []byte(`{"tools":[{"name":"web_search"}]}`)

	ConvertCodexResponseToClaude(
		context.Background(),
		"gpt-5.4",
		originalReq,
		nil,
		[]byte(`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_123","name":"web_search"}}`),
		&param,
	)

	out := ConvertCodexResponseToClaude(
		context.Background(),
		"gpt-5.4",
		originalReq,
		nil,
		[]byte("data: {\"type\":\"response.function_call_arguments.done\",\"arguments\":\"{\\\"query\\\":\\\"today news\\\"}\"}"),
		&param,
	)

	joined := strings.Join(out, "")
	if !strings.Contains(joined, `"type":"content_block_delta"`) {
		t.Fatalf("expected synthesized content_block_delta, got %q", joined)
	}
	if !strings.Contains(joined, `"partial_json":"{\"query\":\"today news\"}"`) {
		t.Fatalf("expected partial_json from done event, got %q", joined)
	}
}

func TestConvertCodexResponseToClaude_DoesNotDuplicateArgumentsDoneAfterDelta(t *testing.T) {
	var param any
	originalReq := []byte(`{"tools":[{"name":"web_search"}]}`)

	ConvertCodexResponseToClaude(
		context.Background(),
		"gpt-5.4",
		originalReq,
		nil,
		[]byte(`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_123","name":"web_search"}}`),
		&param,
	)

	ConvertCodexResponseToClaude(
		context.Background(),
		"gpt-5.4",
		originalReq,
		nil,
		[]byte("data: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{\\\"query\\\":\"}"),
		&param,
	)

	out := ConvertCodexResponseToClaude(
		context.Background(),
		"gpt-5.4",
		originalReq,
		nil,
		[]byte("data: {\"type\":\"response.function_call_arguments.done\",\"arguments\":\"{\\\"query\\\":\\\"today news\\\"}\"}"),
		&param,
	)

	joined := strings.Join(out, "")
	if strings.Contains(joined, `"partial_json":"{\"query\":\"today news\"}"`) {
		t.Fatalf("expected done event to be ignored after delta, got %q", joined)
	}
}
