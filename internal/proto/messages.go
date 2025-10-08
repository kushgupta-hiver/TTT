package proto

import "github.com/kushgupta-hiver/TTT/internal/engine"

// ---- Client -> Server ----
type ClientMsg struct {
	Type      string `json:"type"`                 // "join" | "move" | "leave" | "ping" | "rejoin"
	Position  *int   `json:"position,omitempty"`   // for "move"
	MsgID     string `json:"msgId,omitempty"`      // idempotency
	ClientSeq int    `json:"clientSeq,omitempty"`  // ordering
	Token     string `json:"token,omitempty"`      // for "rejoin" (optional if sent as JSON)
}

// ---- Server -> Client ----
type Assigned struct {
	Type        string      `json:"type"` // "assigned"
	You         engine.Mark `json:"you"`
	RejoinToken string      `json:"rejoin_token,omitempty"`
}

type Start struct {
	Type     string    `json:"type"` // "start"
	Board    [9]string `json:"board"`
	YourTurn bool      `json:"your_turn"`
}

type State struct {
	Type      string      `json:"type"` // "state"
	Board     [9]string   `json:"board"`
	NextTurn  engine.Mark `json:"next_turn"`
	LastMove  *MoveInfo   `json:"last_move,omitempty"`
	ServerSeq int         `json:"serverSeq"`
}

type MoveInfo struct {
	By  engine.Mark `json:"by"`
	Pos int         `json:"pos"`
}

type Result struct {
	Type   string `json:"type"` // "result"
	Status string `json:"status"`
}

// Informational message when opponent disconnects but grace is active.
type OpponentLeft struct {
	Type    string `json:"type"`     // "opponent_left"
	GraceMS int64  `json:"grace_ms"` // remaining grace milliseconds (approx)
}

type Error struct {
	Type   string `json:"type"` // "error"
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}
