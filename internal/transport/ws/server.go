package ws

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
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
	GracePeriod  time.Duration
}

type Server interface{ http.Handler }

type server struct {
	cfg Config
	eng engine.Engine

	mu      sync.Mutex
	pending *conn

	seq    atomic.Int64
	rooms  map[string]*roomSlot // 4-digit code => slot
	tokens map[string]*tokenRef // rejoin token => reference
}

type roomSlot struct {
	code  string
	wait  *conn
	x, o  *conn
	room  match.Room
	xID   string // stable playerID for X
	oID   string // stable playerID for O

	// timers for WS-level notify (room already enforces grace internally)
	pendingResultTimer *time.Timer
}

type tokenRef struct {
	code     string
	mark     engine.Mark
	playerID string
	slot     *roomSlot
}

func NewServer(cfg Config, eng engine.Engine) Server {
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 2 * time.Second
	}
	return &server{
		cfg:    cfg,
        eng:    eng,
		rooms:  make(map[string]*roomSlot),
		tokens: make(map[string]*tokenRef),
	}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
		CompressionMode: websocket.CompressionDisabled,
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
	go c.writer()

	// Routing
	path := r.URL.Path
	if token := s.parseRejoinToken(path, r.URL.Query().Get("rejoin")); token != "" {
		s.handleRejoin(c, token)
		return
	}
	if code := s.parseRoomCode(path); code != "" {
		s.pairInRoom(c, code)
		return
	}
	_ = s.pairLegacy(c)
}

// ---------- Routing helpers ----------

func (s *server) parseRoomCode(path string) string {
	if !strings.HasPrefix(path, "/ws") {
		return ""
	}
	rest := strings.TrimPrefix(path, "/ws")
	rest = strings.TrimPrefix(rest, "/")
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" || rest == "rejoin" {
		return ""
	}
	if len(rest) != 4 {
		return ""
	}
	for _, ch := range rest {
		if ch < '0' || ch > '9' {
			return ""
		}
	}
	return rest
}

func (s *server) parseRejoinToken(path string, q string) string {
	// /ws/rejoin/<token> OR any ?rejoin=<token>
	if q != "" {
		return q
	}
	prefix := "/ws/rejoin/"
	if strings.HasPrefix(path, prefix) {
		tok := strings.TrimPrefix(path, prefix)
		return strings.TrimSuffix(tok, "/")
	}
	return ""
}

// ---------- Pairing / Legacy ----------

func (s *server) pairInRoom(c2 *conn, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slot := s.rooms[code]
	if slot == nil {
		slot = &roomSlot{code: code}
		s.rooms[code] = slot
	}

	if slot.x != nil && slot.o != nil {
		_ = c2.writeJSON(proto.Error{Type: "error", Code: "ROOM_FULL"})
		close(c2.send)
		return
	}

	if slot.wait == nil && slot.x == nil && slot.o == nil {
		slot.wait = c2
		log.Printf("[room %s] waiting for opponent (player=%s)", code, c2.id)
		return
	}

	var c1 *conn
	if slot.wait != nil {
		c1 = slot.wait
		slot.wait = nil
	} else {
		slot.wait = c2
		return
	}

	roomID := "ws-room-" + code + "-" + itoa64(s.seq.Add(1))
	rm := match.NewRoom(roomID, s.eng, match.Options{GracePeriod: s.cfg.GracePeriod})

	// assign marks
	c1.mark, c2.mark = engine.X, engine.O
	c1.peer, c2.peer = c2, c1
	c1.room, c2.room = rm, rm
	slot.x, slot.o, slot.room = c1, c2, rm
	slot.xID, slot.oID = c1.id, c2.id

	// register in room
	_ = rm.Join(context.Background(), match.Player{ID: slot.xID, Mark: engine.X})
	_ = rm.Join(context.Background(), match.Player{ID: slot.oID, Mark: engine.O})

	// rejoin tokens
	tokX := newToken()
	tokO := newToken()
	s.tokens[tokX] = &tokenRef{code: code, mark: engine.X, playerID: slot.xID, slot: slot}
	s.tokens[tokO] = &tokenRef{code: code, mark: engine.O, playerID: slot.oID, slot: slot}

	log.Printf("[room %s] paired X=%s O=%s grace=%s", code, slot.xID, slot.oID, s.cfg.GracePeriod)

	// Assigned + start
	_ = c1.writeJSON(proto.Assigned{Type: "assigned", You: c1.mark, RejoinToken: tokX})
	_ = c2.writeJSON(proto.Assigned{Type: "assigned", You: c2.mark, RejoinToken: tokO})

	st := rm.State()
	_ = c1.writeJSON(proto.Start{Type: "start", Board: boardToStrings(st.Board), YourTurn: st.NextTurn == c1.mark})
	_ = c2.writeJSON(proto.Start{Type: "start", Board: boardToStrings(st.Board), YourTurn: st.NextTurn == c2.mark})

	go c1.reader(rm, c1, c2, code)
	go c2.reader(rm, c2, c1, code)
}

