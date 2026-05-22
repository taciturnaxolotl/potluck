package web

import (
	"net/http"
)

// handleAllocations returns per-user pool allocation derived from pool_keys.
func (s *Server) handleAllocations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Q.ListPoolAllocations(r.Context())
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}

	var totalDailyLimit int64
	for _, row := range rows {
		totalDailyLimit += toInt64(row.DailyLimitMicros)
	}

	type entry struct {
		UserID           string  `json:"user_id"`
		DisplayName      string  `json:"display_name"`
		Email            string  `json:"email"`
		KeyCount         int64   `json:"key_count"`
		DailyLimitMicros int64   `json:"daily_limit_micros"`
		TodayMicros      int64   `json:"today_micros"`
		TotalMicros      int64   `json:"total_micros"`
		RequestCount     int64   `json:"request_count"`
		ShareFraction    float64 `json:"share_fraction"`
		RemainingToday   int64   `json:"remaining_today"`
	}

	out := make([]entry, 0, len(rows))
	for _, row := range rows {
		daily := toInt64(row.DailyLimitMicros)
		today := toInt64(row.TodayMicros)
		var frac float64
		if totalDailyLimit > 0 {
			frac = float64(daily) / float64(totalDailyLimit)
		}
		out = append(out, entry{
			UserID:           row.UserID,
			DisplayName:      row.DisplayName,
			Email:            row.Email,
			KeyCount:         toInt64(row.KeyCount),
			DailyLimitMicros: daily,
			TodayMicros:      today,
			TotalMicros:      toInt64(row.TotalMicros),
			RequestCount:     toInt64(row.RequestCount),
			ShareFraction:    frac,
			RemainingToday:   daily - today,
		})
	}

	writeJSON(w, 200, map[string]any{
		"total_daily_limit_micros": totalDailyLimit,
		"users":                    out,
	})
}
