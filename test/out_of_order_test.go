package test

import (
	"context"
	"testing"

	"github.com/kushgupta-hiver/TTT/internal/engine"
	"github.com/kushgupta-hiver/TTT/internal/match"
)

func TestOutOfOrderRejected_AtRoomLevel(t *testing.T) {
	ctx := context.Background()
	e := engine.NewEngine()
	r := match.NewRoom("r-ooo", e, match.Options{})

	_ = r.Join(ctx, match.Player{ID: "px", Mark: engine.X})
	_ = r.Join(ctx, match.Player{ID: "po", Mark: engine.O})

	// First move should have ClientSeq 1; we intentionally send 2
	_, err := r.Submit(ctx, engine.Move{
		PlayerID:  "px",
		Position:  0,
		MsgID:     "m1",
		ClientSeq: 2,
		Mark:      engine.X,
	})
	if err == nil || err != engine.ErrOutOfOrder {
		t.Fatalf("expected ErrOutOfOrder, got %v", err)
	}
}
