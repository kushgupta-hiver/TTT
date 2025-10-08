package ws

import (
	"net/http"

	"github.com/kushgupta-hiver/TTT/internal/engine"
)

// Config allows future tuning (paths, timeouts, buffer sizes, etc.)
type Config struct{}

// Server is an HTTP handler that upgrades to WebSocket.
type Server interface {
	http.Handler
}

type server struct {
	cfg Config
	eng engine.Engine
}

func NewServer(cfg Config, eng engine.Engine) Server {
	return &server{cfg: cfg, eng: eng}
}

// ServeHTTP currently returns 501 until implemented (TDD red phase).
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "websocket not implemented", http.StatusNotImplemented)
}
