package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type AccessLog struct {
	Timestamp   time.Time `json:"timestamp"`
	Event       string    `json:"event"`
	RequestID   string    `json:"request_id"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	RemoteIP    string    `json:"remote_ip,omitempty"`
	UserAgent   string    `json:"user_agent,omitempty"`
	Status      int       `json:"status"`
	LatencyMS   int64     `json:"latency_ms"`
	AuthFailure string    `json:"auth_failure,omitempty"`
	Panic       bool      `json:"panic,omitempty"`
}

type RequestLog struct {
	Timestamp             time.Time `json:"timestamp"`
	Event                 string    `json:"event"`
	RequestID             string    `json:"request_id"`
	AppID                 string    `json:"app_id,omitempty"`
	Model                 string    `json:"model"`
	VertexModel           string    `json:"vertex_model"`
	Stream                bool      `json:"stream"`
	ServiceTier           string    `json:"service_tier,omitempty"`
	ReasoningEffort       string    `json:"reasoning_effort,omitempty"`
	TrafficType           string    `json:"traffic_type,omitempty"`
	Status                int       `json:"status"`
	LatencyMS             int64     `json:"latency_ms"`
	QueueWaitMS           int64     `json:"queue_wait_ms,omitempty"`
	ModelQueueDepth       int       `json:"model_queue_depth,omitempty"`
	ModelQueueMax         int       `json:"model_queue_max,omitempty"`
	ModelInFlight         int       `json:"model_in_flight,omitempty"`
	ModelConcurrencyLimit int       `json:"model_concurrency_limit,omitempty"`
	UpstreamOperation     string    `json:"upstream_operation,omitempty"`
	UpstreamStatus        int       `json:"upstream_status,omitempty"`
	UpstreamClass         string    `json:"upstream_classification,omitempty"`
	PromptTokens          int       `json:"prompt_tokens,omitempty"`
	CompletionTokens      int       `json:"completion_tokens,omitempty"`
	TotalTokens           int       `json:"total_tokens,omitempty"`
	CachedTokens          int       `json:"cached_tokens,omitempty"`
	ThoughtsTokens        int       `json:"thoughts_tokens,omitempty"`
	Error                 string    `json:"error,omitempty"`
}

type JSONLLogger struct {
	path     string
	maxBytes int64
	file     *os.File
	mu       sync.Mutex
}

func New(path string, maxBytes ...int64) (*JSONLLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	limit := int64(100 * 1024 * 1024)
	if len(maxBytes) > 0 && maxBytes[0] > 0 {
		limit = maxBytes[0]
	}
	return &JSONLLogger{path: path, maxBytes: limit, file: f}, nil
}

func (l *JSONLLogger) WriteAccess(entry AccessLog) {
	if entry.Event == "" {
		entry.Event = "access"
	}
	l.write(entry)
}

func (l *JSONLLogger) Write(entry RequestLog) {
	if entry.Event == "" {
		entry.Event = "request"
	}
	l.write(entry)
}

func (l *JSONLLogger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *JSONLLogger) write(entry any) {
	if l == nil {
		return
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	b = append(b, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		l.file = f
	}
	l.rotateIfNeeded(int64(len(b)))
	_, _ = l.file.Write(b)
}

func (l *JSONLLogger) rotateIfNeeded(nextBytes int64) {
	if l.maxBytes <= 0 || l.file == nil {
		return
	}
	info, err := l.file.Stat()
	if err != nil || info.Size()+nextBytes <= l.maxBytes {
		return
	}
	_ = l.file.Close()
	rotated := l.path + "." + time.Now().UTC().Format("20060102T150405.000000000")
	_ = os.Rename(l.path, rotated)
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		l.file = nil
		return
	}
	l.file = f
}
