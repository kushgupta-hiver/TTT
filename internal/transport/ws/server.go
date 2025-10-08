package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kushgupta-hiver/TTT/internal/engine"
	"github.com/kushgupta-hiver/TTT/internal/match"
	"github.com/kushgupta-hiver/TTT/internal/proto"
	"nhooyr.io/websocket"
)

type Config struct {
	WriteTimeout time.Duration
}

type Server interface{ http.Handler }

type server struct {
	cfg     Config
	eng     engine.Engine
	mu      sync.Mutex
	pending *conn
	seq     atomic.Int64
}

func NewServer(cfg Config, eng engine.Engine) Server {
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 2 * time.Second
	}
	return &server{cfg: cfg, eng: eng}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
		CompressionMode: websocket.CompressionDisabled, // keep RSV bits simple
	})
	if err != nil {
		http.Error(w, "failed to upgrade", http.StatusBadRequest)
		return
	}

	c := &conn{
		id:   "p" + itoa64(s.seq.Add(1)),
		ws:   ws,
		srv:  s,
		send: make(chan []byte, 32),
	}
	// single writer goroutine (the ONLY writer to ws)
	go c.writer()

	// Try pair. IMPORTANT: do NOT start a reader for pending.
	_ = s.pair(c)
}

func (s *server) pair(c2 *conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pending == nil {
		s.pending = c2
		return false
	}

	c1 := s.pending
	s.pending = nil

	roomID := "ws-room-" + itoa64(s.seq.Add(1))
	rm := match.NewRoom(roomID, s.eng, match.Options{GracePeriod: 0})

	c1.mark, c2.mark = engine.X, engine.O
	c1.peer, c2.peer = c2, c1
	c1.room, c2.room = rm, rm

	_ = rm.Join(context.Background(), match.Player{ID: c1.id, Mark: c1.mark})
	_ = rm.Join(context.Background(), match.Player{ID: c2.id, Mark: c2.mark})

	// Initial messages via writer queue
	_ = c1.writeJSON(proto.Assigned{Type: "assigned", You: c1.mark})
	_ = c2.writeJSON(proto.Assigned{Type: "assigned", You: c2.mark})

	st := rm.State()
	_ = c1.writeJSON(proto.Start{Type: "start", Board: boardToStrings(st.Board), YourTurn: st.NextTurn == c1.mark})
	_ = c2.writeJSON(proto.Start{Type: "start", Board: boardToStrings(st.Board), YourTurn: st.NextTurn == c2.mark})

	// Start readers ONLY now that paired
	go c1.reader(rm, c1, c2)
	go c2.reader(rm, c2, c1)
	return true
}

type conn struct {
	id     string
	mark   engine.Mark
	ws     *websocket.Conn
	srv    *server
	peer   *conn
	room   match.Room
	send   chan []byte
	closed atomic.Bool
}

func (c *conn) writer() {
	// single writer loop with deadline
	for msg := range c.send {
		ctx, cancel := context.WithTimeout(context.Background(), c.srv.cfg.WriteTimeout)
		_ = c.ws.Write(ctx, websocket.MessageText, msg)
		cancel()
	}
	// after channel is closed, send a normal close frame once
	_ = c.ws.Close(websocket.StatusNormalClosure, "bye")
}

func (c *conn) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case c.send <- b:
		return nil
	case <-time.After(c.srv.cfg.WriteTimeout):
		return context.DeadlineExceeded
	}
}

func (c *conn) reader(rm match.Room, self *conn, peer *conn) {
	ctx := context.Background()
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			c.handleDisconnect()
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var msg proto.ClientMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			_ = c.writeJSON(proto.Error{Type: "error", Code: "BAD_JSON", Detail: err.Error()})
			continue
		}
		switch strings.ToLower(msg.Type) {
		case "move":
			if self == nil || rm == nil || peer == nil {
				_ = c.writeJSON(proto.Error{Type: "error", Code: "NOT_READY"})
				continue
			}
			if msg.Position == nil {
				_ = c.writeJSON(proto.Error{Type: "error", Code: "INVALID", Detail: "missing position"})
				continue
			}
			mv := engine.Move{
				PlayerID:  self.id,
				Position:  *msg.Position,
				MsgID:     msg.MsgID,
				ClientSeq: msg.ClientSeq,
				Mark:      self.mark,
			}
			ns, err := rm.Submit(ctx, mv)
			if err != nil {
				_ = c.writeJSON(proto.Error{Type: "error", Code: engineErrCode(err), Detail: err.Error()})
				continue
			}
			stateMsg := proto.State{
				Type:      "state",
				Board:     boardToStrings(ns.Board),
				NextTurn:  ns.NextTurn,
				ServerSeq: ns.ServerSeq,
			}
			_ = self.writeJSON(stateMsg)
			_ = peer.writeJSON(stateMsg)

			if ns.Status != engine.InProgress {
				res := proto.Result{Type: "result", Status: outcomeText(ns.Status)}
				_ = self.writeJSON(res)
				_ = peer.writeJSON(res)
			}
		case "leave":
			c.handleDisconnect()
			return
		case "ping":
			// no-op
		default:
			_ = c.writeJSON(proto.Error{Type: "error", Code: "UNKNOWN_TYPE"})
		}
	}
}

func (c *conn) handleDisconnect() {
	if c.closed.Swap(true) {
		return
	}
	// Forfeit if in a room, notify peer
	if c.room != nil && c.peer != nil && !c.peer.closed.Load() {
		_ = c.room.Leave(context.Background(), c.id)
		st := c.room.State()
		_ = c.peer.writeJSON(proto.Result{Type: "result", Status: outcomeText(st.Status)})
	}
	// Stop writer (which will send a Close frame once)
	close(c.send)
}

func boardToStrings(b engine.Board) [9]string {
	var out [9]string
	for i := 0; i < len(b); i++ {
		out[i] = string(b[i])
	}
	return out
}

func outcomeText(o engine.Outcome) string {
	switch o {
	case engine.XWins:
		return "X wins!"
	case engine.OWins:
		return "O wins!"
	case engine.Draw:
		return "Draw"
	default:
		return "In progress"
	}
}

func engineErrCode(err error) string {
	switch {
	case errors.Is(err, engine.ErrNotYourTurn):
		return "NOT_YOUR_TURN"
	case errors.Is(err, engine.ErrInvalidPosition):
		return "INVALID_POSITION"
	case errors.Is(err, engine.ErrCellTaken):
		return "CELL_TAKEN"
	case errors.Is(err, engine.ErrOutOfOrder):
		return "OUT_OF_ORDER"
	case errors.Is(err, engine.ErrTerminal):
		return "TERMINAL"
	default:
		return "UNKNOWN"
	}
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(buf[i:])
}
