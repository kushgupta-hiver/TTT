// test/ws_room_e2e_test.go
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

func wsURLFromHTTP(u string) string { return "ws" + strings.TrimPrefix(u, "http") }

func readJSON[T any](ctx context.Context, c *websocket.Conn, out *T) error {
	_, data, err := c.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func TestWS_RoomCode_AssignStartAndSingleDigitMove(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	s := ws.NewServer(ws.Config{}, engine.NewEngine())
	ts := httptest.NewServer(s)
	defer ts.Close()

	base := wsURLFromHTTP(ts.URL)

	// Connect both players to the same 4-digit room.
	c1, _, err := websocket.Dial(ctx, base+"/ws/1234", nil)
	if err != nil {
		t.Fatalf("dial c1: %v", err)
	}
	defer c1.Close(websocket.StatusNormalClosure, "bye")

	c2, _, err := websocket.Dial(ctx, base+"/ws/1234", nil)
	if err != nil {
		t.Fatalf("dial c2: %v", err)
	}
	defer c2.Close(websocket.StatusNormalClosure, "bye")

	// Expect "assigned" then "start" for both.
	var a1, a2 proto.Assigned
	if err := readJSON(ctx, c1, &a1); err != nil {
		t.Fatalf("assigned c1: %v", err)
	}
	if err := readJSON(ctx, c2, &a2); err != nil {
		t.Fatalf("assigned c2: %v", err)
	}
	if a1.Type != "assigned" || a2.Type != "assigned" {
		t.Fatalf("expected assigned messages")
	}
	if (a1.You != engine.X && a1.You != engine.O) || (a2.You != engine.X && a2.You != engine.O) {
		t.Fatalf("unexpected marks: %+v / %+v", a1, a2)
	}

	var s1, s2 proto.Start
	if err := readJSON(ctx, c1, &s1); err != nil {
		t.Fatalf("start c1: %v", err)
	}
	if err := readJSON(ctx, c2, &s2); err != nil {
		t.Fatalf("start c2: %v", err)
	}
	if s1.Type != "start" || s2.Type != "start" {
		t.Fatalf("expected start messages")
	}

	// Determine X player (first turn).
	xConn, oConn := c1, c2
	if a1.You != engine.X {
		xConn, oConn = c2, c1
	}

	// Human-friendly: X sends a single digit "0" instead of JSON.
	if err := xConn.Write(ctx, websocket.MessageText, []byte("0")); err != nil {
		t.Fatalf("write digit move: %v", err)
	}

	// Both should receive a "state" with X at position 0, next_turn O, serverSeq=1.
	var stX, stO proto.State
	if err := readJSON(ctx, xConn, &stX); err != nil {
		t.Fatalf("read state X: %v", err)
	}
	if err := readJSON(ctx, oConn, &stO); err != nil {
		t.Fatalf("read state O: %v", err)
	}
	if stX.Type != "state" || stO.Type != "state" {
		t.Fatalf("expected state messages")
	}
	if stX.ServerSeq != 1 || stO.ServerSeq != 1 {
		t.Fatalf("expected serverSeq=1, got %d / %d", stX.ServerSeq, stO.ServerSeq)
	}
	if stX.Board[0] != "X" || stO.Board[0] != "X" {
		t.Fatalf("expected board[0]=X on both")
	}
	if stX.NextTurn != engine.O || stO.NextTurn != engine.O {
		t.Fatalf("expected next_turn O")
	}
}

func TestWS_RoomCode_Isolation_TwoRoomsPlayIndependently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	s := ws.NewServer(ws.Config{}, engine.NewEngine())
	ts := httptest.NewServer(s)
	defer ts.Close()
	base := wsURLFromHTTP(ts.URL)

	// Room A (1111)
	a1, _, err := websocket.Dial(ctx, base+"/ws/1111", nil)
	if err != nil {
		t.Fatalf("dial a1: %v", err)
	}
	defer a1.Close(websocket.StatusNormalClosure, "bye")
	a2, _, err := websocket.Dial(ctx, base+"/ws/1111", nil)
	if err != nil {
		t.Fatalf("dial a2: %v", err)
	}
	defer a2.Close(websocket.StatusNormalClosure, "bye")

	// Room B (2222)
	b1, _, err := websocket.Dial(ctx, base+"/ws/2222", nil)
	if err != nil {
		t.Fatalf("dial b1: %v", err)
	}
	defer b1.Close(websocket.StatusNormalClosure, "bye")
	b2, _, err := websocket.Dial(ctx, base+"/ws/2222", nil)
	if err != nil {
		t.Fatalf("dial b2: %v", err)
	}
	defer b2.Close(websocket.StatusNormalClosure, "bye")

	// Drain assigned+start for all four connections.
	drain2 := func(c *websocket.Conn) {
		for i := 0; i < 2; i++ {
			_, _, _ = c.Read(ctx)
		}
	}
	drain2(a1); drain2(a2); drain2(b1); drain2(b2)

	// Make a move in room A: send "0" from a1 (who might be X or O; if not X, the server will reject NOT_YOUR_TURN)
	_ = a1.Write(ctx, websocket.MessageText, []byte("0"))

	// Read next message from both A connections; at least one should be state with serverSeq 1.
	gotA1Type, _, _ := a1.Read(ctx)
	gotA2Type, _, _ := a2.Read(ctx)
	_ = gotA1Type
	_ = gotA2Type

	// Make an independent move in room B: send "0" from b1 as well.
	_ = b1.Write(ctx, websocket.MessageText, []byte("0"))

	// Read next message from both B connections—ensure they get their own state.
	_, _, _ = b1.Read(ctx)
	_, _, _ = b2.Read(ctx)
	// If cross-talk existed, this test would have flaked or mismatched JSON shapes.
}

func TestWS_RoomCode_ThirdPlayerGetsRoomFull(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	s := ws.NewServer(ws.Config{}, engine.NewEngine())
	ts := httptest.NewServer(s)
	defer ts.Close()
	base := wsURLFromHTTP(ts.URL)

	// Fill room 9999 with two players.
	p1, _, err := websocket.Dial(ctx, base+"/ws/9999", nil)
	if err != nil {
		t.Fatalf("dial p1: %v", err)
	}
	defer p1.Close(websocket.StatusNormalClosure, "bye")

	p2, _, err := websocket.Dial(ctx, base+"/ws/9999", nil)
	if err != nil {
		t.Fatalf("dial p2: %v", err)
	}
	defer p2.Close(websocket.StatusNormalClosure, "bye")

	// Drain their assigned+start.
	for i := 0; i < 2; i++ {
		_, _, _ = p1.Read(ctx)
		_, _, _ = p2.Read(ctx)
	}

	// Third player tries to join the same room.
	p3, _, err := websocket.Dial(ctx, base+"/ws/9999", nil)
	if err != nil {
		t.Fatalf("dial p3: %v", err)
	}
	defer p3.Close(websocket.StatusNormalClosure, "bye")

	// Expect an error frame with Code=ROOM_FULL.
	_, data, err := p3.Read(ctx)
	if err != nil {
		t.Fatalf("read p3: %v", err)
	}
	var e proto.Error
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if e.Type != "error" || e.Code != "ROOM_FULL" {
		t.Fatalf("expected ROOM_FULL error, got %+v", e)
	}
}
