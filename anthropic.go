package mockllm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// AnthropicProvider handles Anthropic request/response mocking
type AnthropicProvider struct {
	mocks []AnthropicMock
}

// NewAnthropicProvider creates a new Anthropic AnthropicProvider with the given mocks
func NewAnthropicProvider(mocks []AnthropicMock) *AnthropicProvider {
	return &AnthropicProvider{mocks: mocks}
}

// Handle processes an Anthropic messages request
func (p *AnthropicProvider) Handle(w http.ResponseWriter, r *http.Request) {
	// Check for required headers
	if r.Header.Get("x-api-key") == "" {
		http.Error(w, "Missing x-api-key header", http.StatusUnauthorized)
		return
	}

	if r.Header.Get("anthropic-version") == "" {
		http.Error(w, "Missing anthropic-version header", http.StatusBadRequest)
		return
	}

	// Read the raw request so we can inspect fields that are not represented by
	// MessageNewParams (notably stream) and normalize API shorthand before the
	// SDK decodes it.
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read body: %v", err), http.StatusBadRequest)
		return
	}

	var rawMap map[string]any
	if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	normalizedBody, err := normalizeAnthropicShorthand(bodyBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Parse the normalized request into the SDK type.
	var requestBody anthropic.MessageNewParams
	if err := json.Unmarshal(normalizedBody, &requestBody); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Find a matching mock
	mock := p.findMatchingMock(requestBody, r.Header)
	if mock == nil {
		requestBodyBytes, err := json.MarshalIndent(requestBody, "", "  ")
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to encode request body: %v", err),
				http.StatusInternalServerError)
			return
		}

		http.Error(w, fmt.Sprintf("No matching mock found. Request: %s",
			string(requestBodyBytes)), http.StatusNotFound)
		return
	}

	if stream, _ := rawMap["stream"].(bool); stream {
		p.handleStreamingResponse(w, mock.Response)
	} else {
		p.handleNonStreamingResponse(w, mock.Response)
	}
}

// normalizeAnthropicShorthand converts the string form of tool_result.content
// into the block-list form expected by anthropic-sdk-go.
// Both forms are supported by the Anthropic API, but the Go SDK type models this field only as array not the string format
func normalizeAnthropicShorthand(body []byte) ([]byte, error) {
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}

	messages, _ := request["messages"].([]any)
	for _, messageValue := range messages {
		message, _ := messageValue.(map[string]any)
		content, _ := message["content"].([]any)
		for _, blockValue := range content {
			block, _ := blockValue.(map[string]any)
			if block["type"] != "tool_result" {
				continue
			}
			if text, ok := block["content"].(string); ok {
				block["content"] = []any{map[string]any{
					"type": "text",
					"text": text,
				}}
			}
		}
	}

	return json.Marshal(request)
}

// findMatchingMock finds the first mock that matches the request
func (p *AnthropicProvider) findMatchingMock(request anthropic.MessageNewParams, headers http.Header) *AnthropicMock {
	for _, mock := range p.mocks {
		if p.requestsMatch(mock.Match, request) && headersMatch(mock.Match.Headers, headers) {
			return &mock
		}
	}
	return nil
}

