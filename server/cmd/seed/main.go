// seed inserts fake usage data into the dev database for UI testing.
// Run from the server/ directory:
//
//	go run ./cmd/seed
package main

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/taciturnaxolotl/potluck/internal/config"
	"github.com/taciturnaxolotl/potluck/internal/store"

	_ "modernc.org/sqlite"
)

var models = []struct {
	id     string
	inPPM  float64 // price per million input tokens
	outPPM float64
}{
	{"claude-sonnet-4-6", 4.62, 23.1},
	{"claude-haiku-4-5", 1.54, 7.7},
	{"gpt-4.1-mini", 0.56, 2.24},
	{"deepseek-ai/DeepSeek-V4-Flash", 0.196, 0.392},
	{"Qwen/Qwen3-32B", 1.26, 1.26},
}

func main() {
	cfg := config.MustGet()
	// Resolve DB path: try the configured path, then ../data/potluck.db
	// (the server runs from the repo root; the seed tool runs from server/).
	dbPath := cfg.DatabaseURL
	if rows, err2 := sql.Open("sqlite", dbPath); err2 == nil {
		var n int
		if err3 := rows.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM users").Scan(&n); err3 != nil || n == 0 {
			dbPath = "../data/potluck.db"
		}
		rows.Close()
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	q := store.New(db)
	ctx := context.Background()

	// Find all users.
	rows, err := db.QueryContext(ctx, "SELECT id FROM users")
	if err != nil {
		fmt.Fprintf(os.Stderr, "query users: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			userIDs = append(userIDs, id)
		}
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "rows iteration: %v\n", err)
		os.Exit(1)
	}
	if len(userIDs) == 0 {
		fmt.Fprintf(os.Stderr, "no users found — sign in first, then seed\n")
		os.Exit(1)
	}
	fmt.Printf("Seeding usage for %d users\n", len(userIDs))

	rng := rand.New(rand.NewSource(42))
	total := 0
	for _, userID := range userIDs {
		total += seedUser(ctx, db, q, rng, userID)
	}
	fmt.Printf("Inserted %d spend records total\n", total)
}

func seedUser(ctx context.Context, db *sql.DB, q *store.Queries, rng *rand.Rand, userID string) int {
	// Find or create a conversation.
	var convID string
	row := db.QueryRowContext(ctx, "SELECT id FROM conversations WHERE user_id = ? LIMIT 1", userID)
	if err := row.Scan(&convID); err != nil {
		convID = uuid.NewString()
		now := time.Now().Unix()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO conversations (id, user_id, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			convID, userID, "seed conversation", now, now,
		); err != nil {
			fmt.Fprintf(os.Stderr, "create conversation for %s: %v\n", userID, err)
			return 0
		}
	}

	now := time.Now()
	inserted := 0

	for daysAgo := 29; daysAgo >= 0; daysAgo-- {
		recentBias := float64(30-daysAgo) / 30.0
		nReqs := rng.Intn(int(5*recentBias) + 1)

		for range nReqs {
			m := models[rng.Intn(len(models))]

			dayStart := now.AddDate(0, 0, -daysAgo).Truncate(24 * time.Hour)
			startedAt := dayStart.Add(time.Duration(rng.Intn(86400)) * time.Second)
			finishedAt := startedAt.Add(time.Duration(2+rng.Intn(30)) * time.Second)

			inTok := int64(100 + rng.Intn(3000))
			outTok := int64(50 + rng.Intn(1500))
			amountMicros := int64(
				(float64(inTok)/1_000_000)*m.inPPM*1_000_000 +
					(float64(outTok)/1_000_000)*m.outPPM*1_000_000,
			)

			streamID := uuid.NewString()
			if _, err := db.ExecContext(ctx, `
				INSERT INTO streams (id, conversation_id, user_id, idempotency_key, model, status, started_at, finished_at)
				VALUES (?, ?, ?, ?, ?, 'done', ?, ?)`,
				streamID, convID, userID, uuid.NewString(), m.id, startedAt.Unix(), finishedAt.Unix(),
			); err != nil {
				fmt.Fprintf(os.Stderr, "insert stream: %v\n", err)
				continue
			}

			if _, err := q.UpsertSpend(ctx, store.UpsertSpendParams{
				ID:           uuid.NewString(),
				UserID:       userID,
				StreamID:     streamID,
				Model:        m.id,
				InputTokens:  inTok,
				OutputTokens: outTok,
				AmountMicros: amountMicros,
				IsEstimated:  0,
				CreatedAt:    startedAt.Unix(),
			}); err != nil {
				fmt.Fprintf(os.Stderr, "insert spend: %v\n", err)
				continue
			}
			inserted++
		}
	}
	fmt.Printf("  user %s: %d records\n", userID, inserted)
	return inserted
}
