package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/go-llm-gateway/internal/openai"
)

const (
	jobStatusQueued    = "queued"
	jobStatusRunning   = "running"
	jobStatusSucceeded = "succeeded"
	jobStatusFailed    = "failed"
	jobStatusCanceled  = "canceled"
)

type chatJob struct {
	ID             string                         `json:"id"`
	Object         string                         `json:"object"`
	Status         string                         `json:"status"`
	CreatedAt      time.Time                      `json:"created_at"`
	UpdatedAt      time.Time                      `json:"updated_at"`
	Model          string                         `json:"model"`
	Response       *openai.ChatCompletionResponse `json:"response,omitempty"`
	Error          *openai.ErrorBody              `json:"error,omitempty"`
	cancel         context.CancelFunc
	idempotencyKey string
	scope          string
}

type jobStore interface {
	Create(scope, idempotencyKey string, req openai.ChatCompletionRequest) (*chatJob, bool)
	Get(id string) (*chatJob, bool)
	MarkRunning(id string, cancel context.CancelFunc) bool
	Complete(id string, resp openai.ChatCompletionResponse)
	Fail(id string, status int, msg, code string)
	Cancel(id string) (*chatJob, bool)
}

type memoryJobStore struct {
	mu       sync.Mutex
	jobs     map[string]*chatJob
	byKey    map[string]string
	retain   time.Duration
	requests map[string]openai.ChatCompletionRequest
}

func newMemoryJobStore(retain time.Duration) *memoryJobStore {
	if retain <= 0 {
		retain = time.Hour
	}
	return &memoryJobStore{
		jobs:     map[string]*chatJob{},
		byKey:    map[string]string{},
		retain:   retain,
		requests: map[string]openai.ChatCompletionRequest{},
	}
}

func (s *memoryJobStore) Create(scope, idempotencyKey string, req openai.ChatCompletionRequest) (*chatJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	if idempotencyKey != "" {
		key := scope + "\x00" + idempotencyKey
		if id, ok := s.byKey[key]; ok {
			if job, ok := s.jobs[id]; ok {
				return cloneJob(job), false
			}
		}
	}
	now := time.Now().UTC()
	job := &chatJob{
		ID:             "chatjob-" + randomID(),
		Object:         "chat.job",
		Status:         jobStatusQueued,
		CreatedAt:      now,
		UpdatedAt:      now,
		Model:          req.Model,
		idempotencyKey: idempotencyKey,
		scope:          scope,
	}
	s.jobs[job.ID] = job
	s.requests[job.ID] = req
	if idempotencyKey != "" {
		s.byKey[scope+"\x00"+idempotencyKey] = job.ID
	}
	return cloneJob(job), true
}

func (s *memoryJobStore) request(id string) (openai.ChatCompletionRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.requests[id]
	return req, ok
}

func (s *memoryJobStore) Get(id string) (*chatJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	job, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	return cloneJob(job), true
}

func (s *memoryJobStore) MarkRunning(id string, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok || job.Status != jobStatusQueued {
		return false
	}
	job.Status = jobStatusRunning
	job.UpdatedAt = time.Now().UTC()
	job.cancel = cancel
	return true
}

func (s *memoryJobStore) Complete(id string, resp openai.ChatCompletionResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok || job.Status == jobStatusCanceled {
		return
	}
	job.Status = jobStatusSucceeded
	job.UpdatedAt = time.Now().UTC()
	job.Response = &resp
	job.cancel = nil
	delete(s.requests, id)
}

func (s *memoryJobStore) Fail(id string, status int, msg, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok || job.Status == jobStatusCanceled {
		return
	}
	job.Status = jobStatusFailed
	job.UpdatedAt = time.Now().UTC()
	job.Error = &openai.ErrorBody{Message: msg, Type: errorTypeForStatus(status), Code: code}
	job.cancel = nil
	delete(s.requests, id)
}

func (s *memoryJobStore) Cancel(id string) (*chatJob, bool) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return nil, false
	}
	if job.Status == jobStatusQueued || job.Status == jobStatusRunning {
		if job.cancel != nil {
			job.cancel()
		}
		job.Status = jobStatusCanceled
		job.UpdatedAt = time.Now().UTC()
		job.Error = &openai.ErrorBody{Message: "job canceled", Type: "server_error", Code: "canceled"}
		job.cancel = nil
		delete(s.requests, id)
	}
	out := cloneJob(job)
	s.mu.Unlock()
	return out, true
}

func (s *memoryJobStore) pruneLocked(now time.Time) {
	for id, job := range s.jobs {
		if job.Status == jobStatusQueued || job.Status == jobStatusRunning || now.Sub(job.UpdatedAt) < s.retain {
			continue
		}
		delete(s.jobs, id)
		delete(s.requests, id)
		if job.idempotencyKey != "" {
			delete(s.byKey, job.scope+"\x00"+job.idempotencyKey)
		}
	}
}

func cloneJob(job *chatJob) *chatJob {
	out := *job
	out.cancel = nil
	return &out
}

func jobScope(r *http.Request) string {
	if authz := strings.TrimSpace(r.Header.Get("Authorization")); authz != "" {
		return authz
	}
	if app := strings.TrimSpace(r.Header.Get("X-App-ID")); app != "" {
		return "app:" + app
	}
	return "anonymous"
}

func errorTypeForStatus(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "server_overloaded"
	case status >= 500:
		return "server_error"
	default:
		return "invalid_request_error"
	}
}

func isCanceledError(err error) bool {
	return errors.Is(err, context.Canceled)
}
