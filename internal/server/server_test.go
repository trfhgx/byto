package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
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
type fakeRateLimitGemini struct{ fakeGemini }
type fakeCatalogVerifier struct {
	fakeGemini
	results map[string]error
}
type fakeReasoningGemini struct {
	fakeGemini
	t               *testing.T
	wantBudget      int
	wantInclude     *bool
	wantTrafficType string
}
type blockingGemini struct {
	fakeGemini
	started chan struct{}
	unblock chan struct{}
	mu      sync.Mutex
	current int
	maxSeen int
}

func (f fakeGemini) GenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, opts gemini.RequestOptions) (gemini.GenerateResponse, error) {
	return gemini.GenerateResponse{Candidates: []gemini.Candidate{{Content: gemini.Content{Parts: []gemini.Part{{Text: "ok"}}}, FinishReason: "STOP"}}, UsageMetadata: gemini.UsageMetadata{PromptTokenCount: 3, CandidatesTokenCount: 2, TotalTokenCount: 5, CachedContentTokenCount: 1, TrafficType: "ON_DEMAND_PRIORITY"}}, nil
}
func (f fakeResourceExhaustedGemini) GenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, opts gemini.RequestOptions) (gemini.GenerateResponse, error) {
	return gemini.GenerateResponse{}, &gemini.VertexError{Operation: "generateContent", Status: http.StatusTooManyRequests, Body: `{"error":{"status":"RESOURCE_EXHAUSTED"}}`}
}
func (f fakeRateLimitGemini) GenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, opts gemini.RequestOptions) (gemini.GenerateResponse, error) {
	return gemini.GenerateResponse{}, &gemini.VertexError{Operation: "generateContent", Status: http.StatusTooManyRequests, Body: `{"error":{"status":"QUOTA_EXCEEDED"}}`}
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
func (f *blockingGemini) GenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, opts gemini.RequestOptions) (gemini.GenerateResponse, error) {
	f.mu.Lock()
	f.current++
	if f.current > f.maxSeen {
		f.maxSeen = f.current
	}
	f.mu.Unlock()
	f.started <- struct{}{}
	select {
	case <-ctx.Done():
		f.mu.Lock()
		f.current--
		f.mu.Unlock()
		return gemini.GenerateResponse{}, ctx.Err()
	case <-f.unblock:
		f.mu.Lock()
		f.current--
		f.mu.Unlock()
		return gemini.GenerateResponse{Candidates: []gemini.Candidate{{Content: gemini.Content{Parts: []gemini.Part{{Text: "ok"}}}, FinishReason: "STOP"}}}, nil
	}
}
func (f *blockingGemini) maxConcurrent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxSeen
}
func (f fakeGemini) StreamGenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, opts gemini.RequestOptions, onChunk func(gemini.GenerateResponse) error) error {
	return onChunk(gemini.GenerateResponse{Candidates: []gemini.Candidate{{Content: gemini.Content{Parts: []gemini.Part{{Text: "hello"}}}}}, UsageMetadata: gemini.UsageMetadata{TrafficType: "ON_DEMAND_PRIORITY"}})
}
func (f fakeGemini) CountTokens(ctx context.Context, model string, in gemini.GenerateRequest) error {
	return nil
}
func (f fakeCatalogVerifier) CountTokens(ctx context.Context, model string, in gemini.GenerateRequest) error {
	return f.results[model]
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
	logger := testLogger(t, path)
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: path, RequestTimeoutSeconds: 5}
	return New(cfg, fakeGemini{}, logger)
}

func testLogger(t *testing.T, path string) *gwlog.JSONLLogger {
	t.Helper()
	logger, err := gwlog.New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Errorf("close logger: %v", err)
		}
	})
	return logger
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
	logger := testLogger(t, logPath)
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
	logger := testLogger(t, path)
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
	logger := testLogger(t, path)
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: path, LogMaxBytes: 1024 * 1024, RequestTimeoutSeconds: 5}
	s := New(cfg, fakeResourceExhaustedGemini{}, logger)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"temporary_resource_exhausted"`) {
		t.Fatalf("body missing resource exhausted code: %s", w.Body.String())
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

func TestChatCompletionClassifiesQuotaRateLimit(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/requests.jsonl"
	logger := testLogger(t, path)
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: path, LogMaxBytes: 1024 * 1024, RequestTimeoutSeconds: 5}
	s := New(cfg, fakeRateLimitGemini{}, logger)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"quota_or_rate_limit"`) {
		t.Fatalf("body missing quota code: %s", w.Body.String())
	}
}

