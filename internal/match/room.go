package match

import (
	"context"
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

// TODO: real implementation later
func NewRoom(id string, eng engine.Engine, opts Options) Room {
	panic("TODO: implement match.NewRoom")
}
