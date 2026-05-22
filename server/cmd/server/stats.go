package main

import (
	"encoding/json"
	"net/http"
	"time"

	"charm.land/log/v2"

	"github.com/taciturnaxolotl/potluck/internal/money"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

// publicStatsHandler returns the snapshot used by the public splash page.
// Anonymous, no PII, intentionally cheap. Numbers are rolled up at request
// time — at our scale a five-row aggregate is not worth caching.
func publicStatsHandler(q *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		now := time.Now()
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()

		balance, err := q.PoolTotalBalance(ctx)
		if err != nil {
			statsFail(w, err)
			return
		}
		spentTodayRaw, err := q.PoolSpentSince(ctx, startOfDay)
		if err != nil {
			statsFail(w, err)
			return
		}
		spentToday := toInt64(spentTodayRaw)
		contributors, err := q.PoolContributorCount(ctx)
		if err != nil {
			statsFail(w, err)
			return
		}
		users, err := q.PoolUserCount(ctx)
		if err != nil {
			statsFail(w, err)
			return
		}
		keys, err := q.PoolActiveKeyCount(ctx)
		if err != nil {
			statsFail(w, err)
			return
		}
		tokens, err := q.PoolTokensGuzzled(ctx)
		if err != nil {
			statsFail(w, err)
			return
		}
		inTok := toInt64(tokens.InputTokens)
		outTok := toInt64(tokens.OutputTokens)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=15")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"balance_micros":     balance,
			"balance_usd":        money.Micros(balance).USDString(),
			"spent_today_micros": spentToday,
			"spent_today_usd":    money.Micros(spentToday).USDString(),
			"contributors":       contributors,
			"users":              users,
			"active_keys":        keys,
			"input_tokens":       inTok,
			"output_tokens":      outTok,
			"total_tokens":       inTok + outTok,
			"as_of":              now.Unix(),
		})
	}
}

func statsFail(w http.ResponseWriter, err error) {
	log.Error("public stats", "err", err)
	http.Error(w, `{"error":{"code":"internal","message":"stats unavailable"}}`, http.StatusInternalServerError)
}

// toInt64 unboxes the interface{} sqlc emits for SUM aggregates.
func toInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case nil:
		return 0
	}
	return 0
}
