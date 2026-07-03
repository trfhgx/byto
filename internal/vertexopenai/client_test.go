package vertexopenai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/example/go-llm-gateway/internal/gemini"
	"github.com/example/go-llm-gateway/internal/openai"
)

type staticToken string
type roundTripFunc func(*http.Request) (*http.Response, error)

func (t staticToken) Token(ctx context.Context) (string, error) {
	return string(t), nil
}

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestChatCompletionsUsesOpenAICompatibleEndpointAndAuth(t *testing.T) {
	var gotBody map[string]any
	client := NewTestClient("https://example.test", time.Second, staticToken("tok"), "p", "global")
	client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/projects/p/locations/global/endpoints/openapi/chat/completions" {
			t.Fatalf("path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Fatalf("authorization %q", got)
		}
		if got := r.Header.Get("X-Goog-User-Project"); got != "p" {
			t.Fatalf("x-goog-user-project %q", got)
		}
		if got := r.Header.Get("X-Vertex-AI-LLM-Shared-Request-Type"); got != "priority" {
			t.Fatalf("shared request type %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"xai/grok-4.20-reasoning","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
		}, nil
	})}

	maxTokens := 2
	_, err := client.ChatCompletions(context.Background(), openai.ChatCompletionRequest{
		Model:     "xai/grok-4.20-reasoning",
		Messages:  []openai.Message{{Role: "user", Content: json.RawMessage(`"Reply with exactly OK."`)}},
		MaxTokens: &maxTokens,
		ExtraBody: openai.ExtraBody{Google: openai.GoogleExtra{
			CachedContent: "projects/p/locations/global/cachedContents/cache-1",
		}},
	}, gemini.RequestOptions{LLMRequestType: "shared", LLMSharedRequestType: "priority"})
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["model"] != "xai/grok-4.20-reasoning" {
		t.Fatalf("model %#v", gotBody["model"])
	}
	if gotBody["max_tokens"].(float64) != 2 {
		t.Fatalf("max_tokens %#v", gotBody["max_tokens"])
	}
	if _, ok := gotBody["extra_body"]; ok {
		t.Fatalf("extra_body should not be forwarded: %#v", gotBody)
	}
}

func TestStreamChatCompletionsParsesSSE(t *testing.T) {
	client := NewTestClient("https://example.test", time.Second, staticToken("tok"), "p", "global")
	client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"openai/gpt-oss-20b-maas\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":null}]}\n\n" +
			"data: [DONE]\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	var chunks []string
	err := client.StreamChatCompletions(context.Background(), openai.ChatCompletionRequest{
		Model:    "openai/gpt-oss-20b-maas",
		Messages: []openai.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}, gemini.RequestOptions{}, func(chunk openai.StreamChunk) error {
		if len(chunk.Choices) > 0 {
			chunks = append(chunks, chunk.Choices[0].Delta.Content)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(chunks, "") != "OK" {
		t.Fatalf("chunks %#v", chunks)
	}
}
