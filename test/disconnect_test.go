package test

import (
	"context"
	"testing"
	"time"

	"github.com/kushgupta-hiver/TTT/internal/engine"
	"github.com/kushgupta-hiver/TTT/internal/match"
)

func TestOpponentWinsOnDisconnect_NoGrace(t *testing.T) {
	ctx := context.Background()
	e := engine.NewEngine()
	r := match.NewRoom("r-disc", e, match.Options{GracePeriod: 0})

	if err := r.Join(ctx, match.Player{ID: "px", Mark: engine.X}); err != nil {
		t.Fatalf("join X: %v", err)
	}
	if err := r.Join(ctx, match.Player{ID: "po", Mark: engine.O}); err != nil {
		t.Fatalf("join O: %v", err)
	}

	if err := r.Leave(ctx, "po"); err != nil {
		t.Fatalf("leave O: %v", err)
	}

	s := r.State()
	if s.Status != engine.XWins {
		t.Fatalf("expected XWins after O disconnects, got %v", s.Status)
	}
}

// Optional: grace period where opponent can rejoin
func TestRejoinWithinGrace_PreventsForfeit(t *testing.T) {
	t.Skip("enable when grace rejoin implemented")

	ctx := context.Background()
	e := engine.NewEngine()
	r := match.NewRoom("r-grace", e, match.Options{GracePeriod: 800 * time.Millisecond})

	_ = r.Join(ctx, match.Player{ID: "px", Mark: engine.X})
	_ = r.Join(ctx, match.Player{ID: "po", Mark: engine.O})

	_ = r.Leave(ctx, "po")

	// Simulate rejoin within grace
	time.Sleep(200 * time.Millisecond)
	_ = r.Join(ctx, match.Player{ID: "po", Mark: engine.O})

	s := r.State()
	if s.Status != engine.InProgress {
		t.Fatalf("expected InProgress after rejoin within grace, got %v", s.Status)
	}
}
