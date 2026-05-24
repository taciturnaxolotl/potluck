package pool

// billing_ingest.go — pulls /billing/usage/requests per key, matches rows
// to potluck_requests, and writes pool_key_billing_rows + user_daily_spend.
//
// Attribution rules (applied in order):
//
//  1. Two rows with same (model, token_usage) on the same key within 1s →
//     later row is a pioneer double-log → AttrDuplicate.
//  2. Billing row matches a potluck_requests row by (key, model, time ±15s,
//     tokens ±5%) → AttrMatched, cost to requester.
//  3. Endpoint contains "llmaj"/"judge" and there's a matched opus call
//     on this key within the last 90s → AttrJudgePaired, cost to that user.
//  4. Everything else → AttrOwnerFallback, cost to key owner.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

// Attribution integer constants (mirror the migration comments).
const (
	AttrMatched       = int64(0)
	AttrJudgePaired   = int64(1)
	AttrOwnerFallback = int64(2)
	AttrDuplicate     = int64(3)
)

// billingItem is one row from /billing/usage/requests.
type billingItem struct {
	ID          string  `json:"id"`
	CreatedAt   string  `json:"created_at"`
	CreditUsage float64 `json:"credit_usage"`
	TokenUsage  int64   `json:"token_usage"`
	Cost        float64 `json:"cost"`
	Endpoint    string  `json:"endpoint"`
	Model       string  `json:"model"`
}

type billingPage struct {
	Items      []billingItem `json:"items"`
	TotalCount int           `json:"total_count"`
	Page       int           `json:"page"`
	PageSize   int           `json:"page_size"`
}

// reqKey is the lookup key for potluck_requests matching.
type reqKey struct {
	model string
	ts    int64
}

// errBillingUnauthorized signals a 401 on the billing endpoint.
var errBillingUnauthorized = fmt.Errorf("billing: unauthorized")

// fetchBillingItems returns all billing rows with created_at > sinceUnix,
// oldest-first.
func fetchBillingItems(ctx context.Context, httpClient *http.Client, apiKey string, sinceUnix int64) ([]billingItem, error) {
	var collected []billingItem

	for page := 1; ; page++ {
		url := fmt.Sprintf("https://api.pioneer.ai/billing/usage/requests?limit=100&page=%d", page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			return nil, errBillingUnauthorized
		}
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("billing/usage/requests: HTTP %d", resp.StatusCode)
		}

		var pg billingPage
		if err := json.Unmarshal(body, &pg); err != nil {
			return nil, fmt.Errorf("decode billing page %d: %w", page, err)
		}

		doneEarly := false
		for _, item := range pg.Items {
			if parsePioneerTime(item.CreatedAt) <= sinceUnix {
				doneEarly = true
				break
			}
			collected = append(collected, item)
		}

		if doneEarly || len(pg.Items) == 0 || page*100 >= pg.TotalCount {
			break
		}
	}

	// Reverse to oldest-first.
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}
	return collected, nil
}

