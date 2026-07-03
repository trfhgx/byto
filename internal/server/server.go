package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/go-llm-gateway/internal/auth"
	"github.com/example/go-llm-gateway/internal/catalog"
	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/gemini"
	gwlog "github.com/example/go-llm-gateway/internal/logging"
	"github.com/example/go-llm-gateway/internal/openai"
)

type GeminiClient interface {
	GenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, opts gemini.RequestOptions) (gemini.GenerateResponse, error)
	StreamGenerateContent(ctx context.Context, model string, in gemini.GenerateRequest, opts gemini.RequestOptions, onChunk func(gemini.GenerateResponse) error) error
	CountTokens(ctx context.Context, model string, in gemini.GenerateRequest) error
	CreateCachedContent(ctx context.Context, body json.RawMessage) (json.RawMessage, error)
	ListCachedContents(ctx context.Context, query url.Values) (json.RawMessage, error)
	GetCachedContent(ctx context.Context, id string) (json.RawMessage, error)
	DeleteCachedContent(ctx context.Context, id string) (json.RawMessage, error)
}

type Server struct {
	cfg          config.Config
	resolver     *config.ModelResolver
	gemini       GeminiClient
	logger       *gwlog.JSONLLogger
	apiKeys      map[string]struct{}
	catalogMu    sync.RWMutex
	modelCatalog map[string]catalog.Model
	limiters     *adaptiveLimiters
}

func New(cfg config.Config, gem GeminiClient, logger *gwlog.JSONLLogger) *Server {
	keys := map[string]struct{}{}
	for _, k := range cfg.GatewayAPIKeys {
		keys[k] = struct{}{}
	}
	s := &Server{cfg: cfg, resolver: config.NewModelResolver(cfg), gemini: gem, logger: logger, apiKeys: keys, modelCatalog: map[string]catalog.Model{}, limiters: newAdaptiveLimiters(cfg)}
	if cfg.ModelCatalogPath != "" {
		if c, err := catalog.Load(cfg.ModelCatalogPath); err == nil {
			s.setModelCatalog(c)
		}
	}
	return s
}

func NewFromConfig(cfg config.Config) (*Server, error) {
	if cfg.ModelCatalogPath != "" {
		if c, err := catalog.Load(cfg.ModelCatalogPath); err == nil {
			cfg.AllowedModels = c.EnabledAvailableIDs()
		} else if len(cfg.AllowedModels) == 0 {
			return nil, fmt.Errorf("load model catalog: %w", err)
		} else {
			log.Printf("model catalog load warning path=%s err=%v", cfg.ModelCatalogPath, err)
		}
	}
	logger, err := gwlog.New(cfg.LogPath, cfg.LogMaxBytes)
	if err != nil {
		return nil, err
	}
	gem := gemini.NewClient(cfg, auth.NewDefaultTokenProvider())
	s := New(cfg, gem, logger)
	if cfg.ModelCatalogRefreshOnStart && cfg.ModelCatalogPath != "" {
		go s.refreshModelCatalog(cfg.ModelCatalogPath)
	}
	return s, nil
}

func (s *Server) refreshModelCatalog(path string) {
	supported := catalog.SupportedGoogleGeminiModels()
	c, err := catalog.Load(path)
	if err != nil {
		log.Printf("model catalog refresh load warning path=%s err=%v", path, err)
		return
	}
	c.MergeSupported(supported)
	verified, hardFailures, inconclusive := s.verifyModelCatalogCandidates(&c, supported)
	if err := catalog.Save(path, c); err != nil {
		log.Printf("model catalog refresh save warning path=%s err=%v", path, err)
		return
	}
	s.setModelCatalog(c)
	s.resolver.SetAllowedModels(c.EnabledAvailableIDs())
	log.Printf("model catalog refreshed path=%s source=supported_google_gemini_models supported_models=%d verified=%d hard_failures=%d inconclusive=%d enabled_available=%d", path, len(supported), verified, hardFailures, inconclusive, len(c.EnabledAvailableIDs()))
}

