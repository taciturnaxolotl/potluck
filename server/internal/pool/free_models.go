package pool

// free_models.go — periodic refresh of the self-hosted free inference model list.
//
// Models from the free provider are stored in models_catalog with a "free/"
// prefix (e.g. "free/llama-3.1-8b"). Requests whose model field starts with
// "free/" are routed to the free provider after stripping the prefix. No pool
// key is consumed and no spend is recorded.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	charmlog "charm.land/log/v2"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

const FreeModelsRefreshInterval = 5 * time.Minute

// FreeModelsRefresher fetches available models from the free provider on a
// background ticker and upserts them into models_catalog with a "free/" prefix.
type FreeModelsRefresher struct {
	baseURL    string
	q          *store.Queries
	httpClient *http.Client
	log        *charmlog.Logger
}

// NewFreeModelsRefresher creates a FreeModelsRefresher.
func NewFreeModelsRefresher(baseURL string, q *store.Queries, log *charmlog.Logger) *FreeModelsRefresher {
	return &FreeModelsRefresher{
		baseURL:    strings.TrimRight(baseURL, "/"),
		q:          q,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		log:        log,
	}
}

// Run starts the refresh loop. Call in a goroutine; returns when ctx is done.
func (r *FreeModelsRefresher) Run(ctx context.Context) {
	r.log.Info("free models refresher starting", "base_url", r.baseURL, "interval", FreeModelsRefreshInterval)
	r.refresh(ctx)

	t := time.NewTicker(FreeModelsRefreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.refresh(ctx)
		}
	}
}

type freeModel struct {
	ID string `json:"id"`
}

func (r *FreeModelsRefresher) refresh(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// r.baseURL already includes /v1 (e.g. http://host:8000/v1), so append /models directly.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/models", nil)
	if err != nil {
		r.log.Warn("free provider: build request failed", "err", err)
		return
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		r.log.Warn("free provider: /v1/models fetch failed", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		r.log.Warn("free provider: /v1/models non-2xx", "status", resp.StatusCode, "body", string(b))
		return
	}

	b, _ := io.ReadAll(resp.Body)
	var out struct {
		Data []freeModel `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		r.log.Warn("free provider: /v1/models parse failed", "err", err)
		return
	}

	now := time.Now().Unix()
	upserted := 0
	for _, m := range out.Data {
		catalogID := "omlx/" + m.ID
		_ = r.q.UpsertModelCatalog(ctx, store.UpsertModelCatalogParams{
			ID:          catalogID,
			Label:       prettifyFreeLabel(m.ID),
			IsChat:      1,
			Tier:        nullString("free"),
			RawJson:     "{}",
			RefreshedAt: now,
			// input/output price stay NULL (zero-cost)
		})
		upserted++
	}
	r.log.Info("free provider: models refreshed", "count", upserted)
}

// prettifyFreeLabel converts slug-style model IDs into readable labels.
// Same logic as nvidia's prettifyModelLabel but without org prefix stripping
// since free model IDs don't have org prefixes.
func prettifyFreeLabel(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool {
		return r == '-' || r == '_'
	})
	var words []string
	for _, p := range parts {
		if p == "" {
			continue
		}
		words = append(words, strings.ToUpper(p[:1])+p[1:])
	}
	return strings.Join(words, " ")
}
