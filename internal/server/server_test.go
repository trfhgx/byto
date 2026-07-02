package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/go-llm-gateway/internal/catalog"
	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/gemini"
	gwlog "github.com/example/go-llm-gateway/internal/logging"
	"github.com/example/go-llm-gateway/internal/openai"
)

type fakeGemini struct{}
type fakeResourceExhaustedGemini struct{ fakeGemini }
type fakeReasoningGemini struct {
	fakeGemini
	t               *testing.T
	wantBudget      int
	wantInclude     *bool
	wantTrafficType string
}

func (f fakeGemini) GenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, opts gemini.RequestOptions) (gemini.GenerateResponse, error) {
	return gemini.GenerateResponse{Candidates: []gemini.Candidate{{Content: gemini.Content{Parts: []gemini.Part{{Text: "ok"}}}, FinishReason: "STOP"}}, UsageMetadata: gemini.UsageMetadata{PromptTokenCount: 3, CandidatesTokenCount: 2, TotalTokenCount: 5, CachedContentTokenCount: 1, TrafficType: "ON_DEMAND_PRIORITY"}}, nil
}
func (f fakeResourceExhaustedGemini) GenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, opts gemini.RequestOptions) (gemini.GenerateResponse, error) {
	return gemini.GenerateResponse{}, &gemini.VertexError{Operation: "generateContent", Status: http.StatusTooManyRequests, Body: `{"error":{"status":"RESOURCE_EXHAUSTED"}}`}
}
func (f fakeReasoningGemini) GenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, opts gemini.RequestOptions) (gemini.GenerateResponse, error) {
	if in.GenerationConfig == nil || in.GenerationConfig.ThinkingConfig == nil || in.GenerationConfig.ThinkingConfig.ThinkingBudget == nil {
		f.t.Fatalf("missing thinkingConfig: %#v", in.GenerationConfig)
	}
	if got := *in.GenerationConfig.ThinkingConfig.ThinkingBudget; got != f.wantBudget {
		f.t.Fatalf("thinkingBudget %d, want %d", got, f.wantBudget)
	}
	if f.wantInclude != nil {
		if in.GenerationConfig.ThinkingConfig.IncludeThoughts == nil || *in.GenerationConfig.ThinkingConfig.IncludeThoughts != *f.wantInclude {
			f.t.Fatalf("includeThoughts %#v, want %v", in.GenerationConfig.ThinkingConfig.IncludeThoughts, *f.wantInclude)
		}
	}
	trafficType := f.wantTrafficType
	if trafficType == "" {
		trafficType = "ON_DEMAND_PRIORITY"
	}
	return gemini.GenerateResponse{
		Candidates:    []gemini.Candidate{{Content: gemini.Content{Parts: []gemini.Part{{Text: "ok"}}}, FinishReason: "STOP"}},
		UsageMetadata: gemini.UsageMetadata{PromptTokenCount: 3, CandidatesTokenCount: 2, TotalTokenCount: 10, ThoughtsTokenCount: 5, TrafficType: trafficType},
	}, nil
}
func (f fakeGemini) StreamGenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, opts gemini.RequestOptions, onChunk func(gemini.GenerateResponse) error) error {
	return onChunk(gemini.GenerateResponse{Candidates: []gemini.Candidate{{Content: gemini.Content{Parts: []gemini.Part{{Text: "hello"}}}}}, UsageMetadata: gemini.UsageMetadata{TrafficType: "ON_DEMAND_PRIORITY"}})
}
func (f fakeGemini) CreateCachedContent(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"name":"projects/p/locations/global/cachedContents/cache-1"}`), nil
}
func (f fakeGemini) ListCachedContents(ctx context.Context, query url.Values) (json.RawMessage, error) {
	return json.RawMessage(`{"cachedContents":[{"name":"projects/p/locations/global/cachedContents/cache-1"}]}`), nil
}
func (f fakeGemini) GetCachedContent(ctx context.Context, id string) (json.RawMessage, error) {
	return json.RawMessage(`{"name":"projects/p/locations/global/cachedContents/` + id + `"}`), nil
}
func (f fakeGemini) DeleteCachedContent(ctx context.Context, id string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func testServer(t *testing.T) *Server {
	t.Helper()
	path := t.TempDir() + "/requests.jsonl"
	logger, err := gwlog.New(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: path, RequestTimeoutSeconds: 5}
	return New(cfg, fakeGemini{}, logger)
}

func TestChatCompletion(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("body %s", w.Body.String())
	}
}

func TestChatCompletionRequiresModel(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "model is required") {
		t.Fatalf("body %s", w.Body.String())
	}
}

func TestChatCompletionDefaultsToPriorityServiceTier(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"traffic_type":"ON_DEMAND_PRIORITY"`) {
		t.Fatalf("body missing traffic_type: %s", w.Body.String())
	}
}