func TestAdaptiveConcurrencyLimitsPerModel(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/requests.jsonl"
	logger := testLogger(t, path)
	gem := &blockingGemini{started: make(chan struct{}, 2), unblock: make(chan struct{})}
	cfg := config.Config{
		Project:                    "p",
		Location:                   "global",
		AllowedModels:              []string{"gemini-3.1-pro-preview"},
		ModelAliases:               map[string]string{},
		GatewayAPIKeys:             []string{"k"},
		VertexBaseURL:              "http://vertex",
		LogPath:                    path,
		LogMaxBytes:                1024 * 1024,
		RequestTimeoutSeconds:      5,
		AdaptiveConcurrencyEnabled: true,
		AdaptiveConcurrencyMin:     1,
		AdaptiveConcurrencyInitial: 1,
		AdaptiveConcurrencyMax:     1,
	}
	s := New(cfg, gem, logger)
	handler := s.Routes()
	body := `{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer k")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		firstDone <- w
	}()
	<-gem.started

	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer k")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		secondDone <- w
	}()

	select {
	case <-gem.started:
		t.Fatal("second request entered upstream before the first released its permit")
	case <-time.After(25 * time.Millisecond):
	}

	close(gem.unblock)
	for _, done := range []chan *httptest.ResponseRecorder{firstDone, secondDone} {
		w := <-done
		if w.Code != http.StatusOK {
			t.Fatalf("status %d body %s", w.Code, w.Body.String())
		}
		if w.Header().Get("X-Byto-Model-Concurrency-Limit") != "1" {
			t.Fatalf("missing concurrency header: %#v", w.Header())
		}
	}
	if got := gem.maxConcurrent(); got != 1 {
		t.Fatalf("max concurrent upstream calls %d, want 1", got)
	}
}

func TestAdaptiveQueueFull(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/requests.jsonl"
	logger := testLogger(t, path)
	gem := &blockingGemini{started: make(chan struct{}, 1), unblock: make(chan struct{})}
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: path, LogMaxBytes: 1024 * 1024, RequestTimeoutSeconds: 5, AdaptiveConcurrencyEnabled: true, AdaptiveConcurrencyMin: 1, AdaptiveConcurrencyInitial: 1, AdaptiveConcurrencyMax: 1, AdaptiveQueueMaxDepth: 0, AdaptiveQueueMaxWaitMS: 100}
	s := New(cfg, gem, logger)
	handler := s.Routes()
	body := `{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`
	done := make(chan struct{})
	go func() {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer k")
		handler.ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()
	<-gem.started
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), `"code":"queue_full"`) {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	close(gem.unblock)
	<-done
}

func TestAdaptiveQueueTimeout(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/requests.jsonl"
	logger := testLogger(t, path)
	gem := &blockingGemini{started: make(chan struct{}, 1), unblock: make(chan struct{})}
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: path, LogMaxBytes: 1024 * 1024, RequestTimeoutSeconds: 5, AdaptiveConcurrencyEnabled: true, AdaptiveConcurrencyMin: 1, AdaptiveConcurrencyInitial: 1, AdaptiveConcurrencyMax: 1, AdaptiveQueueMaxDepth: 1, AdaptiveQueueMaxWaitMS: 20}
	s := New(cfg, gem, logger)
	handler := s.Routes()
	body := `{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`
	done := make(chan struct{})
	go func() {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer k")
		handler.ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()
	<-gem.started
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), `"code":"queue_timeout"`) {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	close(gem.unblock)
	<-done
}

func TestAdaptiveQueueClientCancel(t *testing.T) {
	limiters := newAdaptiveLimiters(config.Config{AdaptiveConcurrencyEnabled: true, AdaptiveConcurrencyMin: 1, AdaptiveConcurrencyInitial: 1, AdaptiveConcurrencyMax: 1, AdaptiveQueueMaxDepth: 1, AdaptiveQueueMaxWaitMS: 1000})
	p, err := limiters.acquire(context.Background(), "gemini-test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	waitDone := make(chan error, 1)
	go func() {
		_, err := limiters.acquire(ctx, "gemini-test")
		waitDone <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if err := <-waitDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued acquire err %v, want canceled", err)
	}
	p.release(nil)
	p2, err := limiters.acquire(context.Background(), "gemini-test")
	if err != nil {
		t.Fatal(err)
	}
	p2.release(nil)
}

func TestAsyncJobCreatePollCompletionAndIdempotency(t *testing.T) {
	s := testServer(t)
	body := `{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/jobs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer k")
	req.Header.Set("Idempotency-Key", "retry-1")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	var job chatJob
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	req2 := httptest.NewRequest("POST", "/v1/chat/jobs", strings.NewReader(body))
	req2.Header.Set("Authorization", "Bearer k")
	req2.Header.Set("Idempotency-Key", "retry-1")
	w2 := httptest.NewRecorder()
	s.Routes().ServeHTTP(w2, req2)
	if !strings.Contains(w2.Body.String(), job.ID) {
		t.Fatalf("idempotent response changed: %s want %s", w2.Body.String(), job.ID)
	}
	deadline := time.Now().Add(time.Second)
	for {
		get := httptest.NewRequest("GET", "/v1/chat/jobs/"+job.ID, nil)
		get.Header.Set("Authorization", "Bearer k")
		gw := httptest.NewRecorder()
		s.Routes().ServeHTTP(gw, get)
		if gw.Code != http.StatusOK {
			t.Fatalf("get status %d body %s", gw.Code, gw.Body.String())
		}
		var current chatJob
		if err := json.Unmarshal(gw.Body.Bytes(), &current); err != nil {
			t.Fatal(err)
		}
		if current.Status == jobStatusSucceeded {
			if current.Response == nil || !strings.Contains(current.Response.Choices[0].Message.Content, "ok") {
				t.Fatalf("missing response: %#v", current.Response)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not complete: %s", gw.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAsyncJobCancel(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/requests.jsonl"
	logger := testLogger(t, path)
	gem := &blockingGemini{started: make(chan struct{}, 1), unblock: make(chan struct{})}
	cfg := config.Config{Project: "p", Location: "global", AllowedModels: []string{"gemini-3.1-pro-preview"}, ModelAliases: map[string]string{}, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: path, LogMaxBytes: 1024 * 1024, RequestTimeoutSeconds: 5, AsyncJobRetentionSeconds: 60, AsyncJobTimeoutSeconds: 5}
	s := New(cfg, gem, logger)
	body := `{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/jobs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	var job chatJob
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	<-gem.started
	del := httptest.NewRequest("DELETE", "/v1/chat/jobs/"+job.ID, nil)
	del.Header.Set("Authorization", "Bearer k")
	dw := httptest.NewRecorder()
	s.Routes().ServeHTTP(dw, del)
	if dw.Code != http.StatusOK || !strings.Contains(dw.Body.String(), `"status":"canceled"`) {
		t.Fatalf("delete status %d body %s", dw.Code, dw.Body.String())
	}
}

func TestUnauthorized(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
}

func TestModelsListAllowsUnauthenticated(t *testing.T) {
	s := testServer(t)
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
	logger := testLogger(t, path)
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

	req = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}]}`))
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
	for _, want := range []string{`"event":"access"`, `"method":"GET"`, `"path":"/healthz"`, `"path":"/v1/models"`, `"path":"/v1/chat/completions"`, `"remote_ip":"192.0.2.10"`, `"user_agent":"test-agent"`, `"auth_failure":"missing_authorization"`} {
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
	logger := testLogger(t, logPath)
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

func TestRefreshModelCatalogVerifiesCandidates(t *testing.T) {
	dir := t.TempDir()
	catalogPath := dir + "/models.json"
	logPath := dir + "/requests.jsonl"
	modelCatalog := catalog.Catalog{Version: 1, Models: []catalog.Model{
		{
			ID:        "gemini-2.5-flash",
			Publisher: "google",
			Enabled:   false,
			Available: false,
		},
		{
			ID:        "gemini-2.5-pro",
			Publisher: "google",
			Enabled:   true,
			Available: true,
		},
		{
			ID:        "gemini-3.5-flash",
			Publisher: "google",
			Enabled:   true,
			Available: true,
		},
	}}
	b, err := json.Marshal(modelCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, b, 0644); err != nil {
		t.Fatal(err)
	}
	logger := testLogger(t, logPath)
	cfg := config.Config{Project: "p", Location: "global", ModelCatalogPath: catalogPath, GatewayAPIKeys: []string{"k"}, VertexBaseURL: "http://vertex", LogPath: logPath, RequestTimeoutSeconds: 5}
	s := New(cfg, fakeCatalogVerifier{results: map[string]error{
		"gemini-2.5-pro":   &gemini.VertexError{Operation: "countTokens", Status: http.StatusNotFound, Body: `{"error":{"status":"NOT_FOUND"}}`},
		"gemini-3.5-flash": context.DeadlineExceeded,
	}}, logger)

	s.refreshModelCatalog(catalogPath)

	refreshed, err := catalog.Load(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]catalog.Model{}
	for _, m := range refreshed.Models {
		byID[m.ID] = m
	}
	if !byID["gemini-2.5-flash"].Enabled || !byID["gemini-2.5-flash"].Available {
		t.Fatalf("successful verification should promote model: %#v", byID["gemini-2.5-flash"])
	}
	if byID["gemini-2.5-pro"].Enabled || byID["gemini-2.5-pro"].Available {
		t.Fatalf("hard verification failure should disable model: %#v", byID["gemini-2.5-pro"])
	}
	if !byID["gemini-3.5-flash"].Enabled || !byID["gemini-3.5-flash"].Available {
		t.Fatalf("inconclusive verification should keep previous state: %#v", byID["gemini-3.5-flash"])
	}
}

func TestPersistentLog(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/requests.jsonl"
	logger := testLogger(t, path)
	logger.Write(gwlog.RequestLog{Timestamp: time.Now(), RequestID: "abc", Model: "gemini-3.1-pro-preview", Status: 200})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "abc") {
		t.Fatalf("log missing entry: %s", string(b))
	}
}
