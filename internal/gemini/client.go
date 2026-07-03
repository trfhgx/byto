package gemini

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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/example/go-llm-gateway/internal/auth"
	"github.com/example/go-llm-gateway/internal/catalog"
	"github.com/example/go-llm-gateway/internal/config"
)

type Client struct {
	project  string
	location string
	baseURL  string
	http     *http.Client
	tokens   auth.TokenProvider
	retry    RetryConfig
}

type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

type VertexError struct {
	Operation string
	Status    int
	Body      string
}

func (e *VertexError) Error() string {
	return fmt.Sprintf("vertex %s status %d: %s", e.Operation, e.Status, e.Body)
}

func NewClient(cfg config.Config, tokens auth.TokenProvider) *Client {
	return &Client{
		project:  cfg.Project,
		location: cfg.Location,
		baseURL:  strings.TrimRight(cfg.VertexBaseURL, "/"),
		http:     &http.Client{Timeout: cfg.RequestTimeout()},
		tokens:   tokens,
		retry: RetryConfig{
			MaxAttempts:  cfg.VertexRetryMaxAttempts,
			InitialDelay: time.Duration(cfg.VertexRetryInitialMS) * time.Millisecond,
			MaxDelay:     time.Duration(cfg.VertexRetryMaxMS) * time.Millisecond,
		},
	}
}

func (c *Client) GenerateContent(ctx context.Context, model string, in GenerateRequest, opts RequestOptions) (GenerateResponse, error) {
	var out GenerateResponse
	body, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	u := c.modelMethodURL(model, "generateContent")
	resp, err := c.do(ctx, http.MethodPost, u, body, "generateContent", opts)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return out, &VertexError{Operation: "generateContent", Status: resp.StatusCode, Body: string(b)}
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) StreamGenerateContent(ctx context.Context, model string, in GenerateRequest, opts RequestOptions, onChunk func(GenerateResponse) error) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	u := c.modelMethodURL(model, "streamGenerateContent")
	resp, err := c.do(ctx, http.MethodPost, u, body, "streamGenerateContent", opts)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &VertexError{Operation: "streamGenerateContent", Status: resp.StatusCode, Body: string(b)}
	}
	return parseStream(resp.Body, onChunk)
}

func (c *Client) CountTokens(ctx context.Context, model string, in GenerateRequest) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	u := c.modelMethodURL(model, "countTokens")
	resp, err := c.do(ctx, http.MethodPost, u, body, "countTokens", RequestOptions{})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &VertexError{Operation: "countTokens", Status: resp.StatusCode, Body: string(b)}
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}

func (c *Client) CreateCachedContent(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	normalized, err := c.normalizeCachedContentBody(body)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/v1/projects/%s/locations/%s/cachedContents", c.baseURL, url.PathEscape(c.project), url.PathEscape(c.location))
	return c.rawJSON(ctx, http.MethodPost, u, normalized, "cachedContents.create")
}

func (c *Client) ListCachedContents(ctx context.Context, query url.Values) (json.RawMessage, error) {
	u := fmt.Sprintf("%s/v1/projects/%s/locations/%s/cachedContents", c.baseURL, url.PathEscape(c.project), url.PathEscape(c.location))
	if encoded := normalizeCacheListQuery(query).Encode(); encoded != "" {
		u += "?" + encoded
	}
	return c.rawJSON(ctx, http.MethodGet, u, nil, "cachedContents.list")
}

func (c *Client) GetCachedContent(ctx context.Context, id string) (json.RawMessage, error) {
	u := c.cacheResourceURL(id)
	return c.rawJSON(ctx, http.MethodGet, u, nil, "cachedContents.get")
}

func (c *Client) DeleteCachedContent(ctx context.Context, id string) (json.RawMessage, error) {
	u := c.cacheResourceURL(id)
	return c.rawJSON(ctx, http.MethodDelete, u, nil, "cachedContents.delete")
}

