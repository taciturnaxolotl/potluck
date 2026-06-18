// Package registry provides a multi-provider registry that maps provider IDs
// to fantasy.Provider instances. It handles construction of the correct
// fantasy provider based on the provider type stored in the database.
package registry

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

// ProviderType identifies how to construct a fantasy provider.
type ProviderType string

const (
	TypeOpenAICompat ProviderType = "openai_compat"
	TypeAnthropic    ProviderType = "anthropic"
	TypeGoogle       ProviderType = "google"
	TypeOpenRouter   ProviderType = "openrouter"
)

// ProviderConfig holds the configuration for a single upstream provider.
type ProviderConfig struct {
	ID      string
	Type    ProviderType
	Name    string
	BaseURL string
	Active  bool
	IsFree  bool
	Config  map[string]string // provider-specific config from config_json
}

// Registry manages multiple upstream providers and creates fantasy.Provider
// instances on demand.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]ProviderConfig
	q         *store.Queries // set when loaded from DB, enables Reload
}

// New creates a registry from a list of provider configs.
func New(configs []ProviderConfig) *Registry {
	m := make(map[string]ProviderConfig, len(configs))
	for _, c := range configs {
		m[c.ID] = c
	}
	return &Registry{providers: m}
}

// LoadFromDB loads all active providers from the database.
func LoadFromDB(ctx context.Context, q *store.Queries) (*Registry, error) {
	rows, err := q.ListActiveProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	configs := make([]ProviderConfig, 0, len(rows))
	for _, r := range rows {
		configs = append(configs, ProviderConfig{
			ID:      r.ID,
			Type:    ProviderType(r.Type),
			Name:    r.Name,
			BaseURL: r.BaseUrl,
			Active:  r.Active == 1,
			IsFree:  r.IsFree == 1,
			// TODO: parse config_json into map when needed
		})
	}
	m := make(map[string]ProviderConfig, len(configs))
	for _, c := range configs {
		m[c.ID] = c
	}
	return &Registry{providers: m, q: q}, nil
}

// Reload refreshes the registry from the database. Safe to call concurrently.
func (r *Registry) Reload(ctx context.Context) error {
	if r.q == nil {
		return fmt.Errorf("registry not loaded from DB")
	}
	rows, err := r.q.ListActiveProviders(ctx)
	if err != nil {
		return fmt.Errorf("list providers: %w", err)
	}
	m := make(map[string]ProviderConfig, len(rows))
	for _, row := range rows {
		m[row.ID] = ProviderConfig{
			ID:      row.ID,
			Type:    ProviderType(row.Type),
			Name:    row.Name,
			BaseURL: row.BaseUrl,
			Active:  row.Active == 1,
			IsFree:  row.IsFree == 1,
		}
	}
	r.mu.Lock()
	r.providers = m
	r.mu.Unlock()
	return nil
}

// Get returns the config for a provider ID.
func (r *Registry) Get(providerID string) (ProviderConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.providers[providerID]
	return c, ok
}

// List returns all registered provider configs.
func (r *Registry) List() []ProviderConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderConfig, 0, len(r.providers))
	for _, c := range r.providers {
		out = append(out, c)
	}
	return out
}

// ToFantasy creates a fantasy.Provider for the given provider ID and API key.
// For free providers, apiKey is ignored.
func (r *Registry) ToFantasy(providerID, apiKey string) (fantasy.Provider, error) {
	cfg, ok := r.Get(providerID)
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", providerID)
	}
	return newFantasyProvider(cfg, apiKey)
}

// ResolveModel splits a prefixed model ID into (providerID, upstreamModel).
// If no prefix is found, returns ("pioneer", modelID) as the default.
// Format: "provider_id/model_name" e.g. "openrouter/claude-sonnet-4"
func (r *Registry) ResolveModel(model string) (providerID, upstreamModel string) {
	if idx := strings.IndexByte(model, '/'); idx > 0 {
		prefix := model[:idx]
		if _, ok := r.Get(prefix); ok {
			return prefix, model[idx+1:]
		}
		// Backward compat: "free/" maps to "omlx"
		if prefix == "free" {
			if _, ok := r.Get("omlx"); ok {
				return "omlx", model[idx+1:]
			}
		}
	}
	// Default to pioneer for bare model names (backward compat).
	return "pioneer", model
}

func newFantasyProvider(cfg ProviderConfig, apiKey string) (fantasy.Provider, error) {
	switch cfg.Type {
	case TypeAnthropic:
		opts := []anthropic.Option{
			anthropic.WithName(cfg.Name),
		}
		if cfg.BaseURL != "" {
			opts = append(opts, anthropic.WithBaseURL(cfg.BaseURL))
		}
		if apiKey != "" {
			opts = append(opts, anthropic.WithAPIKey(apiKey))
		}
		return anthropic.New(opts...)

	case TypeGoogle:
		opts := []google.Option{
			google.WithName(cfg.Name),
		}
		if apiKey != "" {
			opts = append(opts, google.WithGeminiAPIKey(apiKey))
		}
		if cfg.BaseURL != "" {
			opts = append(opts, google.WithBaseURL(cfg.BaseURL))
		}
		return google.New(opts...)

	case TypeOpenRouter:
		opts := []openrouter.Option{
			openrouter.WithName(cfg.Name),
		}
		if apiKey != "" {
			opts = append(opts, openrouter.WithAPIKey(apiKey))
		}
		return openrouter.New(opts...)

	default:
		// Everything else (openai_compat, nvidia, omlx, free, generic, any future
		// OpenAI-shaped provider) uses the openaicompat constructor. This is
		// the safe default since most LLM providers speak OpenAI-compatible API.
		// Fantasy expects the base URL to include /v1, so append it if missing.
		baseURL := cfg.BaseURL
		if !strings.HasSuffix(baseURL, "/v1") {
			baseURL = strings.TrimRight(baseURL, "/") + "/v1"
		}
		opts := []openaicompat.Option{
			openaicompat.WithBaseURL(baseURL),
			openaicompat.WithName(cfg.Name),
		}
		if apiKey != "" {
			opts = append(opts, openaicompat.WithAPIKey(apiKey))
		}
		return openaicompat.New(opts...)
	}
}
