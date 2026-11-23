package protocol

import "strings"

// responsesOutputMessageText extracts assistant text from a Responses API
// payload when the convenience field output_text is absent. It walks output[]
// items with type=message and concatenates content parts whose type is
// output_text or text. It does not invent text when none is present, and it
// ignores reasoning items and other non-message output.
func responsesOutputMessageText(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	items, ok := payload["output"].([]any)
	if !ok {
		return ""
	}
	var builder strings.Builder
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if manifestString(item["type"]) != "message" {
			continue
		}
		for _, rawPart := range manifestArray(item["content"]) {
			switch part := rawPart.(type) {
			case string:
				builder.WriteString(part)
			case map[string]any:
				kind := manifestString(part["type"])
				if kind == "output_text" || kind == "text" {
					builder.WriteString(manifestString(part["text"]))
				}
			}
		}
	}
	return strings.TrimSpace(builder.String())
}
