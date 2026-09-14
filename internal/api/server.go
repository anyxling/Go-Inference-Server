package api

import (
	"github.com/monikaliu/go-inference-server/internal/worker"
)

type Server struct {
	generator worker.Generator
}

func NewServer(g worker.Generator) *Server {
	return &Server{generator: g}
}
