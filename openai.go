package mockllm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
)

// OpenAIProvider handles OpenAI Chat Completions API request/response mocking
type OpenAIProvider struct {
	mocks []OpenAIMock
}

// NewOpenAIProvider creates a new OpenAI provider with the given mocks
func NewOpenAIProvider(mocks []OpenAIMock) *OpenAIProvider {
	return &OpenAIProvider{
		mocks: mocks,
	}
}

// Handle processes an OpenAI chat completion request
func (p *OpenAIProvider) Handle(w http.ResponseWriter, r *http.Request) {
	// Parse request body to check for stream
	// This is due to a limitation on the ChatCompletionNewParams type in the SDK
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read body: %v", err), http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Check for stream: true in raw JSON
	var rawMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Parse the incoming request into SDK type
	var requestBody openai.ChatCompletionNewParams
	if err := json.NewDecoder(bytes.NewBuffer(bodyBytes)).Decode(&requestBody); err != nil {
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

	// Return the response
	isStream := false
	if streamVal, ok := rawMap["stream"].(bool); ok {
		isStream = streamVal
	}

	if isStream {
		p.handleStreamingResponse(w, mock.Response)
	} else {
		p.handleNonStreamingResponse(w, mock.Response)
	}
}

// findMatchingMock finds the first mock that matches the request
func (p *OpenAIProvider) findMatchingMock(request openai.ChatCompletionNewParams, headers http.Header) *OpenAIMock {
	for _, mock := range p.mocks {
		if p.requestsMatch(mock.Match, request) && headersMatch(mock.Match.Headers, headers) {
			return &mock
		}
	}
	return nil
}

// requestsMatch checks if two requests are equivalent
func (p *OpenAIProvider) requestsMatch(expected OpenAIRequestMatch, actual openai.ChatCompletionNewParams) bool {
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
		// Check if the last message contains the expected message
		if len(actual.Messages) == 0 {
			return false
		}
		lastMessage := actual.Messages[len(actual.Messages)-1]
		if *lastMessage.GetRole() != *expected.Message.GetRole() {
			return false
		}
		strExpected, ok := expected.Message.GetContent().AsAny().(*string)
		if !ok {
			return false
		}
		strActual, ok := lastMessage.GetContent().AsAny().(*string)
		if !ok {
			return false
		}
		return strings.Contains(*strActual, *strExpected)
	default:
		return false
	}
}

// handleNonStreamingResponse sends a JSON response
func (p *OpenAIProvider) handleNonStreamingResponse(w http.ResponseWriter, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

// handleStreamingResponse sends a streaming SSE response
func (p *OpenAIProvider) handleStreamingResponse(w http.ResponseWriter, response openai.ChatCompletion) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported by server", http.StatusInternalServerError)
		return
	}

	// Helper to send a chunk
	sendChunk := func(chunk openai.ChatCompletionChunk) {
		jsonBytes, err := json.Marshal(chunk)
		if err != nil {
			fmt.Printf("Failed to encode chunk: %v\n", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", jsonBytes)
		flusher.Flush()
	}

	// 1. Initial chunk with role
	chunk1 := openai.ChatCompletionChunk{
		ID:      response.ID,
		Object:  "chat.completion.chunk",
		Created: response.Created,
		Model:   response.Model,
		Choices: []openai.ChatCompletionChunkChoice{
			{
				Index: 0,
				Delta: openai.ChatCompletionChunkChoiceDelta{
					Role: string(response.Choices[0].Message.Role),
				},
			},
		},
	}
	sendChunk(chunk1)

	// 2. Content chunk
	chunk2 := openai.ChatCompletionChunk{
		ID:      response.ID,
		Object:  "chat.completion.chunk",
		Created: response.Created,
		Model:   response.Model,
		Choices: []openai.ChatCompletionChunkChoice{
			{
				Index: 0,
				Delta: openai.ChatCompletionChunkChoiceDelta{
					Content: response.Choices[0].Message.Content,
				},
			},
		},
	}
	sendChunk(chunk2)

	// 2.5 ToolCalls chunk
	if len(response.Choices[0].Message.ToolCalls) > 0 {
		var toolCallDeltas []openai.ChatCompletionChunkChoiceDeltaToolCall
		for i, tc := range response.Choices[0].Message.ToolCalls {
			toolCallDeltas = append(toolCallDeltas, openai.ChatCompletionChunkChoiceDeltaToolCall{
				Index: int64(i),
				ID:    tc.ID,
				Type:  tc.Type,
				Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}

		chunkTool := openai.ChatCompletionChunk{
			ID:      response.ID,
			Object:  "chat.completion.chunk",
			Created: response.Created,
			Model:   response.Model,
			Choices: []openai.ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: openai.ChatCompletionChunkChoiceDelta{
						ToolCalls: toolCallDeltas,
					},
				},
			},
		}
		sendChunk(chunkTool)
	}

	// 3. Finish reason chunk
	chunk3 := openai.ChatCompletionChunk{
		ID:      response.ID,
		Object:  "chat.completion.chunk",
		Created: response.Created,
		Model:   response.Model,
		Choices: []openai.ChatCompletionChunkChoice{
			{
				Index:        0,
				Delta:        openai.ChatCompletionChunkChoiceDelta{},
				FinishReason: response.Choices[0].FinishReason,
			},
		},
	}
	sendChunk(chunk3)

	// 4. DONE
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}
