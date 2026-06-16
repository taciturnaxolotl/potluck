package web

import (
	"encoding/json"
	"net/http"
	"time"
)

// handleListModels returns the model catalog from the DB.
// The catalog is populated (and refreshed hourly) by pool.ModelsRefresher.
// We never do live pioneer fetches here — that keeps the endpoint fast and
// immune to pool-key availability issues.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Q.ListModelCatalog(r.Context())
	if err != nil {
		writeErr(w, 500, "db_error", "could not read model catalog")
		return
	}

	// refreshed_at: min(refreshed_at) across catalog rows (oldest entry).
	var refreshedAt int64
	if ts, err2 := s.Q.GetModelCatalogRefreshedAt(r.Context()); err2 == nil {
		refreshedAt = toInt64(ts)
	}

	// Stats cover the last 48h. Merge billing-row stats (pioneer) with
	// potluck_requests stats (all providers). Request-based stats take
	// precedence since they use the full prefixed model ID that matches
	// the catalog.
	since := time.Now().Add(-48 * time.Hour).Unix()
	statsMap := make(map[string]any)

	// First: billing-row stats (legacy pioneer, unprefixed model names).
	billingRows, _ := s.Q.ListModelStats(r.Context(), since)
	for _, st := range billingRows {
		var avgTps *float64
		if st.AvgTps.Valid {
			avgTps = &st.AvgTps.Float64
		}
		var totalIn, totalOut float64
		if st.TotalInputTokens.Valid {
			totalIn = st.TotalInputTokens.Float64
		}
		if st.TotalOutputTokens.Valid {
			totalOut = st.TotalOutputTokens.Float64
		}
		statsMap[st.Model] = map[string]any{
			"request_count":       st.RequestCount,
			"total_input_tokens":  totalIn,
			"total_output_tokens": totalOut,
			"avg_tps":             avgTps,
		}
	}

	// Second: request-based stats (all providers, prefixed model names).
	// These override billing-row stats when keys match.
	reqRows, _ := s.Q.ListModelStatsFromRequests(r.Context(), since)
	for _, st := range reqRows {
		var avgTps *float64
		if st.AvgTps.Valid {
			avgTps = &st.AvgTps.Float64
		}
		var totalIn, totalOut float64
		if st.TotalInputTokens.Valid {
			totalIn = st.TotalInputTokens.Float64
		}
		if st.TotalOutputTokens.Valid {
			totalOut = st.TotalOutputTokens.Float64
		}
		statsMap[st.Model] = map[string]any{
			"request_count":       st.RequestCount,
			"total_input_tokens":  totalIn,
			"total_output_tokens": totalOut,
			"avg_tps":             avgTps,
		}
	}

	type caps struct {
		Thinking struct {
			Supported bool `json:"supported"`
		} `json:"thinking"`
		ImageInput struct {
			Supported bool `json:"supported"`
		} `json:"image_input"`
		StructuredOutputs struct {
			Supported bool `json:"supported"`
		} `json:"structured_outputs"`
	}
	type rawModel struct {
		MaxTokens    int  `json:"max_tokens"`
		Capabilities caps `json:"capabilities"`
	}

	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		// Convert micros-per-million back to USD-per-million for the frontend.
		var inputPerMil float64
		if m.InputPricePerMillionMicros.Valid {
			inputPerMil = float64(m.InputPricePerMillionMicros.Int64) / 1_000_000
		}
		var outputPerMil *float64
		if m.OutputPricePerMillionMicros.Valid && m.OutputPricePerMillionMicros.Int64 > 0 {
			v := float64(m.OutputPricePerMillionMicros.Int64) / 1_000_000
			outputPerMil = &v
		}

		var ctxWindow int64
		if m.ContextWindow.Valid {
			ctxWindow = m.ContextWindow.Int64
		}
		var tier string
		if m.Tier.Valid {
			tier = m.Tier.String
		}

		var rm rawModel
		_ = json.Unmarshal([]byte(m.RawJson), &rm)

		out = append(out, map[string]any{
			"id":                 m.ID,
			"label":              m.Label,
			"description":        m.Description,
			"context_window":     ctxWindow,
			"max_output_tokens":  rm.MaxTokens,
			"input_per_mil":      inputPerMil,
			"output_per_mil":     outputPerMil,
			"license":            "",
			"tier":               tier,
			"thinking":           rm.Capabilities.Thinking.Supported,
			"image_input":        rm.Capabilities.ImageInput.Supported,
			"structured_outputs": rm.Capabilities.StructuredOutputs.Supported,
			"stats":              statsMap[m.ID],
		})
	}

	writeJSON(w, 200, map[string]any{
		"models":       out,
		"refreshed_at": refreshedAt,
	})
}
