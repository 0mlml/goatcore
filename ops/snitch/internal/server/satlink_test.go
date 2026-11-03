package server

import (
	"errors"
	"testing"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
	"github.com/jackc/pgx/v5"
)

func TestOptionalSatlinkPrevious(t *testing.T) {
	t.Run("first sample has no comparison", func(t *testing.T) {
		got, err := optionalSatlinkPrevious(nil, pgx.ErrNoRows)
		if err != nil || got != nil {
			t.Fatalf("got result=%v err=%v, want nil result and nil error", got, err)
		}
	})

	t.Run("database failure remains visible", func(t *testing.T) {
		wantErr := errors.New("database unavailable")
		if _, err := optionalSatlinkPrevious(nil, wantErr); !errors.Is(err, wantErr) {
			t.Fatalf("got error %v, want %v", err, wantErr)
		}
	})

	t.Run("previous sample is preserved", func(t *testing.T) {
		want := &types.SatlinkProbeResult{ID: 7}
		got, err := optionalSatlinkPrevious(want, nil)
		if err != nil || got != want {
			t.Fatalf("got result=%v err=%v, want result=%v and nil error", got, err, want)
		}
	})
}
