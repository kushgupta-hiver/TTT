package ws

import (
	"context"
	"log"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

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
	cfg Config
	eng engine.Engine

	mu      sync.Mutex
	pending *conn // used only for legacy /ws pairing (no room code)

	seq   atomic.Int64
	rooms map[string]*roomSlot // 4-digit code => room slot
}

type roomSlot struct {
	waiting *conn      // one waiting player
	x, o    *conn      // active players once paired
	room    match.Room // created when second joins
}

func NewServer(cfg Config, eng engine.Engine) Server {
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 2 * time.Second
	}
	return &server{cfg: cfg, eng: eng, rooms: make(map[string]*roomSlot)}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: nil,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		log.Printf("websocket accept failed: %v (remote=%s path=%s)", err, r.RemoteAddr, r.URL.Path)
		http.Error(w, "failed to upgrade", http.StatusBadRequest)
		return
	}

	c := &conn{
		id:   "p" + itoa64(s.seq.Add(1)),
		ws:   ws,
		srv:  s,
		send: make(chan []byte, 32),
	}

	// single writer goroutine (ONLY writer)
	go c.writer()

	// Room code from path: /ws/<code>  (if empty -> legacy auto-match)
	if code := s.parseRoomCode(r.URL.Path); code != "" {
		s.pairInRoom(c, code)
		return
	}

	// Legacy: auto-match without room code
	_ = s.pairLegacy(c)
}

func (s *server) parseRoomCode(path string) string {
	// expect /ws or /ws/<code>
	if !strings.HasPrefix(path, "/ws") {
		return ""
	}
	rest := strings.TrimPrefix(path, "/ws")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return ""
	}
	// accept 4-digit numeric, but don't be strict here
	if len(rest) == 4 {
		for _, ch := range rest {
			if ch < '0' || ch > '9' {
				return ""
			}
		}
		return rest
	}
	return ""
}

func (s *server) pairInRoom(c2 *conn, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slot := s.rooms[code]
	if slot == nil {
		slot = &roomSlot{}
		s.rooms[code] = slot
	}

	// If already 2 players active -> reject (room full)
	if slot.x != nil && slot.o != nil {
		_ = c2.writeJSON(proto.Error{Type: "error", Code: "ROOM_FULL"})
		close(c2.send)
		return
	}

	// If no one waiting, park this conn
	if slot.waiting == nil && slot.x == nil && slot.o == nil {
		slot.waiting = c2
		// Wait for opponent; do NOT start a reader yet
		return
	}

	// Someone waiting -> pair now
	var c1 *conn
	if slot.waiting != nil {
		c1 = slot.waiting
		slot.waiting = nil
	} else {
		// corrupt state: unexpected, but fallback to wait
		slot.waiting = c2
		return
	}

	// Create a fresh match.Room for this code
	roomID := "ws-room-" + code + "-" + itoa64(s.seq.Add(1))
	rm := match.NewRoom(roomID, s.eng, match.Options{GracePeriod: 0})

	// Assign marks: first=X, second=O
	c1.mark, c2.mark = engine.X, engine.O
	c1.peer, c2.peer = c2, c1
	c1.room, c2.room = rm, rm
	slot.x, slot.o, slot.room = c1, c2, rm

	_ = rm.Join(context.Background(), match.Player{ID: c1.id, Mark: c1.mark})
	_ = rm.Join(context.Background(), match.Player{ID: c2.id, Mark: c2.mark})

	// Assigned + start
	_ = c1.writeJSON(proto.Assigned{Type: "assigned", You: c1.mark})
	_ = c2.writeJSON(proto.Assigned{Type: "assigned", You: c2.mark})

	st := rm.State()
	_ = c1.writeJSON(proto.Start{Type: "start", Board: boardToStrings(st.Board), YourTurn: st.NextTurn == c1.mark})
	_ = c2.writeJSON(proto.Start{Type: "start", Board: boardToStrings(st.Board), YourTurn: st.NextTurn == c2.mark})

	// Readers only after pairing
	go c1.reader(rm, c1, c2, code)
	go c2.reader(rm, c2, c1, code)
}

