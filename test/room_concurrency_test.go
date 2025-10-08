package test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/kushgupta-hiver/TTT/internal/engine"
	"github.com/kushgupta-hiver/TTT/internal/match"
)

func TestConcurrentMoves_OnlyOneAccepted_WithPerRoomLock(t *testing.T) {
	ctx := context.Background()
	e := engine.NewEngine()
	r := match.NewRoom("r1", e, match.Options{GracePeriod: 0})

	// Join two players
	if err := r.Join(ctx, match.Player{ID: "px", Mark: engine.X}); err != nil {
		t.Fatalf("join X: %v", err)
	}
	if err := r.Join(ctx, match.Player{ID: "po", Mark: engine.O}); err != nil {
		t.Fatalf("join O: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)

	go func() {
		defer wg.Done()
		_, err := r.Submit(ctx, engine.Move{
			PlayerID:  "px",
			Position:  0,
			MsgID:     "mx",
			ClientSeq: 1,
			Mark:      engine.X,
		})
		errs <- err
	}()

	go func() {
		defer wg.Done()
		_, err := r.Submit(ctx, engine.Move{
			PlayerID:  "po",
			Position:  1,
			MsgID:     "mo",
			ClientSeq: 1,
			Mark:      engine.O,
		})
		errs <- err
	}()

	wg.Wait()
	close(errs)

	var accepted, rejected int
	for err := range errs {
		if err == nil {
			accepted++
		} else if errors.Is(err, engine.ErrNotYourTurn) || errors.Is(err, engine.ErrOutOfOrder) {
			rejected++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if accepted != 1 || rejected != 1 {
		t.Fatalf("expected 1 accepted and 1 rejected, got accepted=%d rejected=%d", accepted, rejected)
	}

	state := r.State()
	if state.ServerSeq != 1 {
		t.Fatalf("expected server seq 1, got %d", state.ServerSeq)
	}
	if state.Board[0] != engine.X {
		t.Fatalf("expected X to have made the first move at position 0")
	}
	if state.NextTurn != engine.O {
		t.Fatalf("expected next turn to be O")
	}
}
