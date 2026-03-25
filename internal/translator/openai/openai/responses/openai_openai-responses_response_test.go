package responses

import (
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func parseOpenAIResponsesSSE(t *testing.T, chunk string) (string, gjson.Result) {
	t.Helper()

	lines := strings.Split(chunk, "\n")
	if len(lines) < 2 {
		t.Fatalf("unexpected SSE chunk: %q", chunk)
	}

	event := strings.TrimSpace(strings.TrimPrefix(lines[0], "event:"))
	dataLine := strings.TrimSpace(strings.TrimPrefix(lines[1], "data:"))
	if !gjson.Valid(dataLine) {
		t.Fatalf("invalid SSE payload: %q", dataLine)
	}
	return event, gjson.Parse(dataLine)
}

func TestConvertOpenAIChatCompletionsResponseToOpenAIResponses_MultipleToolCallIndices(t *testing.T) {
	in := []string{
		`data: {"id":"chatcmpl-tool","object":"chat.completion.chunk","created":1700000000,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_alpha","type":"function","function":{"name":"alpha","arguments":"{\"a\":"}},{"index":1,"id":"call_bravo","type":"function","function":{"name":"bravo","arguments":"{\"b\":"}}]}}]}`,
		`data: {"id":"chatcmpl-tool","object":"chat.completion.chunk","created":1700000000,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}},{"index":1,"function":{"arguments":"2}"}}]}}]`,
		`data: {"id":"chatcmpl-tool","object":"chat.completion.chunk","created":1700000000,"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}

	request := []byte(`{"model":"gpt-5","parallel_tool_calls":true}`)

	var param any
	var out []string
	for _, line := range in {
		out = append(out, ConvertOpenAIChatCompletionsResponseToOpenAIResponses(context.Background(), "gpt-5", request, request, []byte(line), &param)...)
	}

	addedPos := map[int]int{}
	donePos := map[int]int{}
	itemDonePos := map[int]int{}
	deltaByIndex := map[int]string{}
	completedPos := -1

	for i, chunk := range out {
		ev, data := parseOpenAIResponsesSSE(t, chunk)
		switch ev {
		case "response.output_item.added":
			if data.Get("item.type").String() == "function_call" {
				idx := int(data.Get("output_index").Int())
				if _, ok := addedPos[idx]; !ok {
					addedPos[idx] = i
				}
			}
		case "response.function_call_arguments.delta":
			idx := int(data.Get("output_index").Int())
			deltaByIndex[idx] += data.Get("delta").String()
		case "response.function_call_arguments.done":
			donePos[int(data.Get("output_index").Int())] = i
		case "response.output_item.done":
			if data.Get("item.type").String() == "function_call" {
				itemDonePos[int(data.Get("output_index").Int())] = i
			}
		case "response.completed":
			completedPos = i
			if got := data.Get("response.output.#").Int(); got != 2 {
				t.Fatalf("response.output len = %d, want 2", got)
			}
			if got := data.Get("response.output.0.call_id").String(); got != "call_alpha" {
				t.Fatalf("response.output[0].call_id = %q, want %q", got, "call_alpha")
			}
			if got := data.Get("response.output.1.call_id").String(); got != "call_bravo" {
				t.Fatalf("response.output[1].call_id = %q, want %q", got, "call_bravo")
			}
			if got := data.Get("response.output.0.arguments").String(); got != `{"a":1}` {
				t.Fatalf("response.output[0].arguments = %q", got)
			}
			if got := data.Get("response.output.1.arguments").String(); got != `{"b":2}` {
				t.Fatalf("response.output[1].arguments = %q", got)
			}
		}
	}

	for _, idx := range []int{0, 1} {
		if _, ok := addedPos[idx]; !ok {
			t.Fatalf("missing response.output_item.added for output_index %d", idx)
		}
		if _, ok := donePos[idx]; !ok {
			t.Fatalf("missing response.function_call_arguments.done for output_index %d", idx)
		}
		if _, ok := itemDonePos[idx]; !ok {
			t.Fatalf("missing response.output_item.done for output_index %d", idx)
		}
		if !(addedPos[idx] < donePos[idx] && donePos[idx] < itemDonePos[idx]) {
			t.Fatalf("unexpected function event order for index %d: added=%d done=%d itemDone=%d", idx, addedPos[idx], donePos[idx], itemDonePos[idx])
		}
	}
	if deltaByIndex[0] != `{"a":1}` {
		t.Fatalf("delta for output_index 0 = %q", deltaByIndex[0])
	}
	if deltaByIndex[1] != `{"b":2}` {
		t.Fatalf("delta for output_index 1 = %q", deltaByIndex[1])
	}
	if completedPos == -1 || !(itemDonePos[1] < completedPos) {
		t.Fatalf("response.completed should be after final function item.done: itemDone=%d completed=%d", itemDonePos[1], completedPos)
	}
}

func TestConvertOpenAIChatCompletionsResponseToOpenAIResponses_WaitsForRealCallID(t *testing.T) {
	in := []string{
		`data: {"id":"chatcmpl-late-id","object":"chat.completion.chunk","created":1700000001,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"lookup","arguments":"{\"q\":\"hel"}}]}}]}`,
		`data: {"id":"chatcmpl-late-id","object":"chat.completion.chunk","created":1700000001,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_lookup","function":{"arguments":"lo\"}"}}]}}]}`,
		`data: {"id":"chatcmpl-late-id","object":"chat.completion.chunk","created":1700000001,"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}

	var param any
	first := ConvertOpenAIChatCompletionsResponseToOpenAIResponses(context.Background(), "gpt-5", nil, nil, []byte(in[0]), &param)
	for _, chunk := range first {
		ev, data := parseOpenAIResponsesSSE(t, chunk)
		if ev == "response.output_item.added" && data.Get("item.type").String() == "function_call" {
			t.Fatalf("function_call item should not be emitted before real call_id is known: %s", chunk)
		}
	}

	var out []string
	out = append(out, first...)
	out = append(out, ConvertOpenAIChatCompletionsResponseToOpenAIResponses(context.Background(), "gpt-5", nil, nil, []byte(in[1]), &param)...)
	out = append(out, ConvertOpenAIChatCompletionsResponseToOpenAIResponses(context.Background(), "gpt-5", nil, nil, []byte(in[2]), &param)...)

	var (
		addedCallID     string
		doneArgs        string
		completedCallID string
	)
	for _, chunk := range out {
		ev, data := parseOpenAIResponsesSSE(t, chunk)
		switch ev {
		case "response.output_item.added":
			if data.Get("item.type").String() == "function_call" {
				addedCallID = data.Get("item.call_id").String()
			}
		case "response.function_call_arguments.done":
			doneArgs = data.Get("arguments").String()
		case "response.completed":
			completedCallID = data.Get("response.output.0.call_id").String()
		}
	}

	if addedCallID != "call_lookup" {
		t.Fatalf("function item call_id = %q, want %q", addedCallID, "call_lookup")
	}
	if completedCallID != "call_lookup" {
		t.Fatalf("response.output call_id = %q, want %q", completedCallID, "call_lookup")
	}
	if doneArgs != `{"q":"hello"}` {
		t.Fatalf("arguments.done = %q", doneArgs)
	}
}
