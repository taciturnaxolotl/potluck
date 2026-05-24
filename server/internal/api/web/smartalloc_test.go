package web

import (
	"testing"
)

// Test the bootstrap: no historical data → degrades cleanly to fair-share.
func TestSmartAllocate_BootstrapEqualSplit(t *testing.T) {
	pool := int64(1000)
	spends := []int64{0, 0, 0, 0}
	histories := []UserHistory{{}, {}, {}, {}} // all zero — no history
	got := smartAllocate(pool, spends, histories, 0.5)

	for i, a := range got {
		if a.Allowance != 250 {
			t.Errorf("user %d: allowance=%d, want 250 (no-history equal split)", i, a.Allowance)
		}
		if a.Floor != 250 {
			t.Errorf("user %d: floor=%d, want 250", i, a.Floor)
		}
		if a.Bonus != 0 {
			t.Errorf("user %d: bonus=%d, want 0", i, a.Bonus)
		}
	}
}

// One heavy historical user, two light historical donors, one bootstrap user.
// Heavy user gets a bonus from the light users' predicted slack.
// Bootstrap user neither donates nor receives.
func TestSmartAllocate_HeavyUserGetsBonus(t *testing.T) {
	pool := int64(1000)
	spends := []int64{100, 0, 0, 0}
	histories := []UserHistory{
		// user 0: heavy — 28 of last 30 days active, $1000/day avg
		{AvgActiveDaySpend: 1000, ActivityRate: 28.0 / 30.0, DaysWithSpend: 28},
		// user 1: light — $20/day, 6 of 30 days active
		{AvgActiveDaySpend: 20, ActivityRate: 6.0 / 30.0, DaysWithSpend: 6},
		// user 2: light — $50/day, 10 of 30 days active
		{AvgActiveDaySpend: 50, ActivityRate: 10.0 / 30.0, DaysWithSpend: 10},
		// user 3: bootstrap — no history
		{},
	}
	got := smartAllocate(pool, spends, histories, 0.0) // start of day, full confidence

	if got[0].Floor != 250 {
		t.Errorf("user 0 floor=%d, want 250", got[0].Floor)
	}
	if got[0].PredictedTotal < 800 {
		t.Errorf("user 0 predicted_total=%d, want >= 800", got[0].PredictedTotal)
	}
	if got[0].Bonus <= 0 {
		t.Errorf("user 0 bonus=%d, want > 0 (heavy user gets surplus)", got[0].Bonus)
	}
	// Light users donate (their predicted total is well below fair share).
	if !got[1].IsDonating {
		t.Errorf("user 1 should be donating (light user)")
	}
	if !got[2].IsDonating {
		t.Errorf("user 2 should be donating (light user)")
	}
	// Bootstrap user gets exactly fairShare floor, no bonus, no donation.
	if got[3].Bonus != 0 {
		t.Errorf("bootstrap user bonus=%d, want 0", got[3].Bonus)
	}
	if got[3].Allowance != 250 {
		t.Errorf("bootstrap user allowance=%d, want 250", got[3].Allowance)
	}
}

// Light user gets a spike — their floor is still equal share, never lower.
func TestSmartAllocate_LightUserSpike(t *testing.T) {
	pool := int64(1000)
	spends := []int64{300, 0, 0, 0}
	histories := []UserHistory{
		// user 0: typically spends $20/day, 5 of 30 days active
		{AvgActiveDaySpend: 20, ActivityRate: 5.0 / 30.0, DaysWithSpend: 5},
		// user 1-3: no history
		{}, {}, {},
	}
	got := smartAllocate(pool, spends, histories, 0.5)

	// User 0 has already spent $300, way over their $3 prediction.
	// Floor is max(fairShare=250, spent=300) = 300. Allowance >= 300.
	if got[0].Floor != 300 {
		t.Errorf("user 0 floor=%d, want 300 (max of fairShare and spent)", got[0].Floor)
	}
	if got[0].Allowance < 300 {
		t.Errorf("user 0 allowance=%d < spent=300 — never claw back!", got[0].Allowance)
	}
}

