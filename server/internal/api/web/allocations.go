package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

// handleAllocations returns the pool capacity + per-user allowance/spend view.
func (s *Server) handleAllocations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.buildAllocations(r))
}

// handleRecomputeAllocations recalculates per-user shared allowances using
// smartAllocate (history-aware fair share). Anyone can trigger.
func (s *Server) handleRecomputeAllocations(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	if err := s.RunSmartAllocation(r.Context(), u.ID); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	writeJSON(w, 200, s.buildAllocations(r))
}

// historyWindowDays is how far back smartAllocate looks for prediction data.
const historyWindowDays = 30

// RunSmartAllocation executes the smart-allocation algorithm and writes a
// fresh row per user into user_daily_allowances. Exported so the background
// recompute goroutine and the manual handler can share one code path.
//
// setByUserID is the user id to record as the trigger; pass "system" for the
// periodic recompute.
func (s *Server) RunSmartAllocation(ctx context.Context, setByUserID string) error {
	now := time.Now()
	day := now.UTC().Unix() / 86400
	dayStart := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	dayFraction := now.Sub(dayStart).Hours() / 24.0
	windowStart := now.AddDate(0, 0, -historyWindowDays).Unix()
	windowEnd := dayStart.Unix() // exclude today; we only want completed history

	rows, err := s.Q.ListPoolAllocations(ctx)
	if err != nil {
		return fmt.Errorf("list pool allocations: %w", err)
	}

	spendRows, err := s.Q.ListAllUsersLiveSpendToday(ctx, dayStart.Unix())
	if err != nil {
		return fmt.Errorf("list live spend: %w", err)
	}
	spentByUser := map[string]int64{}
	for _, sp := range spendRows {
		if sp.UserID.Valid {
			spentByUser[sp.UserID.String] = toInt64(sp.SharedSpentMicros)
		}
	}

	// Historical profile per user.
	histRows, err := s.Q.UserHistoryProfile(ctx, store.UserHistoryProfileParams{
		PioneerCreatedAt:   windowStart,
		PioneerCreatedAt_2: windowEnd,
	})
	if err != nil {
		return fmt.Errorf("user history profile: %w", err)
	}
	historyByUser := map[string]UserHistory{}
	for _, h := range histRows {
		uid := ""
		if h.UserID.Valid {
			uid = h.UserID.String
		}
		if uid == "" {
			continue
		}
		days := toInt64(h.DaysWithSpend)
		total := toInt64(h.TotalSpendMicros)
		var avg int64
		if days > 0 {
			avg = total / days
		}
		historyByUser[uid] = UserHistory{
			AvgActiveDaySpend: avg,
			ActivityRate:      float64(days) / float64(historyWindowDays),
			DaysWithSpend:     days,
		}
	}

	var totalShared, spentTodayShared int64
	for _, row := range rows {
		totalShared += toInt64(row.DailyLimitMicros)
		spentTodayShared += toInt64(row.TodayMicros)
	}
	remainingPool := totalShared - spentTodayShared
	if remainingPool < 0 {
		remainingPool = 0
	}

	userIDs := make([]string, len(rows))
	spends := make([]int64, len(rows))
	histories := make([]UserHistory, len(rows))
	for i, row := range rows {
		userIDs[i] = row.UserID
		spends[i] = spentByUser[row.UserID]
		histories[i] = historyByUser[row.UserID] // zero value if absent
	}

	allocations := smartAllocate(remainingPool, spends, histories, dayFraction)

	// "system" is not a real user ID — resolve to the first user in the pool
	// so the FK constraint on set_by_user_id is satisfied.
	resolvedBy := setByUserID
	if resolvedBy == "system" && len(userIDs) > 0 {
		resolvedBy = userIDs[0]
	}

	nowUnix := now.Unix()
	for i, id := range userIDs {
		a := allocations[i]
		if err := s.Q.UpsertUserDailyAllowance(ctx, store.UpsertUserDailyAllowanceParams{
			UserID:                id,
			Day:                   day,
			SharedAllowanceMicros: a.Allowance,
			FloorMicros:           a.Floor,
			BonusMicros:           a.Bonus,
			PredictedTotalMicros:  a.PredictedTotal,
			HistoryDaysUsed:       a.HistoryDays,
			SetAt:                 nowUnix,
			SetByUserID:           resolvedBy,
		}); err != nil {
			return fmt.Errorf("upsert allowance for %s: %w", id, err)
		}
	}
	return nil
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
	nowUtc := time.Now().UTC()
	day := nowUtc.Unix() / 86400
	dayStartUnix := time.Date(nowUtc.Year(), nowUtc.Month(), nowUtc.Day(), 0, 0, 0, 0, time.UTC).Unix()

	rows, _ := s.Q.ListPoolAllocations(r.Context())
	liveSpendRows, _ := s.Q.ListAllUsersLiveSpendToday(r.Context(), dayStartUnix)
	allowRows, _ := s.Q.ListUserDailyAllowancesForDay(r.Context(), day)

	spendByUser := map[string]struct{ shared, private int64 }{}
	for _, sp := range liveSpendRows {
		if sp.UserID.Valid {
			spendByUser[sp.UserID.String] = struct{ shared, private int64 }{
				toInt64(sp.SharedSpentMicros),
				toInt64(sp.PrivateSpentMicros),
			}
		}
	}
	allowByUser := map[string]store.UserDailyAllowance{}
	for _, a := range allowRows {
		allowByUser[a.UserID] = a
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
		UserID                       string  `json:"user_id"`
		DisplayName                  string  `json:"display_name"`
		Email                        string  `json:"email"`
		KeyCount                     int64   `json:"key_count"`
		SharedContributionMicros     int64   `json:"shared_contribution_micros"`
		PrivateReservationMicros     int64   `json:"private_reservation_micros"`
		SharedAllowanceTodayMicros   int64   `json:"shared_allowance_today_micros"`
		SharedAllowanceFloorMicros   int64   `json:"shared_allowance_floor_micros"`
		SharedAllowanceBonusMicros   int64   `json:"shared_allowance_bonus_micros"`
		PredictedTotalTodayMicros    int64   `json:"predicted_total_today_micros"`
		HistoryDaysUsed              int64   `json:"history_days_used"`
		IsDonating                   bool    `json:"is_donating"`
		SharedSpentTodayMicros       int64   `json:"shared_spent_today_micros"`
		PrivateSpentTodayMicros      int64   `json:"private_spent_today_micros"`
		SharedRemainingTodayMicros   int64   `json:"shared_remaining_today_micros"`
		ShareFraction                float64 `json:"share_fraction"`
	}

	// Live smart-allocation estimate for users with no stored allowance yet.
	// Same algorithm as the recompute, so the estimate matches what a
	// recompute would write.
	now := time.Now()
	dayStart := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	dayFraction := now.Sub(dayStart).Hours() / 24.0
	windowStart := now.AddDate(0, 0, -historyWindowDays).Unix()
	windowEnd := dayStart.Unix()

	histRows, _ := s.Q.UserHistoryProfile(r.Context(), store.UserHistoryProfileParams{
		PioneerCreatedAt:   windowStart,
		PioneerCreatedAt_2: windowEnd,
	})
	historyByUser := map[string]UserHistory{}
	for _, h := range histRows {
		uid := ""
		if h.UserID.Valid {
			uid = h.UserID.String
		}
		if uid == "" {
			continue
		}
		days := toInt64(h.DaysWithSpend)
		total := toInt64(h.TotalSpendMicros)
		var avg int64
		if days > 0 {
			avg = total / days
		}
		historyByUser[uid] = UserHistory{
			AvgActiveDaySpend: avg,
			ActivityRate:      float64(days) / float64(historyWindowDays),
			DaysWithSpend:     days,
		}
	}

	liveSpends := make([]int64, len(rows))
	liveHistories := make([]UserHistory, len(rows))
	for i, row := range rows {
		liveSpends[i] = spendByUser[row.UserID].shared
		liveHistories[i] = historyByUser[row.UserID]
	}
	liveAllocations := smartAllocate(remaining, liveSpends, liveHistories, dayFraction)

	out := make([]userEntry, 0, len(rows))
	for i, row := range rows {
		shared := toInt64(row.DailyLimitMicros)
		// share_fraction reflects contribution to the pool, not the divy split.
		var frac float64
		if totalShared > 0 {
			frac = float64(shared) / float64(totalShared)
		}

		stored, hasStored := allowByUser[row.UserID]
		live := liveAllocations[i]

		var (
			allowance      int64
			floor          int64
			bonus          int64
			predicted      int64
			historyDays    int64
		)
		if hasStored {
			allowance = stored.SharedAllowanceMicros
			floor = stored.FloorMicros
			bonus = stored.BonusMicros
			predicted = stored.PredictedTotalMicros
			historyDays = stored.HistoryDaysUsed
		} else {
			allowance = live.Allowance
			floor = live.Floor
			bonus = live.Bonus
			predicted = live.PredictedTotal
			historyDays = live.HistoryDays
		}

		spend := spendByUser[row.UserID]
		out = append(out, userEntry{
			UserID:                       row.UserID,
			DisplayName:                  row.DisplayName,
			Email:                        row.Email,
			KeyCount:                     toInt64(row.KeyCount),
			SharedContributionMicros:     shared,
			PrivateReservationMicros:     toInt64(row.PrivateReservationMicros),
			SharedAllowanceTodayMicros:   allowance,
			SharedAllowanceFloorMicros:   floor,
			SharedAllowanceBonusMicros:   bonus,
			PredictedTotalTodayMicros:    predicted,
			HistoryDaysUsed:              historyDays,
			IsDonating:                   live.IsDonating,
			SharedSpentTodayMicros:       spend.shared,
			PrivateSpentTodayMicros:      spend.private,
			SharedRemainingTodayMicros:   allowance - spend.shared,
			ShareFraction:                frac,
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

	// Total redistribution surplus = sum of all bonuses (post-allocation).
	var redistributionSurplus int64
	for _, u := range out {
		redistributionSurplus += u.SharedAllowanceBonusMicros
	}

	return map[string]any{
		"pool": map[string]any{
			"total_shared_micros":            totalShared,
			"spent_today_shared_micros":      spentTodayShared,
			"remaining_pool_today_micros":    remaining,
			"active_key_count":               activeKeyCount,
			"active_team_count":              activeKeyCount, // TODO: dedupe by team_id
			"redistribution_surplus_micros":  redistributionSurplus,
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
