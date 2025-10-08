package test

import (
	"testing"

	"github.com/kushgupta-hiver/TTT/internal/engine"
)

func TestOutcome_WinRowsColsDiags(t *testing.T) {
	e := engine.NewEngine()

	type tc struct {
		name   string
		board  engine.Board
		out    engine.Outcome
	}
	tests := []tc{
		{
			name: "row 0 X wins",
			board: engine.Board{
				engine.X, engine.X, engine.X,
				engine.Empty, engine.O, engine.Empty,
				engine.Empty, engine.O, engine.Empty,
			},
			out: engine.XWins,
		},
		{
			name: "col 1 O wins",
			board: engine.Board{
				engine.X, engine.O, engine.Empty,
				engine.Empty, engine.O, engine.X,
				engine.Empty, engine.O, engine.Empty,
			},
			out: engine.OWins,
		},
		{
			name: "diag X wins",
			board: engine.Board{
				engine.X, engine.O, engine.Empty,
				engine.Empty, engine.X, engine.O,
				engine.Empty, engine.Empty, engine.X,
			},
			out: engine.XWins,
		},
		{
			name: "draw",
			board: engine.Board{
				engine.X, engine.O, engine.X,
				engine.X, engine.O, engine.O,
				engine.O, engine.X, engine.X,
			},
			out: engine.Draw,
		},
		{
			name: "in-progress",
			board: engine.Board{
				engine.X, engine.O, engine.X,
				engine.Empty, engine.O, engine.Empty,
				engine.O, engine.X, engine.Empty,
			},
			out: engine.InProgress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.Outcome(tt.board)
			if got != tt.out {
				t.Fatalf("expected %v, got %v", tt.out, got)
			}
		})
	}
}

func TestApplyMove_ValidFlowAndTurn(t *testing.T) {
	e := engine.NewEngine()
	state := e.NewGame() // expect X to start

	// X plays 0
	ns, err := e.ApplyMove(state, engine.Move{
		PlayerID:  "px",
		Position:  0,
		MsgID:     "m1",
		ClientSeq: 1,
		Mark:      engine.X,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if ns.Board[0] != engine.X {
		t.Fatalf("expected board[0]=X")
	}
	if ns.NextTurn != engine.O {
		t.Fatalf("expected next turn O")
	}
	if ns.ServerSeq != 1 {
		t.Fatalf("expected server seq 1, got %d", ns.ServerSeq)
	}

	// Same player tries again -> not your turn
	_, err = e.ApplyMove(ns, engine.Move{
		PlayerID:  "px",
		Position:  1,
		MsgID:     "m2",
		ClientSeq: 2,
		Mark:      engine.X,
	})
	if err == nil || err != engine.ErrNotYourTurn {
		t.Fatalf("expected ErrNotYourTurn, got %v", err)
	}

	// O tries to play on a taken cell -> cell taken
	_, err = e.ApplyMove(ns, engine.Move{
		PlayerID:  "po",
		Position:  0,
		MsgID:     "m3",
		ClientSeq: 2,
		Mark:      engine.O,
	})
	if err == nil || err != engine.ErrCellTaken {
		t.Fatalf("expected ErrCellTaken, got %v", err)
	}
}

func TestApplyMove_OutOfOrderRejected(t *testing.T) {
	e := engine.NewEngine()
	state := e.NewGame()

	// ClientSeq should be ServerSeq+1 (0->1). Skip to 2 to simulate out-of-order.
	_, err := e.ApplyMove(state, engine.Move{
		PlayerID:  "px",
		Position:  0,
		MsgID:     "m1",
		ClientSeq: 2, // out of order
		Mark:      engine.X,
	})
	if err == nil || err != engine.ErrOutOfOrder {
		t.Fatalf("expected ErrOutOfOrder, got %v", err)
	}
}

func TestApplyMove_TerminalGuard(t *testing.T) {
	e := engine.NewEngine()
	// set a terminal board (X wins)
	b := engine.Board{
		engine.X, engine.X, engine.X,
		engine.O, engine.O, engine.Empty,
		engine.Empty, engine.Empty, engine.Empty,
	}
	s := engine.State{
		Board:     b,
		NextTurn:  engine.O,
		Status:    engine.XWins,
		ServerSeq: 5,
	}

	_, err := e.ApplyMove(s, engine.Move{
		PlayerID:  "po",
		Position:  5,
		MsgID:     "afterTerminal",
		ClientSeq: 6,
		Mark:      engine.O,
	})
	if err == nil || err != engine.ErrTerminal {
		t.Fatalf("expected ErrTerminal, got %v", err)
	}
}
