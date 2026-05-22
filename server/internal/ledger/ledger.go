// Package ledger holds balance, contribution, and spend math.
//
// Money is int64 micros (see internal/money). Balance is recomputed on
// demand from sums; we don't keep a denormalised running total. With ~10
// users this is fine.
package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/taciturnaxolotl/potluck/internal/money"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

var ErrInsufficientFunds = errors.New("insufficient_funds")

type Service struct {
	q *store.Queries

	MinBalanceToStart    money.Micros
	MaxConcurrentStreams int
}

func New(q *store.Queries, minBalance money.Micros, maxConcurrent int) *Service {
	return &Service{q: q, MinBalanceToStart: minBalance, MaxConcurrentStreams: maxConcurrent}
}

// Balance returns contributions − spends for a user.
func (s *Service) Balance(ctx context.Context, userID string) (money.Micros, error) {
	contribs, err := s.q.SumContributions(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("ledger: sum contributions: %w", err)
	}
	spends, err := s.q.SumSpends(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("ledger: sum spends: %w", err)
	}
	return money.Micros(toInt64(contribs) - toInt64(spends)), nil
}

// Contribute records a positive contribution.
func (s *Service) Contribute(ctx context.Context, userID string, amount money.Micros, note string) (store.Contribution, error) {
	if amount <= 0 {
		return store.Contribution{}, fmt.Errorf("ledger: contribution must be positive")
	}
	return s.q.CreateContribution(ctx, store.CreateContributionParams{
		ID:           uuid.NewString(),
		UserID:       userID,
		AmountMicros: int64(amount),
		Note:         note,
		CreatedAt:    time.Now().Unix(),
	})
}

// CanStart returns nil if the user is allowed to begin a new stream right now,
// or ErrInsufficientFunds / a "too_many_streams" error.
func (s *Service) CanStart(ctx context.Context, userID string) error {
	bal, err := s.Balance(ctx, userID)
	if err != nil {
		return err
	}
	if bal < s.MinBalanceToStart {
		return ErrInsufficientFunds
	}
	active, err := s.q.CountActiveStreamsForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("ledger: count active: %w", err)
	}
	if int(active) >= s.MaxConcurrentStreams {
		return errors.New("too_many_streams")
	}
	return nil
}

// SettleStream records the final spend for a stream. Idempotent on stream_id.
func (s *Service) SettleStream(ctx context.Context, userID, streamID, model string, inputTokens, outputTokens int64, amount money.Micros, estimated bool) error {
	est := int64(0)
	if estimated {
		est = 1
	}
	_, err := s.q.UpsertSpend(ctx, store.UpsertSpendParams{
		ID:           uuid.NewString(),
		UserID:       userID,
		StreamID:     streamID,
		Model:        model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		AmountMicros: int64(amount),
		IsEstimated:  est,
		CreatedAt:    time.Now().Unix(),
	})
	return err
}

// toInt64 unboxes the interface{} sqlc emits for SUM/COALESCE aggregates.
func toInt64(v interface{}) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		// SQLite occasionally returns SUM as REAL; we never store fractions
		// in the underlying column so a float here is a precision-safe int.
		return int64(x)
	case nil:
		return 0
	}
	return 0
}
