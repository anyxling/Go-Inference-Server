package api

import (
	"fmt"
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
	Timeouts             Timeouts
	MaxActive            int
	MaxOutputTokensLimit int
	RequestBodyLimit     int64
}

func (cfg Config) Validate() error {
	if cfg.MaxActive < 1 {
		return fmt.Errorf("max-active should be at least %d, got %d", 1, cfg.MaxActive)
	}
	if cfg.Timeouts.Total <= 0 {
		return fmt.Errorf("total-timeout should be positive, got %d", cfg.Timeouts.Total)
	}
	if cfg.Timeouts.FirstToken <= 0 {
		return fmt.Errorf("first-token-timeout should be positive, got %d", cfg.Timeouts.FirstToken)
	}
	if cfg.Timeouts.Idle <= 0 {
		return fmt.Errorf("idle-timeout should be positive, got %d", cfg.Timeouts.Idle)
	}
	if cfg.MaxOutputTokensLimit <= 0 {
		return fmt.Errorf("max-output-tokens should be positive, got %d", cfg.MaxOutputTokensLimit)
	}
	if cfg.RequestBodyLimit <= 0 {
		return fmt.Errorf("request-body-limit should be positive, got %d", cfg.RequestBodyLimit)
	}
	return nil
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
		Timeouts:             DefaultTimeouts(),
		MaxActive:            2,
		MaxOutputTokensLimit: 1024,
		RequestBodyLimit:     1 << 20,
	}
}
