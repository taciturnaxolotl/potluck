package web

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

// handleAllocations returns the pool capacity + per-user allowance/spend view.
func (s *Server) handleAllocations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.buildAllocations(r))
}

// handleRecomputeAllocations recalculates per-user shared allowances.
// Anyone can trigger. Allowances only ever grow (no claw-back).
func (s *Server) handleRecomputeAllocations(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	day := time.Now().UTC().Unix() / 86400
	now := time.Now().Unix()

	rows, err := s.Q.ListPoolAllocations(r.Context())
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}

	spendRows, err := s.Q.ListUserDailySpendForDay(r.Context(), day)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	spentByUser := map[string]int64{}
	for _, sp := range spendRows {
		spentByUser[sp.UserID] = sp.SharedSpentMicros
	}

	var totalShared, totalSpent int64
	for _, row := range rows {
		totalShared += toInt64(row.DailyLimitMicros)
	}
	for _, sp := range spendRows {
		totalSpent += sp.SharedSpentMicros
	}
	remaining := totalShared - totalSpent
	if remaining < 0 {
		remaining = 0
	}

	for _, row := range rows {
		shared := toInt64(row.DailyLimitMicros)
		var fairShare int64
		if totalShared > 0 {
			fairShare = int64(float64(remaining) * float64(shared) / float64(totalShared))
		}
		spent := spentByUser[row.UserID]
		allowance := spent + fairShare

		_ = s.Q.UpsertUserDailyAllowance(r.Context(), store.UpsertUserDailyAllowanceParams{
			UserID:                row.UserID,
			Day:                   day,
			SharedAllowanceMicros: allowance,
			SetAt:                 now,
			SetByUserID:           u.ID,
		})
	}

	writeJSON(w, 200, s.buildAllocations(r))
}

// buildAllocations computes the full allocations payload.
func (s *Server) buildAllocations(r *http.Request) map[string]any {
	day := time.Now().UTC().Unix() / 86400

	rows, _ := s.Q.ListPoolAllocations(r.Context())
	spendRows, _ := s.Q.ListUserDailySpendForDay(r.Context(), day)
	allowRows, _ := s.Q.ListUserDailyAllowancesForDay(r.Context(), day)

	spendByUser := map[string]struct{ shared, private int64 }{}
	for _, sp := range spendRows {
		spendByUser[sp.UserID] = struct{ shared, private int64 }{sp.SharedSpentMicros, sp.PrivateSpentMicros}
	}
	allowByUser := map[string]int64{}
	for _, a := range allowRows {
		allowByUser[a.UserID] = a.SharedAllowanceMicros
	}

	var totalShared, spentTodayShared int64
	activeKeyCount := 0
	for _, row := range rows {
		totalShared += toInt64(row.DailyLimitMicros)
		spentTodayShared += toInt64(row.TodayMicros)
		activeKeyCount += int(toInt64(row.KeyCount))
	}
	remaining := totalShared - spentTodayShared
	if remaining < 0 {
		remaining = 0
	}

	type userEntry struct {
		UserID                     string  `json:"user_id"`
		DisplayName                string  `json:"display_name"`
		Email                      string  `json:"email"`
		KeyCount                   int64   `json:"key_count"`
		SharedContributionMicros   int64   `json:"shared_contribution_micros"`
		PrivateReservationMicros   int64   `json:"private_reservation_micros"`
		SharedAllowanceTodayMicros int64   `json:"shared_allowance_today_micros"`
		SharedSpentTodayMicros     int64   `json:"shared_spent_today_micros"`
		PrivateSpentTodayMicros    int64   `json:"private_spent_today_micros"`
		SharedRemainingTodayMicros int64   `json:"shared_remaining_today_micros"`
		ShareFraction              float64 `json:"share_fraction"`
	}

	out := make([]userEntry, 0, len(rows))
	for _, row := range rows {
		shared := toInt64(row.DailyLimitMicros)
		var frac float64
		if totalShared > 0 {
			frac = float64(shared) / float64(totalShared)
		}
		allowance := allowByUser[row.UserID]
		if allowance == 0 && totalShared > 0 {
			allowance = int64(float64(remaining) * frac)
			if allowance < 0 {
				allowance = 0
			}
		}
		spend := spendByUser[row.UserID]
		out = append(out, userEntry{
			UserID:                     row.UserID,
			DisplayName:                row.DisplayName,
			Email:                      row.Email,
			KeyCount:                   toInt64(row.KeyCount),
			SharedContributionMicros:   shared,
			PrivateReservationMicros:   0,
			SharedAllowanceTodayMicros: allowance,
			SharedSpentTodayMicros:     spend.shared,
			PrivateSpentTodayMicros:    spend.private,
			SharedRemainingTodayMicros: allowance - spend.shared,
			ShareFraction:              frac,
		})
	}

	// Last recompute metadata.
	var lastRecompute any
	if lr, err := s.Q.GetLatestRecompute(r.Context(), day); err == nil && !errors.Is(err, sql.ErrNoRows) {
		if lr.SetAt > 0 {
			lastRecompute = map[string]any{
				"at":              lr.SetAt,
				"by_user_id":      lr.SetByUserID,
				"by_display_name": lr.SetByUserID, // TODO: join users table
			}
		}
	}

	return map[string]any{
		"pool": map[string]any{
			"total_shared_micros":         totalShared,
			"spent_today_shared_micros":   spentTodayShared,
			"remaining_pool_today_micros": remaining,
			"active_key_count":            activeKeyCount,
			"active_team_count":           activeKeyCount, // TODO: dedupe by team_id
		},
		"users":          out,
		"last_recompute": lastRecompute,
	}
}

func sqlNullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
