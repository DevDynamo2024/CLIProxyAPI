package util

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// NormalizeOpenAIToolsPayload removes/normalizes provider-specific tool wrappers so that
// OpenAI-compatible backends won't reject the request with unknown parameters such as
// "tools.defer_loading".
//
// It is intentionally best-effort and schema-tolerant:
//   - If "tools" is an object wrapper like {"defer_loading":true,"tools":[...]}, it unwraps it.
//   - If "tool_choice" is nested under tools, it hoists it to top-level when absent.
//   - If "tools" is an array, it removes "defer_loading" fields from tool entries.
//   - If "tools" remains an object/invalid type after normalization, it drops "tools" entirely.
func NormalizeOpenAIToolsPayload(payload []byte) []byte {
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
			return NormalizeOpenAIToolsPayload(out)
		}

		// Still an object (unknown schema). OpenAI expects tools to be an array; drop it to avoid 400s.
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

