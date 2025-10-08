package match

import "context"

type RoomCreatedEvent struct {
	RoomID string
	X      Player
	O      Player
}

type Matchmaker interface {
	Enqueue(ctx context.Context, p Player) error
	Close() error
}

// onRoom is called whenever a room is created
func NewMatchmaker(onRoom func(RoomCreatedEvent)) Matchmaker {
	panic("TODO: implement match.NewMatchmaker")
}
