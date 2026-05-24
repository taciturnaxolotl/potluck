package main

import (
	"context"
	"time"

	"charm.land/log/v2"

	"github.com/taciturnaxolotl/potluck/internal/api/web"
)

// runAllocationRecomputer drives a periodic call to web.Server.RunSmartAllocation.
// The handler version is for the manual recompute button; this version is the
// background sweep so day-of-week and time-of-day patterns get applied as the
// day progresses (light users donating, heavy users receiving) without
// requiring anyone to hit the button.
//
// setByUserID is "system" so the dashboard can distinguish automatic
// recomputes from manual ones if it wants to.
func runAllocationRecomputer(ctx context.Context, s *web.Server, intervalSeconds int) {
	if intervalSeconds <= 0 {
		log.Info("allocation recomputer disabled (interval <= 0)")
		return
	}
	interval := time.Duration(intervalSeconds) * time.Second
	log.Info("Starting allocation recomputer", "interval", interval)

	// Run once on boot so a freshly-started server doesn't wait `interval`
	// before producing allowance rows.
	if err := s.RunSmartAllocation(ctx, "system"); err != nil {
		log.Warn("initial allocation recompute failed", "err", err)
	}

	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("allocation recomputer stopping")
			return
		case <-t.C:
			if err := s.RunSmartAllocation(ctx, "system"); err != nil {
				log.Warn("allocation recompute failed", "err", err)
			}
		}
	}
}
