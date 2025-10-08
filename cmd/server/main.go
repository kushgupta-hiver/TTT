package main

import (
	"log"
	"net/http"
	"os"

	"github.com/kushgupta-hiver/TTT/internal/engine"
	"github.com/kushgupta-hiver/TTT/internal/transport/ws"
)

func main() {
	addr := ":8000"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}

	mux := http.NewServeMux()

	// Create ONE ws handler instance
	wsHandler := ws.NewServer(ws.Config{}, engine.NewEngine())

	mux.Handle("/ws", wsHandler)   // matches exactly /ws
	mux.Handle("/ws/", wsHandler)  // matches /ws/<anything>, e.g., /ws/1234

	// Optional info page
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("TicTacToe WS server.\nTry: ws://<host>/ws  (auto-match)\nOr:  ws://<host>/ws/1234  (room)\n"))
	}))

	log.Printf("listening on %s ...", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
