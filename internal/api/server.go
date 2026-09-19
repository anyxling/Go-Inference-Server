package api

import (
	"time"

	"github.com/monikaliu/go-inference-server/internal/worker"
)

type Server struct {
	generator worker.Generator
	config    Config
	slots     chan struct{}
}

func NewServer(g worker.Generator, cfg Config) *Server {
	slots := make(chan struct{}, cfg.MaxActive)
	return &Server{generator: g, config: cfg, slots: slots}
}

type Config struct {
	Timeouts  Timeouts
	MaxActive int
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

func DefaultConfig() Config {
	return Config{
		Timeouts:  DefaultTimeouts(),
		MaxActive: 2,
	}
}
