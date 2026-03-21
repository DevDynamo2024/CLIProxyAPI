// Package claude provides response translation functionality for Codex to Claude Code API compatibility.
// This package handles the conversion of Codex API responses into Claude Code-compatible
// Server-Sent Events (SSE) format, implementing a sophisticated state machine that manages
// different response types including text content, thinking processes, and function calls.
// The translation ensures proper sequencing of SSE events and maintains state across
// multiple response chunks to provide a seamless streaming experience.
package claude

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var (
	dataTag = []byte("data:")
)

// ConvertCodexResponseToClaudeParams holds parameters for response conversion.
type ConvertCodexResponseToClaudeParams struct {
	HasToolCall               bool
	BlockIndex                int
	HasReceivedArgumentsDelta bool
	WebSearchCalls            []hostedWebSearchCall
}

type hostedWebSearchCall struct {
	ItemID    string
	ToolUseID string
	Action    string
	Query     string
	Queries   []string
	URL       string
}

func (call hostedWebSearchCall) IsSearchRequest() bool {
	return isHostedWebSearchSearchAction(call.Action)
}

type hostedWebSearchResult struct {
	URL       string
	Title     string
	CitedText string
}

// ConvertCodexResponseToClaude performs sophisticated streaming response format conversion.
// This function implements a complex state machine that translates Codex API responses
// into Claude Code-compatible Server-Sent Events (SSE) format. It manages different response types
// and handles state transitions between content blocks, thinking processes, and function calls.
//
// Response type states: 0=none, 1=content, 2=thinking, 3=function
// The function maintains state across multiple calls to ensure proper SSE event sequencing.
//
// Parameters:
//   - ctx: The context for the request, used for cancellation and timeout handling
//   - modelName: The name of the model being used for the response (unused in current implementation)
//   - rawJSON: The raw JSON response from the Codex API
//   - param: A pointer to a parameter object for maintaining state between calls
//
// Returns:
//   - []string: A slice of strings, each containing a Claude Code-compatible JSON response
func ConvertCodexResponseToClaude(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []string {
	if *param == nil {
		*param = &ConvertCodexResponseToClaudeParams{
			HasToolCall: false,
			BlockIndex:  0,
		}
	}

	// log.Debugf("rawJSON: %s", string(rawJSON))
	if !bytes.HasPrefix(rawJSON, dataTag) {
		return []string{}
	}
	rawJSON = bytes.TrimSpace(rawJSON[5:])

	output := ""
	rootResult := gjson.ParseBytes(rawJSON)
	typeResult := rootResult.Get("type")
	typeStr := typeResult.String()
	template := ""
	if typeStr == "response.created" {
		template = `{"type":"message_start","message":{"id":"","type":"message","role":"assistant","model":"claude-opus-4-1-20250805","stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0},"content":[],"stop_reason":null}}`
		template, _ = sjson.Set(template, "message.model", rootResult.Get("response.model").String())
		template, _ = sjson.Set(template, "message.id", rootResult.Get("response.id").String())

		output = "event: message_start\n"
		output += fmt.Sprintf("data: %s\n\n", template)
	} else if typeStr == "response.reasoning_summary_part.added" {
		template = `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`
		template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)

		output = "event: content_block_start\n"
		output += fmt.Sprintf("data: %s\n\n", template)
	} else if typeStr == "response.reasoning_summary_text.delta" {
		template = `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}`
		template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
		template, _ = sjson.Set(template, "delta.thinking", rootResult.Get("delta").String())

		output = "event: content_block_delta\n"
		output += fmt.Sprintf("data: %s\n\n", template)
	} else if typeStr == "response.reasoning_summary_part.done" {
		template = `{"type":"content_block_stop","index":0}`
		template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
		(*param).(*ConvertCodexResponseToClaudeParams).BlockIndex++

		output = "event: content_block_stop\n"
		output += fmt.Sprintf("data: %s\n\n", template)

	} else if typeStr == "response.content_part.added" {
		template = `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`
		template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)

		output = "event: content_block_start\n"
		output += fmt.Sprintf("data: %s\n\n", template)
	} else if typeStr == "response.output_text.delta" {
		template = `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`
		template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
		template, _ = sjson.Set(template, "delta.text", rootResult.Get("delta").String())

		output = "event: content_block_delta\n"
		output += fmt.Sprintf("data: %s\n\n", template)
	} else if typeStr == "response.content_part.done" {
		template = `{"type":"content_block_stop","index":0}`
		template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
		(*param).(*ConvertCodexResponseToClaudeParams).BlockIndex++

		output = "event: content_block_stop\n"
		output += fmt.Sprintf("data: %s\n\n", template)
	} else if typeStr == "response.completed" {
		searchResults := collectHostedWebSearchResults(rootResult.Get("response.output"))
		for _, call := range (*param).(*ConvertCodexResponseToClaudeParams).WebSearchCalls {
			if !call.IsSearchRequest() {
				continue
			}
			webSearchBlock := `{"type":"content_block_start","index":0,"content_block":{"type":"web_search_tool_result","tool_use_id":"","content":[]}}`
			webSearchBlock, _ = sjson.Set(webSearchBlock, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
			webSearchBlock, _ = sjson.Set(webSearchBlock, "content_block.tool_use_id", call.ToolUseID)
			webSearchBlock, _ = sjson.SetRaw(webSearchBlock, "content_block.content", buildHostedWebSearchResultContent(call, searchResults))

			output += "event: content_block_start\n"
			output += fmt.Sprintf("data: %s\n\n", webSearchBlock)

			template = `{"type":"content_block_stop","index":0}`
			template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
			(*param).(*ConvertCodexResponseToClaudeParams).BlockIndex++

			output += "event: content_block_stop\n"
			output += fmt.Sprintf("data: %s\n\n", template)
		}

		template = `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`
		p := (*param).(*ConvertCodexResponseToClaudeParams).HasToolCall
		stopReason := rootResult.Get("response.stop_reason").String()
		if p {
			template, _ = sjson.Set(template, "delta.stop_reason", "tool_use")
		} else if stopReason == "max_tokens" || stopReason == "stop" {
			template, _ = sjson.Set(template, "delta.stop_reason", stopReason)
		} else {
			template, _ = sjson.Set(template, "delta.stop_reason", "end_turn")
		}
		inputTokens, outputTokens, cachedTokens := extractResponsesUsage(rootResult.Get("response.usage"))
		template, _ = sjson.Set(template, "usage.input_tokens", inputTokens)
		template, _ = sjson.Set(template, "usage.output_tokens", outputTokens)
		if cachedTokens > 0 {
			template, _ = sjson.Set(template, "usage.cache_read_input_tokens", cachedTokens)
		}
		if webSearchRequests := rootResult.Get("response.tool_usage.web_search.num_requests").Int(); webSearchRequests > 0 {
			template, _ = sjson.Set(template, "usage.server_tool_use.web_search_requests", webSearchRequests)
		}

		output += "event: message_delta\n"
		output += fmt.Sprintf("data: %s\n\n", template)
		output += "event: message_stop\n"
		output += `data: {"type":"message_stop"}`
		output += "\n\n"
	} else if typeStr == "response.output_item.added" {
		itemResult := rootResult.Get("item")
		itemType := itemResult.Get("type").String()
		if itemType == "function_call" {
			(*param).(*ConvertCodexResponseToClaudeParams).HasToolCall = true
			(*param).(*ConvertCodexResponseToClaudeParams).HasReceivedArgumentsDelta = false
			template = `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"","name":"","input":{}}}`
			template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
			template, _ = sjson.Set(template, "content_block.id", itemResult.Get("call_id").String())
			{
				// Restore original tool name if shortened
				name := itemResult.Get("name").String()
				rev := buildReverseMapFromClaudeOriginalShortToOriginal(originalRequestRawJSON)
				if orig, ok := rev[name]; ok {
					name = orig
				}
				template, _ = sjson.Set(template, "content_block.name", name)
			}

			output = "event: content_block_start\n"
			output += fmt.Sprintf("data: %s\n\n", template)

			template = `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`
			template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)

			output += "event: content_block_delta\n"
			output += fmt.Sprintf("data: %s\n\n", template)
		}
	} else if typeStr == "response.output_item.done" {
		itemResult := rootResult.Get("item")
		itemType := itemResult.Get("type").String()
		if itemType == "function_call" {
			template = `{"type":"content_block_stop","index":0}`
			template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
			(*param).(*ConvertCodexResponseToClaudeParams).BlockIndex++

			output = "event: content_block_stop\n"
			output += fmt.Sprintf("data: %s\n\n", template)
		} else if itemType == "web_search_call" {
			call, ok := parseHostedWebSearchCall(itemResult)
			if ok {
				if call.IsSearchRequest() {
					(*param).(*ConvertCodexResponseToClaudeParams).WebSearchCalls = append((*param).(*ConvertCodexResponseToClaudeParams).WebSearchCalls, call)
				}

				if call.IsSearchRequest() {
					template = `{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"","name":"web_search","input":{}}}`
					template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
					template, _ = sjson.Set(template, "content_block.id", call.ToolUseID)

					output = "event: content_block_start\n"
					output += fmt.Sprintf("data: %s\n\n", template)

					template = `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`
					template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
					template, _ = sjson.Set(template, "delta.partial_json", buildHostedWebSearchInputJSON(call))

					output += "event: content_block_delta\n"
					output += fmt.Sprintf("data: %s\n\n", template)

					template = `{"type":"content_block_stop","index":0}`
					template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
					(*param).(*ConvertCodexResponseToClaudeParams).BlockIndex++

					output += "event: content_block_stop\n"
					output += fmt.Sprintf("data: %s\n\n", template)
				}
			}
		}
	} else if typeStr == "response.function_call_arguments.delta" {
		(*param).(*ConvertCodexResponseToClaudeParams).HasReceivedArgumentsDelta = true
		template = `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`
		template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
		template, _ = sjson.Set(template, "delta.partial_json", rootResult.Get("delta").String())

		output += "event: content_block_delta\n"
		output += fmt.Sprintf("data: %s\n\n", template)
	} else if typeStr == "response.function_call_arguments.done" {
		// Some upstreams emit the full arguments only in the terminal "done" event.
		// Synthesize a single delta so downstream Claude clients still receive tool input.
		if !(*param).(*ConvertCodexResponseToClaudeParams).HasReceivedArgumentsDelta {
			if args := rootResult.Get("arguments").String(); args != "" {
				template = `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`
				template, _ = sjson.Set(template, "index", (*param).(*ConvertCodexResponseToClaudeParams).BlockIndex)
				template, _ = sjson.Set(template, "delta.partial_json", args)

				output += "event: content_block_delta\n"
				output += fmt.Sprintf("data: %s\n\n", template)
			}
		}
	}

	return []string{output}
}

