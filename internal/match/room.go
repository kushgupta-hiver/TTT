package match

import (
	"context"
	"sync"
	"time"

	"github.com/kushgupta-hiver/TTT/internal/engine"
)

type Player struct {
	ID   string
	Mark engine.Mark
}

type Options struct {
	GracePeriod time.Duration // 0 = immediate forfeit on leave
}

type Room interface {
	ID() string
	Join(ctx context.Context, p Player) error
	Submit(ctx context.Context, m engine.Move) (engine.State, error)
	Leave(ctx context.Context, playerID string) error
	State() engine.State
}

type room struct {
	id   string
	eng  engine.Engine
	opts Options

	mu    sync.Mutex
	state engine.State

	players   map[string]engine.Mark     // playerID -> mark
	marks     map[engine.Mark]string     // mark -> playerID
	hist      map[string]engine.State    // msgID -> state (idempotency)
	connected map[string]bool            // playerID -> currently connected
	timers    map[string]*time.Timer     // playerID -> grace timer
}

func NewRoom(id string, eng engine.Engine, opts Options) Room {
	return &room{
		id:        id,
		eng:       eng,
		opts:      opts,
		state:     eng.NewGame(),
		players:   make(map[string]engine.Mark, 2),
		marks:     make(map[engine.Mark]string, 2),
		hist:      make(map[string]engine.State, 8),
		connected: make(map[string]bool, 2),
		timers:    make(map[string]*time.Timer, 2),
	}
}

func (r *room) ID() string { return r.id }

func (r *room) Join(_ context.Context, p Player) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Rejoin existing player (idempotent on mark)
	if mk, ok := r.players[p.ID]; ok {
		if mk == p.Mark {
			r.connected[p.ID] = true
			// cancel any pending forfeit
			if t := r.timers[p.ID]; t != nil {
				if t.Stop() {
					delete(r.timers, p.ID)
				} else {
					// Timer already fired; state may already be terminal
					delete(r.timers, p.ID)
				}
			}
			return nil
		}
		// mark mismatch: ignore (could return error in stricter API)
		return nil
	}

	// New player
	// mark already claimed by someone else?
	if owner, ok := r.marks[p.Mark]; ok && owner != p.ID {
		// reject silently
		return nil
	}
	r.players[p.ID] = p.Mark
	r.marks[p.Mark] = p.ID
	r.connected[p.ID] = true
	return nil
}

func (r *room) Submit(_ context.Context, m engine.Move) (engine.State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Idempotency: if we've seen this MsgID, return the same state
	if s, ok := r.hist[m.MsgID]; ok {
		return s, nil
	}

	// Known player & mark must match
	mk, ok := r.players[m.PlayerID]
	if !ok || mk != m.Mark {
		return r.state, engine.ErrNotYourTurn
	}

	// Apply to current state
	ns, err := r.eng.ApplyMove(r.state, m)
	if err != nil {
		return r.state, err
	}

	// Commit + record
	r.state = ns
	r.hist[m.MsgID] = ns
	return ns, nil
}

func (r *room) Leave(_ context.Context, playerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	leaverMark, ok := r.players[playerID]
	if !ok {
		return nil
	}

	// Already terminal? nothing to do
	if r.state.Status != engine.InProgress {
		r.connected[playerID] = false
		return nil
	}

	// Mark as disconnected
	r.connected[playerID] = false

	// Immediate forfeit if grace is zero
	if r.opts.GracePeriod == 0 {
		if leaverMark == engine.X {
			r.state.Status = engine.OWins
		} else if leaverMark == engine.O {
			r.state.Status = engine.XWins
		}
		return nil
	}

	// With grace: schedule a forfeit if player doesn't return
	if r.timers[playerID] == nil {
		r.timers[playerID] = time.AfterFunc(r.opts.GracePeriod, func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			// If still disconnected and game is still running, award win to opponent
			if !r.connected[playerID] && r.state.Status == engine.InProgress {
				if leaverMark == engine.X {
					r.state.Status = engine.OWins
				} else {
					r.state.Status = engine.XWins
				}
			}
			delete(r.timers, playerID)
		})
	}
	return nil
}

func (r *room) State() engine.State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}
