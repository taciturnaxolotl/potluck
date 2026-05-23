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

	var totalShared int64
	for _, row := range rows {
		totalShared += toInt64(row.DailyLimitMicros)
	}

	// Build the user list with their spend, then run max-min fair share.
	userIDs := make([]string, len(rows))
	spends := make([]int64, len(rows))
	for i, row := range rows {
		userIDs[i] = row.UserID
		spends[i] = spentByUser[row.UserID]
	}
	allowances := fairShareAllocate(totalShared, spends)

	for i, id := range userIDs {
		_ = s.Q.UpsertUserDailyAllowance(r.Context(), store.UpsertUserDailyAllowanceParams{
			UserID:                id,
			Day:                   day,
			SharedAllowanceMicros: allowances[i],
			SetAt:                 now,
			SetByUserID:           u.ID,
		})
	}

	writeJSON(w, 200, s.buildAllocations(r))
}

// fairShareAllocate distributes `pool` across users using max-min fair share
// (water-filling). Each user's allowance is at least their existing spend
// (we never claw back what was already used); remaining capacity is split
// equally among users whose spend is below the running fair-share level.
//
// Algorithm:
//  1. Anyone whose spend exceeds the equal split gets locked at their spend.
//  2. The remaining pool is split equally among the rest.
//  3. Repeat until no new lock-ins happen (converges in <= n passes).
//
// If total spend already exceeds the pool, every user gets exactly their
// spend (i.e. the pool is over-allocated and the table will show $0 left).
func fairShareAllocate(pool int64, spends []int64) []int64 {
	n := len(spends)
	if n == 0 {
		return nil
	}

	allowances := make([]int64, n)
	locked := make([]bool, n)

	for {
		// Sum of locked allowances and count of unlocked users.
		var lockedTotal int64
		unlocked := 0
		for i := range spends {
			if locked[i] {
				lockedTotal += allowances[i]
			} else {
				unlocked++
			}
		}
		if unlocked == 0 {
			break
		}
		remaining := pool - lockedTotal
		if remaining < 0 {
			remaining = 0
		}
		share := remaining / int64(unlocked)

		// Lock anyone whose spend exceeds the current share. Their
		// allowance gets pinned to their spend.
		newLocks := 0
		for i, sp := range spends {
			if locked[i] {
				continue
			}
			if sp > share {
				allowances[i] = sp
				locked[i] = true
				newLocks++
			}
		}
		if newLocks == 0 {
			// Stable: everyone unlocked gets the current share.
			for i := range spends {
				if !locked[i] {
					allowances[i] = share
				}
			}
			break
		}
	}

	return allowances
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

	// Live fair-share estimate for users with no stored allowance yet.
	// Same algorithm as the recompute, so the estimate matches what a
	// recompute would produce.
	liveSpends := make([]int64, len(rows))
	for i, row := range rows {
		liveSpends[i] = spendByUser[row.UserID].shared
	}
	liveAllowances := fairShareAllocate(totalShared, liveSpends)

	out := make([]userEntry, 0, len(rows))
	for i, row := range rows {
		shared := toInt64(row.DailyLimitMicros)
		// share_fraction reflects contribution to the pool, not the divy split.
		var frac float64
		if totalShared > 0 {
			frac = float64(shared) / float64(totalShared)
		}
		allowance, hasStored := allowByUser[row.UserID], false
		if _, ok := allowByUser[row.UserID]; ok {
			hasStored = true
		}
		if !hasStored {
			allowance = liveAllowances[i]
		}
		spend := spendByUser[row.UserID]
		out = append(out, userEntry{
			UserID:                     row.UserID,
			DisplayName:                row.DisplayName,
			Email:                      row.Email,
			KeyCount:                   toInt64(row.KeyCount),
			SharedContributionMicros:   shared,
			PrivateReservationMicros:   toInt64(row.PrivateReservationMicros),
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
