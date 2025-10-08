package match

import (
	"context"
	"sync/atomic"
	"time"
)

type RoomCreatedEvent struct {
	RoomID string
	X      Player
	O      Player
}

type Matchmaker interface {
	Enqueue(ctx context.Context, p Player) error
	Close() error
}

type matchmaker struct {
	q       chan Player
	done    chan struct{}
	onRoom  func(RoomCreatedEvent)
	counter atomic.Int64
}

func NewMatchmaker(onRoom func(RoomCreatedEvent)) Matchmaker {
	m := &matchmaker{
		q:      make(chan Player, 1024),
		done:   make(chan struct{}),
		onRoom: onRoom,
	}
	go m.loop()
	return m
}

func (m *matchmaker) Enqueue(ctx context.Context, p Player) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.done:
		return context.Canceled
	case m.q <- p:
		return nil
	}
}

func (m *matchmaker) Close() error {
	close(m.done)
	return nil
}

func (m *matchmaker) loop() {
	var pending *Player

	for {
		select {
		case <-m.done:
			return
		case p := <-m.q:
			if pending == nil {
				// keep the first player
				pp := p
				pending = &pp
				continue
			}
			// pair pending with p
			roomID := m.newRoomID()
			first := *pending
			second := p

			// deterministic roles: first -> X, second -> O
			ev := RoomCreatedEvent{
				RoomID: roomID,
				X:      Player{ID: first.ID, Mark: "X"},
				O:      Player{ID: second.ID, Mark: "O"},
			}
			m.onRoom(ev)
			pending = nil
		case <-time.After(5 * time.Millisecond):
			// small tick to allow default case to yield
		}
	}
}

func (m *matchmaker) newRoomID() string {
	val := m.counter.Add(1)
	return "room-" + itoa64(val)
}

func itoa64(n int64) string {
	// small, allocation-light positive int to string
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