// ConvertCodexResponseToClaudeNonStream converts a non-streaming Codex response to a non-streaming Claude Code response.
// This function processes the complete Codex response and transforms it into a single Claude Code-compatible
// JSON response. It handles message content, tool calls, reasoning content, and usage metadata, combining all
// the information into a single response that matches the Claude Code API format.
//
// Parameters:
//   - ctx: The context for the request, used for cancellation and timeout handling
//   - modelName: The name of the model being used for the response (unused in current implementation)
//   - rawJSON: The raw JSON response from the Codex API
//   - param: A pointer to a parameter object for the conversion (unused in current implementation)
//
// Returns:
//   - string: A Claude Code-compatible JSON response containing all message content and metadata
func ConvertCodexResponseToClaudeNonStream(_ context.Context, _ string, originalRequestRawJSON, _ []byte, rawJSON []byte, _ *any) string {
	revNames := buildReverseMapFromClaudeOriginalShortToOriginal(originalRequestRawJSON)

	rootResult := gjson.ParseBytes(rawJSON)
	if rootResult.Get("type").String() != "response.completed" {
		return ""
	}

	responseData := rootResult.Get("response")
	if !responseData.Exists() {
		return ""
	}

	out := `{"id":"","type":"message","role":"assistant","model":"","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}`
	out, _ = sjson.Set(out, "id", responseData.Get("id").String())
	out, _ = sjson.Set(out, "model", responseData.Get("model").String())
	inputTokens, outputTokens, cachedTokens := extractResponsesUsage(responseData.Get("usage"))
	out, _ = sjson.Set(out, "usage.input_tokens", inputTokens)
	out, _ = sjson.Set(out, "usage.output_tokens", outputTokens)
	if cachedTokens > 0 {
		out, _ = sjson.Set(out, "usage.cache_read_input_tokens", cachedTokens)
	}

	hasToolCall := false
	webSearchCalls := []hostedWebSearchCall{}
	searchResults := collectHostedWebSearchResults(responseData.Get("output"))

	if output := responseData.Get("output"); output.Exists() && output.IsArray() {
		output.ForEach(func(_, item gjson.Result) bool {
			switch item.Get("type").String() {
			case "reasoning":
				thinkingBuilder := strings.Builder{}
				if summary := item.Get("summary"); summary.Exists() {
					if summary.IsArray() {
						summary.ForEach(func(_, part gjson.Result) bool {
							if txt := part.Get("text"); txt.Exists() {
								thinkingBuilder.WriteString(txt.String())
							} else {
								thinkingBuilder.WriteString(part.String())
							}
							return true
						})
					} else {
						thinkingBuilder.WriteString(summary.String())
					}
				}
				if thinkingBuilder.Len() == 0 {
					if content := item.Get("content"); content.Exists() {
						if content.IsArray() {
							content.ForEach(func(_, part gjson.Result) bool {
								if txt := part.Get("text"); txt.Exists() {
									thinkingBuilder.WriteString(txt.String())
								} else {
									thinkingBuilder.WriteString(part.String())
								}
								return true
							})
						} else {
							thinkingBuilder.WriteString(content.String())
						}
					}
				}
				if thinkingBuilder.Len() > 0 {
					block := `{"type":"thinking","thinking":""}`
					block, _ = sjson.Set(block, "thinking", thinkingBuilder.String())
					out, _ = sjson.SetRaw(out, "content.-1", block)
				}
			case "message":
				if content := item.Get("content"); content.Exists() {
					if content.IsArray() {
						content.ForEach(func(_, part gjson.Result) bool {
							if part.Get("type").String() == "output_text" {
								text := part.Get("text").String()
								if text != "" {
									block := `{"type":"text","text":""}`
									block, _ = sjson.Set(block, "text", text)
									if citations := buildClaudeWebSearchCitations(text, part.Get("annotations")); citations != "" {
										block, _ = sjson.SetRaw(block, "citations", citations)
									}
									out, _ = sjson.SetRaw(out, "content.-1", block)
								}
							}
							return true
						})
					} else {
						text := content.String()
						if text != "" {
							block := `{"type":"text","text":""}`
							block, _ = sjson.Set(block, "text", text)
							out, _ = sjson.SetRaw(out, "content.-1", block)
						}
					}
				}
			case "function_call":
				hasToolCall = true
				name := item.Get("name").String()
				if original, ok := revNames[name]; ok {
					name = original
				}

				toolBlock := `{"type":"tool_use","id":"","name":"","input":{}}`
				toolBlock, _ = sjson.Set(toolBlock, "id", item.Get("call_id").String())
				toolBlock, _ = sjson.Set(toolBlock, "name", name)
				inputRaw := "{}"
				if argsStr := item.Get("arguments").String(); argsStr != "" && gjson.Valid(argsStr) {
					argsJSON := gjson.Parse(argsStr)
					if argsJSON.IsObject() {
						inputRaw = argsJSON.Raw
					}
				}
				toolBlock, _ = sjson.SetRaw(toolBlock, "input", inputRaw)
				out, _ = sjson.SetRaw(out, "content.-1", toolBlock)
			case "web_search_call":
				call, ok := parseHostedWebSearchCall(item)
				if !ok {
					return true
				}
				if !call.IsSearchRequest() {
					return true
				}
				webSearchCalls = append(webSearchCalls, call)

				serverToolBlock := `{"type":"server_tool_use","id":"","name":"web_search","input":{}}`
				serverToolBlock, _ = sjson.Set(serverToolBlock, "id", call.ToolUseID)
				serverToolBlock, _ = sjson.SetRaw(serverToolBlock, "input", buildHostedWebSearchInputJSON(call))
				out, _ = sjson.SetRaw(out, "content.-1", serverToolBlock)

				searchResultBlock := `{"type":"web_search_tool_result","tool_use_id":"","content":[]}`
				searchResultBlock, _ = sjson.Set(searchResultBlock, "tool_use_id", call.ToolUseID)
				searchResultBlock, _ = sjson.SetRaw(searchResultBlock, "content", buildHostedWebSearchResultContent(call, searchResults))
				out, _ = sjson.SetRaw(out, "content.-1", searchResultBlock)
			}
			return true
		})
	}

	if webSearchRequests := responseData.Get("tool_usage.web_search.num_requests").Int(); webSearchRequests > 0 || len(webSearchCalls) > 0 {
		if webSearchRequests == 0 {
			webSearchRequests = int64(len(webSearchCalls))
		}
		out, _ = sjson.Set(out, "usage.server_tool_use.web_search_requests", webSearchRequests)
	}

	if stopReason := responseData.Get("stop_reason"); stopReason.Exists() && stopReason.String() != "" {
		out, _ = sjson.Set(out, "stop_reason", stopReason.String())
	} else if hasToolCall {
		out, _ = sjson.Set(out, "stop_reason", "tool_use")
	} else {
		out, _ = sjson.Set(out, "stop_reason", "end_turn")
	}

	if stopSequence := responseData.Get("stop_sequence"); stopSequence.Exists() && stopSequence.String() != "" {
		out, _ = sjson.SetRaw(out, "stop_sequence", stopSequence.Raw)
	}

	return out
}