func (s *Server) verifyModelCatalogCandidates(c *catalog.Catalog, supported []catalog.LiveModel) (verified, hardFailures, inconclusive int) {
	index := map[string]int{}
	for i, m := range c.Models {
		index[m.ID] = i
	}
	for _, model := range supported {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		i, ok := index[id]
		if !ok {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		err := s.gemini.CountTokens(ctx, id, catalogVerificationRequest())
		cancel()
		if err == nil {
			c.Models[i].Enabled = true
			c.Models[i].Available = true
			c.Models[i].Notes = "Verified by Vertex countTokens for the configured project/location."
			now := time.Now().UTC()
			c.Models[i].LastSeenAt = &now
			verified++
			continue
		}
		if isHardModelVerificationFailure(err) {
			c.Models[i].Enabled = false
			c.Models[i].Available = false
			c.Models[i].Notes = fmt.Sprintf("Disabled because Vertex countTokens verification failed for this project/location: %s", compactError(err))
			hardFailures++
			continue
		}
		if c.Models[i].Notes == "" {
			c.Models[i].Notes = fmt.Sprintf("Vertex countTokens verification was inconclusive; keeping previous catalog state: %s", compactError(err))
		}
		inconclusive++
	}
	return verified, hardFailures, inconclusive
}

func catalogVerificationRequest() gemini.GenerateRequest {
	maxOutputTokens := 1
	return gemini.GenerateRequest{
		Contents: []gemini.Content{{Role: "user", Parts: []gemini.Part{{Text: "ok"}}}},
		GenerationConfig: &gemini.GenerationConfig{
			MaxOutputTokens: &maxOutputTokens,
		},
	}
}

func isHardModelVerificationFailure(err error) bool {
	var ve *gemini.VertexError
	if !errors.As(err, &ve) {
		return false
	}
	switch ve.Status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return true
	default:
		return false
	}
}

func compactError(err error) string {
	msg := strings.TrimSpace(err.Error())
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) > 300 {
		msg = msg[:300] + "..."
	}
	return msg
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/v1/models", s.models)
	mux.HandleFunc("/v1/models/", s.model)
	mux.HandleFunc("/v1/caches", s.caches)
	mux.HandleFunc("/v1/caches/", s.cache)
	mux.HandleFunc("/v1/chat/completions", s.chatCompletions)
	return requestID(s.accessLog(recoverPanic(mux)))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "method_not_allowed")
		return
	}
	models := s.resolver.ListModels()
	data := make([]openai.ModelInfo, 0, len(models))
	for _, m := range models {
		data = append(data, s.modelInfo(m))
	}
	writeJSON(w, http.StatusOK, openai.ModelListResponse{Object: "list", Data: data})
}

func (s *Server) model(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "method_not_allowed")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid_api_key")
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/models/"))
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "model not found", "not_found")
		return
	}
	resolved, err := s.resolver.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error(), "not_found")
		return
	}
	info := s.modelInfo(resolved)
	if resolved != id {
		info.ID = id
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) caches(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid_api_key")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout())
	defer cancel()
	switch r.Method {
	case http.MethodGet:
		out, err := s.gemini.ListCachedContents(ctx, r.URL.Query())
		s.writeVertexProxyResult(w, out, err)
	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024*1024))
		if err != nil {
			writeError(w, http.StatusBadRequest, "read request body failed", "invalid_request_error")
			return
		}
		out, err := s.gemini.CreateCachedContent(ctx, json.RawMessage(body))
		s.writeVertexProxyResult(w, out, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "method_not_allowed")
	}
}

