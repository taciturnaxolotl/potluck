package pool

// models_refresher.go — hourly refresh of model catalogs across all providers.
// Dispatches to provider-specific ModelFetcher implementations.

import (
	"context"
	"net/http"
	"time"

	charmlog "charm.land/log/v2"

	"github.com/taciturnaxolotl/potluck/internal/provider/registry"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

const ModelsRefreshInterval = time.Hour

// ModelsRefresher fetches model catalogs on a background ticker.
type ModelsRefresher struct {
	q          *store.Queries
	manager    *Manager
	reg        *registry.Registry
	httpClient *http.Client
	log        *charmlog.Logger
}

// NewModelsRefresher creates a ModelsRefresher.
func NewModelsRefresher(q *store.Queries, m *Manager, reg *registry.Registry, log *charmlog.Logger) *ModelsRefresher {
	return &ModelsRefresher{
		q:          q,
		manager:    m,
		reg:        reg,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		log:        log,
	}
}

// Run starts the refresh loop. Call in a goroutine.
func (r *ModelsRefresher) Run(ctx context.Context) {
	r.log.Info("models refresher starting", "interval", ModelsRefreshInterval)
	r.refreshAll(ctx)

	t := time.NewTicker(ModelsRefreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.refreshAll(ctx)
		}
	}
}

// refreshAll iterates over all active providers and refreshes their model catalogs.
func (r *ModelsRefresher) refreshAll(ctx context.Context) {
	providers := r.reg.List()
	for _, p := range providers {
		fetcher := GetModelFetcher(string(p.Type))
		if fetcher == nil {
			continue
		}

		// Pick a key for this provider (needed for auth on some endpoints).
		var apiKey string
		sel, err := r.manager.PickForProvider(ctx, p.ID)
		if err == nil && sel != nil {
			apiKey = sel.APIKey()
		}

		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		models, err := fetcher.FetchModels(ctx, r.httpClient, p.BaseURL, apiKey)
		cancel()

		if err != nil {
			r.log.Warn("models refresher: fetch failed",
				"provider", p.ID, "err", err)
			continue
		}

		upserted := 0
		for _, params := range models {
			if err := r.q.UpsertModelCatalog(ctx, params); err == nil {
				upserted++
			}
		}
		r.log.Info("models refresher: catalog updated",
			"provider", p.ID, "models", len(models), "upserted", upserted)
	}
}
