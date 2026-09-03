package mockllm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

func TestNormalizeAnthropicToolResultStringContent(t *testing.T) {
	normalized, err := normalizeAnthropicShorthand([]byte(`{
		"model":"claude-sonnet-4-5",
		"max_tokens":100,
		"messages":[{"role":"user","content":[{
			"type":"tool_result",
			"tool_use_id":"toolu_123",
			"content":"tool output",
			"is_error":false
		}]}]
	}`))
	if err != nil {
		t.Fatalf("normalizeAnthropicShorthand() error = %v", err)
	}

	var request anthropic.MessageNewParams
	if err := json.Unmarshal(normalized, &request); err != nil {
		t.Fatalf("unmarshal normalized request: %v", err)
	}

	result := request.Messages[0].Content[0].OfToolResult
	if result == nil {
		t.Fatal("normalized content is not a tool_result")
	}
	if result.ToolUseID != "toolu_123" {
		t.Fatalf("tool_use_id = %q", result.ToolUseID)
	}
	if len(result.Content) != 1 || result.Content[0].OfText == nil ||
		result.Content[0].OfText.Text != "tool output" {
		t.Fatalf("normalized content = %#v", result.Content)
	}
}

func TestAnthropicContainsToolResultMatchesOnlyID(t *testing.T) {
	var actual anthropic.MessageNewParams
	actualJSON, err := normalizeAnthropicShorthand([]byte(`{
		"model":"claude-sonnet-4-5",
		"max_tokens":100,
		"messages":[{"role":"user","content":[{
			"type":"tool_result",
			"tool_use_id":"toolu_123",
			"content":"actual output",
			"is_error":false
		}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(actualJSON, &actual); err != nil {
		t.Fatal(err)
	}

	var expected AnthropicRequestMatch
	if err := json.Unmarshal([]byte(`{
		"match_type":"contains",
		"message":{"role":"user","content":[{
			"type":"tool_result",
			"tool_use_id":"toolu_123",
			"content":"ignored output",
			"is_error":true
		}]}
	}`), &expected); err != nil {
		t.Fatal(err)
	}

	if !NewAnthropicProvider(nil).requestsMatch(expected, actual) {
		t.Fatal("expected tool_result to match by tool_use_id only")
	}

	expected.Message.Role = anthropic.MessageParamRoleAssistant
	if NewAnthropicProvider(nil).requestsMatch(expected, actual) {
		t.Fatal("tool_result matched a different role")
	}
}

func TestAnthropicRequestFieldConstraints(t *testing.T) {
	var actual anthropic.MessageNewParams
	if err := json.Unmarshal([]byte(`{
		"model":"claude-sonnet-4-5",
		"max_tokens":100,
		"system":[
			{"type":"text","text":"Use the installed arithmetic skill."},
			{"type":"text","text":"Always call tools when instructed."}
		],
		"tools":[
			{"name":"mcp__calculator__add_numbers","description":"Add numbers","input_schema":{"type":"object"}},
			{"name":"Read","description":"Read a file","input_schema":{"type":"object"}}
		],
		"messages":[{"role":"user","content":[{"type":"text","text":"add 3 and 5"}]}]
	}`), &actual); err != nil {
		t.Fatal(err)
	}

	var expected AnthropicRequestMatch
	if err := json.Unmarshal([]byte(`{
		"match_type":"contains",
		"message":{"role":"user","content":[{"type":"text","text":"add 3"}]},
		"system_contains":["arithmetic skill","call tools"],
		"tool_names":["mcp__calculator__add_numbers","Read"]
	}`), &expected); err != nil {
		t.Fatal(err)
	}

	provider := NewAnthropicProvider(nil)
	if !provider.requestsMatch(expected, actual) {
		t.Fatal("expected request-level constraints to match")
	}

	tests := []struct {
		name   string
		mutate func(*AnthropicRequestMatch)
	}{
		{
			name: "missing system text",
			mutate: func(match *AnthropicRequestMatch) {
				match.SystemContains = append(match.SystemContains, "missing instruction")
			},
		},
		{
			name: "missing tool",
			mutate: func(match *AnthropicRequestMatch) {
				match.ToolNames = append(match.ToolNames, "missing_tool")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := expected
			candidate.SystemContains = append([]string(nil), expected.SystemContains...)
			candidate.ToolNames = append([]string(nil), expected.ToolNames...)
			test.mutate(&candidate)
			if provider.requestsMatch(candidate, actual) {
				t.Fatal("request matched an unsatisfied request-level constraint")
			}
		})
	}
}

func TestAnthropicStreamingEventsDecodeWithSDK(t *testing.T) {
	var response anthropic.Message
	if err := json.Unmarshal([]byte(`{
		"id":"msg_123","type":"message","role":"assistant","model":"claude-sonnet-4-5",
		"content":[
			{"type":"text","text":"working"},
			{"type":"tool_use","id":"toolu_123","name":"Bash","input":{"command":"pwd"}}
		],
		"stop_reason":"tool_use","stop_sequence":null,
		"usage":{"input_tokens":10,"output_tokens":4}
	}`), &response); err != nil {
		t.Fatal(err)
	}

	var match AnthropicRequestMatch
	if err := json.Unmarshal([]byte(`{
		"match_type":"contains",
		"message":{"role":"user","content":[{"type":"text","text":"hello"}]}
	}`), &match); err != nil {
		t.Fatal(err)
	}

	provider := NewAnthropicProvider([]AnthropicMock{{Match: match, Response: response}})
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-sonnet-4-5","max_tokens":100,"stream":true,
		"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]
	}`))
	request.Header.Set("x-api-key", "test-key")
	request.Header.Set("anthropic-version", "2023-06-01")
	recorder := httptest.NewRecorder()
	provider.Handle(recorder, request)

	httpResponse := recorder.Result()
	stream := ssestream.NewStream[anthropic.MessageStreamEventUnion](
		ssestream.NewDecoder(httpResponse), nil,
	)
	defer stream.Close()

	var accumulated anthropic.Message
	for stream.Next() {
		if err := accumulated.Accumulate(stream.Current()); err != nil {
			t.Fatalf("accumulate stream: %v", err)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("decode stream: %v", err)
	}

	if accumulated.ID != response.ID || accumulated.StopReason != response.StopReason {
		t.Fatalf("accumulated message metadata = %#v", accumulated)
	}
	if len(accumulated.Content) != 2 || accumulated.Content[0].Text != "working" ||
		string(accumulated.Content[1].Input) != `{"command":"pwd"}` {
		t.Fatalf("accumulated content = %#v", accumulated.Content)
	}
}