func (s *Server) cache(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid_api_key")
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/caches/"), "/")
	if id == "" {
		writeError(w, http.StatusNotFound, "cache not found", "not_found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout())
	defer cancel()
	switch r.Method {
	case http.MethodGet:
		out, err := s.gemini.GetCachedContent(ctx, id)
		s.writeVertexProxyResult(w, out, err)
	case http.MethodDelete:
		out, err := s.gemini.DeleteCachedContent(ctx, id)
		s.writeVertexProxyResult(w, out, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "method_not_allowed")
	}
}

func (s *Server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	reqID := getRequestID(r)
	appID := r.Header.Get("X-App-ID")
	logEntry := gwlog.RequestLog{Timestamp: start.UTC(), RequestID: reqID, AppID: appID}
	defer func() { s.logger.Write(logEntry) }()

	if r.Method != http.MethodPost {
		logEntry.Status = http.StatusMethodNotAllowed
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "method_not_allowed")
		return
	}
	if !s.authorized(r) {
		logEntry.Status = http.StatusUnauthorized
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid_api_key")
		return
	}

	var req openai.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logEntry.Status = http.StatusBadRequest
		logEntry.Error = err.Error()
		writeError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
		return
	}
	logEntry.Model = req.Model
	if err := openai.ValidateRequest(req); err != nil {
		logEntry.Status = http.StatusBadRequest
		logEntry.Error = err.Error()
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	model, err := s.resolver.Resolve(req.Model)
	if err != nil {
		logEntry.Status = http.StatusBadRequest
		logEntry.Error = err.Error()
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_model")
		return
	}
	logEntry.VertexModel = model
	logEntry.Stream = req.Stream

	serviceTier, requestOpts, err := vertexRequestOptions(req.ServiceTier)
	if err != nil {
		logEntry.Status = http.StatusBadRequest
		logEntry.Error = err.Error()
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	logEntry.ServiceTier = serviceTier

	greq, err := gemini.FromOpenAI(req)
	if err != nil {
		logEntry.Status = http.StatusBadRequest
		logEntry.Error = err.Error()
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	reasoningEffort, err := s.applyReasoningConfig(model, req, &greq)
	if err != nil {
		logEntry.Status = http.StatusBadRequest
		logEntry.Error = err.Error()
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	logEntry.ReasoningEffort = reasoningEffort

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout())
	defer cancel()
	permit, err := s.limiters.acquire(ctx, model)
	if err != nil {
		logEntry.Status = http.StatusTooManyRequests
		logEntry.Error = err.Error()
		writeError(w, http.StatusTooManyRequests, "model is busy; retry later", "server_overloaded")
		return
	}
	if permit.Limit > 0 {
		w.Header().Set("X-Byto-Queue-Wait-Ms", fmt.Sprint(permit.Wait.Milliseconds()))
		w.Header().Set("X-Byto-Model-In-Flight", fmt.Sprint(permit.InFlight))
		w.Header().Set("X-Byto-Model-Concurrency-Limit", fmt.Sprint(permit.Limit))
		logEntry.QueueWaitMS = permit.Wait.Milliseconds()
		logEntry.ModelInFlight = permit.InFlight
		logEntry.ModelConcurrencyLimit = permit.Limit
	}
	var upstreamErr error
	defer func() { permit.release(upstreamErr) }()
	if req.Stream {
		upstreamErr = s.stream(ctx, w, model, reqID, greq, requestOpts, &logEntry, start)
		return
	}
	gresp, err := s.gemini.GenerateContent(ctx, model, greq, requestOpts)
	upstreamErr = err
	logEntry.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		logEntry.Status = vertexGatewayStatus(err)
		logEntry.Error = err.Error()
		setUpstreamLog(&logEntry, err)
		writeError(w, logEntry.Status, err.Error(), "vertex_error")
		return
	}
	logEntry.Status = http.StatusOK
	logEntry.PromptTokens = gresp.UsageMetadata.PromptTokenCount
	logEntry.CompletionTokens = gresp.UsageMetadata.CandidatesTokenCount
	logEntry.TotalTokens = gresp.UsageMetadata.TotalTokenCount
	logEntry.CachedTokens = gresp.UsageMetadata.CachedContentTokenCount
	logEntry.ThoughtsTokens = gresp.UsageMetadata.ThoughtsTokenCount
	logEntry.TrafficType = gresp.UsageMetadata.TrafficType
	var completionDetails *openai.CompletionTokensDetails
	if logEntry.ThoughtsTokens > 0 {
		completionDetails = &openai.CompletionTokensDetails{ReasoningTokens: logEntry.ThoughtsTokens}
	}
	resp := openai.ChatCompletionResponse{
		ID: "chatcmpl-" + reqID, Object: "chat.completion", Created: time.Now().Unix(), Model: model,
		Choices: []openai.Choice{{Index: 0, Message: openai.ResponseMessage{Role: "assistant", Content: gemini.TextFromResponse(gresp)}, FinishReason: gemini.FinishReason(gresp)}},
		Usage:   openai.Usage{PromptTokens: logEntry.PromptTokens, CompletionTokens: logEntry.CompletionTokens, TotalTokens: logEntry.TotalTokens, CachedTokens: logEntry.CachedTokens, CompletionTokensDetails: completionDetails, TrafficType: logEntry.TrafficType},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) stream(ctx context.Context, w http.ResponseWriter, model, reqID string, greq gemini.GenerateRequest, opts gemini.RequestOptions, logEntry *gwlog.RequestLog, start time.Time) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		logEntry.Status = http.StatusInternalServerError
		writeError(w, http.StatusInternalServerError, "streaming unsupported", "server_error")
		return errors.New("streaming unsupported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	created := time.Now().Unix()
	logEntry.Status = http.StatusOK
	first := true
	err := s.gemini.StreamGenerateContent(ctx, model, greq, opts, func(chunk gemini.GenerateResponse) error {
		text := gemini.TextFromResponse(chunk)
		logEntry.PromptTokens += chunk.UsageMetadata.PromptTokenCount
		logEntry.CompletionTokens += chunk.UsageMetadata.CandidatesTokenCount
		logEntry.TotalTokens += chunk.UsageMetadata.TotalTokenCount
		logEntry.CachedTokens += chunk.UsageMetadata.CachedContentTokenCount
		logEntry.ThoughtsTokens += chunk.UsageMetadata.ThoughtsTokenCount
		if chunk.UsageMetadata.TrafficType != "" {
			logEntry.TrafficType = chunk.UsageMetadata.TrafficType
		}
		if first {
			first = false
			sendSSE(w, openai.StreamChunk{ID: "chatcmpl-" + reqID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []openai.StreamChoice{{Index: 0, Delta: openai.StreamDelta{Role: "assistant"}, FinishReason: nil}}})
		}
		if text != "" {
			sendSSE(w, openai.StreamChunk{ID: "chatcmpl-" + reqID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []openai.StreamChoice{{Index: 0, Delta: openai.StreamDelta{Content: text}, FinishReason: nil}}})
		}
		flusher.Flush()
		return nil
	})
	logEntry.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		logEntry.Error = err.Error()
		setUpstreamLog(logEntry, err)
		log.Printf("stream error request_id=%s err=%v", reqID, err)
		return err
	}
	finish := "stop"
	sendSSE(w, openai.StreamChunk{ID: "chatcmpl-" + reqID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []openai.StreamChoice{{Index: 0, Delta: openai.StreamDelta{}, FinishReason: &finish}}})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
	return nil
}

func sendSSE(w http.ResponseWriter, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(w, "data: %s\n\n", string(b))
}

func (s *Server) writeVertexProxyResult(w http.ResponseWriter, out json.RawMessage, err error) {
	if err != nil {
		if ve, ok := err.(*gemini.VertexError); ok {
			status := http.StatusBadGateway
			if ve.Status >= 400 && ve.Status < 500 {
				status = ve.Status
			}
			writeError(w, status, ve.Error(), "vertex_error")
			return
		}
		writeError(w, http.StatusBadGateway, err.Error(), "vertex_error")
		return
	}
	writeRawJSON(w, http.StatusOK, out)
}

func setUpstreamLog(entry *gwlog.RequestLog, err error) {
	if ve, ok := err.(*gemini.VertexError); ok {
		entry.UpstreamOperation = ve.Operation
		entry.UpstreamStatus = ve.Status
		entry.UpstreamClass = classifyUpstreamStatus(ve.Status)
	}
}

func vertexGatewayStatus(err error) int {
	if ve, ok := err.(*gemini.VertexError); ok && ve.Status == http.StatusTooManyRequests {
		return http.StatusTooManyRequests
	}
	return http.StatusBadGateway
}

func classifyUpstreamStatus(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "rate_limited"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "auth_or_permission"
	case status >= 400 && status < 500:
		return "client_error"
	case status >= 500:
		return "upstream_error"
	default:
		return "unknown"
	}
}

func (s *Server) setModelCatalog(c catalog.Catalog) {
	models := make(map[string]catalog.Model, len(c.Models))
	for _, m := range c.Models {
		if strings.TrimSpace(m.ID) != "" {
			models[m.ID] = m
		}
	}
	s.catalogMu.Lock()
	s.modelCatalog = models
	s.catalogMu.Unlock()
}

func (s *Server) modelInfo(id string) openai.ModelInfo {
	info := openai.ModelInfo{ID: id, Object: "model", OwnedBy: "google"}
	s.catalogMu.RLock()
	m, ok := s.modelCatalog[id]
	s.catalogMu.RUnlock()
	if !ok {
		return info
	}
	info.DisplayName = m.DisplayName
	info.Family = m.Family
	info.Enabled = boolPtr(m.Enabled)
	info.Available = boolPtr(m.Available)
	info.LaunchStage = m.LaunchStage
	info.VersionState = m.VersionState
	info.SupportedActions = append([]string(nil), m.SupportedActions...)
	sort.Strings(info.SupportedActions)
	info.SupportedParameters = append([]string(nil), m.Capabilities.GenerationParameters...)
	sort.Strings(info.SupportedParameters)
	caps := openai.ModelCapabilities{
		ReasoningEffort: append([]string(nil), m.Capabilities.ReasoningEffort...),
		Input:           append([]string(nil), m.Capabilities.Input...),
		Output:          append([]string(nil), m.Capabilities.Output...),
	}
	if m.Capabilities.Streaming {
		caps.Streaming = boolPtr(true)
	}
	if m.Capabilities.Tools {
		caps.Tools = boolPtr(true)
	}
	if m.Capabilities.JSONMode {
		caps.JSONMode = boolPtr(true)
	}
	if hasCapabilities(caps) {
		info.Capabilities = &caps
	}
	info.Notes = m.Notes
	if m.LastSeenAt != nil {
		info.LastSeenAt = m.LastSeenAt.Format(time.RFC3339)
	}
	return info
}

func hasCapabilities(c openai.ModelCapabilities) bool {
	return len(c.ReasoningEffort) > 0 || len(c.Input) > 0 || len(c.Output) > 0 || c.Streaming != nil || c.Tools != nil || c.JSONMode != nil
}

func boolPtr(v bool) *bool {
	return &v
}

func (s *Server) authorized(r *http.Request) bool {
	if s.cfg.GatewayAllowUnauthenticated {
		return true
	}
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		return false
	}
	key := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	_, ok := s.apiKeys[key]
	return ok
}

