package config

import (
	"fmt"
	"strings"
)

type ModelResolver struct {
	DefaultModel        string
	AllowedModels       map[string]struct{}
	AllowAnyGeminiModel bool
	Aliases             map[string]string
}

func NewModelResolver(c Config) ModelResolver {
	allowed := make(map[string]struct{}, len(c.AllowedModels))
	for _, m := range c.AllowedModels {
		allowed[m] = struct{}{}
	}
	aliases := map[string]string{}
	for k, v := range c.ModelAliases {
		aliases[k] = v
	}
	return ModelResolver{DefaultModel: c.DefaultModel, AllowedModels: allowed, AllowAnyGeminiModel: c.AllowAnyGeminiModel, Aliases: aliases}
}

func (r ModelResolver) Resolve(requested string) (string, error) {
	m := strings.TrimSpace(requested)
	if m == "" {
		m = r.DefaultModel
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

func (r ModelResolver) ListModels() []string {
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
	return out
}
