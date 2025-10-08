package engine

import "errors"

type Mark string

const (
	Empty Mark = ""
	X     Mark = "X"
	O     Mark = "O"
)

type Outcome int

const (
	InProgress Outcome = iota
	XWins
	OWins
	Draw
)

var (
	ErrNotYourTurn     = errors.New("not your turn")
	ErrInvalidPosition = errors.New("invalid position")
	ErrCellTaken       = errors.New("cell already taken")
	ErrOutOfOrder      = errors.New("out of order client seq")
	ErrTerminal        = errors.New("game already finished")
)

type Board [9]Mark

type Move struct {
	PlayerID  string
	Position  int
	MsgID     string
	ClientSeq int
	Mark      Mark
}

type MoveInfo struct {
	By  Mark
	Pos int
}

type State struct {
	Board     Board
	NextTurn  Mark
	Status    Outcome
	ServerSeq int
	LastMove  *MoveInfo
}

type Engine interface {
	NewGame() State
	ApplyMove(s State, m Move) (State, error)
	Outcome(b Board) Outcome
}

type engineImpl struct{}

func NewEngine() Engine { return &engineImpl{} }

func (e *engineImpl) NewGame() State {
	return State{
		Board:     Board{},
		NextTurn:  X,
		Status:    InProgress,
		ServerSeq: 0,
		LastMove:  nil,
	}
}

func (e *engineImpl) ApplyMove(s State, m Move) (State, error) {
	if s.Status != InProgress {
		return s, ErrTerminal
	}
	// sequencing: client must send next number
	if m.ClientSeq != s.ServerSeq+1 {
		return s, ErrOutOfOrder
	}
	// turn enforcement
	if m.Mark != s.NextTurn {
		return s, ErrNotYourTurn
	}
	// position validity
	if m.Position < 0 || m.Position >= len(s.Board) {
		return s, ErrInvalidPosition
	}
	if s.Board[m.Position] != Empty {
		return s, ErrCellTaken
	}

	// apply
	ns := s
	ns.Board[m.Position] = m.Mark
	ns.LastMove = &MoveInfo{By: m.Mark, Pos: m.Position}
	ns.ServerSeq++

	// recompute outcome
	ns.Status = e.Outcome(ns.Board)
	if ns.Status == InProgress {
		if s.NextTurn == X {
			ns.NextTurn = O
		} else {
			ns.NextTurn = X
		}
	} else {
		// terminal; next turn doesn't matter but keep current value
	}

	return ns, nil
}

func (e *engineImpl) Outcome(b Board) Outcome {
	lines := [8][3]int{
		{0, 1, 2}, {3, 4, 5}, {6, 7, 8},
		{0, 3, 6}, {1, 4, 7}, {2, 5, 8},
		{0, 4, 8}, {2, 4, 6},
	}

	for _, ln := range lines {
		a, b1, c := b[ln[0]], b[ln[1]], b[ln[2]]
		if a != Empty && a == b1 && b1 == c {
			if a == X {
				return XWins
			}
			return OWins
		}
	}

	// any empty? still in progress
	for _, v := range b {
		if v == Empty {
			return InProgress
		}
	}
	return Draw
}
