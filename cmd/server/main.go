package main

import (
	"context"
	"errors"
	"flag"
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
	defaults := api.DefaultConfig()

	totalTimeout := flag.Duration("total-timeout", defaults.Timeouts.Total, "total timeouts allowed")
	firstTokenTimeout := flag.Duration("first-token-timeout", defaults.Timeouts.FirstToken, "timeouts for first token")
	idleTimeout := flag.Duration("idle-timeout", defaults.Timeouts.Idle, "timeouts for idling")
	maxActive := flag.Int("max-active", defaults.MaxActive, "the max number of active requests")
	maxOutputTokens := flag.Int("max-output-tokens", defaults.MaxOutputTokensLimit, "the max number of output tokens for LLM")
	requestBodyLimit := flag.Int64("request-body-limit", defaults.RequestBodyLimit, "body limit for request")
	addr := flag.String("addr", ":8080", "the port to listen to")
	shutdownTimeout := flag.Duration("shutdown-timeout", 30*time.Second, "timeouts for shut down")
	workerURL := flag.String("worker-url", "", "empty uses the built-in fake worker")

	flag.Parse()

	timeouts := api.Timeouts{Total: *totalTimeout, FirstToken: *firstTokenTimeout, Idle: *idleTimeout}

	config := api.Config{Timeouts: timeouts, MaxActive: *maxActive, MaxOutputTokensLimit: *maxOutputTokens, RequestBodyLimit: *requestBodyLimit}

	if err := config.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	var gen worker.Generator

	if *workerURL == "" {
		gen = &worker.Fake{Tokens: []string{"a", "b", "c"}, Delay:  100 * time.Millisecond,}
		log.Printf("empty worker url hence use default fake worker")
	} else {
		gen = worker.NewHTTPClient(*workerURL, 2*time.Second)
		log.Printf("use worker url %v", *workerURL)
	}

	baseCtx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()

	mux := http.NewServeMux()

	server := http.Server{
		Addr:              *addr,
		Handler:           api.RequestID(mux),
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	app := api.NewServer(gen, config)

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

		shutdownCtx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
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