func extractResponsesUsage(usage gjson.Result) (int64, int64, int64) {
	if !usage.Exists() || usage.Type == gjson.Null {
		return 0, 0, 0
	}

	inputTokens := usage.Get("input_tokens").Int()
	outputTokens := usage.Get("output_tokens").Int()
	cachedTokens := usage.Get("input_tokens_details.cached_tokens").Int()

	if cachedTokens > 0 {
		if inputTokens >= cachedTokens {
			inputTokens -= cachedTokens
		} else {
			inputTokens = 0
		}
	}

	return inputTokens, outputTokens, cachedTokens
}

// buildReverseMapFromClaudeOriginalShortToOriginal builds a map[short]original from original Claude request tools.
func buildReverseMapFromClaudeOriginalShortToOriginal(original []byte) map[string]string {
	tools := gjson.GetBytes(original, "tools")
	rev := map[string]string{}
	if !tools.IsArray() {
		return rev
	}
	var names []string
	arr := tools.Array()
	for i := 0; i < len(arr); i++ {
		n := arr[i].Get("name").String()
		if n != "" {
			names = append(names, n)
		}
	}
	if len(names) > 0 {
		m := buildShortNameMap(names)
		for orig, short := range m {
			rev[short] = orig
		}
	}
	return rev
}