func authFailureReason(r *http.Request) string {
	authz := r.Header.Get("Authorization")
	if strings.TrimSpace(authz) == "" {
		return "missing_authorization"
	}
	if !strings.HasPrefix(authz, "Bearer ") {
		return "malformed_authorization"
	}
	if strings.TrimSpace(strings.TrimPrefix(authz, "Bearer ")) == "" {
		return "empty_bearer_token"
	}
	return "invalid_bearer_token"
}

func vertexRequestOptions(serviceTier string) (string, gemini.RequestOptions, error) {
	tier := strings.ToLower(strings.TrimSpace(serviceTier))
	switch tier {
	case "", "auto", "high", "priority":
		return "priority", gemini.RequestOptions{LLMRequestType: "shared", LLMSharedRequestType: "priority"}, nil
	case "default", "standard", "on_demand", "on-demand":
		return "standard", gemini.RequestOptions{LLMRequestType: "shared"}, nil
	case "flex":
		return "flex", gemini.RequestOptions{LLMRequestType: "shared", LLMSharedRequestType: "flex"}, nil
	case "dedicated", "provisioned", "provisioned_throughput", "provisioned-throughput":
		return "dedicated", gemini.RequestOptions{LLMRequestType: "dedicated"}, nil
	default:
		return "", gemini.RequestOptions{}, fmt.Errorf("unsupported service_tier %q; use priority, standard, flex, or dedicated", serviceTier)
	}
}

