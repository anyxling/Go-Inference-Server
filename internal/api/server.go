package api

import (
	"time"

	"github.com/monikaliu/go-inference-server/internal/worker"
)

type Server struct {
	generator worker.Generator
	timeouts  Timeouts
}

func NewServer(g worker.Generator, t Timeouts) *Server {
	return &Server{generator: g, timeouts: t}
}

type Timeouts struct {
	Total      time.Duration
	FirstToken time.Duration
	Idle       time.Duration
}

func DefaultTimeouts() Timeouts {
	return Timeouts{
		Total:      120 * time.Second,
		FirstToken: 30 * time.Second,
		Idle:       15 * time.Second,
	}
}
