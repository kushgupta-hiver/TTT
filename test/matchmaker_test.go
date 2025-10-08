package test

import (
	"context"
	"testing"
	"time"

	"github.com/kushgupta-hiver/TTT/internal/engine"
	"github.com/kushgupta-hiver/TTT/internal/match"
)

func TestAutoMatch_TwoPlayers_CreateRoom(t *testing.T) {
	e := engine.NewEngine()
	_ = e // engine may be injected into room in the onRoom callback later

	events := make(chan match.RoomCreatedEvent, 1)
	mm := match.NewMatchmaker(func(ev match.RoomCreatedEvent) {
		events <- ev
	})
	defer mm.Close()

	ctx := context.Background()
	if err := mm.Enqueue(ctx, match.Player{ID: "p1"}); err != nil {
		t.Fatalf("enqueue p1: %v", err)
	}
	if err := mm.Enqueue(ctx, match.Player{ID: "p2"}); err != nil {
		t.Fatalf("enqueue p2: %v", err)
	}

	select {
	case ev := <-events:
		if ev.X.ID == ev.O.ID {
			t.Fatalf("expected two distinct players")
		}
		if ev.X.ID == "" || ev.O.ID == "" || ev.RoomID == "" {
			t.Fatalf("expected non-empty room and player ids")
		}
	default:
		// allow a small wait since matching is async
		select {
		case ev := <-events:
			if ev.X.ID == ev.O.ID {
				t.Fatalf("expected two distinct players")
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("expected a room to be created")
		}
	}
}

func TestMatchmaker_SixtyPlayers_CreateThirtyRooms(t *testing.T) {
	events := make(chan match.RoomCreatedEvent, 64)
	mm := match.NewMatchmaker(func(ev match.RoomCreatedEvent) {
		events <- ev
	})
	defer mm.Close()

	ctx := context.Background()
	for i := 0; i < 60; i++ {
		id := "p" + strconvI(i)
		if err := mm.Enqueue(ctx, match.Player{ID: id}); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
	}

	// Collect rooms
	count := 0
	timeout := time.After(1 * time.Second)
	for count < 30 {
		select {
		case <-events:
			count++
		case <-timeout:
			t.Fatalf("expected 30 rooms, got %d", count)
		}
	}
}

func strconvI(i int) string {
	// tiny helper to avoid importing strconv in multiple tests
	return string([]byte{'0' + byte(i/10), '0' + byte(i%10)})
}
