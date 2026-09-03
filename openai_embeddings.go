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

// OpenAIEmbeddingProvider handles OpenAI Embeddings API request/response mocking
type OpenAIEmbeddingProvider struct {
	mocks []OpenAIEmbeddingMock
}

// NewOpenAIEmbeddingProvider creates a new provider with the given mocks
func NewOpenAIEmbeddingProvider(mocks []OpenAIEmbeddingMock) *OpenAIEmbeddingProvider {
	return &OpenAIEmbeddingProvider{
		mocks: mocks,
	}
}

// Handle processes an OpenAI embeddings request
func (p *OpenAIEmbeddingProvider) Handle(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read body: %v", err), http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var requestBody openai.EmbeddingNewParams
	if err := json.NewDecoder(bytes.NewBuffer(bodyBytes)).Decode(&requestBody); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	mock := p.findMatchingMock(requestBody, r.Header)
	if mock == nil {
		requestBodyBytes, err := json.MarshalIndent(requestBody, "", "  ")
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to encode request body: %v", err), http.StatusInternalServerError)
			return
		}

		http.Error(w, fmt.Sprintf("No matching mock found. Request: %s", string(requestBodyBytes)), http.StatusNotFound)
		return
	}

	response := mock.Response
	if requestBody.Dimensions.Value > 0 {
		newData := make([]openai.Embedding, len(response.Data))
		for i, data := range response.Data {
			newData[i] = data
			if int64(len(data.Embedding)) > requestBody.Dimensions.Value {
				newData[i].Embedding = data.Embedding[:requestBody.Dimensions.Value]
			}
		}
		response.Data = newData
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

func (p *OpenAIEmbeddingProvider) findMatchingMock(request openai.EmbeddingNewParams, headers http.Header) *OpenAIEmbeddingMock {
	for _, mock := range p.mocks {
		if p.requestsMatch(mock.Match, request) && headersMatch(mock.Match.Headers, headers) {
			return &mock
		}
	}
	return nil
}

func (p *OpenAIEmbeddingProvider) requestsMatch(expected OpenAIEmbeddingRequestMatch, actual openai.EmbeddingNewParams) bool {
	jsonExpected, err := json.Marshal(expected.Input)
	if err != nil {
		return false
	}
	jsonActual, err := json.Marshal(actual.Input)
	if err != nil {
		return false
	}

	switch expected.MatchType {
	case MatchTypeExact:
		return bytes.Equal(jsonExpected, jsonActual)
	case MatchTypeContains:
		strExp := strings.Trim(string(jsonExpected), `"`)
		return strings.Contains(string(jsonActual), strExp)
	default:
		return false
	}
}
