package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/monikaliu/go-inference-server/internal/api"
	"github.com/monikaliu/go-inference-server/internal/worker"
)

func main() {
	baseCtx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()

	mux := http.NewServeMux()

	server := http.Server{
		Addr:              ":8080",
		Handler:           api.RequestID(mux),
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	app := api.NewServer(&worker.Fake{
		Tokens: []string{"a", "b", "c"},
		Delay:  100 * time.Millisecond,
	}, api.DefaultConfig())

	mux.HandleFunc("GET /health", app.HealthHandler)
	mux.HandleFunc("POST /v1/inference", app.InferenceHandler)

	server.BaseContext = func(net.Listener) context.Context { return baseCtx }

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Printf("shutdown signal received")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := server.Shutdown(shutdownCtx)
		if err != nil {
			log.Printf("graceful shutdown timed out, cancelling active requests")
			cancelAll()
			server.Close()
		}

		err = <-errCh
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server exited with error: %v", err)
		}
	case err := <-errCh:
		log.Printf("server exited with error %v", err)
		return
	}
}
