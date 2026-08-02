package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/opensearch-query-gateway/internal/api"
	"github.com/example/opensearch-query-gateway/internal/config"
	"github.com/example/opensearch-query-gateway/internal/dslbuilder"
	"github.com/example/opensearch-query-gateway/internal/executor"
	"github.com/example/opensearch-query-gateway/internal/parser"
	"github.com/example/opensearch-query-gateway/internal/rules"
)

func main() {
	cfg, err := config.Load(os.Getenv("GATEWAY_CONFIG"))
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	p := parser.New()

	re, err := rules.New(cfg.Rules.BootstrapFile, cfg.Rules.Timeout)
	if err != nil {
		log.Fatalf("rules engine: %v", err)
	}
	defer re.Close()

	builder := dslbuilder.New()

	osClient, err := executor.NewClient(cfg.OpenSearch)
	if err != nil {
		log.Fatalf("opensearch client: %v", err)
	}

	handler := api.NewSearchHandler(p, re, builder, osClient)

	limiter := api.NewRateLimiter(cfg.RateLimit.RPS, cfg.RateLimit.Burst, cfg.RateLimit.TTL)
	mux := api.NewRouter(handler, limiter)

	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("gateway listening on %s", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
