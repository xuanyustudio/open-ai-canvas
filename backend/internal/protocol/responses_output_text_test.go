package protocol

import (
	"context"
	"encoding/json"
	"testing"
)

// Shaped like the work.191u.cn non-streaming gpt-6-sol failure: HTTP 200,
// status=completed, no top-level output_text, reasoning item then a message
// whose text lives at output[].content[].text.
func missingOutputTextResponsesPayload(messageText string) map[string]any {
	return map[string]any{
		"id":     "resp_test_missing_output_text",
		"object": "response",
		"status": "completed",
		"model":  "gpt-6-sol",
		"error":  nil,
		"text": map[string]any{
			"format":    map[string]any{"type": "text"},
			"verbosity": "medium",
		},
		"reasoning": map[string]any{
			"effort":  "medium",
			"summary": nil,
		},
		"incomplete_details": nil,
		"output": []any{
			map[string]any{
				"id":                "rs_test",
				"type":              "reasoning",
				"summary":           []any{},
				"content":           []any{},
				"encrypted_content": "opaque-not-text",
			},
			map[string]any{
				"id":     "msg_test",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []any{
					map[string]any{
						"type":        "output_text",
						"text":        messageText,
						"annotations": []any{},
						"logprobs":    []any{},
					},
				},
			},
		},
		"usage": map[string]any{
			"input_tokens":  4440,
			"output_tokens": 31,
			"total_tokens":  4471,
		},
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestResponsesOutputMessageTextFallsBackToMessageContent(t *testing.T) {
	got := responsesOutputMessageText(missingOutputTextResponsesPayload("fallback from output array"))
	if got != "fallback from output array" {
		t.Fatalf("text = %q", got)
	}
}

func TestResponsesOutputMessageTextConcatenatesMessageParts(t *testing.T) {
	payload := missingOutputTextResponsesPayload("hello")
	output := payload["output"].([]any)
	message := output[1].(map[string]any)
	message["content"] = []any{
		map[string]any{"type": "output_text", "text": "hello "},
		map[string]any{"type": "text", "text": "world"},
	}
	if got := responsesOutputMessageText(payload); got != "hello world" {
		t.Fatalf("text = %q", got)
	}
}

func TestResponsesOutputMessageTextDoesNotInventText(t *testing.T) {
	payload := missingOutputTextResponsesPayload("")
	output := payload["output"].([]any)
	message := output[1].(map[string]any)
	message["content"] = []any{}
	if got := responsesOutputMessageText(payload); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	if got := responsesOutputMessageText(map[string]any{"status": "completed"}); got != "" {
		t.Fatalf("expected empty without output, got %q", got)
	}
}

func TestOfficialOpenAIResponsesParseCreateReadsNestedOutputText(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-responses.yingce-plugin", "openai-response")
	want := "fallback from output array"
	created, err := adapter.ParseCreate(context.Background(), mustJSON(t, missingOutputTextResponsesPayload(want)))
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusSucceeded {
		t.Fatalf("status = %q", created.Status)
	}
	if created.Result == nil || created.Result.Text != want {
		t.Fatalf("result = %#v", created.Result)
	}
}

func TestOfficialOpenAIResponsesParseCreatePrefersTopLevelOutputText(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-responses.yingce-plugin", "openai-response")
	payload := missingOutputTextResponsesPayload("nested should be ignored")
	payload["output_text"] = "top-level output_text wins"
	created, err := adapter.ParseCreate(context.Background(), mustJSON(t, payload))
	if err != nil {
		t.Fatal(err)
	}
	if created.Result == nil || created.Result.Text != "top-level output_text wins" {
		t.Fatalf("result = %#v", created.Result)
	}
}

func TestOfficialOpenAIResponsesParseCreateEmptyWhenNoMessageText(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-responses.yingce-plugin", "openai-response")
	payload := missingOutputTextResponsesPayload("")
	output := payload["output"].([]any)
	message := output[1].(map[string]any)
	message["content"] = []any{}
	created, err := adapter.ParseCreate(context.Background(), mustJSON(t, payload))
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusSucceeded {
		t.Fatalf("status = %q", created.Status)
	}
	if created.Result != nil {
		t.Fatalf("expected nil result when no text is present, got %#v", created.Result)
	}
}

func TestOfficialOpenAIResponsesParseAgentReadsNestedOutputText(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-responses.yingce-plugin", "openai-response")
	agent, ok := adapter.(AgentAdapter)
	if !ok {
		t.Fatal("openai-response does not implement AgentAdapter")
	}
	want := "agent fallback from output array"
	result, err := agent.ParseAgent(context.Background(), mustJSON(t, missingOutputTextResponsesPayload(want)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != want {
		t.Fatalf("agent text = %q", result.Text)
	}
}

func TestBuiltinResponsesParserReadsNestedOutputText(t *testing.T) {
	created, err := parseResponsesResponse(missingOutputTextResponsesPayload("builtin fallback"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Result == nil || created.Result.Text != "builtin fallback" {
		t.Fatalf("result = %#v", created.Result)
	}
}