func (s *Server) applyReasoningConfig(model string, req openai.ChatCompletionRequest, greq *gemini.GenerateRequest) (string, error) {
	effort, err := requestedReasoningEffort(req)
	if err != nil {
		return "", err
	}
	thinkingBudget := req.ExtraBody.Google.ThinkingBudget
	includeThoughts := req.ExtraBody.Google.IncludeThoughts
	if effort == "" && thinkingBudget == nil && includeThoughts == nil {
		return "", nil
	}
	if greq.GenerationConfig == nil {
		greq.GenerationConfig = &gemini.GenerationConfig{}
	}
	greq.GenerationConfig.ThinkingConfig = &gemini.ThinkingConfig{IncludeThoughts: includeThoughts}
	if thinkingBudget != nil {
		if *thinkingBudget < 0 {
			return "", errors.New("extra_body.google.thinking_budget must be non-negative")
		}
		greq.GenerationConfig.ThinkingConfig.ThinkingBudget = thinkingBudget
		return effort, nil
	}
	if effort == "" {
		return "", nil
	}
	budget, err := s.reasoningBudget(model, effort)
	if err != nil {
		return "", err
	}
	greq.GenerationConfig.ThinkingConfig.ThinkingBudget = &budget
	return effort, nil
}

func requestedReasoningEffort(req openai.ChatCompletionRequest) (string, error) {
	top := normalizeReasoningEffort(req.ReasoningEffort)
	google := normalizeReasoningEffort(req.ExtraBody.Google.ReasoningEffort)
	if top != "" && google != "" && top != google {
		return "", fmt.Errorf("conflicting reasoning_effort values %q and %q", req.ReasoningEffort, req.ExtraBody.Google.ReasoningEffort)
	}
	if top != "" {
		return top, nil
	}
	return google, nil
}

