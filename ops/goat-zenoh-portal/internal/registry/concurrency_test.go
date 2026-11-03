package registry_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// On SQLite the single pooled connection serialized every transaction. On
// Postgres the read-then-write transactions take registryWriteLock instead
// (beginLocked). These races would hand one slot to two systems, or two slots
// to one alias, without it.

const racers = 12

func TestAllocateSlotRaceGivesEverySystemItsOwnSlot(t *testing.T) {
	r, err := registry.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if _, err := r.InitializeSlotPool(t.Context(), "race-test", racers); err != nil {
		t.Fatalf("InitializeSlotPool: %v", err)
	}

	var wg sync.WaitGroup
	slots := make([]string, racers)
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := r.AllocateSlot(t.Context(), fmt.Sprintf("system-%02d", i))
			errs[i] = err
			if err == nil {
				slots[i] = s.SlotID
			}
		}()
	}
	wg.Wait()
	seen := map[string]int{}
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("system-%02d: %v", i, errs[i])
		}
		if prev, dup := seen[slots[i]]; dup {
			t.Fatalf("slot %s handed to system-%02d and system-%02d", slots[i], prev, i)
		}
		seen[slots[i]] = i
	}
}

func TestAllocateSlotRaceGivesOneAliasOneSlot(t *testing.T) {
	r, err := registry.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if _, err := r.InitializeSlotPool(t.Context(), "race-test", racers); err != nil {
		t.Fatalf("InitializeSlotPool: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = r.AllocateSlot(t.Context(), "same-system")
		}()
	}
	wg.Wait()
	won := 0
	for i, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, registry.ErrAliasTaken):
		default:
			t.Fatalf("racer %d: %v", i, err)
		}
	}
	if won != 1 {
		t.Fatalf("%d racers claimed a slot for one alias, want exactly 1", won)
	}
}
