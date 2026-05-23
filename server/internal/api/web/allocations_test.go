package web

import (
	"slices"
	"testing"
)

func TestFairShareAllocate(t *testing.T) {
	tests := []struct {
		name   string
		pool   int64
		spends []int64
		want   []int64
	}{
		{
			name:   "empty",
			pool:   1000,
			spends: nil,
			want:   nil,
		},
		{
			name:   "nobody spent anything — equal split",
			pool:   1000,
			spends: []int64{0, 0, 0, 0},
			want:   []int64{250, 250, 250, 250},
		},
		{
			name:   "everyone spent under fair share — still equal",
			pool:   1000,
			spends: []int64{100, 50, 200, 0},
			want:   []int64{250, 250, 250, 250},
		},
		{
			name:   "one user already over fair share — locked, rest split remainder",
			pool:   1000,
			spends: []int64{400, 0, 0, 0},
			want:   []int64{400, 200, 200, 200},
		},
		{
			name:   "two users over fair share — both locked",
			pool:   1000,
			spends: []int64{400, 350, 0, 0},
			want:   []int64{400, 350, 125, 125},
		},
		{
			// pass 1: share=250, user0=500 locks at 500
			// pass 2: remaining=500/3=166, user1=240>166 locks at 240
			// pass 3: remaining=260/2=130 for both unlocked
			name:   "second-order cascade — locking one drops share below another's spend",
			pool:   1000,
			spends: []int64{500, 240, 0, 0},
			want:   []int64{500, 240, 130, 130},
		},
		{
			name:   "actual cascade — lock 500, then 260 > new share",
			pool:   1000,
			spends: []int64{500, 260, 0, 0},
			want:   []int64{500, 260, 120, 120}, // 1000-500=500/3=166; 260>166 -> lock; 1000-500-260=240/2=120
		},
		{
			name:   "pool fully consumed by overspenders",
			pool:   1000,
			spends: []int64{600, 600, 0, 0},
			want:   []int64{600, 600, 0, 0},
		},
		{
			name:   "pool oversubscribed — every user gets their spend, total > pool",
			pool:   1000,
			spends: []int64{500, 500, 500, 0},
			want:   []int64{500, 500, 500, 0},
		},
		{
			name:   "single user",
			pool:   1000,
			spends: []int64{0},
			want:   []int64{1000},
		},
		{
			name:   "single user already over",
			pool:   1000,
			spends: []int64{1500},
			want:   []int64{1500},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fairShareAllocate(tt.pool, tt.spends)
			if !slices.Equal(got, tt.want) {
				t.Errorf("fairShareAllocate(%d, %v)\n  got  %v\n  want %v",
					tt.pool, tt.spends, got, tt.want)
			}
		})
	}
}

// Invariant: nobody gets less than they already spent.
func TestFairShareAllocate_NeverBelowSpend(t *testing.T) {
	cases := [][]int64{
		{100, 200, 300, 0},
		{500, 500, 0, 0},
		{1, 2, 3, 4, 5},
		{0, 0, 0},
	}
	for _, spends := range cases {
		got := fairShareAllocate(1000, spends)
		for i, sp := range spends {
			if got[i] < sp {
				t.Errorf("allowance[%d]=%d < spend=%d (spends=%v, alloc=%v)",
					i, got[i], sp, spends, got)
			}
		}
	}
}

// Invariant: sum of allowances never exceeds pool, unless pool is already
// oversubscribed by spend.
func TestFairShareAllocate_SumBounded(t *testing.T) {
	cases := []struct {
		pool   int64
		spends []int64
	}{
		{1000, []int64{0, 0, 0, 0}},
		{1000, []int64{400, 0, 0, 0}},
		{1000, []int64{500, 500, 0, 0}},
		{1000, []int64{300, 200, 100, 50}},
	}
	for _, c := range cases {
		got := fairShareAllocate(c.pool, c.spends)
		var total, spendTotal int64
		for i, a := range got {
			total += a
			spendTotal += c.spends[i]
		}
		// Either total <= pool, OR total == sum(spends) when oversubscribed.
		if total > c.pool && total != spendTotal {
			t.Errorf("pool=%d spends=%v alloc=%v sum=%d (over budget, not just spend)",
				c.pool, c.spends, got, total)
		}
	}
}
