package api

import "net/http"

// NewRouter sestaví HTTP routy služby. Logging, request ID a recover jsou
// zapnuté globálně, rate limiting jen na API endpointy — health check
// endpointy zůstávají neomezené, aby je throttling nezasahoval (load
// balancery/orchestrátory je volají často a nezávisle na provozu API).
//
// Využívá method-aware vzory http.ServeMux z Go 1.22 ("POST /v1/search").
func NewRouter(s *Server, limiter *RateLimiter, clientIP KeyFunc) http.Handler {
	if clientIP == nil {
		clientIP = ClientIP(false)
	}
	mux := http.NewServeMux()

	limited := func(h http.HandlerFunc) http.Handler {
		return Chain(h, limiter.Middleware)
	}

	mux.Handle("POST /v1/search", limited(s.Search))
	mux.Handle("POST /v1/translate", limited(s.Translate))
	mux.Handle("GET /v1/schema/introspect", limited(s.Introspect))
	mux.Handle("POST /v1/schema/introspect", limited(s.Introspect))
	mux.Handle("GET /v1/schema/prolog", limited(s.PrologFacts))
	mux.Handle("POST /v1/schema/prolog", limited(s.PrologFacts))

	mux.HandleFunc("GET /healthz", s.Healthz)
	mux.HandleFunc("GET /readyz", s.Readyz)

	return Chain(mux, RequestIDMiddleware, LoggingMiddleware(clientIP), RecoverMiddleware)
}
