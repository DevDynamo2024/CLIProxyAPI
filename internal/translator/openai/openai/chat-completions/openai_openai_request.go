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

	// Some clients may persist provider-specific "thinking" blocks in messages history.
	// OpenAI-compatible backends can reject these with errors like:
	//   messages.N.content.M: Invalid `signature` in `thinking` block
	// To keep failover (e.g., Claude -> GPT) resilient, strip thinking blocks on OpenAI-format requests.
	out = stripThinkingBlocksFromOpenAIMessages(out)

	return out
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
