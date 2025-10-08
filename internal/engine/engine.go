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

// TODO: real implementation later
func NewEngine() Engine { panic("TODO: implement engine.NewEngine") }
