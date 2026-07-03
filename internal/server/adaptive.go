package server

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/gemini"
)

type adaptiveLimiters struct {
	enabled bool
	min     int
	initial int
	max     int

	mu      sync.Mutex
	byModel map[string]*adaptiveLimiter
}

type adaptivePermit struct {
	limiter  *adaptiveLimiter
	Wait     time.Duration
	Limit    int
	InFlight int
	once     sync.Once
}

type adaptiveLimiter struct {
	mu       sync.Mutex
	inFlight int
	limit    int
	min      int
	max      int
	clean    int
	notify   chan struct{}
}

func newAdaptiveLimiters(cfg config.Config) *adaptiveLimiters {
	min := cfg.AdaptiveConcurrencyMin
	if min <= 0 {
		min = 1
	}
	initial := cfg.AdaptiveConcurrencyInitial
	if initial < min {
		initial = min
	}
	maximum := cfg.AdaptiveConcurrencyMax
	if maximum < initial {
		maximum = initial
	}
	return &adaptiveLimiters{
		enabled: cfg.AdaptiveConcurrencyEnabled,
		min:     min,
		initial: initial,
		max:     maximum,
		byModel: map[string]*adaptiveLimiter{},
	}
}

func (a *adaptiveLimiters) acquire(ctx context.Context, model string) (*adaptivePermit, error) {
	if a == nil || !a.enabled {
		return &adaptivePermit{}, nil
	}
	limiter := a.forModel(model)
	return limiter.acquire(ctx)
}

func (a *adaptiveLimiters) forModel(model string) *adaptiveLimiter {
	a.mu.Lock()
	defer a.mu.Unlock()
	if l, ok := a.byModel[model]; ok {
		return l
	}
	l := &adaptiveLimiter{
		limit:  a.initial,
		min:    a.min,
		max:    a.max,
		notify: make(chan struct{}),
	}
	a.byModel[model] = l
	return l
}

func (l *adaptiveLimiter) acquire(ctx context.Context) (*adaptivePermit, error) {
	start := time.Now()
	l.mu.Lock()
	for {
		if l.inFlight < l.limit {
			l.inFlight++
			p := &adaptivePermit{
				limiter:  l,
				Wait:     time.Since(start),
				Limit:    l.limit,
				InFlight: l.inFlight,
			}
			l.mu.Unlock()
			return p, nil
		}
		notify := l.notify
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-notify:
			l.mu.Lock()
		}
	}
}

func (p *adaptivePermit) release(err error) {
	if p == nil || p.limiter == nil {
		return
	}
	p.once.Do(func() {
		p.limiter.release(err)
	})
}

func (l *adaptiveLimiter) release(err error) {
	l.mu.Lock()
	if l.inFlight > 0 {
		l.inFlight--
	}
	switch {
	case isResourceExhausted(err):
		next := int(math.Floor(float64(l.limit) * 0.7))
		if next < l.min {
			next = l.min
		}
		l.limit = next
		l.clean = 0
	case err == nil:
		l.clean++
		if l.clean >= l.limit && l.limit < l.max {
			l.limit++
			l.clean = 0
		}
	default:
		l.clean = 0
	}
	close(l.notify)
	l.notify = make(chan struct{})
	l.mu.Unlock()
}

func isResourceExhausted(err error) bool {
	var ve *gemini.VertexError
	if !errors.As(err, &ve) {
		return false
	}
	return ve.Status == http.StatusTooManyRequests || strings.Contains(ve.Body, "RESOURCE_EXHAUSTED")
}
