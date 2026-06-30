package config

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type ModelResolver struct {
	mu                  sync.RWMutex
	AllowedModels       map[string]struct{}
	AllowAnyGeminiModel bool
	Aliases             map[string]string
}

func NewModelResolver(c Config) *ModelResolver {
	allowed := make(map[string]struct{}, len(c.AllowedModels))
	for _, m := range c.AllowedModels {
		allowed[m] = struct{}{}
	}
	aliases := map[string]string{}
	for k, v := range c.ModelAliases {
		aliases[k] = v
	}
	return &ModelResolver{AllowedModels: allowed, AllowAnyGeminiModel: c.AllowAnyGeminiModel, Aliases: aliases}
}

func (r *ModelResolver) Resolve(requested string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := strings.TrimSpace(requested)
	if m == "" {
		return "", fmt.Errorf("model is required")
	}
	if resolved, ok := r.Aliases[m]; ok {
		m = resolved
	}
	if _, ok := r.AllowedModels[m]; ok {
		return m, nil
	}
	if r.AllowAnyGeminiModel && strings.HasPrefix(m, "gemini-") {
		return m, nil
	}
	return "", fmt.Errorf("model %q is not allowed", requested)
}

func (r *ModelResolver) ListModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.AllowedModels)+len(r.Aliases))
	seen := map[string]struct{}{}
	for m := range r.AllowedModels {
		out = append(out, m)
		seen[m] = struct{}{}
	}
	for alias := range r.Aliases {
		if _, ok := seen[alias]; !ok {
			out = append(out, alias)
		}
	}
	sort.Strings(out)
	return out
}

func (r *ModelResolver) SetAllowedModels(models []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	allowed := make(map[string]struct{}, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m != "" {
			allowed[m] = struct{}{}
		}
	}
	r.AllowedModels = allowed
}