func normalizeReasoningEffort(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return ""
	case "none", "off", "disabled":
		return "off"
	case "minimal", "low":
		return "low"
	case "medium":
		return "medium"
	case "hard", "high":
		return "high"
	default:
		return strings.ToLower(strings.TrimSpace(v))
	}
}

func (s *Server) reasoningBudget(model, effort string) (int, error) {
	canonical := normalizeReasoningEffort(effort)
	if canonical == "" {
		return 0, nil
	}
	s.catalogMu.RLock()
	m, ok := s.modelCatalog[model]
	s.catalogMu.RUnlock()
	if ok {
		if len(m.Capabilities.ReasoningEffort) > 0 && !containsReasoningEffort(m.Capabilities.ReasoningEffort, canonical) {
			return 0, fmt.Errorf("reasoning_effort %q is not supported by model %q", effort, model)
		}
		if m.Capabilities.ReasoningBudgets != nil {
			if budget, ok := m.Capabilities.ReasoningBudgets[canonical]; ok {
				return budget, nil
			}
		}
	}
	budget, ok := defaultReasoningBudgets()[canonical]
	if !ok {
		return 0, fmt.Errorf("unsupported reasoning_effort %q; use off, low, medium, or high", effort)
	}
	return budget, nil
}

func containsReasoningEffort(values []string, effort string) bool {
	for _, v := range values {
		if normalizeReasoningEffort(v) == effort {
			return true
		}
	}
	return false
}

func defaultReasoningBudgets() map[string]int {
	return map[string]int{
		"off":    0,
		"low":    256,
		"medium": 1024,
		"high":   4096,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeRawJSON(w http.ResponseWriter, status int, b []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if len(b) == 0 {
		_, _ = w.Write([]byte("{}\n"))
		return
	}
	_, _ = w.Write(b)
	if b[len(b)-1] != '\n' {
		_, _ = w.Write([]byte("\n"))
	}
}
func writeError(w http.ResponseWriter, status int, msg, typ string) {
	writeJSON(w, status, openai.ErrorResponse{Error: openai.ErrorBody{Message: msg, Type: typ}})
}

type requestIDKey struct{}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = randomID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

type panicKey struct{}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusRecorder) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic recovered request_id=%s method=%s path=%s panic=%v", getRequestID(r), r.Method, r.URL.Path, v)
				if flag, ok := r.Context().Value(panicKey{}).(*bool); ok {
					*flag = true
				}
				writeError(w, http.StatusInternalServerError, "internal server error", "server_error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		panicked := false
		r = r.WithContext(context.WithValue(r.Context(), panicKey{}, &panicked))
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		entry := gwlog.AccessLog{
			Timestamp: start.UTC(),
			RequestID: getRequestID(r),
			Method:    r.Method,
			Path:      r.URL.Path,
			RemoteIP:  remoteIP(r),
			UserAgent: r.UserAgent(),
			Status:    status,
			LatencyMS: time.Since(start).Milliseconds(),
		}
		if status == http.StatusUnauthorized {
			entry.AuthFailure = authFailureReason(r)
		}
		if panicked {
			entry.Panic = true
		}
		s.logger.WriteAccess(entry)
	})
}

func remoteIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); v != "" {
		if i := strings.Index(v, ","); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		return v
	}
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func getRequestID(r *http.Request) string {
	if v, ok := r.Context().Value(requestIDKey{}).(string); ok {
		return v
	}
	return randomID()
}
func randomID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
