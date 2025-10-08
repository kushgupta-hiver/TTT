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
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("TicTacToe WS server. Connect via /ws\n"))
	}))
	mux.Handle("/ws", ws.NewServer(ws.Config{}, engine.NewEngine()))

	log.Printf("listening on %s ...", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