func ClaudeTokenCount(ctx context.Context, count int64) string {
	return fmt.Sprintf(`{"input_tokens":%d}`, count)
}

func parseHostedWebSearchCall(item gjson.Result) (hostedWebSearchCall, bool) {
	if item.Get("type").String() != "web_search_call" {
		return hostedWebSearchCall{}, false
	}

	call := hostedWebSearchCall{
		ItemID:    item.Get("id").String(),
		ToolUseID: hostedWebSearchToolUseID(item.Get("id").String()),
		Action:    item.Get("action.type").String(),
		URL:       item.Get("action.url").String(),
	}
	if query := item.Get("action.query").String(); query != "" {
		call.Query = query
	}
	if queries := item.Get("action.queries"); queries.Exists() && queries.IsArray() {
		queries.ForEach(func(_, query gjson.Result) bool {
			if q := query.String(); q != "" {
				call.Queries = append(call.Queries, q)
				if call.Query == "" {
					call.Query = q
				}
			}
			return true
		})
	}
	if call.Query == "" && call.URL == "" {
		return hostedWebSearchCall{}, false
	}
	return call, true
}

func isHostedWebSearchSearchAction(action string) bool {
	return action == "" || action == "search"
}

func hostedWebSearchToolUseID(itemID string) string {
	if itemID == "" {
		return "srvtoolu_web_search"
	}
	if strings.HasPrefix(itemID, "srvtoolu_") {
		return itemID
	}
	if strings.HasPrefix(itemID, "ws_") {
		return "srvtoolu_" + strings.TrimPrefix(itemID, "ws_")
	}
	return "srvtoolu_" + itemID
}

