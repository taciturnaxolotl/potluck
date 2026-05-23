package web

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	now := time.Now()

	since30 := now.AddDate(0, 0, -29).Unix()
	daily, err := s.Q.SpendByDay(r.Context(), store.SpendByDayParams{
		AttributedUserID: sql.NullString{String: u.ID, Valid: true},
		PioneerCreatedAt: since30,
	})
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}

	since7 := now.AddDate(0, 0, -6).Unix()
	byModel, err := s.Q.SpendByDayAndModel(r.Context(), store.SpendByDayAndModelParams{
		AttributedUserID: sql.NullString{String: u.ID, Valid: true},
		PioneerCreatedAt: since7,
	})
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}

	// Flatten sql.NullFloat64 aggregates to plain numbers for JSON.
	type dailyRow struct {
		Day          int64   `json:"day"`
		AmountMicros int64   `json:"amount_micros"`
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
	}
	type modelRow struct {
		Day          int64   `json:"day"`
		Model        string  `json:"model"`
		AmountMicros int64   `json:"amount_micros"`
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
	}

	flatDaily := make([]dailyRow, len(daily))
	for i, d := range daily {
		flatDaily[i] = dailyRow{
			Day:          d.Day,
			AmountMicros: int64(d.AmountMicros.Float64),
			InputTokens:  int64(d.InputTokens.Float64),
			OutputTokens: int64(d.OutputTokens.Float64),
		}
	}

	flatByModel := make([]modelRow, len(byModel))
	for i, m := range byModel {
		flatByModel[i] = modelRow{
			Day:          m.Day,
			Model:        m.Model,
			AmountMicros: int64(m.AmountMicros.Float64),
			InputTokens:  int64(m.InputTokens.Float64),
			OutputTokens: int64(m.OutputTokens.Float64),
		}
	}

	writeJSON(w, 200, map[string]any{
		"daily":    flatDaily,
		"by_model": flatByModel,
	})
}