// Invariant: allowance >= fairShare for all users.
func TestSmartAllocate_AllowanceNeverBelowFairShare(t *testing.T) {
	pool := int64(1000)
	cases := []struct {
		name      string
		spends    []int64
		histories []UserHistory
	}{
		{
			name:   "no history",
			spends: []int64{0, 50, 100, 200},
			histories: []UserHistory{
				{}, {}, {}, {},
			},
		},
		{
			name:   "mixed history",
			spends: []int64{500, 0, 0, 0},
			histories: []UserHistory{
				{AvgActiveDaySpend: 800, ActivityRate: 0.9, DaysWithSpend: 27},
				{AvgActiveDaySpend: 50, ActivityRate: 0.2, DaysWithSpend: 6},
				{}, {},
			},
		},
	}
	fairShare := pool / 4

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := smartAllocate(pool, c.spends, c.histories, 0.3)
			for i, a := range got {
				if a.Allowance < fairShare && a.Allowance < c.spends[i] {
					t.Errorf("user %d allowance=%d, want >= fairShare=%d (spends=%d)",
						i, a.Allowance, fairShare, c.spends[i])
				}
			}
		})
	}
}

// Invariant: allowance >= spent for all users (no claw-back).
func TestSmartAllocate_NeverBelowSpend(t *testing.T) {
	pool := int64(1000)
	spends := []int64{600, 400, 100, 0}
	histories := make([]UserHistory, 4)
	got := smartAllocate(pool, spends, histories, 0.7)
	for i, a := range got {
		if a.Allowance < spends[i] {
			t.Errorf("user %d allowance=%d < spent=%d", i, a.Allowance, spends[i])
		}
	}
}

// As the day progresses, light users donate more (confidence decays).
func TestSmartAllocate_DayFractionDecay(t *testing.T) {
	pool := int64(1000)
	spends := []int64{0, 0, 0, 0}
	// user 0 heavy, user 1 light, others no history
	histories := []UserHistory{
		{AvgActiveDaySpend: 800, ActivityRate: 0.9, DaysWithSpend: 27},
		{AvgActiveDaySpend: 50, ActivityRate: 0.2, DaysWithSpend: 6},
		{}, {},
	}

	earlyBonus := smartAllocate(pool, spends, histories, 0.0)[0].Bonus
	lateBonus := smartAllocate(pool, spends, histories, 0.9)[0].Bonus

	// Late in the day, more of the light user's predicted slack is already
	// "earned" — they're not going to use the rest. Bonus to heavy user
	// should be at least as much as early in the day (probably more).
	if lateBonus < earlyBonus {
		t.Errorf("bonus shrank as day progressed: early=%d late=%d", earlyBonus, lateBonus)
	}
}

// New user with zero history shows up: they get the equal share, full stop.
func TestSmartAllocate_NewUserNeutral(t *testing.T) {
	pool := int64(1000)
	spends := []int64{0, 0, 0, 0}
	histories := []UserHistory{
		{AvgActiveDaySpend: 500, ActivityRate: 0.8, DaysWithSpend: 24},
		{}, {}, {}, // three new users
	}
	got := smartAllocate(pool, spends, histories, 0.3)

	// New users: no bonus, exactly fairShare floor.
	for i := 1; i < 4; i++ {
		if got[i].Bonus != 0 {
			t.Errorf("new user %d got bonus=%d, want 0", i, got[i].Bonus)
		}
		if got[i].Allowance != 250 {
			t.Errorf("new user %d allowance=%d, want 250", i, got[i].Allowance)
		}
	}
}

// Empty user list — return nil, no panics.
func TestSmartAllocate_Empty(t *testing.T) {
	got := smartAllocate(1000, nil, nil, 0.5)
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// Mismatched histories length: should fail safe to no-history.
func TestSmartAllocate_HistoriesMismatch(t *testing.T) {
	pool := int64(1000)
	spends := []int64{0, 0, 0, 0}
	histories := []UserHistory{{}, {}} // too short
	got := smartAllocate(pool, spends, histories, 0.5)
	if len(got) != 4 {
		t.Fatalf("got len=%d, want 4", len(got))
	}
	for i, a := range got {
		if a.Allowance != 250 {
			t.Errorf("user %d allowance=%d, want 250 (mismatch should fail safe)", i, a.Allowance)
		}
	}
}

// Predict edge cases.
func TestUserHistory_Predict(t *testing.T) {
	fairShare := int64(250)

	// No data
	if got := (UserHistory{}).Predict(fairShare); got != fairShare {
		t.Errorf("empty history Predict=%d, want %d", got, fairShare)
	}

	// Insufficient days
	h := UserHistory{AvgActiveDaySpend: 500, ActivityRate: 0.1, DaysWithSpend: 2}
	if got := h.Predict(fairShare); got != fairShare {
		t.Errorf("under-threshold Predict=%d, want %d", got, fairShare)
	}

	// Sufficient data
	h = UserHistory{AvgActiveDaySpend: 1000, ActivityRate: 0.5, DaysWithSpend: 15}
	if got := h.Predict(fairShare); got != 500 {
		t.Errorf("Predict=%d, want 500", got)
	}
}
