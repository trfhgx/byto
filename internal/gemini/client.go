package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
}

func NewClient(cfg config.Config, tokens auth.TokenProvider) *Client {
	return &Client{
		project:  cfg.Project,
		location: cfg.Location,
		baseURL:  strings.TrimRight(cfg.VertexBaseURL, "/"),
		http:     &http.Client{Timeout: cfg.RequestTimeout()},
		tokens:   tokens,
	}
}

func (c *Client) GenerateContent(ctx context.Context, model string, in GenerateRequest) (GenerateResponse, error) {
	var out GenerateResponse
	body, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	req, err := c.newRequest(ctx, model, "generateContent", body)
	if err != nil {
		return out, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return out, fmt.Errorf("vertex generateContent status %d: %s", resp.StatusCode, string(b))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) StreamGenerateContent(ctx context.Context, model string, in GenerateRequest, onChunk func(GenerateResponse) error) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := c.newRequest(ctx, model, "streamGenerateContent", body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("vertex streamGenerateContent status %d: %s", resp.StatusCode, string(b))
	}
	return parseStream(resp.Body, onChunk)
}

func (c *Client) ListPublisherModels(ctx context.Context) ([]catalog.LiveModel, error) {
	tok, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	var out []catalog.LiveModel
	pageToken := ""
	for {
		u := fmt.Sprintf("%s/v1beta1/publishers/google/models?pageSize=1000", c.baseURL)
		if pageToken != "" {
			u += "&pageToken=" + url.QueryEscape(pageToken)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("User-Agent", "go-llm-gateway/1.0")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return nil, fmt.Errorf("vertex list publisher models status %d: %s", resp.StatusCode, string(b))
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

func (c *Client) newRequest(ctx context.Context, model string, method string, body []byte) (*http.Request, error) {
	tok, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:%s", c.baseURL, url.PathEscape(c.project), url.PathEscape(c.location), url.PathEscape(model), method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
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
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "[" || line == "]" {
			continue
		}
		line = strings.TrimSuffix(line, ",")
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var gr GenerateResponse
		if err := json.Unmarshal([]byte(line), &gr); err != nil {
			continue
		}
		if err := onChunk(gr); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	// Some proxies buffer the JSON array without newlines. Handle that case with a best-effort decoder.
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
	return &Client{project: project, location: location, baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}, tokens: tokens}
}