func (c *Client) ListPublisherModels(ctx context.Context) ([]catalog.LiveModel, error) {
	tok, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	var out []catalog.LiveModel
	pageToken := ""
	for {
		u := fmt.Sprintf("%s/v1beta1/publishers/google/models?pageSize=300", c.baseURL)
		if pageToken != "" {
			u += "&pageToken=" + url.QueryEscape(pageToken)
		}
		resp, err := c.doWithToken(ctx, http.MethodGet, u, nil, "listPublisherModels", tok, RequestOptions{})
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return nil, &VertexError{Operation: "listPublisherModels", Status: resp.StatusCode, Body: string(b)}
		}
		var body struct {
			PublisherModels []struct {
				Name             string         `json:"name"`
				DisplayName      string         `json:"displayName"`
				LaunchStage      string         `json:"launchStage"`
				VersionState     string         `json:"versionState"`
				SupportedActions map[string]any `json:"supportedActions"`
			} `json:"publisherModels"`
			Models []struct {
				Name             string         `json:"name"`
				DisplayName      string         `json:"displayName"`
				LaunchStage      string         `json:"launchStage"`
				VersionState     string         `json:"versionState"`
				SupportedActions map[string]any `json:"supportedActions"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, m := range body.PublisherModels {
			out = append(out, catalog.LiveModel{ID: modelIDFromName(m.Name), DisplayName: m.DisplayName, Publisher: "google", LaunchStage: m.LaunchStage, VersionState: m.VersionState, SupportedActions: supportedActionKeys(m.SupportedActions)})
		}
		for _, m := range body.Models {
			out = append(out, catalog.LiveModel{ID: modelIDFromName(m.Name), DisplayName: m.DisplayName, Publisher: "google", LaunchStage: m.LaunchStage, VersionState: m.VersionState, SupportedActions: supportedActionKeys(m.SupportedActions)})
		}
		if body.NextPageToken == "" {
			return out, nil
		}
		pageToken = body.NextPageToken
	}
}

func (c *Client) rawJSON(ctx context.Context, method, u string, body []byte, operation string) (json.RawMessage, error) {
	resp, err := c.do(ctx, method, u, body, operation, RequestOptions{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &VertexError{Operation: operation, Status: resp.StatusCode, Body: string(out)}
	}
	if len(out) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(out), nil
}

func (c *Client) do(ctx context.Context, method, u string, body []byte, operation string, opts RequestOptions) (*http.Response, error) {
	tok, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	return c.doWithToken(ctx, method, u, body, operation, tok, opts)
}

func (c *Client) doWithToken(ctx context.Context, method, u string, body []byte, operation, tok string, opts RequestOptions) (*http.Response, error) {
	attempts := c.retry.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
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
		delay := c.retryDelay(attempt, resp)
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

func (c *Client) retryDelay(attempt int, resp *http.Response) time.Duration {
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
	base := c.retry.InitialDelay
	if base <= 0 {
		base = 250 * time.Millisecond
	}
	maxDelay := c.retry.MaxDelay
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

func (c *Client) modelMethodURL(model, method string) string {
	return fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:%s", c.baseURL, url.PathEscape(c.project), url.PathEscape(c.location), url.PathEscape(model), method)
}

func (c *Client) cacheResourceURL(id string) string {
	name := strings.Trim(strings.TrimSpace(id), "/")
	if !strings.HasPrefix(name, "projects/") {
		name = fmt.Sprintf("projects/%s/locations/%s/cachedContents/%s", c.project, c.location, name)
	}
	return c.baseURL + "/v1/" + escapeResourceName(name)
}

func (c *Client) normalizeCachedContentBody(body json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("cache body is required")
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("invalid cache json: %w", err)
	}
	if model, ok := obj["model"].(string); ok {
		model = strings.TrimSpace(model)
		if strings.HasPrefix(model, "gemini-") {
			obj["model"] = fmt.Sprintf("projects/%s/locations/%s/publishers/google/models/%s", c.project, c.location, model)
		}
	}
	return json.Marshal(obj)
}

func normalizeCacheListQuery(in url.Values) url.Values {
	out := url.Values{}
	for k, values := range in {
		key := k
		switch k {
		case "page_size":
			key = "pageSize"
		case "page_token":
			key = "pageToken"
		}
		for _, v := range values {
			out.Add(key, v)
		}
	}
	return out
}

func escapeResourceName(name string) string {
	parts := strings.Split(name, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
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

func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func (c *Client) newRequest(ctx context.Context, model string, method string, body []byte) (*http.Request, error) {
	tok, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	u := c.modelMethodURL(model, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-User-Project", c.project)
	req.Header.Set("User-Agent", "go-llm-gateway/1.0")
	return req, nil
}

func modelIDFromName(name string) string {
	if i := strings.LastIndex(name, "/models/"); i >= 0 {
		return name[i+len("/models/"):]
	}
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

func supportedActionKeys(actions map[string]any) []string {
	out := make([]string, 0, len(actions))
	for k, v := range actions {
		if b, ok := v.(bool); ok && !b {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func parseStream(r io.Reader, onChunk func(GenerateResponse) error) error {
	br := bufio.NewReader(r)

	// Skip leading whitespace/newlines
	for {
		b, err := br.Peek(1)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n' {
			_, _ = br.ReadByte()
			continue
		}
		break
	}

	b, err := br.Peek(1)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(br)
	if b[0] == '[' {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if delim, ok := t.(json.Delim); !ok || delim != '[' {
			return fmt.Errorf("expected [ delimiter, got %v", t)
		}
		for dec.More() {
			var gr GenerateResponse
			if err := dec.Decode(&gr); err != nil {
				return err
			}
			if err := onChunk(gr); err != nil {
				return err
			}
		}
		// Read closing ']'
		_, _ = dec.Token()
		return nil
	}

	for {
		var gr GenerateResponse
		if err := dec.Decode(&gr); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if err := onChunk(gr); err != nil {
			return err
		}
	}
	return nil
}

func TextFromResponse(r GenerateResponse) string {
	if len(r.Candidates) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range r.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

func FinishReason(r GenerateResponse) string {
	if len(r.Candidates) == 0 {
		return "stop"
	}
	fr := strings.ToLower(r.Candidates[0].FinishReason)
	switch fr {
	case "", "stop":
		return "stop"
	case "max_tokens", "max_token", "max_tokens_reached":
		return "length"
	case "safety":
		return "content_filter"
	default:
		return "stop"
	}
}

func NewTestClient(baseURL string, timeout time.Duration, tokens auth.TokenProvider, project, location string) *Client {
	return &Client{project: project, location: location, baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}, tokens: tokens, retry: RetryConfig{MaxAttempts: 1, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}}
}
