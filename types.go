package mockllm

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// Very simple mock configuration - just maps requests to responses using official SDK types

// Config holds all the mock responses
type Config struct {
	OpenAI           []OpenAIMock          `json:"openai,omitempty"`
	OpenAIResponse   []OpenAIResponseMock  `json:"openai_response,omitempty"`
	OpenAIEmbeddings []OpenAIEmbeddingMock `json:"openai_embeddings,omitempty"`
	Anthropic        []AnthropicMock       `json:"anthropic,omitempty"`
	// ListenAddr is the address to listen on. Defaults to 0.0.0.0:0 (any IP address and ephemeral port)
	ListenAddr string `json:"listen_addr,omitempty"`
}

type MatchType string

const (
	MatchTypeExact    MatchType = "exact"
	MatchTypeContains MatchType = "contains"
)

type HeaderMatch struct {
	Name      string    `json:"name"`                 // Header name (case-insensitive per HTTP spec)
	Value     string    `json:"value"`                // Value to match against
	MatchType MatchType `json:"match_type,omitempty"` // "exact" (default) or "contains"
}

type OpenAIRequestMatch struct {
	MatchType MatchType                              `json:"match_type"`
	Message   openai.ChatCompletionMessageParamUnion `json:"message"`
	Headers   []HeaderMatch                          `json:"headers,omitempty"`
}

// OpenAIMock maps an OpenAI request to a response using official SDK types
type OpenAIMock struct {
	Name     string                `json:"name"`     // identifier for this mock
	Match    OpenAIRequestMatch    `json:"match"`    // Match type and value
	Response openai.ChatCompletion `json:"response"` // OpenAI response to return (ChatCompletion or ChatCompletionChunk)
}

type OpenAIResponseRequestMatch struct {
	MatchType MatchType                             `json:"match_type"`
	Input     responses.ResponseNewParamsInputUnion `json:"input"` // Input to match against (typically OfString)
	Headers   []HeaderMatch                         `json:"headers,omitempty"`
}

// OpenAIResponseMock maps an OpenAI Responses API request to a response using official SDK types
type OpenAIResponseMock struct {
	Name     string                     `json:"name"`     // identifier for this mock
	Match    OpenAIResponseRequestMatch `json:"match"`    // Match type and value
	Response responses.Response         `json:"response"` // OpenAI Responses API response to return
}

type AnthropicRequestMatch struct {
	MatchType      MatchType              `json:"match_type"`
	Message        anthropic.MessageParam `json:"message"`
	Headers        []HeaderMatch          `json:"headers,omitempty"`
	SystemContains []string               `json:"system_contains,omitempty"`
	ToolNames      []string               `json:"tool_names,omitempty"`
}

// AnthropicMock maps an Anthropic request to a response using official SDK types
type AnthropicMock struct {
	Name     string                `json:"name"`     // identifier for this mock
	Match    AnthropicRequestMatch `json:"match"`    // Match type and value
	Response anthropic.Message     `json:"response"` // Anthropic response to return (Message or streaming event)
}

type OpenAIEmbeddingRequestMatch struct {
	MatchType MatchType                           `json:"match_type"`
	Input     openai.EmbeddingNewParamsInputUnion `json:"input"`
	Headers   []HeaderMatch                       `json:"headers,omitempty"`
}

// OpenAIEmbeddingMock maps an OpenAI embeddings request to a response using official SDK types
type OpenAIEmbeddingMock struct {
	Name     string                         `json:"name"`     // identifier for this mock
	Match    OpenAIEmbeddingRequestMatch    `json:"match"`    // Match type and value
	Response openai.CreateEmbeddingResponse `json:"response"` // OpenAI embeddings response to return
}
