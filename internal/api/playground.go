package api

import (
	"embed"
	"net/http"
)

//go:embed playground/*
var playgroundFiles embed.FS

// NewPlaygroundRouter exposes only translation and static demo assets.
// It deliberately does not register any handlers that access the backend.
func NewPlaygroundRouter(s *Server, limiter *RateLimiter, clientIP KeyFunc) http.Handler {
	if clientIP == nil {
		clientIP = ClientIP(false)
	}
	mux := http.NewServeMux()
	for route, asset := range map[string]string{
		"GET /{$}":                   "index.html",
		"GET /assets/playground.css": "playground.css",
		"GET /assets/playground.js":  "playground.js",
	} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, playgroundFiles, "playground/"+asset)
		})
	}
	mux.Handle("POST /v1/translate", Chain(http.HandlerFunc(s.Translate), limiter.Middleware))
	mux.HandleFunc("GET /healthz", s.Healthz)
	mux.HandleFunc("GET /readyz", s.Healthz)

	secure := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Frame-Options", "DENY")
			next.ServeHTTP(w, r)
		})
	}
	return Chain(mux, RequestIDMiddleware, LoggingMiddleware(clientIP), RecoverMiddleware, secure)
}
