package openai

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// normalizeOpenAIToolsPayload removes/normalizes provider-specific tool wrappers so that
// OpenAI backends won't reject the request with unknown parameters such as "tools.defer_loading".
//
// This is duplicated from internal normalization logic on purpose: the SDK handlers
// cannot import internal packages.
func normalizeOpenAIToolsPayload(payload []byte) []byte {
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
		if updated, err := sjson.DeleteBytes(out, "tools.defer_loading"); err == nil {
			out = updated
		}

		if !gjson.GetBytes(out, "tool_choice").Exists() {
			if nestedChoice := gjson.GetBytes(out, "tools.tool_choice"); nestedChoice.Exists() {
				if updated, err := sjson.SetRawBytes(out, "tool_choice", []byte(nestedChoice.Raw)); err == nil {
					out = updated
				}
			}
		}

		if nested := gjson.GetBytes(out, "tools.tools"); nested.Exists() && nested.IsArray() {
			if updated, err := sjson.SetRawBytes(out, "tools", []byte(nested.Raw)); err == nil {
				out = updated
			}
			return normalizeOpenAIToolsPayload(out)
		}

		if gjson.GetBytes(out, "tools").IsObject() {
			if updated, err := sjson.DeleteBytes(out, "tools"); err == nil {
				out = updated
			}
		}
		return out

	default:
		if updated, err := sjson.DeleteBytes(out, "tools"); err == nil {
			out = updated
		}
		return out
	}
}

