package handlers

import (
	"fmt"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// The runner band on the onboarding queue.
//
// Two of the queue's stages are not decided by a person: Approved waits for
// the onboard-runner to mint, and Cut queued waits for it to revoke. A healthy
// runner claims either within seconds of its 30 s poll. Without this band, a row
// the runner never claimed looks identical to one it is about to claim, and the
// only way to learn why is to read the runner's container log and compare its
// build against the console's. A runner built before the offboard queue, for
// example, leaves queued cuts waiting while the person's email stays blocked for
// a fresh invite.
//
// The band answers the question an admin actually has — "is anything going
// to pick this up?" — from two facts the store already holds: how long each
// row has waited since it was handed to the runner, and when a runner last
// claimed anything. It never guesses at the runner's version; it reports
// what the runner did and did not do.

// runnerLateAfter is how long a row may wait for the runner before the band
// calls it late. The runner polls every 30 s and claims within seconds, so
// five minutes is well past any healthy pickup and short enough that an
// admin who just approved someone sees the problem on their next reload.
const runnerLateAfter = 5 * time.Minute

// runnerWaitRow is one row the runner has left waiting past runnerLateAfter.
type runnerWaitRow struct {
	ID, Handle, Env string
	// Status is the store status ("approved" / "offboard-requested"); Stage is
	// the tab label an admin can find it under.
	Status, Stage string
	// Waited is the human age since the row was handed to the runner.
	Waited string
	// Cut is true for a queued offboard, false for an approved mint.
	Cut bool
}

// runnerQueueVars is the band's payload; nil when nothing is late.
type runnerQueueVars struct {
	Rows []runnerWaitRow
	// LastClaim is when any runner last claimed work, as a human age
	// ("3m ago"), or "never".
	LastClaim string
	// Blind is true when the runner claimed OTHER work after the oldest late
	// row was queued: it was alive and polling, and left this row behind. The
	// only way that happens is a runner build that does not read the queue
	// the row is in. False means no claim since the row was queued — the
	// runner is down, or blind, and the band says it cannot tell which.
	Blind bool
	// AllCuts is true when every late row is a queued offboard, the shape of
	// a runner built before the offboard leg.
	AllCuts bool
	// Oldest is the longest wait among the late rows.
	Oldest string
}

// runnerQueue builds the band from the full proposals pull, the last claim
// time and the clock. Rows are late when they have waited longer than
// lateAfter since the moment they were handed to the runner: the admin
// decision for an approved mint, the admin click for a queued cut. A row with
// no such timestamp cannot be judged and is left out rather than guessed at.
func runnerQueue(all []onboardstore.Proposal, lastClaim int64, now time.Time, lateAfter time.Duration) *runnerQueueVars {
	if lateAfter <= 0 {
		lateAfter = runnerLateAfter
	}
	var rows []runnerWaitRow
	var oldestQueued int64
	allCuts := true
	for _, p := range all {
		var queued int64
		cut := false
		switch p.Status {
		case onboardstore.ProposalApproved:
			if p.DecidedAt == nil {
				continue
			}
			queued = *p.DecidedAt
		case onboardstore.ProposalOffboardRequested:
			if p.OffboardRequestedAt == 0 {
				continue
			}
			queued = p.OffboardRequestedAt
			cut = true
		default:
			continue
		}
		waited := now.Sub(time.Unix(queued, 0))
		if waited <= lateAfter {
			continue
		}
		if !cut {
			allCuts = false
		}
		if oldestQueued == 0 || queued < oldestQueued {
			oldestQueued = queued
		}
		rows = append(rows, runnerWaitRow{
			ID: p.ID, Handle: p.Handle, Env: p.Env, Status: p.Status,
			Stage:  onboardStageLabel(p.Status),
			Waited: waitAge(waited),
			Cut:    cut,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	v := &runnerQueueVars{
		Rows:      rows,
		LastClaim: "never",
		AllCuts:   allCuts,
		Oldest:    waitAge(now.Sub(time.Unix(oldestQueued, 0))),
	}
	if lastClaim > 0 {
		v.LastClaim = waitAge(now.Sub(time.Unix(lastClaim, 0))) + " ago"
		v.Blind = lastClaim > oldestQueued
	}
	return v
}

// waitAge renders a duration the way the queue's row ages do (onboard-queue.js
// `ago`): whole seconds under a minute, then minutes, hours, days.
func waitAge(d time.Duration) string {
	s := int64(d.Seconds())
	if s < 0 {
		s = 0
	}
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh", s/3600)
	default:
		return fmt.Sprintf("%dd", s/86400)
	}
}
