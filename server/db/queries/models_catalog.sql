-- models_catalog: hourly-refreshed cache of /v1/models + /base-models.

-- name: UpsertModelCatalog :exec
INSERT INTO models_catalog (
    id, label, description, context_window, max_output_tokens,
    is_chat, tier,
    input_price_per_million_micros, output_price_per_million_micros,
    raw_json, refreshed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    label                           = excluded.label,
    description                     = excluded.description,
    context_window                  = excluded.context_window,
    max_output_tokens               = excluded.max_output_tokens,
    is_chat                         = excluded.is_chat,
    tier                            = excluded.tier,
    input_price_per_million_micros  = excluded.input_price_per_million_micros,
    output_price_per_million_micros = excluded.output_price_per_million_micros,
    raw_json                        = excluded.raw_json,
    refreshed_at                    = excluded.refreshed_at;

-- name: ListModelCatalog :many
SELECT * FROM models_catalog
WHERE is_chat = 1
ORDER BY tier ASC, label ASC;

-- name: GetModelCatalogRefreshedAt :one
-- Oldest refreshed_at across all models — tells us when the catalog is stale.
SELECT COALESCE(MIN(refreshed_at), 0) FROM models_catalog;

-- name: PruneStaleModels :execrows
-- Delete models for a provider prefix that weren't refreshed in the current cycle.
-- The first arg is the LIKE pattern (e.g. "nvidia/%").
DELETE FROM models_catalog WHERE id LIKE ? AND refreshed_at < ?;
