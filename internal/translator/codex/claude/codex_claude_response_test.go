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

func TestConvertCodexResponseToClaude_IgnoresHostedWebSearchNonSearchActions(t *testing.T) {
	var param any
	originalReq := []byte(`{"tools":[{"type":"web_search_20250305","name":"web_search"}]}`)

	out := ConvertCodexResponseToClaude(
		context.Background(),
		"gpt-5.4",
		originalReq,
		nil,
		[]byte(`data: {"type":"response.output_item.done","item":{"id":"ws_search","type":"web_search_call","status":"completed","action":{"type":"search","query":"Beijing AQI","queries":["Beijing AQI"]}}}`),
		&param,
	)
	joined := strings.Join(out, "")
	if !strings.Contains(joined, `"type":"server_tool_use"`) {
		t.Fatalf("expected search action to emit server_tool_use, got %q", joined)
	}

	out = ConvertCodexResponseToClaude(
		context.Background(),
		"gpt-5.4",
		originalReq,
		nil,
		[]byte(`data: {"type":"response.output_item.done","item":{"id":"ws_open","type":"web_search_call","status":"completed","action":{"type":"open_page","url":"https://example.com/page"}}}`),
		&param,
	)
	if joined = strings.Join(out, ""); joined != "" {
		t.Fatalf("expected open_page action to be ignored, got %q", joined)
	}

	out = ConvertCodexResponseToClaude(
		context.Background(),
		"gpt-5.4",
		originalReq,
		nil,
		[]byte(`data: {"type":"response.output_item.done","item":{"id":"ws_find","type":"web_search_call","status":"completed","action":{"type":"find_in_page","url":"https://example.com/page","query":"humidity"}}}`),
		&param,
	)
	if joined = strings.Join(out, ""); joined != "" {
		t.Fatalf("expected find_in_page action to be ignored, got %q", joined)
	}

	out = ConvertCodexResponseToClaude(
		context.Background(),
		"gpt-5.4",
		originalReq,
		nil,
		[]byte(`data: {"type":"response.completed","response":{"stop_reason":"stop","usage":{"input_tokens":10,"output_tokens":5},"tool_usage":{"web_search":{"num_requests":1}},"output":[{"type":"message","content":[{"type":"output_text","text":"AQI source.","annotations":[{"type":"url_citation","start_index":0,"end_index":3,"title":"AQI Source","url":"https://example.com/aqi"}]}]}]}}`),
		&param,
	)
	joined = strings.Join(out, "")
	if count := strings.Count(joined, `"type":"web_search_tool_result"`); count != 1 {
		t.Fatalf("expected exactly one web_search_tool_result block, got %d in %q", count, joined)
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

func TestConvertCodexResponseToClaudeNonStream_IgnoresHostedWebSearchNonSearchActions(t *testing.T) {
	originalReq := []byte(`{"tools":[{"type":"web_search_20250305","name":"web_search"}]}`)
	raw := []byte(`{"type":"response.completed","response":{"id":"resp_123","model":"gpt-5.4","stop_reason":"stop","usage":{"input_tokens":20,"output_tokens":8},"tool_usage":{"web_search":{"num_requests":1}},"output":[{"id":"ws_search","type":"web_search_call","status":"completed","action":{"type":"search","query":"Beijing weather","queries":["Beijing weather"]}},{"id":"ws_open","type":"web_search_call","status":"completed","action":{"type":"open_page","url":"https://example.com/page"}},{"id":"ws_find","type":"web_search_call","status":"completed","action":{"type":"find_in_page","url":"https://example.com/page","query":"humidity"}},{"id":"msg_1","type":"message","status":"completed","content":[{"type":"output_text","text":"Weather source.","annotations":[{"type":"url_citation","start_index":0,"end_index":7,"title":"Weather Source","url":"https://example.com/weather"}]}],"role":"assistant"}]}}`)

	out := ConvertCodexResponseToClaudeNonStream(context.Background(), "gpt-5.4", originalReq, nil, raw, nil)

	if count := strings.Count(out, `"type":"server_tool_use"`); count != 1 {
		t.Fatalf("expected exactly one server_tool_use block, got %d in %q", count, out)
	}
	if count := strings.Count(out, `"type":"web_search_tool_result"`); count != 1 {
		t.Fatalf("expected exactly one web_search_tool_result block, got %d in %q", count, out)
	}
}