// ingestBillingRows fetches new pioneer billing rows for key and writes
// attribution + spend records.
func (r *Reconciler) ingestBillingRows(ctx context.Context, key store.PoolKey, plaintext string) {
	// Cursor: most recent row we've already ingested for this key.
	cursorRaw, err := r.q.LatestBillingRowTime(ctx, key.ID)
	if err != nil {
		r.log.Error("reconciler: billing cursor", "key_id", key.ID, "err", err)
		return
	}
	since := billingCursorToInt64(cursorRaw)

	items, err := fetchBillingItems(ctx, r.httpClient, plaintext, since)
	if err != nil {
		if err == errBillingUnauthorized {
			return // health probe already handled the 401
		}
		r.log.Error("reconciler: fetch billing items", "key_id", key.ID, "err", err)
		return
	}
	if len(items) == 0 {
		return
	}

	r.log.Info("reconciler: ingesting billing rows",
		"key_id", key.ID, "label", key.Label, "count", len(items))

	now := time.Now().Unix()

	// Load unmatched potluck_requests for this key in the time window.
	windowStart := since
	windowEnd := now + 120
	potluckReqs, err := r.q.ListUnmatchedRequestsForKey(ctx,
		store.ListUnmatchedRequestsForKeyParams{
			PoolKeyID:    sql.NullString{String: key.ID, Valid: true},
			FinishedAt:   sql.NullInt64{Int64: windowStart, Valid: true},
			FinishedAt_2: sql.NullInt64{Int64: windowEnd, Valid: true},
		})
	if err != nil {
		r.log.Error("reconciler: list unmatched", "key_id", key.ID, "err", err)
		return
	}

	// Build time-indexed lookup for matching.
	reqsByKey := make(map[reqKey]*store.PotluckRequest, len(potluckReqs))
	for i := range potluckReqs {
		pr := &potluckReqs[i]
		if !pr.FinishedAt.Valid {
			continue
		}
		reqsByKey[reqKey{model: pr.Model, ts: pr.FinishedAt.Int64}] = pr
	}

	// Track last matched opus call for judge pairing.
	type opusCall struct {
		ts     int64
		userID string
	}
	var lastOpus *opusCall

	dayStart := (now / 86400) * 86400
	day := now / 86400

	// Per-user spend delta for this batch.
	type delta struct{ shared, private int64 }
	spendDeltas := map[string]*delta{}
	addSpend := func(userID string, micros int64, ownsKey bool) {
		d := spendDeltas[userID]
		if d == nil {
			d = &delta{}
			spendDeltas[userID] = d
		}
		if ownsKey {
			d.private += micros
		} else {
			d.shared += micros
		}
	}

	for i := range items {
		item := &items[i]
		ts := parsePioneerTime(item.CreatedAt)
		costMicros := int64(math.Round(item.Cost * 1_000_000))
		creditMicros := int64(math.Round(item.CreditUsage * 10_000))

		isDup := int64(0)
		attr := AttrOwnerFallback
		var matchedID sql.NullString
		var attrUserID sql.NullString

		// 1. Duplicate detection: same model + tokens on same key within 1s.
		if i > 0 {
			prev := &items[i-1]
			prevTs := parsePioneerTime(prev.CreatedAt)
			if prev.Model == item.Model &&
				prev.TokenUsage == item.TokenUsage &&
				absi(ts-prevTs) <= 1 {
				isDup = 1
				attr = AttrDuplicate
			}
		}

		if isDup == 0 {
			if matched := matchRequest(item, ts, reqsByKey); matched != nil {
				// 2. Matched to a potluck request.
				attr = AttrMatched
				matchedID = sql.NullString{String: matched.ID, Valid: true}
				attrUserID = sql.NullString{String: matched.UserID, Valid: true}
				// Track for judge pairing if it's an opus-class model.
				if looksLikeOpus(item.Model) {
					lastOpus = &opusCall{ts: ts, userID: matched.UserID}
				}
			} else if isJudgeEndpoint(item.Endpoint) && lastOpus != nil && absi(ts-lastOpus.ts) <= 90 {
				// 3. Judge paired to preceding opus call.
				attr = AttrJudgePaired
				attrUserID = sql.NullString{String: lastOpus.userID, Valid: true}
			} else {
				// 4. Owner fallback.
				attrUserID = sql.NullString{String: key.UserID, Valid: true}
			}
		}

		_ = r.q.UpsertBillingRow(ctx, store.UpsertBillingRowParams{
			ID:               item.ID,
			PoolKeyID:        key.ID,
			PioneerCreatedAt: ts,
			CreditMicros:     creditMicros,
			CostMicros:       costMicros,
			TokenUsage:       item.TokenUsage,
			Model:            item.Model,
			Endpoint:         item.Endpoint,
			AttributedUserID: attrUserID,
			Attribution:      attr,
			IsDuplicate:      isDup,
			MatchedRequestID: matchedID,
			IngestedAt:       now,
		})

		// Accumulate spend for today only, non-duplicate rows.
		// Route to private bucket only up to the key's private reservation
		// (max - shared). Any overflow spills into shared.
		if isDup == 0 && attrUserID.Valid && ts >= dayStart {
			privateReservation := key.MaxMicros - key.SharedMicros
			ownsKeyAndHasPrivate := attrUserID.String == key.UserID && privateReservation > 0
			if ownsKeyAndHasPrivate {
				// Cap private at reservation; spill remainder to shared.
				// How much private spend has already been attributed in this
				// batch for this user (earlier rows in the same reconciler tick).
				alreadyPrivate := int64(0)
				if d := spendDeltas[attrUserID.String]; d != nil {
					alreadyPrivate = d.private
				}
				// NOTE: this only caps within a single reconciler run.
				// Across ticks, the gate uses GetUserLiveSpendToday which
				// reads already-written rows and applies the same cap logic.
				canPrivate := privateReservation - alreadyPrivate
				if canPrivate < 0 {
					canPrivate = 0
				}
				privateAmount := costMicros
				if privateAmount > canPrivate {
					privateAmount = canPrivate
				}
				spillAmount := costMicros - privateAmount
				if privateAmount > 0 {
					addSpend(attrUserID.String, privateAmount, true)
				}
				if spillAmount > 0 {
					addSpend(attrUserID.String, spillAmount, false)
				}
			} else {
				addSpend(attrUserID.String, costMicros, false)
			}
		}
	}

	// Flush spend deltas to user_daily_spend.
	for userID, d := range spendDeltas {
		_ = r.q.UpsertUserDailySpend(ctx, store.UpsertUserDailySpendParams{
			UserID:             userID,
			Day:                day,
			SharedSpentMicros:  d.shared,
			PrivateSpentMicros: d.private,
		})
	}
}

// matchRequest finds the best-matching potluck_requests row for a billing item.
// Criteria: same model, finished_at within ±15s, total_tokens within 5%.
func matchRequest(item *billingItem, itemTs int64, reqs map[reqKey]*store.PotluckRequest) *store.PotluckRequest {
	const windowSecs = int64(15)
	const tokenTol = 0.05

	var best *store.PotluckRequest
	var bestDiff int64 = math.MaxInt64

	for k, pr := range reqs {
		if k.model != item.Model {
			continue
		}
		diff := absi(k.ts - itemTs)
		if diff > windowSecs {
			continue
		}
		if pr.TotalTokens.Valid && item.TokenUsage > 0 {
			ratio := float64(absi(pr.TotalTokens.Int64-item.TokenUsage)) / float64(item.TokenUsage)
			if ratio > tokenTol {
				continue
			}
		}
		if diff < bestDiff {
			bestDiff = diff
			best = pr
		}
	}
	return best
}

// parsePioneerTime parses pioneer's created_at string to unix seconds.
func parsePioneerTime(s string) int64 {
	formats := []string{
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05.999999+00:00",
		"2006-01-02T15:04:05.999999Z",
		"2006-01-02T15:04:05Z",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}

func absi(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func looksLikeOpus(model string) bool {
	return strings.Contains(strings.ToLower(model), "opus")
}

func isJudgeEndpoint(ep string) bool {
	return strings.Contains(ep, "llmaj") || strings.Contains(ep, "judge")
}

// billingCursorToInt64 unwraps the COALESCE(SUM+MAX) result from LatestBillingRowTime.
func billingCursorToInt64(v interface{}) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case nil:
		return 0
	}
	return 0
}
