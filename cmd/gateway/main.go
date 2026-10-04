package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/peter7775/opensearch-query-gateway/internal/api"
	"github.com/peter7775/opensearch-query-gateway/internal/config"
	"github.com/peter7775/opensearch-query-gateway/internal/dslbuilder"
	"github.com/peter7775/opensearch-query-gateway/internal/executor"
	"github.com/peter7775/opensearch-query-gateway/internal/parser"
	"github.com/peter7775/opensearch-query-gateway/internal/rules"
)

func main() {
	cfg, err := config.Load(os.Getenv("GATEWAY_CONFIG"))
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	re, err := rules.New(cfg.Rules.BootstrapFile, cfg.Rules.Timeout, cfg.Rules.SchemaFiles...)
	if err != nil {
		log.Fatalf("rules engine: %v", err)
	}
	defer re.Close()

	osClient, err := executor.NewClient(cfg.OpenSearch)
	if err != nil {
		log.Fatalf("opensearch client: %v", err)
	}

	server := api.NewServer(
		parser.New(),
		re,
		dslbuilder.New(dslbuilder.WithTimeField(cfg.OpenSearch.TimeField)),
		osClient,
		api.Options{
			OpenSearch:   cfg.OpenSearch,
			Search:       cfg.Search,
			MaxBodyBytes: cfg.Server.MaxBodyBytes,
		},
	)

	clientIP := api.ClientIP(cfg.Server.TrustProxyHeaders)
	limiter := api.NewRateLimiter(cfg.RateLimit.RPS, cfg.RateLimit.Burst, cfg.RateLimit.TTL, clientIP)
	defer limiter.Close()

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           api.NewRouter(server, limiter, clientIP),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("gateway listening on %s (index=%s, opensearch=%v)", cfg.Server.Addr, cfg.OpenSearch.Index, cfg.OpenSearch.Addresses)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Printf("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