func buildHostedWebSearchInputJSON(call hostedWebSearchCall) string {
	input := `{}`
	if call.Query != "" {
		input, _ = sjson.Set(input, "query", call.Query)
	}
	return input
}

func collectHostedWebSearchResults(output gjson.Result) []hostedWebSearchResult {
	if !output.Exists() || !output.IsArray() {
		return nil
	}

	results := []hostedWebSearchResult{}
	seen := map[string]struct{}{}

	output.ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() != "message" {
			return true
		}
		content := item.Get("content")
		if !content.Exists() || !content.IsArray() {
			return true
		}

		content.ForEach(func(_, part gjson.Result) bool {
			if part.Get("type").String() != "output_text" {
				return true
			}
			text := part.Get("text").String()
			annotations := part.Get("annotations")
			if !annotations.Exists() || !annotations.IsArray() {
				return true
			}
			annotations.ForEach(func(_, annotation gjson.Result) bool {
				if annotation.Get("type").String() != "url_citation" {
					return true
				}
				url := annotation.Get("url").String()
				if url == "" {
					return true
				}
				title := annotation.Get("title").String()
				key := url + "\n" + title
				if _, ok := seen[key]; ok {
					return true
				}
				seen[key] = struct{}{}
				results = append(results, hostedWebSearchResult{
					URL:       url,
					Title:     title,
					CitedText: extractCitationText(text, annotation.Get("start_index").Int(), annotation.Get("end_index").Int()),
				})
				return true
			})
			return true
		})
		return true
	})

	return results
}