func (s *server) pairLegacy(c2 *conn) bool {
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

	_ = c1.writeJSON(proto.Assigned{Type: "assigned", You: c1.mark})
	_ = c2.writeJSON(proto.Assigned{Type: "assigned", You: c2.mark})

	st := rm.State()
	_ = c1.writeJSON(proto.Start{Type: "start", Board: boardToStrings(st.Board), YourTurn: st.NextTurn == c1.mark})
	_ = c2.writeJSON(proto.Start{Type: "start", Board: boardToStrings(st.Board), YourTurn: st.NextTurn == c2.mark})

	go c1.reader(rm, c1, c2, "")
	go c2.reader(rm, c2, c1, "")
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

	msgSeq atomic.Int64 // for auto MsgIDs
}

func (c *conn) writer() {
	for msg := range c.send {
		ctx, cancel := context.WithTimeout(context.Background(), c.srv.cfg.WriteTimeout)
		_ = c.ws.Write(ctx, websocket.MessageText, msg)
		cancel()
	}
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

func (c *conn) reader(rm match.Room, self *conn, peer *conn, code string) {
	ctx := context.Background()
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			c.handleDisconnect(code)
			return
		}
		if typ != websocket.MessageText {
			continue
		}

		// --- Human-friendly: a single digit "0..8" is a move ---
		if pos, ok := parseSingleDigit(trimWS(string(data))); ok {
			c.applyMove(rm, self, peer, pos, autoMsgID(self), autoClientSeq(rm))
			continue
		}

		// --- JSON fallback (original protocol) ---
		var msg proto.ClientMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			_ = c.writeJSON(proto.Error{Type: "error", Code: "BAD_JSON", Detail: err.Error()})
			continue
		}
		switch strings.ToLower(msg.Type) {
		case "move":
			if msg.Position == nil {
				_ = c.writeJSON(proto.Error{Type: "error", Code: "INVALID", Detail: "missing position"})
				continue
			}
			seq := msg.ClientSeq
			if seq == 0 {
				seq = autoClientSeq(rm)
			}
			id := msg.MsgID
			if id == "" {
				id = autoMsgID(self)
			}
			c.applyMove(rm, self, peer, *msg.Position, id, seq)
		case "leave":
			c.handleDisconnect(code)
			return
		case "ping":
			// no-op
		default:
			_ = c.writeJSON(proto.Error{Type: "error", Code: "UNKNOWN_TYPE"})
		}
	}
}

func (c *conn) applyMove(rm match.Room, self, peer *conn, pos int, msgID string, clientSeq int) {
	ctx := context.Background()
	mv := engine.Move{
		PlayerID:  self.id,
		Position:  pos,
		MsgID:     msgID,
		ClientSeq: clientSeq,
		Mark:      self.mark,
	}
	ns, err := rm.Submit(ctx, mv)
	if err != nil {
		_ = c.writeJSON(proto.Error{Type: "error", Code: engineErrCode(err), Detail: err.Error()})
		return
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
}

func (c *conn) handleDisconnect(code string) {
	if c.closed.Swap(true) {
		return
	}
	// Forfeit if in a room, notify peer
	if c.room != nil && c.peer != nil && !c.peer.closed.Load() {
		_ = c.room.Leave(context.Background(), c.id)
		st := c.room.State()
		_ = c.peer.writeJSON(proto.Result{Type: "result", Status: outcomeText(st.Status)})
	}

	// If this connection belongs to a room code, tidy slot if both gone
	if code != "" {
		c.srv.mu.Lock()
		slot := c.srv.rooms[code]
		if slot != nil {
			if slot.x == c {
				slot.x = nil
			}
			if slot.o == c {
				slot.o = nil
			}
			if slot.x == nil && slot.o == nil && slot.waiting == nil {
				delete(c.srv.rooms, code)
			}
		}
		c.srv.mu.Unlock()
	}

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

// -------- helpers for human-friendly input --------

func trimWS(s string) string {
	return strings.TrimFunc(s, unicode.IsSpace)
}

func parseSingleDigit(s string) (int, bool) {
	if len(s) != 1 {
		return 0, false
	}
	if s[0] < '0' || s[0] > '8' {
		return 0, false
	}
	return int(s[0] - '0'), true
}

func autoClientSeq(r match.Room) int {
	return r.State().ServerSeq + 1
}

func autoMsgID(c *conn) string {
	n := c.msgSeq.Add(1)
	return c.id + "-" + itoa64(n)
}
