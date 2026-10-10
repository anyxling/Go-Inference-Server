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
	"strings"
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
	workerKind := flag.String("worker-kind", "http", "http (worker/server.py protocol) or vllm (OpenAI-compatible)")
	workerModel := flag.String("worker-model", "Qwen/Qwen2.5-0.5B-Instruct", "model name sent to a vllm worker")

	flag.Parse()

	timeouts := api.Timeouts{Total: *totalTimeout, FirstToken: *firstTokenTimeout, Idle: *idleTimeout}

	config := api.Config{Timeouts: timeouts, MaxActive: *maxActive, MaxOutputTokensLimit: *maxOutputTokens, RequestBodyLimit: *requestBodyLimit}

	if err := config.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	urls := parseWorkerURLs(*workerURL)

	var gen worker.Generator
	switch {
	case *workerURL == "":
		gen = worker.NewPool(&worker.Fake{Tokens: []string{"a", "b", "c"}, Delay: 100 * time.Millisecond})
		log.Printf("no worker url; using the fake worker")
	case len(urls) == 0:
		log.Fatalf("-worker-url %q contains no URLs", *workerURL)
	default:
		var gens []worker.Generator
		for _, u := range urls {
			gens = append(gens, newGenerator(*workerKind, u, *workerModel))
		}
		gen = worker.NewPool(gens...)
		log.Printf("pool of %d %s workers: %v", len(urls), *workerKind, urls)
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

func newGenerator(kind, url, model string) worker.Generator {
	switch {
	case kind == "vllm":
		return worker.NewVLLMClient(url, model, 2*time.Second)
	default:
		return worker.NewHTTPClient(urL, 2*time.Second)
	}
}

func parseWorkerURLs(s string) []string {
	var res []string
	urls := strings.Split(s, ",")
	for i := range urls {
		url := strings.TrimSpace(urls[i])
		if url != "" {
			res = append(res, url)
		}
	}
	return res
}
