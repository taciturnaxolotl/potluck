package v1

import (
	"encoding/json"
	"net/http"
)

// handleListModels returns available models in OpenAI's /v1/models shape.
// Reads from models_catalog (populated hourly by pool.ModelsRefresher).
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Q.ListModelCatalog(r.Context())
	if err != nil || len(rows) == 0 {
		// Empty catalog — return empty list, not an error. Clients handle this.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []any{},
		})
		return
	}

	data := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		var created int64
		if m.RefreshedAt > 0 {
			created = m.RefreshedAt
		}
		data = append(data, map[string]any{
			"id":       m.ID,
			"object":   "model",
			"created":  created,
			"owned_by": "potluck",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   data,
	})
}
