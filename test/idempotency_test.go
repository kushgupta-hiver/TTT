package test

import (
	"context"
	"testing"

	"github.com/kushgupta-hiver/TTT/internal/engine"
	"github.com/kushgupta-hiver/TTT/internal/match"
)

func TestIdempotentMove_SameMsgID_ReturnsSameState(t *testing.T) {
	ctx := context.Background()
	e := engine.NewEngine()
	r := match.NewRoom("r-idem", e, match.Options{})

	_ = r.Join(ctx, match.Player{ID: "px", Mark: engine.X})
	_ = r.Join(ctx, match.Player{ID: "po", Mark: engine.O})

	move := engine.Move{
		PlayerID:  "px",
		Position:  0,
		MsgID:     "same",
		ClientSeq: 1,
		Mark:      engine.X,
	}

	s1, err1 := r.Submit(ctx, move)
	if err1 != nil {
		t.Fatalf("first submit: %v", err1)
	}

	s2, err2 := r.Submit(ctx, move) // duplicate
	if err2 != nil {
		t.Fatalf("second submit should not error, got %v", err2)
	}

	if s1.ServerSeq != s2.ServerSeq {
		t.Fatalf("expected same ServerSeq for idempotent move, got %d vs %d", s1.ServerSeq, s2.ServerSeq)
	}
	if s1.Board != s2.Board {
		t.Fatalf("expected identical board for idempotent move")
	}
}