func (s *server) pairLegacy(c2 *conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pending == nil {
		s.pending = c2
		log.Printf("[legacy] waiting for opponent (player=%s)", c2.id)
		return false
	}
	c1 := s.pending
	s.pending = nil

	roomID := "ws-room-" + itoa64(s.seq.Add(1))
	rm := match.NewRoom(roomID, s.eng, match.Options{GracePeriod: s.cfg.GracePeriod})

	c1.mark, c2.mark = engine.X, engine.O
	c1.peer, c2.peer = c2, c1
	c1.room, c2.room = rm, rm

	_ = rm.Join(context.Background(), match.Player{ID: c1.id, Mark: c1.mark})
	_ = rm.Join(context.Background(), match.Player{ID: c2.id, Mark: c2.mark})

	// tokens
	tokX := newToken()
	tokO := newToken()
	log.Printf("[legacy] paired X=%s O=%s grace=%s", c1.id, c2.id, s.cfg.GracePeriod)
	_ = c1.writeJSON(proto.Assigned{Type: "assigned", You: c1.mark, RejoinToken: tokX})
	_ = c2.writeJSON(proto.Assigned{Type: "assigned", You: c2.mark, RejoinToken: tokO})
	// map tokens back to pseudo-room slot for legacy (minimal)
	slot := &roomSlot{code: "legacy", x: c1, o: c2, room: rm, xID: c1.id, oID: c2.id}
	s.tokens[tokX] = &tokenRef{code: "legacy", mark: engine.X, playerID: c1.id, slot: slot}
	s.tokens[tokO] = &tokenRef{code: "legacy", mark: engine.O, playerID: c2.id, slot: slot}

	st := rm.State()
	_ = c1.writeJSON(proto.Start{Type: "start", Board: boardToStrings(st.Board), YourTurn: st.NextTurn == c1.mark})
	_ = c2.writeJSON(proto.Start{Type: "start", Board: boardToStrings(st.Board), YourTurn: st.NextTurn == c2.mark})

	go c1.reader(rm, c1, c2, "")
	go c2.reader(rm, c2, c1, "")
	return true
}

// ---------- Rejoin flow ----------

func (s *server) handleRejoin(c *conn, token string) {
	s.mu.Lock()
	ref, ok := s.tokens[token]
	if !ok || ref.slot == nil || ref.slot.room == nil {
		s.mu.Unlock()
		_ = c.writeJSON(proto.Error{Type: "error", Code: "BAD_TOKEN"})
		close(c.send)
		return
	}
	slot := ref.slot
	code := ref.code

	// Rebind this socket to the same playerID & mark
	c.id = ref.playerID
	c.mark = ref.mark
	c.room = slot.room

	// Pick peer pointer
	var peer *conn
	if ref.mark == engine.X {
		peer = slot.o
		slot.x = c
		slot.xID = c.id
	} else {
		peer = slot.x
		slot.o = c
		slot.oID = c.id
	}
	c.peer = peer
	s.mu.Unlock()

	// Inform logs and cancel any pending WS timer
	log.Printf("[room %s] rejoin success player=%s mark=%s", code, c.id, c.mark)
	if slot.pendingResultTimer != nil {
		slot.pendingResultTimer.Stop()
		slot.pendingResultTimer = nil
	}

	// Attach to room (this cancels room-level forfeit timer)
	_ = slot.room.Join(context.Background(), match.Player{ID: c.id, Mark: c.mark})

	// Send fresh state
	_ = c.writeJSON(proto.Assigned{Type: "assigned", You: c.mark}) // optional echo
	st := slot.room.State()
	_ = c.writeJSON(proto.Start{Type: "start", Board: boardToStrings(st.Board), YourTurn: st.NextTurn == c.mark})
	go c.reader(slot.room, c, peer, code)
}

