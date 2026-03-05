// Package openai provides request translation functionality for OpenAI to Gemini CLI API compatibility.
// It converts OpenAI Chat Completions requests into Gemini CLI compatible JSON using gjson/sjson only.
package chat_completions

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertOpenAIRequestToOpenAI converts an OpenAI Chat Completions request (raw JSON)
// into a complete Gemini CLI request JSON. All JSON construction uses sjson and lookups use gjson.
//
// Parameters:
//   - modelName: The name of the model to use for the request
//   - rawJSON: The raw JSON request data from the OpenAI API
//   - stream: A boolean indicating if the request is for a streaming response (unused in current implementation)
//
// Returns:
//   - []byte: The transformed request data in Gemini CLI API format
func ConvertOpenAIRequestToOpenAI(modelName string, inputRawJSON []byte, _ bool) []byte {
	out := inputRawJSON

	// Update the "model" field in the JSON payload with the provided modelName.
	// If it fails, keep the original payload.
	if updatedJSON, err := sjson.SetBytes(out, "model", modelName); err == nil {
		out = updatedJSON
	}

	// Some clients (e.g. Claude Code) may wrap OpenAI "tools" with provider-specific knobs like:
	//   "tools": { "defer_loading": true, "tools": [ ... ] }
	// OpenAI rejects unknown parameters such as "tools.defer_loading" with HTTP 400.
	// Normalize these variants before forwarding to OpenAI-compatible backends.
	out = normalizeOpenAIToolsForOpenAI(out)

	// Some clients may persist provider-specific "thinking" blocks in messages history.
	// OpenAI-compatible backends can reject these with errors like:
	//   messages.N.content.M: Invalid `signature` in `thinking` block
	// To keep failover (e.g., Claude -> GPT) resilient, strip thinking blocks on OpenAI-format requests.
	out = stripThinkingBlocksFromOpenAIMessages(out)

	return out
}

func normalizeOpenAIToolsForOpenAI(payload []byte) []byte {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return payload
	}

	tools := gjson.GetBytes(payload, "tools")
	if !tools.Exists() {
		return payload
	}

	out := payload

	switch {
	case tools.IsArray():
		// Best-effort cleanup: drop any provider-specific fields on each tool object.
		// (If a client injected "defer_loading" into individual tool entries.)
		arr := tools.Array()
		for i := 0; i < len(arr); i++ {
			if !arr[i].IsObject() {
				continue
			}
			if arr[i].Get("defer_loading").Exists() {
				if updated, err := sjson.DeleteBytes(out, fmt.Sprintf("tools.%d.defer_loading", i)); err == nil {
					out = updated
				}
			}
			if arr[i].Get("function.defer_loading").Exists() {
				if updated, err := sjson.DeleteBytes(out, fmt.Sprintf("tools.%d.function.defer_loading", i)); err == nil {
					out = updated
				}
			}
		}
		return out

	case tools.IsObject():
		// Remove the known problematic flag first.
		if updated, err := sjson.DeleteBytes(out, "tools.defer_loading"); err == nil {
			out = updated
		}

		// Some wrappers also nest tool_choice under tools; hoist it if needed.
		if !gjson.GetBytes(out, "tool_choice").Exists() {
			if nestedChoice := gjson.GetBytes(out, "tools.tool_choice"); nestedChoice.Exists() {
				if updated, err := sjson.SetRawBytes(out, "tool_choice", []byte(nestedChoice.Raw)); err == nil {
					out = updated
				}
			}
		}

		// If tools.tools is an array, unwrap it into the top-level tools array.
		if nested := gjson.GetBytes(out, "tools.tools"); nested.Exists() && nested.IsArray() {
			if updated, err := sjson.SetRawBytes(out, "tools", []byte(nested.Raw)); err == nil {
				out = updated
			}
			// Re-run cleanup on the unwrapped array (covers defer_loading inside entries).
			return normalizeOpenAIToolsForOpenAI(out)
		}

		// Still an object (unknown schema). OpenAI expects tools to be an array; drop it to avoid 400s.
		if gjson.GetBytes(out, "tools").IsObject() {
			if updated, err := sjson.DeleteBytes(out, "tools"); err == nil {
				out = updated
			}
		}
		return out

	default:
		// Invalid type for tools in OpenAI schema; remove it.
		if updated, err := sjson.DeleteBytes(out, "tools"); err == nil {
			out = updated
		}
		return out
	}
}

func stripThinkingBlocksFromOpenAIMessages(payload []byte) []byte {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return payload
	}

	messages := gjson.GetBytes(payload, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return payload
	}

	out := payload
	msgs := messages.Array()
	for i := 0; i < len(msgs); i++ {
		contentPath := fmt.Sprintf("messages.%d.content", i)
		content := gjson.GetBytes(out, contentPath)
		if !content.Exists() {
			continue
		}

		switch {
		case content.IsArray():
			parts := content.Array()
			removed := false
			for j := len(parts) - 1; j >= 0; j-- {
				partType := strings.TrimSpace(strings.ToLower(parts[j].Get("type").String()))
				if partType == "thinking" || partType == "redacted_thinking" {
					if updated, err := sjson.DeleteBytes(out, fmt.Sprintf("%s.%d", contentPath, j)); err == nil {
						out = updated
						removed = true
					}
				}
			}

			// Avoid emitting empty content arrays; replace with empty string for broad compatibility.
			if removed {
				updatedContent := gjson.GetBytes(out, contentPath)
				if updatedContent.IsArray() && len(updatedContent.Array()) == 0 {
					if updated, err := sjson.SetBytes(out, contentPath, ""); err == nil {
						out = updated
					}
				}
			}

		case content.IsObject():
			partType := strings.TrimSpace(strings.ToLower(content.Get("type").String()))
			if partType == "thinking" || partType == "redacted_thinking" {
				if updated, err := sjson.SetBytes(out, contentPath, ""); err == nil {
					out = updated
				}
			}
		}
	}

	return out
}
