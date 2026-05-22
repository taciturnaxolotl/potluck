package v1

import (
	"encoding/json"
	"net/http"
	"time"
)

// handleListModels returns the list of models we serve. The web UI uses
// this for the model picker; tools like Continue use it for autodiscovery.
//
// For now this is a static list seeded at server build time. When we wire
// the model_prices table this should join against it for capabilities and
// pricing.
func (s *Server) handleListModels(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().Unix()
	models := []map[string]any{
		{"id": "gpt-4o-mini", "object": "model", "created": now, "owned_by": "potluck"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   models,
	})
}