// ---------- Connection ----------

type conn struct {
	id     string
	mark   engine.Mark
	ws     *websocket.Conn
	srv    *server
	peer   *conn
	room   match.Room
	send   chan []byte
	closed atomic.Bool
	msgSeq atomic.Int64
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

		// Quick input (1..9 or 0..8)
		if pos, ok, hint := parseHumanMove(trimWS(string(data))); ok {
			c.applyMove(rm, self, peer, pos, autoMsgID(self), autoClientSeq(rm))
			continue
		} else if hint != "" {
			_ = c.writeJSON(proto.Error{Type: "error", Code: "INVALID_TEXT", Detail: hint})
			continue
		}

		// JSON
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
		case "rejoin":
			if msg.Token == "" {
				_ = c.writeJSON(proto.Error{Type: "error", Code: "MISSING_TOKEN"})
				continue
			}
			c.srv.handleRejoin(c, msg.Token)
			return // new reader attached in handleRejoin
		case "leave":
			c.handleDisconnect(code)
			return
		case "ping":
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
	log.Printf("[room %s] disconnect player=%s mark=%s", code, c.id, c.mark)

	// Update room: this arms (or executes) room-level forfeit
	if c.room != nil {
		_ = c.room.Leave(context.Background(), c.id)
	}

	// Notify peer depending on grace
	if c.peer != nil && !c.peer.closed.Load() {
		if c.srv.cfg.GracePeriod > 0 {
			_ = c.peer.writeJSON(proto.OpponentLeft{Type: "opponent_left", GraceMS: c.srv.cfg.GracePeriod.Milliseconds()})
			// Schedule a result push after grace (room will already flip internally)
			if code != "" {
				c.srv.mu.Lock()
				slot := c.srv.rooms[code]
				if slot != nil {
					if slot.pendingResultTimer != nil {
						slot.pendingResultTimer.Stop()
					}
					slot.pendingResultTimer = time.AfterFunc(c.srv.cfg.GracePeriod+50*time.Millisecond, func() {
						st := c.room.State()
						if st.Status != engine.InProgress && c.peer != nil && !c.peer.closed.Load() {
							_ = c.peer.writeJSON(proto.Result{Type: "result", Status: outcomeText(st.Status)})
						}
					})
				}
				c.srv.mu.Unlock()
			}
		} else {
			// immediate result
			st := c.room.State()
			_ = c.peer.writeJSON(proto.Result{Type: "result", Status: outcomeText(st.Status)})
		}
	}

	close(c.send)
}

// ---------- Helpers ----------

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

func trimWS(s string) string { return strings.TrimFunc(s, unicode.IsSpace) }

// 1..9 -> 0..8 (human), also accept 0..8 (dev)
func parseHumanMove(s string) (pos int, ok bool, hint string) {
	if len(s) != 1 {
		return -1, false, ""
	}
	ch := s[0]
	if ch >= '0' && ch <= '8' {
		return int(ch - '0'), true, ""
	}
	if ch >= '1' && ch <= '9' {
		return int(ch - '1'), true, ""
	}
	if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
		return -1, false, "Type a single digit 1-9 (or 0-8) to place your mark."
	}
	return -1, false, ""
}

func autoClientSeq(r match.Room) int  { return r.State().ServerSeq + 1 }
func autoMsgID(c *conn) string       { return c.id + "-" + itoa64(c.msgSeq.Add(1)) }
func newToken() string {
	var b [16]byte
	_, _ = crypto_rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
