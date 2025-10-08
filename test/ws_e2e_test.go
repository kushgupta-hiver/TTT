package test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kushgupta-hiver/TTT/internal/engine"
	"github.com/kushgupta-hiver/TTT/internal/proto"
	"github.com/kushgupta-hiver/TTT/internal/transport/ws"
	"nhooyr.io/websocket"
)

// helper to make ws:// URL from httptest server
func wsURLFromHTTP(u string) string {
	return "ws" + strings.TrimPrefix(u, "http")
}

func TestWS_AssignStartAndFirstMove(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Spin server with handler
	s := ws.NewServer(ws.Config{}, engine.NewEngine())
	ts := httptest.NewServer(s)
	defer ts.Close()

	u := wsURLFromHTTP(ts.URL)

	// Connect two clients
	cx, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatalf("dial X: %v", err)
	}
	defer cx.Close(websocket.StatusNormalClosure, "bye")

	co, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatalf("dial O: %v", err)
	}
	defer co.Close(websocket.StatusNormalClosure, "bye")

	// Expect "assigned" for both
	var ax, ao proto.Assigned

	readJSON := func(c *websocket.Conn, v any) error {
		_, data, err := c.Read(ctx)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, v)
	}

	if err := readJSON(cx, &ax); err != nil {
		t.Fatalf("read assigned X: %v", err)
	}
	if ax.Type != "assigned" || (ax.You != engine.X && ax.You != engine.O) {
		t.Fatalf("unexpected assigned X: %+v", ax)
	}

	if err := readJSON(co, &ao); err != nil {
		t.Fatalf("read assigned O: %v", err)
	}
	if ao.Type != "assigned" || (ao.You != engine.X && ao.You != engine.O) {
		t.Fatalf("unexpected assigned O: %+v", ao)
	}

	// Expect "start" next for both
	var sx, so proto.Start
	if err := readJSON(cx, &sx); err != nil {
		t.Fatalf("read start X: %v", err)
	}
	if err := readJSON(co, &so); err != nil {
		t.Fatalf("read start O: %v", err)
	}
	if sx.Type != "start" || so.Type != "start" {
		t.Fatalf("expected start messages")
	}

	// Determine who is X (first turn)
	var xConn, oConn *websocket.Conn
	if ax.You == engine.X {
		xConn, oConn = cx, co
	} else {
		xConn, oConn = co, cx
	}

	// X sends a move at position 0 (clientSeq 1)
	move := proto.ClientMsg{
		Type:      "move",
		Position:  intPtr(0),
		MsgID:     "m1",
		ClientSeq: 1,
	}
	if err := xConn.Write(ctx, websocket.MessageText, mustJSON(move)); err != nil {
		t.Fatalf("write move X: %v", err)
	}

	// Both should see a "state" with next_turn O and serverSeq=1
	var st1, st2 proto.State
	if err := readJSON(xConn, &st1); err != nil {
		t.Fatalf("read state on X: %v", err)
	}
	if err := readJSON(oConn, &st2); err != nil {
		t.Fatalf("read state on O: %v", err)
	}
	if st1.Type != "state" || st2.Type != "state" {
		t.Fatalf("expected state type, got %q and %q", st1.Type, st2.Type)
	}
	if st1.ServerSeq != 1 || st2.ServerSeq != 1 {
		t.Fatalf("expected serverSeq 1, got %d and %d", st1.ServerSeq, st2.ServerSeq)
	}
	if st1.Board[0] != "X" || st2.Board[0] != "X" {
		t.Fatalf("expected X at position 0 on both boards")
	}
	if st1.NextTurn != engine.O || st2.NextTurn != engine.O {
		t.Fatalf("expected next_turn O")
	}
}

func TestWS_DisconnectForfeit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	s := ws.NewServer(ws.Config{}, engine.NewEngine())
	ts := httptest.NewServer(s)
	defer ts.Close()
	u := wsURLFromHTTP(ts.URL)

	c1, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatalf("dial 1: %v", err)
	}
	defer c1.Close(websocket.StatusNormalClosure, "bye")

	c2, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}
	defer c2.Close(websocket.StatusNormalClosure, "bye")

	// drain assigned+start for both
	drain2 := func(c *websocket.Conn) {
		for i := 0; i < 2; i++ {
			_, _, _ = c.Read(ctx)
		}
	}
	drain2(c1)
	drain2(c2)

	// Close c2 => c1 should receive a "result" with "<X|O>_WINS"
	_ = c2.Close(websocket.StatusNormalClosure, "leaving")

	// Read next message on c1 and expect result
	_, data, err := c1.Read(ctx)
	if err != nil {
		t.Fatalf("read on c1: %v", err)
	}
	var res proto.Result
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if res.Type != "result" || (!strings.Contains(res.Status, "WINS") && !strings.Contains(res.Status, "wins")) {
		t.Fatalf("expected result with wins, got %+v", res)
	}
}

func intPtr(i int) *int { return &i }

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
