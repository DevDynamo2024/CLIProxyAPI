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

func TestConvertCodexResponseToClaude_MapsHostedWebSearchUsageAndBlocks(t *testing.T) {
	var param any
	originalReq := []byte(`{"tools":[{"type":"web_search_20250305","name":"web_search"}]}`)

	out := ConvertCodexResponseToClaude(
		context.Background(),
		"gpt-5.4",
		originalReq,
		nil,
		[]byte(`data: {"type":"response.output_item.done","item":{"id":"ws_123","type":"web_search_call","status":"completed","action":{"type":"search","query":"Beijing weather today","queries":["Beijing weather today"]}}}`),
		&param,
	)

	joined := strings.Join(out, "")
	if !strings.Contains(joined, `"type":"server_tool_use"`) {
		t.Fatalf("expected server_tool_use block, got %q", joined)
	}
	if !strings.Contains(joined, `"name":"web_search"`) {
		t.Fatalf("expected web_search tool name, got %q", joined)
	}
	if !strings.Contains(joined, `"partial_json":"{\"query\":\"Beijing weather today\"}"`) {
		t.Fatalf("expected hosted search query to be streamed as input_json_delta, got %q", joined)
	}

	out = ConvertCodexResponseToClaude(
		context.Background(),
		"gpt-5.4",
		originalReq,
		nil,
		[]byte(`data: {"type":"response.completed","response":{"stop_reason":"stop","usage":{"input_tokens":10,"output_tokens":5},"tool_usage":{"web_search":{"num_requests":1}},"output":[{"type":"message","content":[{"type":"output_text","text":"Forecast from Metcheck.","annotations":[{"type":"url_citation","start_index":14,"end_index":22,"title":"Metcheck Forecast","url":"https://example.com/forecast"}]}]}]}}`),
		&param,
	)

	joined = strings.Join(out, "")
	if !strings.Contains(joined, `"type":"web_search_tool_result"`) {
		t.Fatalf("expected web_search_tool_result block in completed event, got %q", joined)
	}
	if !strings.Contains(joined, `"usage":{"input_tokens":10,"output_tokens":5,"server_tool_use":{"web_search_requests":1}}`) {
		t.Fatalf("expected web_search usage to be mapped into Claude usage, got %q", joined)
	}
	if !strings.Contains(joined, `"url":"https://example.com/forecast"`) {
		t.Fatalf("expected citation url to be preserved in web_search_tool_result, got %q", joined)
	}
}

func TestConvertCodexResponseToClaudeNonStream_MapsHostedWebSearchBlocks(t *testing.T) {
	originalReq := []byte(`{"tools":[{"type":"web_search_20250305","name":"web_search"}]}`)
	raw := []byte(`{"type":"response.completed","response":{"id":"resp_123","model":"gpt-5.4","stop_reason":"stop","usage":{"input_tokens":20,"output_tokens":8},"tool_usage":{"web_search":{"num_requests":2}},"output":[{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"March 19 top news","queries":["March 19 top news"]}},{"id":"msg_1","type":"message","status":"completed","content":[{"type":"output_text","text":"Top story from Reuters.","annotations":[{"type":"url_citation","start_index":15,"end_index":22,"title":"Reuters Top Story","url":"https://www.reuters.com/world"}]}],"role":"assistant"}]}}`)

	out := ConvertCodexResponseToClaudeNonStream(context.Background(), "gpt-5.4", originalReq, nil, raw, nil)

	if !strings.Contains(out, `"type":"server_tool_use"`) {
		t.Fatalf("expected server_tool_use block, got %q", out)
	}
	if !strings.Contains(out, `"type":"web_search_tool_result"`) {
		t.Fatalf("expected web_search_tool_result block, got %q", out)
	}
	if !strings.Contains(out, `"web_search_requests":2`) {
		t.Fatalf("expected Claude usage to include web_search_requests, got %q", out)
	}
	if !strings.Contains(out, `"citations":[{"type":"web_search_result_location","url":"https://www.reuters.com/world"`) {
		t.Fatalf("expected output_text annotations to map to Claude citations, got %q", out)
	}
}
