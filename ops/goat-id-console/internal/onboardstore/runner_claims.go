package onboardstore

import (
	"context"
	"fmt"
)

// LastRunnerClaim returns the unix time of the newest claim any runner has
// made on this store — a proposal (onboard or offboard leg) or a follow-up
// action — or 0 when nothing has ever been claimed.
//
// It is the one fact that separates "the runner is down" from "the runner is
// running and not reading this queue": a runner that has claimed work
// AFTER a row was queued was alive and polling at that moment, so the row it
// left behind is one its build does not poll, not one it could not reach.
//
// The stale-claim sweep zeroes claimed_at on a row it re-queues, so a row the
// runner died on does not count as a claim here; that is correct — the
// question is when a runner was last known to be running, and a claim that
// had to be rolled back is not evidence of one.
func (s *Store) LastRunnerClaim(ctx context.Context) (int64, error) {
	var last int64
	err := s.db.GetContext(ctx, &last, `
		SELECT MAX(t) FROM (
			SELECT COALESCE(MAX(claimed_at), 0) AS t FROM onboard_proposals
			UNION ALL
			SELECT COALESCE(MAX(claimed_at), 0) AS t FROM onboard_actions
		) AS claims`)
	if err != nil {
		return 0, fmt.Errorf("last runner claim: %w", err)
	}
	return last, nil
}