func TestChatCompletionMapsReasoningEffort(t *testing.T) {
	dir := t.TempDir()
	catalogPath := dir + "/models.json"
	logPath := dir + "/requests.jsonl"
	modelCatalog := catalog.Catalog{Version: 1, Models: []catalog.Model{{
		ID:        "gemini-reasoning",
		Publisher: "google",
		Enabled:   true,
		Available: true,
		Capabilities: catalog.Capabilities{
			ReasoningEffort:  []string{"low", "medium", "high"},
			ReasoningBudgets: map[string]int{"low": 128, "medium": 512, "high": 2048},
		},
	}}}
	b, err := json.Marshal(modelCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, b, 0644); err != nil {
		t.Fatal(err)
	}
	logger, err := gwlog.New(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-reasoning"}, ModelCatalogPath: catalogPath, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: logPath, LogMaxBytes: 1024 * 1024, RequestTimeoutSeconds: 5}
	s := New(cfg, fakeReasoningGemini{t: t, wantBudget: 2048}, logger)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gemini-reasoning","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"reasoning_tokens":5`) {
		t.Fatalf("body missing reasoning tokens: %s", w.Body.String())
	}
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logs), `"reasoning_effort":"high"`) || !strings.Contains(string(logs), `"thoughts_tokens":5`) {
		t.Fatalf("logs missing reasoning fields:\n%s", string(logs))
	}
}

func TestChatCompletionSupportsExplicitThinkingBudget(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/requests.jsonl"
	logger, err := gwlog.New(path)
	if err != nil {
		t.Fatal(err)
	}
	includeThoughts := true
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: path, LogMaxBytes: 1024 * 1024, RequestTimeoutSeconds: 5}
	s := New(cfg, fakeReasoningGemini{t: t, wantBudget: 777, wantInclude: &includeThoughts}, logger)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}],"extra_body":{"google":{"thinking_budget":777,"include_thoughts":true}}}`))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

func TestChatCompletionPreservesResourceExhaustedStatus(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/requests.jsonl"
	logger, err := gwlog.New(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: path, LogMaxBytes: 1024 * 1024, RequestTimeoutSeconds: 5}
	s := New(cfg, fakeResourceExhaustedGemini{}, logger)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	logs := string(b)
	for _, want := range []string{`"status":429`, `"upstream_status":429`, `"upstream_classification":"rate_limited"`, `"path":"/v1/chat/completions"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("log missing %s:\n%s", want, logs)
		}
	}
}

func TestUnauthorized(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
}

func TestAllowUnauthenticated(t *testing.T) {
	s := testServer(t)
	s.cfg.GatewayAllowUnauthenticated = true
	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

func TestAccessLogsHealthModelsAndAuthFailure(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/requests.jsonl"
	logger, err := gwlog.New(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: path, LogMaxBytes: 1024 * 1024, RequestTimeoutSeconds: 5}
	s := New(cfg, fakeGemini{}, logger)

	req := httptest.NewRequest("GET", "/healthz", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("User-Agent", "test-agent")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("health status %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer k")
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("models status %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/v1/models", nil)
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status %d", w.Code)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	logs := string(b)
	for _, want := range []string{`"event":"access"`, `"method":"GET"`, `"path":"/healthz"`, `"path":"/v1/models"`, `"remote_ip":"192.0.2.10"`, `"user_agent":"test-agent"`, `"auth_failure":"missing_authorization"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("log missing %s:\n%s", want, logs)
		}
	}
}

func TestCacheRoutes(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/v1/caches", strings.NewReader(`{"model":"gemini-3.1-pro-preview","contents":[{"role":"user","parts":[{"text":"large context"}]}],"ttl":"3600s"}`))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cachedContents/cache-1") {
		t.Fatalf("create body %s", w.Body.String())
	}

	req = httptest.NewRequest("GET", "/v1/caches", nil)
	req.Header.Set("Authorization", "Bearer k")
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "cachedContents") {
		t.Fatalf("list status %d body %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("DELETE", "/v1/caches/cache-1", nil)
	req.Header.Set("Authorization", "Bearer k")
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status %d body %s", w.Code, w.Body.String())
	}
}

func TestModelMetadataIncludesSupportedParameters(t *testing.T) {
	dir := t.TempDir()
	catalogPath := dir + "/models.json"
	logPath := dir + "/requests.jsonl"
	modelCatalog := catalog.Catalog{Version: 1, Models: []catalog.Model{{
		ID:        "gemini-test",
		Publisher: "google",
		Enabled:   true,
		Available: true,
		Capabilities: catalog.Capabilities{
			Streaming:            true,
			GenerationParameters: []string{"max_tokens", "stop"},
		},
	}}}
	b, err := json.Marshal(modelCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, b, 0644); err != nil {
		t.Fatal(err)
	}
	logger, err := gwlog.New(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-test"}, ModelCatalogPath: catalogPath, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: logPath, RequestTimeoutSeconds: 5}
	s := New(cfg, fakeGemini{}, logger)

	req := httptest.NewRequest("GET", "/v1/models/gemini-test", nil)
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	var info openai.ModelInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if strings.Join(info.SupportedParameters, ",") != "max_tokens,stop" {
		t.Fatalf("supported parameters %#v", info.SupportedParameters)
	}
	if info.Capabilities == nil || info.Capabilities.Streaming == nil || !*info.Capabilities.Streaming {
		t.Fatalf("streaming capability %#v", info.Capabilities)
	}
}

func TestPersistentLog(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/requests.jsonl"
	logger, _ := gwlog.New(path)
	logger.Write(gwlog.RequestLog{Timestamp: time.Now(), RequestID: "abc", Model: "gemini-3.1-pro-preview", Status: 200})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "abc") {
		t.Fatalf("log missing entry: %s", string(b))
	}
}