// requestsMatch checks if two requests are equivalent.
//
// Note: For MatchTypeContains, this function only supports a single content part
// in the expected message. Text blocks match by substring; tool_result blocks match
// only by tool_use_id.
func (p *AnthropicProvider) requestsMatch(expected AnthropicRequestMatch, actual anthropic.MessageNewParams) bool {
	if !anthropicRequestFieldsMatch(expected, actual) {
		return false
	}

	// Simple deep equal comparison for now
	// In the future, we could add more sophisticated matching
	switch expected.MatchType {
	case MatchTypeExact:
		// get Last message from actual
		if len(actual.Messages) == 0 {
			return false
		}
		lastMessage := actual.Messages[len(actual.Messages)-1]
		// Check json is equal
		jsonExpected, err := json.Marshal(expected.Message)
		if err != nil {
			return false
		}
		jsonActual, err := json.Marshal(lastMessage)
		if err != nil {
			return false
		}
		return bytes.Equal(jsonExpected, jsonActual)
	case MatchTypeContains:
		if len(actual.Messages) == 0 {
			return false
		}

		// For simplicity, only support single content part in expected.
		if len(expected.Message.Content) != 1 {
			return false
		}

		lastMessage := actual.Messages[len(actual.Messages)-1]
		if lastMessage.Role != expected.Message.Role {
			return false
		}

		expectedPart := expected.Message.Content[0]
		if expectedPart.OfText != nil {
			for _, part := range lastMessage.Content {
				if part.OfText != nil && strings.Contains(part.OfText.Text, expectedPart.OfText.Text) {
					return true
				}
			}
		}

		if expectedPart.OfToolResult != nil {
			for _, part := range lastMessage.Content {
				if part.OfToolResult != nil &&
					part.OfToolResult.ToolUseID == expectedPart.OfToolResult.ToolUseID {
					return true
				}
			}
		}
	}
	return false
}

// anthropicRequestFieldsMatch checks optional constraints outside the messages
// array. Every configured system substring and tool name must be present.
func anthropicRequestFieldsMatch(expected AnthropicRequestMatch, actual anthropic.MessageNewParams) bool {
	for _, expectedText := range expected.SystemContains {
		found := false
		for _, block := range actual.System {
			if strings.Contains(block.Text, expectedText) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	for _, expectedName := range expected.ToolNames {
		found := false
		for _, tool := range actual.Tools {
			name := tool.GetName()
			if name != nil && *name == expectedName {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// handleNonStreamingResponse sends a JSON response
func (p *AnthropicProvider) handleNonStreamingResponse(w http.ResponseWriter, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

// handleStreamingResponse converts a configured Message into Anthropic Messages
// API server-sent events.
func (p *AnthropicProvider) handleStreamingResponse(w http.ResponseWriter, response anthropic.Message) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported by server", http.StatusInternalServerError)
		return
	}

	sendEvent := func(event string, data any) {
		encoded, err := json.Marshal(data)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
		flusher.Flush()
	}

	startMessage := map[string]any{
		"id":            response.ID,
		"type":          "message",
		"role":          "assistant",
		"model":         response.Model,
		"content":       []any{},
		"stop_reason":   nil,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  response.Usage.InputTokens,
			"output_tokens": int64(0),
		},
	}
	sendEvent("message_start", map[string]any{"type": "message_start", "message": startMessage})

	for index, block := range response.Content {
		switch block.Type {
		case "text":
			sendEvent("content_block_start", map[string]any{
				"type": "content_block_start", "index": index,
				"content_block": map[string]any{"type": "text", "text": ""},
			})
			sendEvent("content_block_delta", map[string]any{
				"type": "content_block_delta", "index": index,
				"delta": map[string]any{"type": "text_delta", "text": block.Text},
			})
		case "tool_use":
			sendEvent("content_block_start", map[string]any{
				"type": "content_block_start", "index": index,
				"content_block": map[string]any{
					"type": "tool_use", "id": block.ID, "name": block.Name,
					"input": map[string]any{},
				},
			})
			sendEvent("content_block_delta", map[string]any{
				"type": "content_block_delta", "index": index,
				"delta": map[string]any{
					"type": "input_json_delta", "partial_json": string(block.Input),
				},
			})
		default:
			continue
		}
		sendEvent("content_block_stop", map[string]any{
			"type": "content_block_stop", "index": index,
		})
	}

	sendEvent("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason": response.StopReason, "stop_sequence": response.StopSequence,
		},
		"usage": map[string]any{"output_tokens": response.Usage.OutputTokens},
	})
	sendEvent("message_stop", map[string]any{"type": "message_stop"})
}
