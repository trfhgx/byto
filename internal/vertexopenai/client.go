package vertexopenai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/example/go-llm-gateway/internal/auth"
	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/gemini"
	"github.com/example/go-llm-gateway/internal/openai"
)

type Client struct {
	project  string
	location string
	baseURL  string
	http     *http.Client
	tokens   auth.TokenProvider
	retry    gemini.RetryConfig
}

func NewClient(cfg config.Config, tokens auth.TokenProvider) *Client {
	return &Client{
		project:  cfg.Project,
		location: cfg.Location,
		baseURL:  openAIBaseURL(cfg.VertexBaseURL, cfg.Location),
		http:     &http.Client{Timeout: cfg.RequestTimeout()},
		tokens:   tokens,
		retry: gemini.RetryConfig{
			MaxAttempts:  cfg.VertexRetryMaxAttempts,
			InitialDelay: time.Duration(cfg.VertexRetryInitialMS) * time.Millisecond,
			MaxDelay:     time.Duration(cfg.VertexRetryMaxMS) * time.Millisecond,
		},
	}
}

func NewTestClient(baseURL string, timeout time.Duration, tokens auth.TokenProvider, project, location string) *Client {
	return &Client{
		project:  project,
		location: location,
		baseURL:  strings.TrimRight(baseURL, "/"),
		http:     &http.Client{Timeout: timeout},
		tokens:   tokens,
		retry:    gemini.RetryConfig{MaxAttempts: 1, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond},
	}
}

func (c *Client) ChatCompletions(ctx context.Context, req openai.ChatCompletionRequest, opts gemini.RequestOptions) (openai.ChatCompletionResponse, error) {
	var out openai.ChatCompletionResponse
	body, err := marshalChatCompletionsRequest(req, false)
	if err != nil {
		return out, err
	}
	resp, err := c.do(ctx, body, "openai.chatCompletions", opts)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return out, &gemini.VertexError{Operation: "openai.chatCompletions", Status: resp.StatusCode, Body: string(b)}
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) StreamChatCompletions(ctx context.Context, req openai.ChatCompletionRequest, opts gemini.RequestOptions, onChunk func(openai.StreamChunk) error) error {
	body, err := marshalChatCompletionsRequest(req, true)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, body, "openai.streamChatCompletions", opts)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &gemini.VertexError{Operation: "openai.streamChatCompletions", Status: resp.StatusCode, Body: string(b)}
	}
	return parseStream(resp.Body, onChunk)
}

func marshalChatCompletionsRequest(req openai.ChatCompletionRequest, stream bool) ([]byte, error) {
	stopSequences, err := openai.StopSequences(req.Stop)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
	}
	if stream {
		body["stream"] = true
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	}
	if req.FrequencyPenalty != nil {
		body["frequency_penalty"] = *req.FrequencyPenalty
	}
	if req.PresencePenalty != nil {
		body["presence_penalty"] = *req.PresencePenalty
	}
	if len(stopSequences) == 1 {
		body["stop"] = stopSequences[0]
	} else if len(stopSequences) > 1 {
		body["stop"] = stopSequences
	}
	if req.Seed != nil {
		body["seed"] = *req.Seed
	}
	if strings.TrimSpace(req.ReasoningEffort) != "" {
		body["reasoning_effort"] = strings.TrimSpace(req.ReasoningEffort)
	}
	return json.Marshal(body)
}

func (c *Client) do(ctx context.Context, body []byte, operation string, opts gemini.RequestOptions) (*http.Response, error) {
	tok, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	attempts := c.retry.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatCompletionsURL(), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Goog-User-Project", c.project)
		req.Header.Set("User-Agent", "go-llm-gateway/1.0")
		if opts.LLMRequestType != "" {
			req.Header.Set("X-Vertex-AI-LLM-Request-Type", opts.LLMRequestType)
		}
		if opts.LLMSharedRequestType != "" {
			req.Header.Set("X-Vertex-AI-LLM-Shared-Request-Type", opts.LLMSharedRequestType)
		}
		resp, err := c.http.Do(req)
		if err == nil && !isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}
		if err == nil && attempt == attempts {
			return resp, nil
		}
		if err != nil {
			lastErr = err
		}
		delay := retryDelay(c.retry, attempt, resp)
		if resp != nil {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
		}
		if err := sleepContext(ctx, delay); err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) chatCompletionsURL() string {
	return fmt.Sprintf("%s/v1/projects/%s/locations/%s/endpoints/openapi/chat/completions", c.baseURL, url.PathEscape(c.project), url.PathEscape(c.location))
}

func openAIBaseURL(baseURL, location string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://aiplatform.googleapis.com"
	}
	if base != "https://aiplatform.googleapis.com" {
		return base
	}
	location = strings.TrimSpace(location)
	if location == "" || location == "global" {
		return base
	}
	return "https://" + location + "-aiplatform.googleapis.com"
}

func parseStream(r io.Reader, onChunk func(openai.StreamChunk) error) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk openai.StreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if err := onChunk(chunk); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func retryDelay(cfg gemini.RetryConfig, attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if v := strings.TrimSpace(resp.Header.Get("Retry-After")); v != "" {
			if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
			if t, err := http.ParseTime(v); err == nil {
				if d := time.Until(t); d > 0 {
					return d
				}
			}
		}
	}
	base := cfg.InitialDelay
	if base <= 0 {
		base = 250 * time.Millisecond
	}
	maxDelay := cfg.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 2 * time.Second
	}
	delay := base * time.Duration(1<<(attempt-1))
	if delay > maxDelay {
		delay = maxDelay
	}
	jitter := time.Duration(rand.Int63n(int64(delay / 2)))
	return delay/2 + jitter
}

func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
