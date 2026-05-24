package web

// Smart pool allocation.
//
// Each user's allowance is broken into two parts:
//
//   floor = max(fairShare, alreadySpentToday)
//   bonus = redistributed-surplus * (this_user_demand / total_heavy_demand)
//
// fairShare is totalShared/nUsers — equal split, the floor everyone gets.
//
// Surplus comes from users whose historical pattern says they'll leave
// part of their fair share unused. That slack flows to users whose
// historical pattern says they need more than their fair share.
//
// Bootstrap: a user with <minHistoryDays days of recorded spend gets a
// neutral prediction (predictedTotal = fairShare), which makes them
// neither a donor nor a recipient. When NO user has history, the system
// degrades cleanly to plain equal-split fair share.
//
// Key invariants (enforced by tests):
//   - allowance[i] >= fairShare for all i
//   - allowance[i] >= spent[i]  for all i
//   - sum(allowance) <= totalShared + max(0, sum(spent) - totalShared)
//     i.e. the only way the pool is oversubscribed is if users already
//     overspent it.

// minHistoryDays is the threshold below which a user's predicted_total
// falls back to the fair share. 3 days is enough to filter out one-off
// spikes; below that, treat them as a default user.
const minHistoryDays = 3

// UserHistory is the per-user historical profile feeding the predictor.
//   AvgActiveDaySpend = total spend over the window / days with any spend
//   ActivityRate      = days with any spend / window length in days
//   DaysWithSpend     = raw count (so we know if we have enough signal)
type UserHistory struct {
	AvgActiveDaySpend int64
	ActivityRate      float64
	DaysWithSpend     int64
}

// Predict returns the expected total spend today for this user.
// Returns fairShare for users with insufficient history (bootstrap).
func (h UserHistory) Predict(fairShare int64) int64 {
	if h.DaysWithSpend < minHistoryDays {
		return fairShare
	}
	return int64(float64(h.AvgActiveDaySpend) * h.ActivityRate)
}

// SmartAllocation is the per-user output of smartAllocate.
type SmartAllocation struct {
	Floor          int64 // max(fairShare, spent)
	Bonus          int64 // redistributed surplus, >= 0
	Allowance      int64 // floor + bonus
	PredictedTotal int64 // for display in the tooltip
	HistoryDays    int64 // 0 = no history, used by UI
	IsDonating     bool  // currently donating slack to the surplus
}

// smartAllocate computes per-user allowances using historical demand
// prediction to redistribute slack from light users to heavy users.
//
// Inputs:
//
//	pool        = totalShared (sum of all active pool keys' shared_micros)
//	spends      = today's shared spend per user, parallel to histories
//	histories   = per-user historical profile (parallel to spends)
//	dayFraction = fraction of UTC day elapsed (0.0 to 1.0)
//
// dayFraction decays predictions toward zero as the day progresses —
// at hour 23, a user predicted for $1000/day with $100 spent has
// expected_remaining ≈ $900 * (1/24) ≈ $37, so most of their slack
// flows to the surplus.
func smartAllocate(pool int64, spends []int64, histories []UserHistory, dayFraction float64) []SmartAllocation {
	n := len(spends)
	if n == 0 {
		return nil
	}
	if len(histories) != n {
		// Caller bug; fail safe by treating everyone as no-history.
		histories = make([]UserHistory, n)
	}

	out := make([]SmartAllocation, n)
	if pool <= 0 {
		// No pool — everyone gets exactly their spend back as floor.
		for i, sp := range spends {
			out[i] = SmartAllocation{
				Floor:       sp,
				Allowance:   sp,
				HistoryDays: histories[i].DaysWithSpend,
			}
		}
		return out
	}

	fairShare := pool / int64(n)

	// ---- pass 1: floors and predictions ---------------------------------
	for i, sp := range spends {
		floor := fairShare
		if sp > floor {
			floor = sp
		}
		out[i] = SmartAllocation{
			Floor:          floor,
			PredictedTotal: histories[i].Predict(fairShare),
			HistoryDays:    histories[i].DaysWithSpend,
		}
	}

	// ---- pass 2: surplus from light users -------------------------------
	// "expected remaining" = predicted slack a user will leave unused.
	// We only donate slack from above the spent line — never claw back.
	// dayFraction clamps prediction confidence: late in the day, even a
	// historically heavy user has limited remaining demand.
	confidence := 1.0 - dayFraction
	if confidence < 0 {
		confidence = 0
	}

	var surplus int64
	for i, sp := range spends {
		a := &out[i]
		// What they're predicted to still spend today, conservatively.
		pred := a.PredictedTotal
		if pred < sp {
			pred = sp // they've already exceeded prediction; trust the live number
		}
		expectedRemaining := int64(float64(pred-sp) * confidence)
		// Their floor minus what they've spent is the room above spent.
		floorAboveSpent := a.Floor - sp
		if floorAboveSpent < 0 {
			floorAboveSpent = 0
		}
		// If their predicted leftover is below their fair-share slack,
		// donate the gap.
		if expectedRemaining < floorAboveSpent {
			donation := floorAboveSpent - expectedRemaining
			surplus += donation
			a.IsDonating = true
		}
	}

	// ---- pass 3: distribute surplus to heavy users ----------------------
	// "Demand" = how much a user is predicted to want above their fair share.
	// Users with no prediction-above-fairShare get zero bonus.
	var totalDemand int64
	demands := make([]int64, n)
	for i := range spends {
		d := out[i].PredictedTotal - fairShare
		if d < 0 {
			d = 0
		}
		demands[i] = d
		totalDemand += d
	}
	if totalDemand > 0 && surplus > 0 {
		for i := range spends {
			if demands[i] == 0 {
				continue
			}
			bonus := surplus * demands[i] / totalDemand
			out[i].Bonus = bonus
		}
	}

	// ---- pass 4: finalize -----------------------------------------------
	for i := range out {
		out[i].Allowance = out[i].Floor + out[i].Bonus
	}

	return out
}