func buildHostedWebSearchResultContent(call hostedWebSearchCall, results []hostedWebSearchResult) string {
	entries := []hostedWebSearchResult{}
	if len(results) > 0 && call.Action != "open_page" {
		entries = append(entries, results...)
	}
	if len(entries) == 0 && call.URL != "" {
		entries = append(entries, hostedWebSearchResult{
			URL:   call.URL,
			Title: call.URL,
		})
	}
	if len(entries) == 0 {
		return `[]`
	}

	content := `[]`
	for index, entry := range entries {
		result := `{"type":"web_search_result"}`
		result, _ = sjson.Set(result, "url", entry.URL)
		if entry.Title != "" {
			result, _ = sjson.Set(result, "title", entry.Title)
		}
		content, _ = sjson.SetRaw(content, fmt.Sprintf("%d", index), result)
	}
	return content
}

func buildClaudeWebSearchCitations(text string, annotations gjson.Result) string {
	if !annotations.Exists() || !annotations.IsArray() {
		return ""
	}

	citations := `[]`
	citationIndex := 0
	annotations.ForEach(func(_, annotation gjson.Result) bool {
		if annotation.Get("type").String() != "url_citation" {
			return true
		}
		url := annotation.Get("url").String()
		if url == "" {
			return true
		}

		citation := `{"type":"web_search_result_location"}`
		citation, _ = sjson.Set(citation, "url", url)
		if title := annotation.Get("title").String(); title != "" {
			citation, _ = sjson.Set(citation, "title", title)
		}
		if citedText := extractCitationText(text, annotation.Get("start_index").Int(), annotation.Get("end_index").Int()); citedText != "" {
			citation, _ = sjson.Set(citation, "cited_text", citedText)
		}
		citations, _ = sjson.SetRaw(citations, fmt.Sprintf("%d", citationIndex), citation)
		citationIndex++
		return true
	})

	if citationIndex == 0 {
		return ""
	}
	return citations
}

func extractCitationText(text string, start, end int64) string {
	if text == "" || start < 0 || end <= start {
		return ""
	}
	runes := []rune(text)
	if start >= int64(len(runes)) {
		return ""
	}
	if end > int64(len(runes)) {
		end = int64(len(runes))
	}
	return string(runes[start:end])
}
