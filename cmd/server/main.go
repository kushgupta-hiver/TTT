package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/kushgupta-hiver/TTT/internal/engine"
	"github.com/kushgupta-hiver/TTT/internal/transport/ws"
)

func main() {
	addr := ":8000"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}

	// Grace period configurable via env (default 30s)
	grace := 30 * time.Second
	if v := os.Getenv("GRACE_SECONDS"); v != "" {
		if d, err := time.ParseDuration(v + "s"); err == nil {
			grace = d
		}
	}
	log.Printf("starting server addr=%s grace_period=%s", addr, grace)

	mux := http.NewServeMux()
	handler := ws.NewServer(ws.Config{
		WriteTimeout: 2 * time.Second,
		GracePeriod:  grace,
	}, engine.NewEngine())

	mux.Handle("/ws", handler)
	mux.Handle("/ws/", handler)

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("TicTacToe WS server.\nUse: ws://host/ws or ws://host/ws/1234\nRejoin: ws://host/ws/rejoin/<token>\n"))
	}))

	log.Printf("listening on %s ...", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
